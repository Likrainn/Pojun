// 漏洞情报状态 API:同步水位(vuln_sync_state 全行)与库内条数,供
// 系统 → 漏洞情报 页展示。只读,不含敏感数据。
package server

import (
	"net/http"
)

func (s *Server) vulnintelStatus(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	m := s.m
	if m == nil || m.vulnintel == nil {
		writeErr(w, 503, "漏洞情报库未初始化")
		return
	}
	st := m.vulnintel.StatusSnapshot(r.Context())
	st["enabled"] = pg.GetBool(settingVulnIntelOn, true)
	st["nuclei_templates_dir"] = m.nucleiDir
	writeJSON(w, 200, st)
}
