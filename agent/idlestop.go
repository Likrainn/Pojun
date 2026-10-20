package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
)

// 无增量自动收尾（idle-stop）：worker 连续若干轮没有写回任何东西时，
// 主动结束这一轮 run，而不是等墙钟预算耗尽被强行掐断。
//
// ## 为什么需要它
//
// 现状是「限定目标 → 限时 → 到点强制总结 → planner 重新派」。这个结构有三个
// 实测出来的问题：
//
//  1. **收尾是被动的**：worker 不知道「该收手了」，只知道「时间快到了赶紧写」。
//     线上实测意图 summary 平均 737 字、最长 2267 字——它把��有东西都倒出来，
//     因为死线强制它这么做。
//  2. **耗尽远多于完成**：线上意图终态分布是 done 18 / exhausted 99 /
//     blocked 82。exhausted 是 done 的 5.5 倍，说明绝大多数探测是**被掐断的**，
//     不是跑完的。
//  3. **接续链正反馈**：planner 看到 exhausted 时的指令是「接近突破被预算掐 →
//     派『接上次进度继续』」。但「接近突破」由 planner 判断，而 planner 只看得到
//     一句 summary 和 trace——summary 越长，planner 越觉得「快到了，再给一次
//     机会」。长 summary → 续派 → 更长的意图 → 更长的 summary。
//
// 线上实测到一条三代接续的链：#635 → #578 → #660，摘要在说「接续两次被后端
// 重启打断的 #635/#578 继续攻 ZStack 管理 API 凭据这条皇冠链（#635 上一轮已
// 跑到 56 轮/28 分钟）」。
//
// ## 判据：产出率，不是时长
//
// 用「连续 N 轮没有写回任何 fact/asset/finding」作为收尾判据，而不是用时长。
// 时长是噪声——一个 worker 连着 10 轮扫同一批端口不写回，那是没产出；但一个
// worker 每轮都在写回新资产，它跑满 30 分钟也不该被砍。
//
// 产出率直接对应「这个方向还有没有价值」，而时长不。
//
// ## 为什么放在 Stop hook
//
// harness.HookRunner.Stop 在**每个 assistant 回合之后**被调用（见 norma 的
// harness/query.go），返回 prevent=true 会终止这一轮 run。这正好是「轮边界」
// 的语义——与 SDK 自己的 MaxTurns/MaxDuration 判定在同一位置。
//
// 另一个可选钩子 SystemReminderFunc 不可用：它只在 Prompt() 时调用一次，
// 不是每轮。
//
// ## 为什么不影响「深度优先」
//
// 深度优先（发现高价值入口就往深打穿）与「限定时长」是直接冲突的。这里的
// 判据绕开了这个冲突：**只在该方向已经不再产出时才收**。一个持续产出的
// worker 不会被这个机制打断——深度探索不受影响，被打断的只有空转。

// idleStopDefaults 是无增量收尾的默认参数。
const (
	// idleStopRounds 是「连续多少轮没有写回就收场」。
	//
	// 3 轮的由来：worker 的正常节奏是「探测 → 写回 → 探测 → 写回」。偶尔有
	// 1-2 轮只探测不写回是正常的（比如在准备一个多步利用链的中间步骤）。
	// 连续 3 轮没有任何写回，基本可以判定这个方向已经探到边界或走偏了。
	//
	// 不取 1：单轮不写回太常见（写回前要先把结论想清楚），会误杀正常 worker。
	// 不取 5：实测一个空转的 worker 能在 5 轮里烧掉几分钟 LLM 时间，而这段时间
	// 完全不产生任何可用的产出。
	idleStopRounds = 3

	// idleStopMinElapsed 是「至少已经跑了多久才允许按无增量收场」。
	//
	// 存在的意义：worker 刚起手的前几轮可能在做准备工作（建工作目录、读前期
	// 事实、拉取目标信息），这些轮次不写回是正常的。给它一段起跑宽限期，
	// 避免开局就被判定为空转。
	idleStopMinElapsed = 3 * time.Minute
)

// idleStopper 在一轮 worker run 内跟踪「连续无写回的轮数」，到阈值就要求收场。
//
// 计数口径：**每一次 Stop 回调算一轮**。Stop 在每个 assistant 回合后调用，
// 所以一轮 ≈ 一次「模型思考 + 一批工具调用」。
type idleStopper struct {
	// writes 指向本 run 的写回计数器（ToolSet.Writes 的来源）。
	writes func() WriteCounts

	// rounds 是连续没有写回的轮数。
	rounds int
	// lastWrites 是上一次看到的写回总量，用来判断「这一轮有没有新增」。
	lastWrites int
	// start 是本 run 的起点，用于起跑宽限期。
	start time.Time
	// stopped 标记是否已经要求过收场（只要求一次）。
	stopped bool
	// mu 保护并发访问（Stop 可能与工具执行并发）。
	mu sync.Mutex
	// reason 记录收场原因，落日志/落库用。
	reason string
}

func newIdleStopper(writes func() WriteCounts) *idleStopper {
	return &idleStopper{writes: writes, start: time.Now()}
}

// check 记录本轮情况，必要时返回 prevent=true 要求收场。
//
// messages 参数不使用——判据只看写回增量，不看模型说了什么。看文本会有两个
// 坏处：模型可以自己宣称「我已经探完了」来骗过判据；而一个正在认真做
// 多步利用链的 worker 会说很多话，被误判成「还在产出」。
func (s *idleStopper) check() (prevent bool, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	if s.writes != nil {
		total = s.writes().Total()
	}
	// 写回增量 > 0 → 有产出，重置连续计数。
	if total > s.lastWrites {
		s.lastWrites = total
		s.rounds = 0
		s.stopped = false
		return false, ""
	}
	// 已经达过阈值：持续返回 prevent，直到「阶段」由调用方推进
	// （idleStopHooks 用 nudged 区分「先提示写回」与「真的收场」）。
	if s.stopped {
		return true, s.reason
	}
	s.rounds++
	// 起跑宽限期内不收场。
	if time.Since(s.start) < idleStopMinElapsed {
		return false, ""
	}
	if s.rounds < idleStopRounds {
		return false, ""
	}
	s.stopped = true
	s.reason = fmt.Sprintf("连续%d轮无写回（本轮写回 %d 条，产出率已归零）", s.rounds, total)
	return true, s.reason
}

