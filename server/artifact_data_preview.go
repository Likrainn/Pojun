package server

// 数据文件在线查看（v0.4.41）：task_artifacts 里 60%+ 是 json/csv/txt/log 类
// 数据证据，此前只能下载、无法在页面上看——「把数据扒下来展示」的关键缺口。
// 这里把数据文件解析成结构化预览（JSON 数组→表格行、CSV/TSV→表格、
// JSONL→逐行、其余→文本），前端 ArtifactDataViewer 直接渲染。
//
// 限额：读入 ≤20MB（与单附件上限一致），JSON 行/CSV 行/文本行各自封顶，
// 超出在响应里标 truncated——证据原文永远可以走 download 拿全量。

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	dataPreviewMaxBytes = 20 << 20
	dataPreviewMaxRows  = 100
	dataPreviewMaxLines = 200
	dataPreviewMaxValue = 300
)

func dataPreviewKind(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	switch ext {
	case "json":
		return "json"
	case "jsonl", "ndjson":
		return "jsonl"
	case "csv":
		return "csv"
	case "tsv":
		return "tsv"
	default:
		return "text"
	}
}

// dataParseResult 是解析后的预览负载。
type dataParseResult struct {
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	BoundFinding int64    `json:"bound_finding"`
	TotalRows    int      `json:"total_rows"`
	Truncated    bool     `json:"truncated"`
	Columns      []string `json:"columns,omitempty"`
	Rows         [][]any  `json:"rows,omitempty"`
	Text         string   `json:"text,omitempty"`
	TotalSize    int64    `json:"total_size"`
}

func parseDataPreview(kind, raw string) *dataParseResult {
	switch kind {
	case "json":
		return parseJSONPreview(raw)
	case "jsonl":
		return parseJSONLPreview(raw)
	case "csv", "tsv":
		return parseCSVPreview(raw, kind)
	default:
		return parseTextPreview(raw)
	}
}

func parseJSONPreview(raw string) *dataParseResult {
	res := &dataParseResult{Type: "json"}
	var v any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &v); err != nil {
		return parseTextPreview(raw)
	}
	switch arr := v.(type) {
	case []any:
		res.TotalRows = len(arr)
		res.Truncated = len(arr) > dataPreviewMaxRows
		// 两遍：先收集并排序列名，再按统一列序摊行——否则列排序后与行错位。
		colSet := map[string]bool{}
		n := len(arr)
		if n > dataPreviewMaxRows {
			n = dataPreviewMaxRows
		}
		for _, item := range arr[:n] {
			if obj, ok := item.(map[string]any); ok {
				for k := range obj {
					if !colSet[k] && len(colSet) < 30 {
						colSet[k] = true
					}
				}
			}
		}
		cols := make([]string, 0, len(colSet))
		for k := range colSet {
			cols = append(cols, k)
		}
		sort.Strings(cols)
		res.Columns = cols
		for _, item := range arr[:n] {
			res.Rows = append(res.Rows, dataRowCells(item, &cols, colSet))
		}
	case map[string]any:
		res.TotalRows = 1
		keys := []string{}
		for k := range arr {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		res.Columns = keys
		cells := make([]any, 0, len(keys))
		for _, k := range keys {
			cells = append(cells, clipValue(arr[k]))
		}
		res.Rows = append(res.Rows, cells)
	default:
		res.Text = strings.TrimSpace(raw)
	}
	return res
}

// dataRowCells 把一条记录摊成与 columns 对齐的单元格；新键遇到时补齐已有行。
func dataRowCells(item any, cols *[]string, colSet map[string]bool) []any {
	obj, ok := item.(map[string]any)
	if !ok {
		return []any{clipValue(item)}
	}
	for k := range obj {
		if !colSet[k] && len(*cols) < 30 {
			colSet[k] = true
			*cols = append(*cols, k)
		}
	}
	cells := make([]any, 0, len(*cols))
	for _, c := range *cols {
		if v, ok := obj[c]; ok {
			cells = append(cells, clipValue(v))
		} else {
			cells = append(cells, nil)
		}
	}
	return cells
}

func clipValue(v any) any {
	s, ok := v.(string)
	if !ok {
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		s = string(b)
	}
	s = strings.TrimSpace(s)
	if len(s) > dataPreviewMaxValue {
		s = s[:dataPreviewMaxValue] + "…"
	}
	return s
}

func parseJSONLPreview(raw string) *dataParseResult {
	res := &dataParseResult{Type: "jsonl"}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	colSet := map[string]bool{}
	cols := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		res.TotalRows++
		if len(res.Rows) >= dataPreviewMaxRows {
			res.Truncated = true
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			res.Rows = append(res.Rows, []any{clipValue(line)})
			continue
		}
		res.Rows = append(res.Rows, dataRowCells(obj, &cols, colSet))
	}
	res.Columns = cols
	return res
}

func parseCSVPreview(raw string, kind string) *dataParseResult {
	res := &dataParseResult{Type: "csv"}
	sep := ','
	if kind == "tsv" {
		sep = '	'
	}
	r := csv.NewReader(strings.NewReader(raw))
	r.Comma = sep
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil || len(records) == 0 {
		return parseTextPreview(raw)
	}
	res.Columns = records[0]
	body := records[1:]
	res.TotalRows = len(body)
	res.Truncated = len(body) > dataPreviewMaxRows
	for i, rec := range body {
		if i >= dataPreviewMaxRows {
			break
		}
		cells := make([]any, 0, len(rec))
		for _, c := range rec {
			cells = append(cells, strings.TrimSpace(c))
		}
		res.Rows = append(res.Rows, cells)
	}
	return res
}

func parseTextPreview(raw string) *dataParseResult {
	res := &dataParseResult{Type: "text"}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	// 去掉末尾的空行（文件以换行结束是常态，不该算作第 N 行）。
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	res.TotalRows = len(lines)
	res.Truncated = len(lines) > dataPreviewMaxLines
	keep := lines
	if len(keep) > dataPreviewMaxLines {
		keep = keep[:dataPreviewMaxLines]
	}
	res.Text = strings.Join(keep, "\n")
	return res
}

func (s *Server) serveArtifactDataPreview(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	aid, err := strconv.ParseInt(r.PathValue("aid"), 10, 64)
	if err != nil || aid <= 0 {
		writeErr(w, 400, "bad artifact id")
		return
	}
	pg := s.m.PG()
	if pg == nil {
		writeErr(w, 500, "PG 不可用")
		return
	}
	a, err := pg.GetTaskArtifact(aid)
	if err != nil || a == nil || a.TaskID != taskID {
		writeErr(w, 404, "artifact not found")
		return
	}
	abs := filepath.Join(s.m.dir, a.Storage)
	st, err := os.Stat(abs)
	if err != nil || !st.Mode().IsRegular() {
		writeErr(w, 404, "artifact file missing")
		return
	}
	raw := ""
	if st.Size() > dataPreviewMaxBytes {
		f, err := os.Open(abs)
		if err != nil {
			writeErr(w, 500, "open failed")
			return
		}
		defer f.Close()
		buf := make([]byte, dataPreviewMaxBytes)
		n, _ := f.Read(buf)
		raw = string(buf[:n])
	} else {
		b, err := os.ReadFile(abs)
		if err != nil {
			writeErr(w, 500, "read failed")
			return
		}
		raw = string(b)
	}
	res := parseDataPreview(dataPreviewKind(a.Name), raw)
	res.TotalSize = st.Size()
	res.Name = a.Name
	res.Kind = a.Kind
	res.BoundFinding = a.FindingID
	writeJSON(w, 200, res)
}

// 解析单测见 artifact_data_preview_test.go。
