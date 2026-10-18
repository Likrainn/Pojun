// 报告导出 API:任务总报告(docx/md)与单漏洞报告(docx)。
// 内容取自 findings 表(含 report 正文/证据);docx 由 report 包内置模板渲染。
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/chu0119/artex/db"
	"github.com/chu0119/artex/report"
)

// exportTaskDocx 任务总报告(Word)。
func (s *Server) exportTaskDocx(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeErr(w, 404, "no active task")
		return
	}
	findings, err := s.m.pg.ListFindingsForExport(db.FindingFilter{TaskID: t.ID}, nil)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	counts := map[string]int{}
	for _, ty := range []string{"root_domain", "ip", "subdomain", "app", "service", "endpoint"} {
		ns, _ := s.m.Assets().QueryByType(ty, 100000, 0)
		if len(ns) > 0 {
			counts[ty] = len(ns)
		}
	}
	doc := report.BuildTaskDocx(report.TaskDocxInput{
		Title:       fmt.Sprintf("渗透测试报告 — %s", t.Description),
		Goal:        t.Goal,
		GeneratedAt: time.Now(),
		AssetCounts: counts,
		Findings:    findings,
	})
	name := fmt.Sprintf("artex-report-task%s-%s.docx", t.ID, time.Now().Format("20060102"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.WriteHeader(200)
	_ = doc.Encode(w)
}

// exportFindingDocx 单漏洞报告(Word)。
func (s *Server) exportFindingDocx(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "bad finding id")
		return
	}
	f, err := s.m.pg.GetFinding(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if f == nil {
		writeErr(w, 404, "finding not found")
		return
	}
	doc := report.BuildFindingDocx(f, time.Now())
	name := fmt.Sprintf("artex-finding-%d-%s.docx", f.ID, time.Now().Format("20060102"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.WriteHeader(200)
	_ = doc.Encode(w)
}
