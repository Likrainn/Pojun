// Package vulnintel 维护定期同步的公开漏洞情报（NVD / CISA KEV），供 agent 在
// 识别目标组件后直接检索已知漏洞与现成利用，而不是每次靠模型现想。
// 数据全部来自公开源；同步出网走全局出口代理。表结构见 db/schema.sql 的
// vuln_cves / vuln_cpe_products / vuln_pocs / vuln_sync_state。
package vulnintel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chu0119/artex/db"
)

// Store 提供漏洞情报的写入（同步器）与检索（agent 工具）。
type Store struct {
	db *db.DB
}

// New wraps an opened database. Tables live in db/schema.sql and are created
// by the idempotent startup migration, so there is no separate Ensure here.
func New(d *db.DB) *Store { return &Store{db: d} }

// CVE 是一次同步写入的最小记录。
type CVE struct {
	ID          string
	Title       string
	Description string
	Severity    string  // CRITICAL/HIGH/MEDIUM/LOW/NONE/UNKNOWN
	Score       *float64
	Vector      string
	Published   time.Time
	Modified    time.Time
	Criteria    []string // cpe:2.3:... 原文（含版本区间语法）
	Refs        []string
}

// UpsertCVE writes one record: the row itself plus a rebuilt (vendor, product)
// inverted index. KEV columns are owned by the KEV sync and are NOT touched.
func (s *Store) UpsertCVE(ctx context.Context, c CVE) error {
	if c.ID == "" {
		return fmt.Errorf("vulnintel: empty cve id")
	}
	criteria := marshalJSONB(c.Criteria)
	refs := marshalJSONB(c.Refs)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO vuln_cves
		(cve_id, source, title, description, severity, cvss_score, cvss_vector,
		 published_at, modified_at, cpe_criteria, refs, synced_at)
		VALUES ($1,'nvd',$2,$3,$4,$5,$6,$7,$8,$9,$10,now())
		ON CONFLICT (cve_id) DO UPDATE SET
		  title=EXCLUDED.title, description=EXCLUDED.description,
		  severity=EXCLUDED.severity, cvss_score=EXCLUDED.cvss_score,
		  cvss_vector=EXCLUDED.cvss_vector, published_at=EXCLUDED.published_at,
		  modified_at=EXCLUDED.modified_at, cpe_criteria=EXCLUDED.cpe_criteria,
		  refs=EXCLUDED.refs, synced_at=now()`,
		c.ID, c.Title, c.Description, c.Severity, c.Score, c.Vector,
		nullTime(c.Published), nullTime(c.Modified), criteria, refs); err != nil {
		return fmt.Errorf("upsert vuln_cves %s: %w", c.ID, err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM vuln_cpe_products WHERE cve_id=$1`, c.ID); err != nil {
		return fmt.Errorf("clear cpe index %s: %w", c.ID, err)
	}
	seen := map[string]bool{}
	for _, cr := range c.Criteria {
		vendor, product, ok := cpeVendorProduct(cr)
		if !ok {
			continue
		}
		key := vendor + "\x00" + product
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := s.db.ExecContext(ctx, `INSERT INTO vuln_cpe_products (cve_id, vendor, product)
			VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, c.ID, vendor, product); err != nil {
			return fmt.Errorf("index cpe %s: %w", c.ID, err)
		}
	}
	return nil
}

// MarkKEV sets the in-the-wild exploitation flag. Rows unknown to the NVD sync
// (KEV moves faster than NVD sometimes) get a stub row so the flag is visible.
func (s *Store) MarkKEV(ctx context.Context, cveID string, added time.Time) error {
	tag := "KEV: " + added.Format("2006-01-02")
	res, err := s.db.ExecContext(ctx, `UPDATE vuln_cves SET kev=true, kev_date_added=$2 WHERE cve_id=$1`, cveID, nullTime(added))
	if err != nil {
		return fmt.Errorf("mark kev %s: %w", cveID, err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO vuln_cves (cve_id, source, title, severity, kev, kev_date_added)
		VALUES ($1,'kev',$2,'UNKNOWN',true,$3) ON CONFLICT (cve_id) DO UPDATE SET kev=true, kev_date_added=$3`,
		cveID, tag, nullTime(added))
	if err != nil {
		return fmt.Errorf("insert kev stub %s: %w", cveID, err)
	}
	return nil
}

