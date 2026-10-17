package vulnintel

import (
	"encoding/json"
	"testing"
)

func TestToCVEPicksPrimaryV31AndExtractsCriteria(t *testing.T) {
	raw := `{"cve":{
		"id":"CVE-2021-41773",
		"published":"2021-10-05T14:15:00.000",
		"lastModified":"2021-10-08T02:11:00.000",
		"descriptions":[
			{"lang":"es","value":"Spanish text."},
			{"lang":"en","value":"A path traversal vulnerability exists in Apache HTTP Server 2.4.49. An attacker may use it."}
		],
		"metrics":{"cvssMetricV31":[
			{"source":"nvd@nist.gov","type":"Secondary","cvssData":{"baseScore":7.5,"baseSeverity":"HIGH","vectorString":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N"}},
			{"source":"nvd@nist.gov","type":"Primary","cvssData":{"baseScore":9.8,"baseSeverity":"CRITICAL","vectorString":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}}
		]},
		"configurations":[{"nodes":[{"operator":"OR","cpeMatch":[
			{"vulnerable":true,"criteria":"cpe:2.3:a:apache:http_server:2.4.49:*:*:*:*:*:*:*"},
			{"vulnerable":false,"criteria":"cpe:2.3:a:apache:http_server:2.4.50:*:*:*:*:*:*:*"}
		]}]}],
		"references":[{"url":"https://httpd.apache.org/security/vulnerabilities_24.html","source":"mlist","tags":["Patch"]}]
	}}`
	var w nvdWrapper
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}
	c, ok := ToCVE(w)
	if !ok {
		t.Fatal("expected record to convert")
	}
	if c.ID != "CVE-2021-41773" {
		t.Fatalf("id=%s", c.ID)
	}
	if c.Score == nil || *c.Score != 9.8 {
		t.Fatalf("want Primary V31 score 9.8, got %v", c.Score)
	}
	if c.Severity != "CRITICAL" || c.Vector != "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H" {
		t.Fatalf("severity=%s vector=%s", c.Severity, c.Vector)
	}
	// Title should be the first sentence, not the whole description.
	if want := "A path traversal vulnerability exists in Apache HTTP Server 2.4.49."; c.Title != want {
		t.Fatalf("title=%q want %q", c.Title, want)
	}
	// Non-vulnerable CPE entries must be excluded.
	if len(c.Criteria) != 1 || c.Criteria[0] != "cpe:2.3:a:apache:http_server:2.4.49:*:*:*:*:*:*:*" {
		t.Fatalf("criteria=%v", c.Criteria)
	}
	if len(c.Refs) != 1 {
		t.Fatalf("refs=%v", c.Refs)
	}
	if c.Published.IsZero() || c.Modified.IsZero() {
		t.Fatalf("timestamps not parsed: pub=%v mod=%v", c.Published, c.Modified)
	}
}

func TestToCVEFallsBackAcrossMetricVersions(t *testing.T) {
	// No V31: V30 wins. No V30 either: V2 (with textual severity from cvssData).
	raw := `{"cve":{"id":"CVE-2010-0001",
		"descriptions":[{"lang":"en","value":"Old record. More text."}],
		"metrics":{"cvssMetricV2":[
			{"source":"nvd@nist.gov","type":"Secondary","cvssData":{"baseScore":7.5,"vectorString":"AV:N/AC:L/Au:N/C:P/I:P/A:P"}}
		]}}}`
	var w nvdWrapper
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}
	c, ok := ToCVE(w)
	if !ok {
		t.Fatal("expected record to convert")
	}
	if c.Score == nil || *c.Score != 7.5 {
		t.Fatalf("score=%v", c.Score)
	}
	// v2 7.5 lands in HIGH on the folded numeric ladder; no textual severity in input.
	if c.Severity != "HIGH" {
		t.Fatalf("severity=%s", c.Severity)
	}
}

func TestToCVENoMetrics(t *testing.T) {
	raw := `{"cve":{"id":"CVE-2099-0001","descriptions":[{"lang":"en","value":"No scoring. Nothing."}]}}`
	var w nvdWrapper
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}
	c, _ := ToCVE(w)
	if c.Severity != "UNKNOWN" || c.Score != nil {
		t.Fatalf("severity=%s score=%v", c.Severity, c.Score)
	}
}

func TestCPEVendorProduct(t *testing.T) {
	cases := []struct {
		in             string
		vendor, product string
		ok             bool
	}{
		{"cpe:2.3:a:apache:http_server:2.4.49:*:*:*:*:*:*:*", "apache", "http_server", true},
		{"cpe:2.3:o:linux:linux_kernel:5.4:*:*:*:*:*:*:*", "linux", "linux_kernel", true},
		{"cpe:2.3:h:cisco:ios:15\\(1\\):*:*:*:*:*:*:*", "cisco", "ios", true}, // escaped parens in version, plain vendor/product
		{"cpe:/a:legacy:format", "", "", false},                               // CPE 2.2 format rejected
		{"not-a-cpe", "", "", false},
	}
	for _, tc := range cases {
		v, p, ok := cpeVendorProduct(tc.in)
		if ok != tc.ok || (ok && (v != tc.vendor || p != tc.product)) {
			t.Fatalf("cpeVendorProduct(%q) = %q,%q,%v want %q,%q,%v", tc.in, v, p, ok, tc.vendor, tc.product, tc.ok)
		}
	}
}

func TestSplitUnescapedColons(t *testing.T) {
	// A product may legitimately contain an escaped colon: it must survive.
	got := splitUnescapedColons(`cpe:2.3:a:weird\:vendor:prod\:uct:1.0:*`)
	if len(got) != 7 {
		t.Fatalf("parts=%v (len %d)", got, len(got))
	}
	if got[3] != `weird\:vendor` || got[4] != `prod\:uct` {
		t.Fatalf("escaped fields lost: %q %q", got[3], got[4])
	}
}
