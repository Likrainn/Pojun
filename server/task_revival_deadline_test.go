package server

import (
	"testing"
	"time"
)

// TestTerminalRevivalResetsDeadline 是任务 13（2026-11-05）事故的回归测试：
//
// 复现：任务在 11-04 因「目标已全部达成」落 done，但 first_run_at/deadline_at
// 仍留在 11-04 的 8 小时预算上；11-05 人类提交「深入漏洞」跟进意图 → admitTask
// 把 done 复活为 running，旧代码只对 timeout 复活重置时钟 → 复活的 coordinator
// 读到已过期的 deadline，1 秒后再次收尾（终局轮「不要再生成新意图」），
// 人工意图被静默吞掉，任务终态 timeout。
//
// 修复后：任何终态（done/failed/timeout）复活都清空 first_run_at/deadline_at，
// 下一次真实运行重新盖章整套预算。
func TestTerminalRevivalResetsDeadline(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer m.Close()

	task, err := m.CreateTask("terminal revival deadline regression", "follow-up must get a fresh budget", nil, 3600, 0)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// 把任务摆成「早已 done 且 deadline 过期」的状态（模拟隔天人工跟进前的现场）。
	expiredFirst := time.Now().Add(-26 * time.Hour)
	expiredDeadline := time.Now().Add(-18 * time.Hour)
	if _, err := m.pg.Exec(
		`UPDATE tasks SET status='done', first_run_at=$2, deadline_at=$3 WHERE id=$1`,
		task.ID, expiredFirst, expiredDeadline); err != nil {
		t.Fatalf("seed expired done task: %v", err)
	}

	// 模拟 admitTask 的终态复活路径（done → running, 非排队）。
	if err := m.ApplyTaskAdmission(task.ID, "done", "running", false, "resume", false); err != nil {
		t.Fatalf("apply admission: %v", err)
	}

	// DB：两个时钟必须清零，状态为 running。
	var status string
	var firstNull, deadlineNull bool
	if err := m.pg.QueryRow(
		`SELECT status, first_run_at IS NULL, deadline_at IS NULL FROM tasks WHERE id=$1`,
		task.ID).Scan(&status, &firstNull, &deadlineNull); err != nil {
		t.Fatalf("read back task: %v", err)
	}
	if status != "running" {
		t.Fatalf("status = %q, want running", status)
	}
	if !firstNull || !deadlineNull {
		t.Fatalf("terminal revival must clear clocks: first_run_at null=%v deadline_at null=%v", firstNull, deadlineNull)
	}

	// 内存态同步：Engine 的 deadline 协调器读的是 lifecycleSnapshot，必须同步清零，
	// 否则新起的协调器仍会拿过期 deadline 当场收尾。
	state := task.lifecycleSnapshot()
	if state.Status != "running" {
		t.Fatalf("memory status = %q, want running", state.Status)
	}
	if state.FirstRunAt != 0 || state.DeadlineAt != 0 {
		t.Fatalf("memory clocks not reset: FirstRunAt=%d DeadlineAt=%d", state.FirstRunAt, state.DeadlineAt)
	}

	// 重新盖章应得到「从现在起算」的新预算，而不是沿用旧 deadline。
	dl, err := m.StampTaskFirstRun(task.ID)
	if err != nil {
		t.Fatalf("stamp first run: %v", err)
	}
	if dl <= 0 {
		t.Fatal("stamp returned no deadline for a task with timeout_seconds=3600")
	}
	if dl < time.Now().Add(50*time.Minute).Unix() {
		t.Fatalf("fresh deadline %d looks stale (expected ≈ now+1h)", dl)
	}
}