// VulnHit 是 search_vulns 的单条结果（详情字段刻意精简，让 LLM 先看摘要）。
type VulnHit struct {
	CVEID     string   `json:"cve_id"`
	Severity  string   `json:"severity"`
	Score     *float64 `json:"cvss_score,omitempty"`
	KEV       bool     `json:"kev"`
	Published string   `json:"published,omitempty"`
	Title     string   `json:"title"`
	// 命中的 (vendor, product) 对，LLM 用它确认检索词与目标组件对得上。
	Matched []string `json:"matched,omitempty"`
	// version 非空且出现在某条 criteria 的版本字段里时置位——粗筛信号，
	// 版本是否真正落在影响区间由 LLM 结合 criteria 判断。
	VersionSeen bool `json:"version_seen_in_criteria"`
}

// SearchVulns 按组件名（可带厂商限定）检索已知漏洞。product 匹配 CPE product
// 段（大小写不敏感，支持 % 通配交给 ILIKE）；KEV 优先、分数次之。
func (s *Store) SearchVulns(ctx context.Context, vendor, product, version string, limit int) ([]VulnHit, error) {
	product = strings.TrimSpace(product)
	if product == "" {
		return nil, fmt.Errorf("product is required")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pattern := strings.ToLower(product)
	if !strings.ContainsAny(pattern, "%_") {
		pattern = "%" + pattern + "%"
	}
	// Fixed placeholders: optional filters are always passed (empty = no filter).
	// Conditionally appended $N breaks PostgreSQL when the numbers skip a slot.
	args := []any{pattern, strings.TrimSpace(vendor), strings.TrimSpace(version), limit}
	rows, err := s.db.QueryContext(ctx, `
SELECT c.cve_id, c.severity, c.cvss_score, c.kev, COALESCE(c.published_at::text,''),
       COALESCE(c.title,''), c.kev_date_added IS NOT NULL AS kev_row,
       COALESCE((
         SELECT string_agg(DISTINCT p.vendor || '/' || p.product, ', ')
         FROM vuln_cpe_products p WHERE p.cve_id = c.cve_id AND p.product ILIKE $1
       ), '') AS matched
FROM vuln_cves c
JOIN vuln_cpe_products p ON p.cve_id = c.cve_id AND p.product ILIKE $1
                        AND ($2 = '' OR p.vendor ILIKE '%' || $2 || '%')
GROUP BY c.id
ORDER BY c.kev DESC,
         (CASE WHEN $3 <> '' AND c.cpe_criteria::text LIKE '%' || $3 || '%' THEN 1 ELSE 0 END) DESC,
         c.cvss_score DESC NULLS LAST, c.published_at DESC
LIMIT $4`, args...)
	if err != nil {
		return nil, fmt.Errorf("search vulns: %w", err)
	}
	defer rows.Close()
	version = strings.TrimSpace(version)
	out := []VulnHit{}
	for rows.Next() {
		var h VulnHit
		var kevRow bool
		var matched string
		if err := rows.Scan(&h.CVEID, &h.Severity, &h.Score, &h.KEV, &h.Published, &h.Title, &kevRow, &matched); err != nil {
			return nil, err
		}
		if kevRow {
			h.KEV = true
		}
		if matched != "" {
			h.Matched = strings.Split(matched, ", ")
		}
		if version != "" && s.criteriaContains(ctx, h.CVEID, version) {
			h.VersionSeen = true
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// criteriaContains reads the stored criteria array and reports whether the
// version string appears as an exact CPE field. Deliberately exact-token
// semantics (e.g. "2.4.49" matching the version slot) — precision filtering is
// the LLM's job on the returned criteria; this only boosts ordering.
func (s *Store) criteriaContains(ctx context.Context, cveID, version string) bool {
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT cpe_criteria FROM vuln_cves WHERE cve_id=$1`, cveID).Scan(&raw); err != nil || len(raw) == 0 {
		return false
	}
	var items []string
	if json.Unmarshal(raw, &items) != nil {
		return false
	}
	for _, it := range items {
		for _, part := range strings.Split(it, ":") {
			if part == version {
				return true
			}
		}
	}
	return false
}

// VulnDetail 是 get_vuln 的返回：完整描述、评分与原始 criteria/refs。
type VulnDetail struct {
	CVEID       string   `json:"cve_id"`
	Severity    string   `json:"severity"`
	Score       *float64 `json:"cvss_score,omitempty"`
	Vector      string   `json:"cvss_vector,omitempty"`
	KEV         bool     `json:"kev"`
	Published   string   `json:"published,omitempty"`
	Modified    string   `json:"modified,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Criteria    []string `json:"cpe_criteria,omitempty"`
	Refs        []string `json:"references,omitempty"`
}

// GetVuln returns the full record for one CVE (nil, nil when absent).
func (s *Store) GetVuln(ctx context.Context, cveID string) (*VulnDetail, error) {
	cveID = strings.ToUpper(strings.TrimSpace(cveID))
	var d VulnDetail
	var criteria, refs []byte
	var pub, mod *time.Time
	err := s.db.QueryRowContext(ctx, `SELECT cve_id, severity, cvss_score, COALESCE(cvss_vector,''),
		kev, published_at, modified_at, title, description, cpe_criteria, refs
		FROM vuln_cves WHERE cve_id=$1`, cveID).
		Scan(&d.CVEID, &d.Severity, &d.Score, &d.Vector, &d.KEV, &pub, &mod, &d.Title, &d.Description, &criteria, &refs)
	if err == nil {
		if pub != nil {
			d.Published = pub.UTC().Format(time.RFC3339)
		}
		if mod != nil {
			d.Modified = mod.UTC().Format(time.RFC3339)
		}
		d.Criteria = unmarshalStringArray(criteria)
		d.Refs = unmarshalStringArray(refs)
		return &d, nil
	}
	if !isNoRows(err) {
		return nil, fmt.Errorf("get vuln %s: %w", cveID, err)
	}
	// KEV stub rows may live in vuln_pocs-adjacent state before NVD catches up;
	// currently there is nothing else to read, so report absence plainly.
	return nil, nil
}

// SyncState mirrors one vuln_sync_state row.
type SyncState struct {
	Source      string     `json:"source"`
	LastSyncAt  *time.Time `json:"last_sync_at,omitempty"`
	Cursor      string     `json:"cursor"`
	TotalSynced int64      `json:"total_synced"`
	LastError   string     `json:"last_error"`
	Enabled     bool       `json:"enabled"`
}

// GetSyncState returns the row for source, zero-value (enabled=true mirrors the
// column default) when absent — callers decide their own enabled semantics.
func (s *Store) GetSyncState(ctx context.Context, source string) (SyncState, error) {
	st := SyncState{Source: source, Enabled: true}
	// to_char forces ISO-8601 on the way out; the ::text cast would return
	// Postgres's own "2024-05-06 07:08:09+00" which nothing here parses.
	var lastSync string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(to_char(last_sync_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),''), cursor, total_synced, last_error, enabled
		FROM vuln_sync_state WHERE source=$1`, source).
		Scan(&lastSync, &st.Cursor, &st.TotalSynced, &st.LastError, &st.Enabled)
	if err != nil {
		if isNoRows(err) {
			return st, nil
		}
		return st, err
	}
	if t, e := time.Parse(time.RFC3339, lastSync); e == nil {
		st.LastSyncAt = &t
	}
	return st, nil
}

// SaveSyncState upserts the watermark row.
func (s *Store) SaveSyncState(ctx context.Context, st SyncState) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO vuln_sync_state (source, last_sync_at, cursor, total_synced, last_error, enabled)
		VALUES ($1, COALESCE($2::timestamptz, now()), $3, $4, $5, $6)
		ON CONFLICT (source) DO UPDATE SET
		  last_sync_at=COALESCE($2::timestamptz, now()), cursor=EXCLUDED.cursor,
		  total_synced=EXCLUDED.total_synced, last_error=EXCLUDED.last_error, enabled=EXCLUDED.enabled`,
		st.Source, st.LastSyncAt, st.Cursor, st.TotalSynced, st.LastError, st.Enabled)
	return err
}

// ---- helpers ----

func marshalJSONB(items []string) any {
	if len(items) == 0 {
		return nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return nil
	}
	return string(b)
}

func unmarshalStringArray(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func isNoRows(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no rows in result set")
}

// cpeVendorProduct extracts (vendor, product) from a CPE 2.3 criteria string:
// cpe:2.3:part:vendor:product:version:...  Escaped colons (\:) are tolerated by
// splitting on unescaped colons only. part must be a/app/o/h (device classes we
// care about); "cpe:2.3" prefixed rows only.
func cpeVendorProduct(criteria string) (vendor, product string, ok bool) {
	if !strings.HasPrefix(criteria, "cpe:2.3:") {
		return "", "", false
	}
	parts := splitUnescapedColons(criteria)
	// cpe:2.3:<part>:<vendor>:<product>:... → indices 2,3,4
	if len(parts) < 5 {
		return "", "", false
	}
	return unescape(parts[3]), unescape(parts[4]), true
}

// splitUnescapedColons splits on ':' not preceded by '\'.
func splitUnescapedColons(s string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			cur.WriteByte(s[i])
			i++
			cur.WriteByte(s[i])
			continue
		}
		if s[i] == ':' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	out = append(out, cur.String())
	return out
}

func unescape(s string) string {
	return strings.ReplaceAll(s, `\`, "")
}
