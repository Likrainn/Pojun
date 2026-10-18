// 漏洞情报状态 API:同步水位(vuln_sync_state 全行)与库内条数,供
// 系统 → 漏洞情报 页展示。只读,不含敏感数据。
package server

import (
	"net/http"
	"strings"

	pgdb "github.com/chu0119/artex/db"
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

// egressHint / egressPortMap 读回连配置(settings;读失败按空串处理)。
func egressHint(pg *pgdb.DB) string {
	v, _, _ := pg.GetSetting("egress_hint")
	return strings.TrimSpace(v)
}

func egressPortMap(pg *pgdb.DB) string {
	v, _, _ := pg.GetSetting("egress_port_map")
	return strings.TrimSpace(v)
}
