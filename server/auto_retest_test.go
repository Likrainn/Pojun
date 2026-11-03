package server

import "testing"

// v0.4.38 自动复测设置读库失败/未配置时用默认值：开、critical、最多 10。
func TestAutoRetestDefaults(t *testing.T) {
	c := autoRetestFromPG(nil)
	if !c.Enabled || c.Max != defaultAutoRetestMax || len(c.Severities) != 1 || c.Severities[0] != "critical" {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestSplitComma(t *testing.T) {
	cases := map[string]int{
		"critical":              1,
		"critical,high":         2,
		" critical , high ,":    2,
		"":                      0,
		"critical,,high,medium": 3,
	}
	for in, want := range cases {
		if got := len(splitComma(in)); got != want {
			t.Fatalf("splitComma(%q)=%v want %d", in, splitComma(in), want)
		}
	}
}
