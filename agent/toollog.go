package agent

import (
	"context"
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
)

// toolLogHooks 把「谁在什么任务下调用了什么工具、结果如何、怎么收场」写进 journal。
//
// 为什么需要它：worker/planner/main 的工具调用此前**完全不可观测**——journal 里只有
// planner 的心跳和 vulnintel 的刷屏，worker 到底在不在干活、卡在哪一步、有没有报错，
// 都只能去翻 transcripts/*.jsonl 的 mtime 才能确认。调优循环（抓日志→看工具使用→
// 调参数）没有这层信号就等于闭眼调。
//
// 设计取舍：
//   - **不打全量命令/输出**。Bash 命令动辄几百字节、工具输出可达几十 KB，全打会把
//     journal 撑爆并拖慢磁盘。只打【首行摘要 + 字节数】+【结果分类】。
//   - **结果分类而非原文**。ok/error/blocked 三态是调优最需要的信号（判断模型是不是
//     在撞 WAF、是不是工具报参数错），原文去 transcript 看。
//   - **每次调用两行**（pre + post）。pre 行给出"要做什么"，post 行给出"做成了吗"。
//     两行都有才能把"卡在调用前"（模型没输出工具）和"卡在调用后"（工具失败）分开。
//
// 它是纯旁路：不拦截、不改输入、不影响决策。判空返回即可。
type toolLogHooks struct {
	// kind 是 agent 角色标签（planner/worker/main/chat），用于日志前缀。
	kind string
	// label 是更具体的运行标签（如 "worker-1 · #42"），可选。
	label string
	// inner 是已有的 hook 链（拦截管控），本层只做旁路观察后转发。
	inner harness.HookRunner
}

// toolLogSink 构造一个只观察不干预的 hook 链（无 inner）。
func toolLogSink(kind string) *toolLogHooks {
	return &toolLogHooks{kind: kind}
}

// wrapToolLog 把日志层套在已有 hook 链外层：先记日志，再转发给 inner。
// inner 为 nil 时本层就是全部。
func wrapToolLog(inner harness.HookRunner, kind string, label string) *toolLogHooks {
	return &toolLogHooks{kind: kind, label: label, inner: inner}
}

// PreToolUse 记录一次工具调用的开始。
func (h *toolLogHooks) PreToolUse(ctx context.Context, name string, input []byte) (bool, string, []byte) {
	h.logPre(ctx, name, input)
	if h.inner != nil {
		return h.inner.PreToolUse(ctx, name, input)
	}
	return false, "", nil
}

// PostToolUse 记录一次工具调用的结果。
func (h *toolLogHooks) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	h.logPost(ctx, name, input, result, isErr)
	if h.inner != nil {
		h.inner.PostToolUse(ctx, name, input, result, isErr)
	}
}

// Stop 记录一次 run 的收场。
func (h *toolLogHooks) Stop(ctx context.Context, messages []llm.Message) (bool, []string, string) {
	h.logStop(ctx, messages)
	if h.inner != nil {
		return h.inner.Stop(ctx, messages)
	}
	return false, nil, ""
}

// --- 日志行 ---

func (h *toolLogHooks) prefix(ctx context.Context) string {
	ri := RunInfoFrom(ctx)
	kind := ri.Kind
	if kind == "" {
		kind = h.kind
	}
	p := "[tool] " + kind
	if h.label != "" {
		p += " " + h.label
	}
	switch {
	case ri.TaskID != 0:
		p += " task=" + strconv.FormatInt(ri.TaskID, 10)
	case ri.SessionID != "":
		p += " session=" + shortID(ri.SessionID)
	}
	if ri.ExplorationID != 0 {
		p += " exp=" + strconv.FormatInt(ri.ExplorationID, 10)
	}
	if ri.IntentID != 0 {
		p += " intent=#" + strconv.FormatInt(ri.IntentID, 10)
	}
	return p
}

func (h *toolLogHooks) logPre(ctx context.Context, name string, input []byte) {
	log.Printf("%s → %s %s", h.prefix(ctx), name, summarizeToolInput(name, input))
}

func (h *toolLogHooks) logPost(ctx context.Context, name string, input, result []byte, isErr bool) {
	log.Printf("%s ← %s %s out=%dB", h.prefix(ctx), name, classifyToolResult(result, isErr), len(result))
}

// logStop 打收场行：最后一个助手回合有没有正文/工具调用。这是区分
// 「正常做完」「空转收场」「模型报错」的第一手信号。
func (h *toolLogHooks) logStop(ctx context.Context, messages []llm.Message) {
	var textLen int
	var tools int
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != llm.RoleAssistant {
			continue
		}
		textLen = len(strings.TrimSpace(m.Text()))
		tools = len(m.ToolUses())
		break
	}
	log.Printf("%s ⏹ stop text=%dB tools=%d msgs=%d", h.prefix(ctx), textLen, tools, len(messages))
}

// --- 工具输入/结果的摘要 ---

// summarizeToolInput 给出工具入参的一行摘要。命令只取第一行（去掉前导空白）并限长，
// 避免把整条多行脚本塞进 journal。
func summarizeToolInput(name string, input []byte) string {
	if len(input) == 0 {
		return ""
	}
	switch name {
	case "Bash":
		var in struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(input, &in) == nil && in.Command != "" {
			return cmdHead(in.Command)
		}
	case "record_fact", "report_finding", "insert_assets", "record_asset":
		var in struct {
			Type  string `json:"type"`
			Title string `json:"title"`
			Kind  string `json:"kind"`
			Name  string `json:"name"`
		}
		if json.Unmarshal(input, &in) == nil {
			var bits []string
			for _, s := range []string{in.Type, in.Kind, in.Name, in.Title} {
				if s != "" {
					bits = append(bits, trunc(s, 48))
				}
			}
			if len(bits) > 0 {
				return strings.Join(bits, " ")
			}
		}
	}
	// 兜底：整体 JSON 压成一行并限长。空对象/空数组没有信息量，返回空串而不是 "{}"。
	s := strings.Join(strings.Fields(string(input)), " ")
	if s == "{}" || s == "[]" {
		return ""
	}
	return trunc(s, 96)
}

// cmdHead 取命令的第一行，限长 96 字符。
func cmdHead(cmd string) string {
	line := cmd
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	return trunc(strings.TrimSpace(line), 96)
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func shortID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

// reToolBlocked 识别「目标侧拦截」的典型措辞（WAF/403/验证码/限流）。与 guard 的
// 归因正则同源，但这里只用于打标签，不参与决策。
var reToolBlocked = regexp.MustCompile(`(?i)\b(403|forbidden|waf|blocked|rate.?limit|429|captcha|denied|too many requests)\b`)

// classifyToolResult 把工具结果压成一个分类标签 + 命中原因。
// ok / error / blocked —— 三态足够调优定位，且长度恒定。
func classifyToolResult(result []byte, isErr bool) string {
	s := string(result)
	if isErr {
		return "error"
	}
	if reToolBlocked.MatchString(s) {
		return "blocked"
	}
	return "ok"
}
