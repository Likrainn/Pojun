// 漏洞情报同步的运行时配置、手动触发与并发控制。
//
// 定时循环（runVulnIntelSync）与页面上的「立即同步」共用同一条 pass：同一时刻
// 只允许一轮，避免两个 goroutine 并发写 vuln_sync_state / 抢 NVD 限速。
package server

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chu0119/artex/vulnintel"
)

// 漏洞情报的可配置项（settings）。
const (
	settingVulnIntelGitHubToken   = "vulnintel_github_token"
	settingVulnIntelPocEnabled    = "vulnintel_poc_enabled"
	settingVulnIntelBackfillDays  = "vulnintel_backfill_days"
	settingVulnIntelMaxRuntimeMin = "vulnintel_max_runtime_minutes"
	// 同步专用出网代理。空 = 走维护出网回退链（装机代理 → 全局 → 搜索代理）。
	settingVulnIntelProxy = "vulnintel_proxy"
)

const (
	defaultVulnIntelMaxRuntimeMin = 30
	defaultVulnIntelInterval      = 6 * time.Hour
)

// errVulnSyncRunning 表示已有一轮同步在跑；调用方按「已接受/稍后再试」处理。
var errVulnSyncRunning = errors.New("vulnintel sync already running")

// vulnIntelProxy resolves the egress proxy for intel sync. 专用代理优先，然后
// 复用维护链（装机代理 → 全局 → 搜索代理）——和拉更新/装工具走同一条出口。
func (m *Manager) vulnIntelProxy() string {
	if m == nil || m.pg == nil {
		return ""
	}
	if v, _, _ := m.pg.GetSetting(settingVulnIntelProxy); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return m.maintenanceProxy()
}

// vulnIntelConfig assembles the full sync config from settings. Every field is
// read fresh so a change in the UI applies on the next pass without a restart.
func (m *Manager) vulnIntelConfig() vulnintel.Config {
	cfg := vulnintel.Config{
		PocEnabled:   m.pg.GetBool(settingVulnIntelPocEnabled, false),
		BackfillDays: intSetting(m.pg, settingVulnIntelBackfillDays, 0),
	}
	cfg.ProxyURL = m.vulnIntelProxy()
	cfg.NvdAPIKey = strings.TrimSpace(settingString(m.pg, settingNvdApiKey))
	if cfg.NvdAPIKey == "" {
		cfg.NvdAPIKey = strings.TrimSpace(os.Getenv("NVD_API_KEY"))
	}
	cfg.GitHubToken = strings.TrimSpace(settingString(m.pg, settingVulnIntelGitHubToken))
	if cfg.GitHubToken == "" {
		cfg.GitHubToken = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if mins := intSetting(m.pg, settingVulnIntelMaxRuntimeMin, defaultVulnIntelMaxRuntimeMin); mins > 0 {
		cfg.MaxRuntime = time.Duration(mins) * time.Minute
	}
	return cfg
}

// SyncVulnIntelNow starts one pass in the background (manual trigger). Returns
// false when a pass is already running. The enabled switch is intentionally
// ignored: clicking "sync now" means sync now.
func (m *Manager) SyncVulnIntelNow() bool {
	if m == nil || m.vulnintel == nil {
		return false
	}
	if !m.vulnSyncMu.TryLock() {
		return false
	}
	m.vulnSyncRunning.Store(true)
	m.vulnSyncStartedAt.Store(time.Now().Unix())
	go func() {
		defer m.vulnSyncMu.Unlock()
		defer m.vulnSyncRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		if err := m.vulnIntelPassLocked(ctx); err != nil {
			log.Printf("[vulnintel] 手动同步未完整完成: %v", err)
		}
	}()
	return true
}

// VulnIntelSyncState reports whether a pass is running and when it started
// (unix seconds, 0 = never). Used by the status API.
func (m *Manager) VulnIntelSyncState() (running bool, startedAt int64) {
	if m == nil {
		return false, 0
	}
	return m.vulnSyncRunning.Load(), m.vulnSyncStartedAt.Load()
}

// runVulnIntelPass is the scheduled-loop entry: it takes the same lock, so a
// manual trigger and the timer can never overlap.
func (m *Manager) runVulnIntelPass(ctx context.Context) error {
	if !m.vulnSyncMu.TryLock() {
		return errVulnSyncRunning
	}
	defer m.vulnSyncMu.Unlock()
	m.vulnSyncRunning.Store(true)
	m.vulnSyncStartedAt.Store(time.Now().Unix())
	defer m.vulnSyncRunning.Store(false)
	return m.vulnIntelPassLocked(ctx)
}

// vulnIntelPassLocked is one full pass: CVE/KEV/PoC sync → asset linking →
// nuclei template tree. The caller holds vulnSyncMu.
func (m *Manager) vulnIntelPassLocked(ctx context.Context) error {
	cfg := m.vulnIntelConfig()
	if err := vulnintel.Run(ctx, m.vulnintel, cfg); err != nil {
		log.Printf("[vulnintel] 本轮同步未完整完成: %v（游标已保留，下轮续跑）", err)
	}
	// 资产联动：新同步的 CVE 反向匹配 service 资产指纹，受影响任务挂 hint。
	// 同步没完整完成也照跑——KEV stub 与已 upsert 的记录都已可查。
	m.linkVulnIntelAssets()
	m.refreshNucleiTemplates(cfg.ProxyURL)
	return nil
}

// refreshNucleiTemplates keeps the local template tree moving. It is deliberately
// called outside the "enabled" gate: scan_with_nuclei is an independent feature
// and the tree must not rot when intel sync is switched off. Caller owns
// vulnSyncMu.
func (m *Manager) refreshNucleiTemplates(proxyURL string) {
	tplCtx, tplCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer tplCancel()
	if err := vulnintel.EnsureTemplates(tplCtx, m.nucleiDir, proxyURL); err != nil {
		log.Printf("[vulnintel] nuclei 模板树更新失败: %v", err)
	}
}

// intSetting reads an integer setting with a default.
func intSetting(pg interface {
	GetSetting(string) (string, bool, error)
}, key string, def int) int {
	v, ok, err := pg.GetSetting(key)
	if err != nil || !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

func settingString(pg interface {
	GetSetting(string) (string, bool, error)
}, key string) string {
	v, _, _ := pg.GetSetting(key)
	return v
}
