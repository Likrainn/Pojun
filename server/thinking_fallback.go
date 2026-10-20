package server

import (
	"strings"

	"github.com/Autumn-27/norma/llm"
)

// thinking 拒绝错误的识别与自动降级。
//
// ## 这个坑为什么值得单独一个文件
//
// MiniMax-M3.1 这类**强制 adaptive thinking** 的模型会直接拒绝
// thinking.type="disabled"（以及 reasoning.effort=none）:
//
//	400 {"error":{"type":"server_error","message":"Upstream request failed:
//	 [bad_request_error] invalid params, model \"MiniMax-M3.1-Flash\" requires
//	 adaptive thinking; thinking.type=\"disabled\" (including reasoning.effort=none)
//	 is not allowed (2013)"}}
//
// 这个坑在 ARTEX 里**已经踩了三次**：裁决路径（server/intercept.go）、
// 压缩路径（agent/compaction.go）、旁路摘要路径（sidequestion/context.go）——
// 三处都各自硬编码了 thinking 参数，各修一次，但只要再有一处新代码带上
// thinking，就会复发第四次。
//
// 实测代价不轻：线上一次配置切换（LongCat → minimax-m3.1）后的两小时里，
// 裁决失败 787 次、压缩失败 130 次，**全部是同一个 400**。裁决路径每次失败
// 都走「按失败策略处理」，于是 787 次工具调用被静默放行——
// 裁决这道关卡在那两小时等于完全失效。
//
// ## 为什么在这里兜底
//
// llmpool 的 shouldFailover 明确把 400 判为「不切换」——
// "a malformed or over-long request fails identically everywhere"。
// 这个判断对「请求本身有问题」是对的，但 thinking 拒绝是**参数配置问题**，
// 不是请求问题：摘掉 thinking 字段后同一个请求就能成功。
//
// 所以在任务侧 LLM 调用的统一入口（taskLLMRuntime.Stream / Complete）做一次
// 「检测 → 摘掉 thinking → 原样重试」，这类错误就能自愈，而不必要求每个
// 调用方都记得别写 thinking。

// thinkingRejectedMarkers 是上游拒绝 thinking 参数时的特征串。
//
// 用小写匹配，因为不同网关的大小写不一致（实测见过 "requires adaptive
// thinking" 与 "thinking.type=\"disabled\" is not allowed" 两种形态，
// 且 2013 这个错误码来自网关而非模型本身）。
var thinkingRejectedMarkers = []string{
	"requires adaptive thinking",
	"thinking.type=\"disabled\"",
	"thinking.type='disabled'",
	"is not allowed (2013)",
	"2013)", // 兜底：部分网关只回错误码
}

// isThinkingRejected 报告 err 是否是「模型拒绝 thinking 参数」这一类错误。
//
// 只在错误文本里找特征串，不看 HTTP 状态码：上游把 400 包在
// server_error 里传回来，走标准库的 statusOf 未必能取到 400，
// 而文本特征在实测的三次踩坑里都稳定出现。
func isThinkingRejected(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, m := range thinkingRejectedMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// requestHasThinking 报告请求里是否带了 thinking 参数。
func requestHasThinking(req llm.CompletionRequest) bool {
	return req.Thinking != ""
}

// stripThinking 返回一份去掉 thinking 参数的请求副本。
//
// 摘掉后模型会按自己的默认走（对 adaptive thinking 模型就是「开思考」），
// 行为与用户没配 thinking 时一致——这正是 profile 里 thinking_type 为空的
// 语义（见 agent/provider.go 的 ThinkingType 注释）。
//
// 注意：reasoning.effort 是 **profile 级**配置（agent.Config.ReasoningEffort），
// 不在请求里，所以这里只清 Thinking。上游报错文本把 reasoning.effort=none
// 也列进去了，但那是 profile 解析时就已经落到请求里的——profile 侧为空时
// 根本不会发这个字段。
func stripThinking(req llm.CompletionRequest) llm.CompletionRequest {
	out := req
	out.Thinking = ""
	return out
}
