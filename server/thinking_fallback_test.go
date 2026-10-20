package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

// 线上实测的错误原文（三个路径都被它咬过）。
const realThinkingReject = `openai: status 400: {"error":{"type":"server_error","message":"Upstream request failed: [bad_request_error] invalid params, model \"MiniMax-M3.1-Flash\" requires adaptive thinking; thinking.type=\"disabled\" (including reasoning.effort=none) is not allowed (2013)"}}`

// 上游拒收 thinking 参数的错误必须被识别出来——这是自动降级的前提。
func TestIsThinkingRejected(t *testing.T) {
	if !isThinkingRejected(errors.New(realThinkingReject)) {
		t.Fatal("实测的 thinking 拒绝错误没被识别出来")
	}
	// 变体：不同网关的措辞略有差异。
	for _, v := range []string{
		`model "X" requires adaptive thinking`,
		`thinking.type="disabled" is not allowed`,
		`thinking.type='disabled' is not allowed`,
		`invalid params (2013)`,
	} {
		if !isThinkingRejected(errors.New(v)) {
			t.Fatalf("变体未被识别: %q", v)
		}
	}
}

// 别的错误不能被误判成 thinking 拒绝——误判会导致把正常的 thinking 参数
// 摘掉，白白改变模型行为。
func TestIsThinkingRejectedRejectsOthers(t *testing.T) {
	for _, v := range []string{
		"",
		"connection reset by peer",
		"status 429: too many requests",
		"status 500: internal server error",
		"context deadline exceeded",
		// 普通的 400（请求本身有问题），摘掉 thinking 也没用。
		`status 400: {"error":{"message":"max_tokens is too large"}}`,
	} {
		if isThinkingRejected(errors.New(v)) {
			t.Fatalf("被误判成 thinking 拒绝: %q", v)
		}
	}
	if isThinkingRejected(nil) {
		t.Fatal("nil 不该被判成 thinking 拒绝")
	}
}

// 只有请求真的带了 thinking 参数时，摘除才有意义。
func TestRequestHasThinking(t *testing.T) {
	if requestHasThinking(llm.CompletionRequest{}) {
		t.Fatal("空请求不该有 thinking")
	}
	if !requestHasThinking(llm.CompletionRequest{Thinking: "disabled"}) {
		t.Fatal("带 thinking 的请求应被识别")
	}
}

// 摘除必须只清 thinking，别的字段一个都不能动——请求里还有 system、
// messages、max_tokens、tools 等，动了任何一个都是静默改变调用语义。
func TestStripThinkingOnlyTouchesThinking(t *testing.T) {
	temp := 0.3
	orig := llm.CompletionRequest{
		System:      []string{"sys"},
		Messages:    []llm.Message{llm.UserText("hi")},
		MaxTokens:   4096,
		Temperature: &temp,
		Thinking:    "disabled",
	}
	got := stripThinking(orig)
	if got.Thinking != "" {
		t.Fatalf("thinking 未被摘掉: %q", got.Thinking)
	}
	if len(got.System) != 1 || got.System[0] != "sys" {
		t.Fatalf("System 被改动: %v", got.System)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("Messages 被改动: %v", got.Messages)
	}
	if got.MaxTokens != 4096 {
		t.Fatalf("MaxTokens 被改动: %d", got.MaxTokens)
	}
	if got.Temperature == nil || *got.Temperature != temp {
		t.Fatalf("Temperature 被改动: %v", got.Temperature)
	}
	// 原请求不能被就地改掉——调用方可能还要用它。
	if orig.Thinking != "disabled" {
		t.Fatalf("原请求被就地修改: %q", orig.Thinking)
	}
}

// 错误文本里出现 "disabled" 但不是 thinking 拒绝的情形不能被误伤
// （粗粒度匹配最常见的翻车方式）。
func TestIsThinkingRejectedNotOverBroad(t *testing.T) {
	// "disabled" 与 thinking 无关，只是恰好同句出现。
	err := errors.New("feature disabled by administrator")
	if isThinkingRejected(err) {
		t.Fatal("与 thinking 无关的 disabled 被误判")
	}
	// 错误文本里含 2013 但不是 thinking 语境——2013 单独出现不作为特征。
	err2 := errors.New("upstream returned 2013 bytes")
	if isThinkingRejected(err2) && !strings.Contains(strings.ToLower(err2.Error()), "2013)") {
		t.Fatalf("误判: %v", err2)
	}
}
