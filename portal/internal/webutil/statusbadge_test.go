// portal/internal/webutil/statusbadge_test.go
package webutil

import "testing"

func TestStatusBadge(t *testing.T) {
	cases := []struct {
		status, wantLabel, wantClass string
	}{
		{"success", "Passed", "ssdlc-badge-success"},
		{"failure", "Blocked", "ssdlc-badge-critical"},
		{"pending", "Running", "ssdlc-badge-warning"},
		{"unknown-status", "unknown-status", "ssdlc-badge-neutral"},
	}
	for _, c := range cases {
		got := StatusBadge(c.status)
		if got.Label != c.wantLabel || got.CSSClass != c.wantClass {
			t.Errorf("StatusBadge(%q) = {%q,%q}, want {%q,%q}",
				c.status, got.Label, got.CSSClass, c.wantLabel, c.wantClass)
		}
	}
}
