package server

import (
	"strings"
	"testing"
)

// v0.4.41：数据文件预览解析——JSON 数组/对象/JSONL/CSV/文本 五类形态。
func TestParseJSONPreviewArray(t *testing.T) {
	raw := `[{"order":"A1","amount":100},{"order":"A2","amount":200},{"order":"A3","amount":300}]`
	r := parseJSONPreview(raw)
	if r.Type != "json" || r.TotalRows != 3 || r.Truncated {
		t.Fatalf("meta=%+v", r)
	}
	if len(r.Columns) != 2 || r.Columns[0] != "amount" || r.Columns[1] != "order" {
		t.Fatalf("columns=%v", r.Columns)
	}
	if len(r.Rows) != 3 || r.Rows[0][1] != "A1" {
		t.Fatalf("rows=%v", r.Rows)
	}
}

func TestParseJSONPreviewObjectAndFallback(t *testing.T) {
	r := parseJSONPreview(`{"code":"200","msg":"ok"}`)
	if r.Type != "json" || r.TotalRows != 1 || len(r.Columns) != 2 {
		t.Fatalf("object meta=%+v", r)
	}
	// 非法 JSON → 退回文本
	r2 := parseJSONPreview("不是 JSON 的内容")
	if r2.Type != "text" || !strings.Contains(r2.Text, "不是 JSON") {
		t.Fatalf("fallback=%+v", r2)
	}
}

func TestParseJSONLAndCSV(t *testing.T) {
	jl := "{\"user\":\"u1\"}\n{\"user\":\"u2\"}\n\n"
	r := parseJSONLPreview(jl)
	if r.TotalRows != 2 || len(r.Rows) != 2 || r.Columns[0] != "user" {
		t.Fatalf("jsonl=%+v", r)
	}
	csvRaw := "name,role\nalice,admin\nbob,user\n"
	rc := parseCSVPreview(csvRaw, "csv")
	if rc.TotalRows != 2 || len(rc.Columns) != 2 || rc.Rows[0][0] != "alice" {
		t.Fatalf("csv=%+v", rc)
	}
	tsv := "a\tb\n1\t2\n"
	rt := parseCSVPreview(tsv, "tsv")
	if len(rt.Columns) != 2 || rt.Rows[0][1] != "2" {
		t.Fatalf("tsv=%+v", rt)
	}
}

func TestParseTextPreviewTruncates(t *testing.T) {
	lines := make([]string, dataPreviewMaxLines+50)
	for i := range lines {
		lines[i] = "line"
	}
	r := parseTextPreview(strings.Join(lines, "\n"))
	if r.Type != "text" || !r.Truncated || r.TotalRows != dataPreviewMaxLines+50 {
		t.Fatalf("meta=%+v", r)
	}
	if got := strings.Count(r.Text, "\n") + 1; got != dataPreviewMaxLines {
		t.Fatalf("text lines=%d want %d", got, dataPreviewMaxLines)
	}
}

// API 信封 {code,msg,data:[...]} 自动下钻到 data 数组（生产最常见的响应形态）。
func TestParseJSONPreviewEnvelope(t *testing.T) {
	raw := `{"code":"200","msg":"ok","data":[{"order":"A1","amount":100},{"order":"A2","amount":200}]}`
	r := parseJSONPreview(raw)
	if r.Envelope != "data" {
		t.Fatalf("envelope=%q want data", r.Envelope)
	}
	if r.TotalRows != 2 || len(r.Rows) != 2 || r.Rows[0][1] != "A1" {
		t.Fatalf("rows=%v", r.Rows)
	}
}
