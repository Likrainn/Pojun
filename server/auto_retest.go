package server

// 任务收尾后的自动复测（v0.4.38）。
//
// 为什么做：平台实测 5 个任务 222 个漏洞、只有 1 条复测——报告会带着大量未验证
// 结论交付。任务收尾（goal_met/自动收尾）后，对 critical 漏洞按设置自动排一条
// 复测队列：串行跑（一次一个会话），上限与严重度可配，默认只测 critical、
// 最多 10 条、可用设置关闭。不碰 high 默认值是为了控制 LLM 成本。

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/chu0119/pojun/db"
)

const (
	settingAutoRetestEnabled = "auto_retest_enabled"
	settingAutoRetestMax     = "auto_retest_max"
	settingAutoRetestSevs    = "auto_retest_severities"

	defaultAutoRetestEnabled = true
	defaultAutoRetestMax     = 10
	defaultAutoRetestSevs    = "critical"
	autoRetestPollInterval   = 5 * time.Second
	autoRetestWaitCap        = 45 * time.Minute
)

type autoRetestSettings struct {
	Enabled    bool
	Max        int
	Severities []string
}

func autoRetestFromPG(pg *db.DB) autoRetestSettings {
	c := autoRetestSettings{Enabled: defaultAutoRetestEnabled, Max: defaultAutoRetestMax}
	c.Severities = []string{"critical"}
	if pg == nil {
		return c
	}
	if v, ok, err := pg.GetSetting(settingAutoRetestEnabled); err == nil && ok {
		c.Enabled = v == "true" || v == "1"
	}
	if n, ok := settingInt(pg, settingAutoRetestMax); ok {
		c.Max = clampInt(n, 1, 50)
	}
	if v, ok, err := pg.GetSetting(settingAutoRetestSevs); err == nil && ok && v != "" {
		c.Severities = nil
		for _, s := range splitComma(v) {
			if db.ValidSeverity(s) {
				c.Severities = append(c.Severities, s)
			}
		}
		if len(c.Severities) == 0 {
			c.Severities = []string{"critical"}
		}
	}
	return c
}

func splitComma(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		if r == ' ' || r == '\t' {
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// autoRetestState 是每任务的串行队列。
type autoRetestState struct {
	mu      sync.Mutex
	queues  map[string][]int64
	running map[string]bool
}

func newAutoRetestState() *autoRetestState {
	return &autoRetestState{queues: map[string][]int64{}, running: map[string]bool{}}
}

// maybeAutoRetest 在任务收尾后调用：拉取候选（pending + 无进行中复测），追加到
// 队列并启动串行执行。失败只记日志，不影响收尾。
func (s *Server) maybeAutoRetest(taskID, reason string) {
	cfg := autoRetestFromPG(s.m.pg)
	if !cfg.Enabled {
		return
	}
	tid, err := strconv.ParseInt(taskID, 10, 64)
	if err != nil || tid <= 0 {
		return
	}
	cands, err := s.m.pg.RetestCandidates(tid, cfg.Severities, cfg.Max)
	if err != nil {
		log.Printf("[auto-retest] task %s 候选查询失败: %v", taskID, err)
		return
	}
	if len(cands) == 0 {
		return
	}
	s.autoRetest.mu.Lock()
	seen := map[int64]bool{}
	for _, id := range s.autoRetest.queues[taskID] {
		seen[id] = true
	}
	for _, c := range cands {
		if !seen[c.ID] {
			s.autoRetest.queues[taskID] = append(s.autoRetest.queues[taskID], c.ID)
		}
	}
	s.autoRetest.mu.Unlock()
	log.Printf("[auto-retest] task %s 收尾（%s），排队 %d 条 %v 复测", taskID, reason, len(cands), cfg.Severities)
	s.pumpAutoRetest(taskID)
}

// pumpAutoRetest 启动队列里的下一条；同一任务同时只跑一个复测会话。
func (s *Server) pumpAutoRetest(taskID string) {
	s.autoRetest.mu.Lock()
	if s.autoRetest.running[taskID] || len(s.autoRetest.queues[taskID]) == 0 {
		s.autoRetest.mu.Unlock()
		return
	}
	id := s.autoRetest.queues[taskID][0]
	s.autoRetest.queues[taskID] = s.autoRetest.queues[taskID][1:]
	s.autoRetest.running[taskID] = true
	s.autoRetest.mu.Unlock()

	go func() {
		defer func() {
			s.autoRetest.mu.Lock()
			s.autoRetest.running[taskID] = false
			s.autoRetest.mu.Unlock()
			s.pumpAutoRetest(taskID)
		}()
		ctx := s.ctx
		if ctx.Err() != nil {
			return
		}
		_, created, err := s.startRetestForFinding(ctx, id, "自动复测：任务收尾后按设置对未复测的 critical 漏洞补测")
		if err != nil {
			log.Printf("[auto-retest] task %s finding #%d 启动失败: %v", taskID, id, err)
			return
		}
		if !created {
			return // 已有进行中复测，跳过
		}
		log.Printf("[auto-retest] task %s finding #%d 复测已启动", taskID, id)
		deadline := time.Now().Add(autoRetestWaitCap)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(autoRetestPollInterval):
			}
			active, err := s.m.pg.HasActiveFindingRetest(id)
			if err != nil {
				log.Printf("[auto-retest] task %s finding #%d 状态查询失败: %v", taskID, id, err)
				return
			}
			if !active {
				return
			}
		}
		log.Printf("[auto-retest] task %s finding #%d 等待超时（%v），继续下一条", taskID, id, autoRetestWaitCap)
	}()
}

// retestQueueSummary 返回队列长度（供设置页/调试日志）。
func (s *Server) retestQueueSummary() int {
	s.autoRetest.mu.Lock()
	defer s.autoRetest.mu.Unlock()
	n := 0
	for _, q := range s.autoRetest.queues {
		n += len(q)
	}
	return n
}

var _ = context.Background
