// 代理池 API:CRUD + 状态。持久化在 settings 表(proxy_pool_entries JSONB +
// proxy_pool_strategy),内存态经 Traffic.SetProxyPool 原子推送。
package server

import (
	"encoding/json"
	"net/http"

	pgdb "github.com/chu0119/artex/db"
	"github.com/chu0119/artex/traffic"
)

const (
	settingProxyPoolEntries  = "proxy_pool_entries"
	settingProxyPoolStrategy = "proxy_pool_strategy"
)

// proxyPoolState 是持久化的池配置。URL 明文入库(与 LLM 配置密码同级别,
// settings 表不对外),对外输出掩码。
type proxyPoolState struct {
	Entries  []traffic.PoolEntry `json:"entries"`
	Strategy string              `json:"strategy"`
}

func (s *Server) loadProxyPool() *proxyPoolState {
	return loadProxyPoolState(s.m.pg)
}

func loadProxyPoolState(pg *pgdb.DB) *proxyPoolState {
	st := proxyPoolState{Strategy: "round_robin"}
	if raw, ok, _ := pg.GetSetting(settingProxyPoolEntries); ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	if v, ok, _ := pg.GetSetting(settingProxyPoolStrategy); ok && v != "" {
		st.Strategy = v
	}
	if st.Entries == nil {
		st.Entries = []traffic.PoolEntry{}
	}
	return &st
}

func (s *Server) saveProxyPool(st proxyPoolState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := s.m.pg.SetSetting(settingProxyPoolEntries, string(raw)); err != nil {
		return err
	}
	return s.m.pg.SetSetting(settingProxyPoolStrategy, st.Strategy)
}

// proxyPoolGet 返回当前配置(含运行状态:熔断计数/掩码)。
func (s *Server) proxyPoolGet(w http.ResponseWriter, r *http.Request) {
	st := s.loadProxyPool()
	running := []traffic.PoolEntry{}
	if t := s.m.traffic; t != nil {
		running = t.ProxyPoolStatus()
	}
	writeJSON(w, 200, map[string]any{
		"entries":  st.Entries,
		"strategy": st.Strategy,
		"running":  running,
		"capturing": s.m.TrafficEnabled(),
	})
}

// proxyPoolPut 全量替换配置并热应用。
func (s *Server) proxyPoolPut(w http.ResponseWriter, r *http.Request) {
	var in proxyPoolState
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.Entries == nil {
		in.Entries = []traffic.PoolEntry{}
	}
	// 校验:URL 可解析、权重下限、名称去重留白
	seen := map[string]bool{}
	for i := range in.Entries {
		e := &in.Entries[i]
		if _, err := traffic.ValidateProxyURL(e.URL); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if e.Weight < 1 {
			e.Weight = 1
		}
		if seen[e.URL] {
			writeErr(w, 400, "重复的代理地址: "+e.URL)
			return
		}
		seen[e.URL] = true
	}
	if t := s.m.traffic; t != nil {
		if err := t.SetProxyPool(in.Entries, in.Strategy); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
	}
	if err := s.saveProxyPool(in); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.proxyPoolGet(w, r)
}
