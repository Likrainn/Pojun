package agent

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/norma/llm"
	"github.com/chu0119/artex/db"
)

// fakeProvider 是一个可控的假 provider：记录每次调用的 MaxTokens 与输入长度，
// 并按脚本返回内容或空正文。
type fakeProvider struct {
	calls []fakeCall
	// reply 返回给每次调用的正文；空串 = 模拟"模型只思考没正文"。
	reply func(call int, maxTokens int, input string) string
	// stop 返回 stop reason
	stop func(call int) string
}

type fakeCall struct {
	MaxTokens int
	Input     string
}

func (f *fakeProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {}
}

func (f *fakeProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	in := ""
	for _, m := range req.Messages {
		in += m.Text()
	}
	f.calls = append(f.calls, fakeCall{MaxTokens: req.MaxTokens, Input: in})
	idx := len(f.calls) - 1
	body := ""
	if f.reply != nil {
		body = f.reply(idx, req.MaxTokens, in)
	}
	stop := ""
	if f.stop != nil {
		stop = f.stop(idx)
	}
	msg := llm.Message{}
	if body != "" {
		msg.Content = []llm.ContentBlock{llm.TextBlock(body)}
	}
	return msg, stop, llm.Usage{InputTokens: 100, OutputTokens: 50}, nil
}

// 压缩的输出预算必须**给足**。
//
// 回归背景：曾经硬编码 MaxTokens: 1500。实测强制 adaptive thinking 的模型
// (MiniMax-M3.1)的思考长度随这个预算线性膨胀——预算 1500/2048/4096/8192 全部
// 得到「思考烧光、正文为空」，压缩 97.5% 失败；给到 32768 才正常出正文。
// 所以默认值必须足够大，且可被调用方覆盖。
func TestCompressOutputBudgetIsGenerous(t *testing.T) {
	prov := &fakeProvider{reply: func(int, int, string) string { return "摘要正文" }}
	c := NewCompactor(prov, "test-model")

	if c.outTokens < 32768 {
		t.Errorf("默认输出预算太小: %d（思考型模型需要足够冗余）", c.outTokens)
	}
	if c.maxDur < 10*time.Minute {
		t.Errorf("默认墙钟上限太小: %v（长输入的思考型模型要跑很久）", c.maxDur)
	}
	// 单块上限必须够小:实测 26 节点一块必失败、9 节点一块 3 块里失败 1 块、
	// 6 节点一块稳定成功。默认取实测最稳的值。
	if c.maxBlockMembers != 6 {
		t.Errorf("默认单块上限 %d，应为 6（实测最稳）", c.maxBlockMembers)
	}
}

// SetOutputTokens 覆盖后，compress 必须真的用新预算。
func TestCompressUsesConfiguredOutputBudget(t *testing.T) {
	prov := &fakeProvider{reply: func(int, int, string) string { return "摘要正文" }}
	c := NewCompactor(prov, "test-model")
	c.SetOutputTokens(65536)

	g := &coldGraph{}
	b := block{Members: []int64{1, 2}}
	body, err := c.compress(context.Background(), g, b, map[int64]*db.Node{})
	if err != nil {
		t.Fatalf("compress 失败: %v", err)
	}
	if body != "摘要正文" {
		t.Errorf("正文不对: %q", body)
	}
	if len(prov.calls) != 1 {
		t.Fatalf("应调用一次,实际 %d 次", len(prov.calls))
	}
	if prov.calls[0].MaxTokens != 65536 {
		t.Errorf("未使用配置的输出预算: %d", prov.calls[0].MaxTokens)
	}
}

// SetOutputTokens(0) 恢复默认；SetMaxDuration 同理。
func TestCompactorSettersFallBackToDefaults(t *testing.T) {
	c := NewCompactor(&fakeProvider{}, "m")
	c.SetOutputTokens(1234)
	c.SetMaxDuration(3 * time.Minute)
	if c.outTokens != 1234 {
		t.Errorf("SetOutputTokens 未生效: %d", c.outTokens)
	}
	if c.maxDur != 3*time.Minute {
		t.Errorf("SetMaxDuration 未生效: %v", c.maxDur)
	}
	c.SetOutputTokens(0)
	c.SetMaxDuration(0)
	if c.outTokens != defaultCompactionTokens {
		t.Errorf("0 应恢复默认预算,得到 %d", c.outTokens)
	}
	if c.maxDur != defaultCompactionDuration {
		t.Errorf("0 应恢复默认时长,得到 %v", c.maxDur)
	}
}

