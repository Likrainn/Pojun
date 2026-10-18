package report

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/chu0119/artex/db"
)

func TestDocxStructure(t *testing.T) {
	d := NewDocx()
	d.H1("渗透测试报告 — 测试任务")
	d.P("生成时间:2026-10-18")
	d.H2("一、概览")
	d.Table([]string{"资产类型", "数量"}, [][]string{{"IP", "770"}, {"服务", "712"}})
	d.Bullet("要点一")
	d.Code("curl http://target/ -H 'X-Test: 1'")
	d.H3("3.1 子标题 <特殊字符> & 引号")
	d.PageBreak()
	d.H2("二、漏洞明细")
	d.P("含 & 尖括号 < > 的段落")

	var buf bytes.Buffer
	if err := d.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty docx")
	}

	// zip 结构与关键部件
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	want := map[string]bool{
		"[Content_Types].xml":          false,
		"_rels/.rels":                  false,
		"word/document.xml":            false,
		"word/styles.xml":              false,
		"word/_rels/document.xml.rels": false,
	}
	for _, f := range zr.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing part: %s", name)
		}
	}

	// document.xml 内容:转义正确、元素存在
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		rc.Close()
		doc := b.String()
		for _, frag := range []string{
			"渗透测试报告 — 测试任务",
			"&lt;特殊字符&gt; &amp; 引号",
			"X-Test: 1",
			"<w:tbl>",
			`w:type="page"`,
			"Heading2",
		} {
			if !strings.Contains(doc, frag) {
				t.Errorf("document.xml missing %q", frag)
			}
		}
		if strings.Contains(doc, "<w:t>unclosed") {
			t.Error("escaping broken")
		}
	}
}

func TestBuildTaskDocxAndFindingDocx(t *testing.T) {
	f := &db.DBFinding{
		ID: 42, VulnClass: "未授权访问", Name: "Admin console exposed",
		Severity: "critical", Summary: "console reachable", Evidence: "curl /admin → 200",
		Worker: "work#1", AssetIDs: []int64{5, 2}, CreatedAt: time.Now(),
		Report: "## 复现步骤\n1. 打开 /admin\n```\ncurl -i http://x/admin\n```\n### 影响\n全量接管",
	}
	in := TaskDocxInput{
		Title: "T", Goal: "G", GeneratedAt: time.Now(),
		AssetCounts: map[string]int{"ip": 3},
		Findings:    []*db.DBFinding{f},
	}
	var buf bytes.Buffer
	if err := BuildTaskDocx(in).Encode(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty task docx")
	}
	buf.Reset()
	if err := BuildFindingDocx(f, time.Now()).Encode(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty finding docx")
	}
}

// TestTaskDocxAggregation 同类多主机上报在总报告里折叠:类别分布一行 +
// 明细按类别分组(变体写法 SQLi/SQL注入 也要折进同一组)。
func TestTaskDocxAggregation(t *testing.T) {
	mk := func(id int64, class, sev string) *db.DBFinding {
		return &db.DBFinding{
			ID: id, VulnClass: class, Name: "MS17-010", Severity: sev,
			Summary: "smb reachable", Evidence: "nmap -p445",
			AssetIDs: []int64{id * 10}, CreatedAt: time.Now(),
		}
	}
	findings := []*db.DBFinding{
		mk(1, "永恒之蓝", "high"), mk(2, "CVE-2017-0144", "high"), mk(3, "已知漏洞", "high"),
		mk(4, "SQLi", "critical"), mk(5, "sql注入", "medium"),
		mk(6, "未授权访问", "low"),
	}
	in := TaskDocxInput{Title: "T", GeneratedAt: time.Now(), Findings: findings}
	var buf bytes.Buffer
	if err := BuildTaskDocx(in).Encode(&buf); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var doc string
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			var b bytes.Buffer
			_, _ = b.ReadFrom(rc)
			rc.Close()
			doc = b.String()
		}
	}
	if doc == "" {
		t.Fatal("no document.xml")
	}
	// 已知漏洞组(3 条,变体已折叠)排第一组;注入组(2 条)次之
	for _, frag := range []string{
		"2.1 已知漏洞(共 3 条)", "2.2 注入(共 2 条)", "2.3 未授权访问(共 1 条)",
		"漏洞类别分布", "涉及资产数",
	} {
		if !strings.Contains(doc, frag) {
			t.Errorf("document.xml missing %q", frag)
		}
	}
}
