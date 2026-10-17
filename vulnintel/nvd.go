package vulnintel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NvdClient talks to the NVD CVE API 2.0. Anonymous access is rate limited to
// ~5 requests / 30s (the client honors 429 Retry-After); an apiKey lifts it to
// 50 / 30s and is optional — configured in the UI, never logged.
type NvdClient struct {
	Base   string // override for tests; default services.nvd.nist.gov
	APIKey string
	HTTP   *http.Client
}

func NewNvdClient(apiKey string, proxyURL string) (*NvdClient, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(proxyURL) != "" {
		u, err := url.Parse(strings.TrimSpace(proxyURL))
		if err != nil {
			return nil, fmt.Errorf("vulnintel: bad proxy url: %w", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &NvdClient{
		Base:   "https://services.nvd.nist.gov",
		APIKey: strings.TrimSpace(apiKey),
		HTTP:   &http.Client{Timeout: 60 * time.Second, Transport: transport},
	}, nil
}

// nvdResponse is the subset of the CVE API 2.0 envelope we consume.
type nvdResponse struct {
	ResultsPerPage  int          `json:"resultsPerPage"`
	StartIndex      int          `json:"startIndex"`
	TotalResults    int          `json:"totalResults"`
	Vulnerabilities []nvdWrapper `json:"vulnerabilities"`
}

type nvdWrapper struct {
	CVE nvdCVE `json:"cve"`
}

type nvdCVE struct {
	ID             string               `json:"id"`
	Published      string               `json:"published"`
	LastModified   string               `json:"lastModified"`
	Descriptions   []nvdDescription     `json:"descriptions"`
	Metrics        map[string]json.RawMessage `json:"metrics"`
	Configurations []nvdConfiguration   `json:"configurations"`
	References     []nvdReference       `json:"references"`
}

type nvdDescription struct {
	Lang  string `json:"lang"`
	Value string `json:"value"`
}

type nvdMetric struct {
	Source   string `json:"source"`
	Type     string `json:"type"`
	CVSSData struct {
		BaseScore  float64 `json:"baseScore"`
		BaseSeverity string `json:"baseSeverity"`
		VectorString string `json:"vectorString"`
	} `json:"cvssData"`
}

type nvdConfiguration struct {
	Nodes []struct {
		CPEMatch []struct {
			Vulnerable            bool   `json:"vulnerable"`
			Criteria              string `json:"criteria"`
			VersionStartIncluding string `json:"versionStartIncluding"`
			VersionStartExcluding string `json:"versionStartExcluding"`
			VersionEndIncluding   string `json:"versionEndIncluding"`
			VersionEndExcluding   string `json:"versionEndExcluding"`
		} `json:"cpeMatch"`
	} `json:"nodes"`
}

type nvdReference struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// nvdTime parses NVD timestamps ("2024-01-02T03:04:05.678").
func nvdTime(s string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05.000", s)
	if t.IsZero() {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

// FetchPage fetches one page. modStart/modEnd bound the lastModified window
// (≤120 days per API contract, enforced by the caller); both empty = unfiltered.
func (c *NvdClient) FetchPage(ctx context.Context, startIndex, resultsPerPage int, modStart, modEnd time.Time) (*nvdResponse, error) {
	base := c.Base
	if base == "" {
		base = "https://services.nvd.nist.gov"
	}
	q := url.Values{}
	q.Set("startIndex", strconv.Itoa(startIndex))
	q.Set("resultsPerPage", strconv.Itoa(resultsPerPage))
	if !modStart.IsZero() {
		q.Set("lastModStartDate", modStart.UTC().Format("2006-01-02T15:04:05.000"))
	}
	if !modEnd.IsZero() {
		q.Set("lastModEndDate", modEnd.UTC().Format("2006-01-02T15:04:05.000"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/rest/json/cves/2.0?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("apiKey", c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		// Honor Retry-After when present; the sync loop re-calls at its own pace.
		ra := resp.Header.Get("Retry-After")
		return nil, &RateLimited{RetryAfter: parseSeconds(ra)}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("nvd api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out nvdResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("nvd api decode: %w", err)
	}
	return &out, nil
}

// RateLimited signals a 429 so the sync loop can back off without treating it
// as a source failure.
type RateLimited struct{ RetryAfter time.Duration }

func (e *RateLimited) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("nvd rate limited, retry after %s", e.RetryAfter)
	}
	return "nvd rate limited"
}

func parseSeconds(s string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// ToCVE converts an API record into the store shape. Scoring picks the first
// Primary V31, then any V31, then V30, then V2 — mirroring NVD's own precedence.
func ToCVE(w nvdWrapper) (CVE, bool) {
	c := w.CVE
	if c.ID == "" {
		return CVE{}, false
	}
	out := CVE{
		ID:        c.ID,
		Published: nvdTime(c.Published),
		Modified:  nvdTime(c.LastModified),
	}
	for _, d := range c.Descriptions {
		if d.Lang == "en" && d.Value != "" {
			out.Description = d.Value
			break
		}
	}
	out.Title = firstSentence(out.Description)
	score, vector, severity := pickScore(c.Metrics)
	out.Score, out.Vector, out.Severity = score, vector, severity
	seen := map[string]bool{}
	for _, cfg := range c.Configurations {
		for _, node := range cfg.Nodes {
			for _, m := range node.CPEMatch {
				if !m.Vulnerable || m.Criteria == "" || seen[m.Criteria] {
					continue
				}
				seen[m.Criteria] = true
				out.Criteria = append(out.Criteria, m.Criteria)
				if m.VersionStartIncluding != "" || m.VersionStartExcluding != "" ||
					m.VersionEndIncluding != "" || m.VersionEndExcluding != "" {
					out.Ranges = append(out.Ranges, CPERange{
						Criteria:       m.Criteria,
						StartIncluding: m.VersionStartIncluding,
						StartExcluding: m.VersionStartExcluding,
						EndIncluding:   m.VersionEndIncluding,
						EndExcluding:   m.VersionEndExcluding,
					})
				}
			}
		}
	}
	for _, r := range c.References {
		if r.URL != "" {
			out.Refs = append(out.Refs, r.URL)
		}
	}
	return out, true
}

// pickScore mirrors NVD precedence: Primary V31 > any V31 > V30 > V2.
func pickScore(metrics map[string]json.RawMessage) (score *float64, vector, severity string) {
	if metrics == nil {
		return nil, "", "UNKNOWN"
	}
	primary, anyHit := pickFromMetricList(metrics["cvssMetricV31"], "Primary")
	if anyHit {
		return primary.score, primary.vector, primary.severity
	}
	primary, anyHit = pickFromMetricList(metrics["cvssMetricV30"], "Primary")
	if anyHit {
		return primary.score, primary.vector, primary.severity
	}
	primary, anyHit = pickFromMetricList(metrics["cvssMetricV2"], "Primary")
	if anyHit {
		return primary.score, primary.vector, primary.severity
	}
	return nil, "", "UNKNOWN"
}

type picked struct {
	score    *float64
	vector   string
	severity string
}

func pickFromMetricList(raw json.RawMessage, wantType string) (picked, bool) {
	if len(raw) == 0 {
		return picked{}, false
	}
	var list []nvdMetric
	if json.Unmarshal(raw, &list) != nil {
		return picked{}, false
	}
	var fallback *nvdMetric
	for i := range list {
		m := list[i]
		if m.CVSSData.BaseScore == 0 && m.CVSSData.VectorString == "" {
			continue
		}
		if m.Type == wantType {
			return toPicked(m), true
		}
		if fallback == nil {
			fallback = &list[i]
		}
	}
	if fallback != nil {
		return toPicked(*fallback), true
	}
	return picked{}, false
}

func toPicked(m nvdMetric) picked {
	s := m.CVSSData.BaseScore
	p := picked{score: &s, vector: m.CVSSData.VectorString, severity: m.CVSSData.BaseSeverity}
	if p.severity == "" {
		p.severity = mapScoreToSeverity(m.CVSSData.BaseScore)
	}
	return p
}

// mapScoreToSeverity maps a numeric base score onto the v3 severity ladder.
// Used as fallback when the source doesn't carry a textual severity; the legacy
// v2 term set is intentionally folded onto this same ladder.
func mapScoreToSeverity(score float64) string {
	switch {
	case score >= 9.0:
		return "CRITICAL"
	case score >= 7.0:
		return "HIGH"
	case score >= 4.0:
		return "MEDIUM"
	case score > 0:
		return "LOW"
	default:
		return "UNKNOWN"
	}
}

// firstSentence trims the description to a one-line title: up to the first
// period followed by a space, capped at 160 runes.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 && i < 200 {
		s = s[:i+1]
	}
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 160 {
		return string(r[:157]) + "..."
	}
	return s
}
