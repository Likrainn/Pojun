// 报告导出 API:任务总报告(docx/md)与单漏洞报告(docx)。
// 内容取自 findings 表(含 report 正文/证据);docx 由 report 包内置模板渲染。
// v0.3.93 起:交付默认剔除误报/重复/忽略/风险接受(A7),资产可读名(A8)、
// 复测记录(A9)、流量证据(A10)一并接进 Word。
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/chu0119/artex/db"
	"github.com/chu0119/artex/report"
)

// exportExcludeStatuses 交付件默认剔除的分拣状态。
// false_positive/duplicate/ignored/risk_accepted 都是「不该出现在客户面前」的
// 三态;pending/in_progress/confirmed/resolved/fixed 正常交付。
var exportExcludeStatuses = []string{
	db.FindingFalsePositive, db.FindingDuplicate, db.FindingIgnored, db.FindingRiskAccepted,
}

// reportAssetNames 批量解析资产 id → 可读名（域名/IP/URL，A8）。
func (s *Server) reportAssetNames(findings []*db.DBFinding) map[int64]string {
	seen := map[int64]bool{}
	var ids []int64
	for _, f := range findings {
		for _, a := range f.AssetIDs {
			if a > 0 && !seen[a] {
				seen[a] = true
				ids = append(ids, a)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	list, err := s.m.pg.Assets().GetByIDs(ids)
	if err != nil {
		return nil
	}
	out := make(map[int64]string, len(list))
	for _, a := range list {
		out[a.ID] = assetDisplayName(a)
	}
	return out
}

// assetDisplayName 资产可读名:按类型取最有辨识度的字段。
func assetDisplayName(a *db.Asset) string {
	switch a.Type {
	case "endpoint", "app", "service":
		if a.URL != "" {
			return a.URL
		}
		if a.Domain != "" && a.Port != nil {
			return fmt.Sprintf("%s:%d", a.Domain, *a.Port)
		}
		if a.IP != "" && a.Port != nil {
			return fmt.Sprintf("%s:%d", a.IP, *a.Port)
		}
	case "subdomain", "root_domain":
		if a.Domain != "" {
			return a.Domain
		}
	case "ip":
		if a.IP != "" {
			return a.IP
		}
	}
	// 兜底:各类型还有值的字段依次试
	if a.URL != "" {
		return a.URL
	}
	if a.Domain != "" {
		return a.Domain
	}
	if a.IP != "" {
		return a.IP
	}
	if a.RootDomain != "" {
		return a.RootDomain
	}
	return ""
}

// reportRetests 批量取各 finding 的复测记录（A9；最新在前，ListFindingRetests 已按 id DESC）。
func (s *Server) reportRetests(findings []*db.DBFinding) map[int64][]*db.FindingRetest {
	out := make(map[int64][]*db.FindingRetest)
	for _, f := range findings {
		rs, err := s.m.pg.ListFindingRetests(f.ID)
		if err != nil || len(rs) == 0 {
			continue
		}
		out[f.ID] = rs
	}
	return out
}

// exportTaskDocx 任务总报告(Word)。
func (s *Server) exportTaskDocx(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeErr(w, 404, "no active task")
		return
	}
	findings, err := s.m.pg.ListFindingsForExport(db.FindingFilter{TaskID: t.ID, ExcludeStatuses: exportExcludeStatuses}, nil)
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
		AssetNames:  s.reportAssetNames(findings),
		Retests:     s.reportRetests(findings),
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
	names := s.reportAssetNames([]*db.DBFinding{f})
	retests, _ := s.m.pg.ListFindingRetests(f.ID)
	doc := report.BuildFindingDocx(f, time.Now(), names, retests)
	name := fmt.Sprintf("artex-finding-%d-%s.docx", f.ID, time.Now().Format("20060102"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.WriteHeader(200)
	_ = doc.Encode(w)
}