// idleStopHooks 把 idleStopper 挂在 harness 的 hook 链上。
//
// 它是**旁路观察 + 收场请求**的组合：
//   - PreToolUse/PostToolUse：原样转发给 inner（日志层不拦截）。
//   - Stop：在 inner 的 Stop 之后检查；inner 决定要停就停，inner 没意见而
//     产出率归零，就要求收场。
//
// 顺序：先转发 inner 再判断。inner 是拦截管控（guard），它说停就必须停；
// 我们只在 inner 放行、且本轮确实没产出时才追加「收场」的要求。
//
// ## 为什么用 blocking 而不是直接 prevent
//
// harness 的 Stop hook 有两种返回：
//   - prevent=true → 立刻终止 run，模型**没有机会写回**。
//   - blocking=[]string → SDK 把这些字符串作为 user 消息注入，模型**再跑一轮**。
//
// 这里要的是后者：worker 跑到一半被判空转时，它可能正拿着一堆「探到了但还没
// 写回」的东西。直接 prevent 会把这些全丢掉——那才是真正的浪费。所以流程是：
//
//	第 1 次触发 → blocking：告诉模型「这个方向探不到新东西了，把已确认的写回，
//	                 然后用一句话总结收手」
//	第 2 次触发 → prevent：模型已经给过总结、还不肯停，就真的收场
//
// stopHookActive 机制保证不会死循环：SDK 注入 blocking 后会把 stopHookActive
// 置位，直到发生一次真正的工具回合才复位。
type idleStopHooks struct {
	inner harness.HookRunner
	stop  *idleStopper
	// onStop 被调用一次（带收场原因），用于落日志/落库。
	onStop func(reason string)
	// nudged 记录是否已经给过 blocking 提示（给过之后才允许 prevent）。
	nudged bool
	// mu 保护 nudged（Stop 可能被并发调用）。
	mu sync.Mutex
}

func (h *idleStopHooks) PreToolUse(ctx context.Context, name string, input []byte) (bool, string, []byte) {
	if h.inner != nil {
		return h.inner.PreToolUse(ctx, name, input)
	}
	return false, "", nil
}

func (h *idleStopHooks) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	if h.inner != nil {
		h.inner.PostToolUse(ctx, name, input, result, isErr)
	}
}

func (h *idleStopHooks) Stop(ctx context.Context, messages []llm.Message) (bool, []string, string) {
	if h.inner != nil {
		prevent, blocking, msg := h.inner.Stop(ctx, messages)
		if prevent {
			return prevent, blocking, msg
		}
	}
	prevent, reason := h.stop.check()
	if !prevent {
		return false, nil, ""
	}
	h.mu.Lock()
	alreadyNudged := h.nudged
	h.nudged = true
	h.mu.Unlock()
	if !alreadyNudged {
		// 第一次触发：给模型一次把已确认结果写回的机会。
		return false, []string{idleStopMessage}, ""
	}
	// 第二次触发：模型已经收到提示还不停，真的收场。
	if h.onStop != nil {
		h.onStop(reason)
	}
	return true, nil, reason
}

// wrapIdleStop 把 idleStopper 套在已有 hook 链外层。
func wrapIdleStop(inner harness.HookRunner, writes func() WriteCounts, onStop func(string)) *idleStopHooks {
	return &idleStopHooks{inner: inner, stop: newIdleStopper(writes), onStop: onStop}
}

// idleStopMessage 是收场时给模型的说明。
//
// 说清「为什么收」很重要：模型看到「预算耗尽被终止」会以为是被惩罚，于是
// 倾向于把还没验证的东西也写成结论；看到「这个方向没有新产出，请把已确认的
// 写回并停手」它会正确地把已确认的收好。
const idleStopMessage = "本轮已连续多轮没有任何新发现被写回——这个方向暂时探不到新的东西了。请立刻停止继续探测：把你**已经确认**的结果写回（资产用 insert_assets、结论用 record_fact、确认漏洞用 report_finding），然后用一句话纯文本总结你探到了什么、卡在哪里，结束本轮。不要为了「有产出」而把没验证的猜测写成事实。"

// logIdleStop 记一条收场日志（统一格式，便于事后统计）。
func logIdleStop(worker string, taskID int64, intentID int64, reason string) {
	log.Printf("[worker %s] task %d intent #%d 无增量自动收场: %s", worker, taskID, intentID, reason)
}

// summarizeWriteDelta 描述一轮的写回增量（调试/日志用）。
func summarizeWriteDelta(cur, prev WriteCounts) string {
	return fmt.Sprintf("事实+%d 资产+%d 漏洞+%d",
		cur.Facts-prev.Facts, cur.Assets-prev.Assets, cur.Findings-prev.Findings)
}

// idleStopStateText 是给 planner 看的一句话说明（写进意图的停止原因）。
//
// planner 靠它区分两种 exhausted：
//   - 「被墙钟掐断」（还有明确的下一步，只是没时间）
//   - 「探到边界」（这个方向本身没东西了）
// 这两种在 planner 的决策里完全相反，混在一起就会导致「一直续派同一方向」。
func idleStopStateText(reason string) string {
	return strings.TrimSpace(reason)
}
