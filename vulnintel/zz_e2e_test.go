package vulnintel

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chu0119/artex/db"
)

// 临时 E2E：快通道真跑一次（真 PG + 真 NVD），验证 PHP 那两个 CVE 能进库。
func TestFastLaneE2E(t *testing.T) {
	proxy := os.Getenv("VULN_PROXY")
	if proxy == "" {
		t.Skip("no proxy")
	}
	dsn, _, err := db.DSN()
	if err != nil {
		t.Skipf("no db: %v", err)
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Skipf("no pg: %v", err)
	}
	defer d.Close()
	st := New(d)

	cfg := FastLaneConfig{
		ProxyURL:   proxy,
		WindowDays: 30,
		MaxRuntime: 25 * time.Minute,
		PageDelay:  time.Second,
	}
	res, err := RunFastLane(context.Background(), st, cfg)
	if err != nil {
		t.Fatalf("fast lane err: %v", err)
	}
	t.Logf("扫 %d 条，入库 %d 条，版本待定 %d 条，游标 %s，截断=%v",
		res.Scanned, res.Stored, res.VersionUndetermined, res.Cursor, res.Truncated)

	// 查最近高危
	hits, err := st.SearchRecentCritical(context.Background(), 30, 50)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("最近 30 天高危 %d 条：", len(hits))
	for _, h := range hits {
		t.Logf("  %s sev=%s score=%.1f kev=%v 未定版本=%v products=%v %s",
			h.CVEID, h.Severity, h.Score, h.KEV, h.VersionUndetermined, h.Products, h.Title)
	}

	// 单独确认两个 PHP CVE
	for _, id := range []string{"CVE-2026-109618", "CVE-2026-109619"} {
		dv, err := st.GetVuln(context.Background(), id)
		if err != nil {
			t.Errorf("%s 查询失败: %v", id, err)
			continue
		}
		if dv == nil {
			t.Logf("%s 不在库里（快通道没拉到）", id)
			continue
		}
		t.Logf("%s 在库里: sev=%s score=%v criteria=%v refs=%d", id, dv.Severity, dv.Score, dv.Criteria, len(dv.Refs))
	}
}
