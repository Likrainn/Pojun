package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/chu0119/pojun/db"
)

// 意图级软预算 + 重复调用检测（v0.4.28，同行对比分析 P0 #3/#5）。
//
// 来源：hackingBuddyGPT 的四维 Limits（MIT）与 PentAGI 的重复检测器（MIT）——只借鉴
// 机制，全部自研实现。设计取向是「软」：预算到顶不杀进程，而是注入一条 steer 指令
// 要求立即写回收尾；重复调用先拦两次（给模型改参数的机会），再放行兜底，绝不造成
// 比现状更差的卡死路径。任务级 timeout / auto-finish 仍是最终兜底。

const (
	settingIntentBudgetEnabled = "intent_budget_enabled"
	settingIntentBudgetTokens  = "intent_budget_tokens"

	defaultIntentBudgetEnabled = true
	defaultIntentBudgetTokens  = 3_000_000
)

type intentBudgetConfig struct {
	Enabled bool
	Tokens  int64
}

// intentBudgetFromPG 读取全局设置并夹逼。读库失败用默认值——设置查询失败绝不能
// 把意图预算变成硬故障。
func intentBudgetFromPG(pg *db.DB) intentBudgetConfig {
	c := intentBudgetConfig{Enabled: defaultIntentBudgetEnabled, Tokens: defaultIntentBudgetTokens}
	if pg == nil {
		return c
	}
	if v, ok, err := pg.GetSetting(settingIntentBudgetEnabled); err == nil && ok {
		c.Enabled = v == "true" || v == "1"
	}
	if n, ok := settingInt(pg, settingIntentBudgetTokens); ok {
		c.Tokens = int64(clampInt(n, 500_000, 20_000_000))
	}
	return c
}

// budgetNudgeText 是预算到顶后的 steer 指令。要求收尾而不是杀进程：模型手里的结论
// 还没写回时，硬杀等于把这一意图的工作全丢掉。
func budgetNudgeText(limit int64) string {
	return fmt.Sprintf(
		"【预算提醒】本意图的 token 消耗已达到预算上限（约 %d）。立即停止扩大探索范围："+
			"把已经拿到的结论用 record_fact（发现漏洞则 report_finding）写回，然后直接给出结束答复。"+
			"不要再开启新的探测动作；没来得及验证的方向留待规划者另派。",
		limit)
}

// repeatTracker 跟踪一条意图内「同名工具+完全相同入参」的连续调用。
// steerHooks 是值拷贝传递，计数器必须以指针共享。
type repeatTracker struct {
	mu       atomic.Pointer[repeatState]
	blockCap int // 同一串最多拦截次数，超过放行兜底（防活锁）
}

type repeatState struct {
	name    string
	hash    string
	streak  int
	blocked int
}

func newRepeatTracker() *repeatTracker {
	return &repeatTracker{blockCap: 2}
}

// canonicalToolInput 把工具入参压成空白无关的规范形（JSON 紧凑化），失败退原文。
func canonicalToolInput(name string, input []byte) string {
	var compact bytes.Buffer
	if err := json.Compact(&compact, input); err != nil {
		return name + "|" + string(input)
	}
	sum := sha256.Sum256(append([]byte(name+"|"), compact.Bytes()...))
	return hex.EncodeToString(sum[:])
}

// observe 记录一次工具调用，返回 (连续相同次数, 是否应拦截)。
// 规则：同一 (tool, 规范化入参) 连续第 3 次起拦截；同一串最多拦 blockCap 次，
// 之后放行（宁可重复也不活锁）。入参一旦变化立即重置。
func (rt *repeatTracker) observe(name string, input []byte) (int, bool) {
	h := canonicalToolInput(name, input)
	for {
		cur := rt.mu.Load()
		if cur == nil {
			next := &repeatState{name: name, hash: h, streak: 1}
			if rt.mu.CompareAndSwap(nil, next) {
				return 1, false
			}
			continue
		}
		if cur.name != name || cur.hash != h {
			next := &repeatState{name: name, hash: h, streak: 1}
			if rt.mu.CompareAndSwap(cur, next) {
				return 1, false
			}
			continue
		}
		next := &repeatState{name: cur.name, hash: cur.hash, streak: cur.streak + 1, blocked: cur.blocked}
		if rt.mu.CompareAndSwap(cur, next) {
			shouldBlock := cur.streak+1 >= 3 && cur.blocked < rt.blockCap
			if shouldBlock {
				next.blocked++
			}
			return cur.streak + 1, shouldBlock
		}
	}
}

func repeatBlockText(streak int) string {
	return fmt.Sprintf(
		"【重复调用提醒】这个工具调用与你最近的调用完全相同（连续第 %d 次），本次未执行。"+
			"同样的输入只会得到同样的输出——请改变其中一项：换目标/换参数/换方法；"+
			"或者承认当前手段已到头，把结论用 record_fact 写回（否定结论也有效）。",
		streak)
}

// intentBudgetWatcher 把一条意图的「预算到顶→steer 提醒」接到 activity 流上。
// usage 活动的 InputTokens/OutputTokens 是会话累计值（agent/capture.go），取最新
// 一条即可判断是否到顶。返回的闭包包在 runIntent 的 emit 外层；disabled 时原样返回。
func intentBudgetWatcher(cfg intentBudgetConfig, steer func(string) error, label string) func(db.Activity) {
	if !cfg.Enabled || cfg.Tokens <= 0 {
		return nil
	}
	var nudged atomic.Bool
	return func(a db.Activity) {
		if a.Kind != "usage" || a.InputTokens == nil || nudged.Load() {
			return
		}
		var out int
		if a.OutputTokens != nil {
			out = *a.OutputTokens
		}
		total := int64(*a.InputTokens) + int64(out)
		if total < cfg.Tokens {
			return
		}
		if !nudged.Swap(true) {
			if err := steer(budgetNudgeText(cfg.Tokens)); err != nil {
				log.Printf("[budget %s] 预算提醒入队失败: %v", label, err)
				return
			}
			log.Printf("[budget %s] 意图 token 达预算上限 %d（usage 累计 %d），已注入收尾提醒", label, cfg.Tokens, total)
		}
	}
}