// 空正文必须把诊断信息带出来。
// 回归背景：以前只回一句「empty body from model」，排查时完全无从下手——
// 不知道是 stop=length(被截断)还是只有思考块没正文。
func TestCompressEmptyBodyCarriesDiagnostics(t *testing.T) {
	prov := &fakeProvider{
		reply: func(int, int, string) string { return "" },
		stop:  func(int) string { return "length" },
	}
	c := NewCompactor(prov, "test-model")
	g := &coldGraph{}
	b := block{Members: []int64{1}}
	_, err := c.compress(context.Background(), g, b, map[int64]*db.Node{})
	if err == nil {
		t.Fatal("空正文应报错")
	}
	msg := err.Error()
	for _, want := range []string{"empty body", "length", "in=", "out="} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误信息缺少 %q: %s", want, msg)
		}
	}
}

// 单块过大时必须自动切分成多个子块分别压缩。
//
// 回归背景：实测 26 个节点的连通块（3084 prompt token）会让思考型模型陷入
// 超长思考——给 32768 预算也烧光、211 秒不出正文。3 个节点（743 token）则
// 正常收尾。说明模型能否收尾取决于**单次输入规模**，与预算大小无关。
// 所以对超大块必须切分，让每次请求的规模落在模型能处理住的范围内。
func TestCompressSplitsOversizedBlock(t *testing.T) {
	var prov fakeProvider
	prov.reply = func(call int, maxTokens int, input string) string {
		return fmt.Sprintf("子块%d", call)
	}
	c := NewCompactor(&prov, "test-model")
	c.SetMaxBlockMembers(4) // 测试用小阈值

	// 10 个成员的块 → 应被切成 3 块(4+4+2)
	members := make([]int64, 10)
	nodeByID := map[int64]*db.Node{}
	for i := range members {
		members[i] = int64(i + 1)
		nodeByID[members[i]] = &db.Node{ID: members[i], Kind: db.KindFact, State: "confirmed"}
	}
	g := &coldGraph{}
	b := block{Members: members}
	bodies, err := c.compressChunked(context.Background(), g, b, nodeByID)
	if err != nil {
		t.Fatalf("compressChunked 失败: %v", err)
	}
	if len(bodies) != 3 {
		t.Errorf("10 个成员按 4 切应得 3 块,得到 %d", len(bodies))
	}
	if len(prov.calls) != 3 {
		t.Errorf("应发起 3 次调用,实际 %d", len(prov.calls))
	}
	// 每次调用的输入里成员数不超过阈值
	for i, call := range prov.calls {
		n := strings.Count(call.Input, "\n- #")
		if n > 4 {
			t.Errorf("第 %d 次调用含 %d 个成员,超过阈值 4", i, n)
		}
	}
}

// 阈值 0 / 负数 = 恢复默认（12），而不是"不切分"。
// 刻意不提供"不切分"：实测超大连通块必然让思考型模型陷进超长思考。
func TestCompressThresholdZeroRestoresDefault(t *testing.T) {
	prov := &fakeProvider{reply: func(int, int, string) string { return "x" }}
	c := NewCompactor(prov, "m")
	c.SetMaxBlockMembers(4)
	c.SetMaxBlockMembers(0)
	if c.maxBlockMembers != defaultCompactionMaxMembers {
		t.Errorf("0 应恢复默认阈值,得到 %d", c.maxBlockMembers)
	}
}

// 压缩输入里节点摘要要正确带出（节点文本可能带引号/换行）。
func TestBuildCompressionInputEscapesNodeText(t *testing.T) {
	nodeByID := map[int64]*db.Node{
		1: {ID: 1, Kind: db.KindFact, State: "confirmed", Payload: []byte(`{"summary":"含\"引号\"和\n换行"}`)},
	}
	g := &coldGraph{}
	in := buildCompressionInput(g, block{Members: []int64{1}}, nodeByID)
	if !strings.Contains(in, `含"引号"`) {
		t.Errorf("节点摘要未带出: %s", in)
	}
}
