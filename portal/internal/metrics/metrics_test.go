package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSnapshotExposition(t *testing.T) {
	s := NewSnapshot()
	s.Gauge("ssdlc_open_pull_requests", "Open pull requests.", map[string]string{"repo": "o/r", "gate": "failure"}, 2)
	s.Gauge("ssdlc_open_pull_requests", "Open pull requests.", map[string]string{"repo": "o/a", "gate": "success"}, 1)
	s.Gauge("ssdlc_sidecar_poll_ok", "1 when the last poll succeeded.", nil, 1)

	var b strings.Builder
	if _, err := s.WriteTo(&b); err != nil {
		t.Fatal(err)
	}
	want := "# HELP ssdlc_open_pull_requests Open pull requests.\n" +
		"# TYPE ssdlc_open_pull_requests gauge\n" +
		"ssdlc_open_pull_requests{gate=\"failure\",repo=\"o/r\"} 2\n" +
		"ssdlc_open_pull_requests{gate=\"success\",repo=\"o/a\"} 1\n" +
		"# HELP ssdlc_sidecar_poll_ok 1 when the last poll succeeded.\n" +
		"# TYPE ssdlc_sidecar_poll_ok gauge\n" +
		"ssdlc_sidecar_poll_ok 1\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestLabelValuesAreEscaped(t *testing.T) {
	s := NewSnapshot()
	s.Gauge("m", "h", map[string]string{"k": "a\"b\\c\nd"}, 1)
	var b strings.Builder
	s.WriteTo(&b)
	if !strings.Contains(b.String(), `m{k="a\"b\\c\nd"} 1`) {
		t.Errorf("escaping wrong: %q", b.String())
	}
}

func TestStoreServesLatestSnapshot(t *testing.T) {
	var st Store
	rec := httptest.NewRecorder()
	st.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("empty store: %d %q", rec.Code, rec.Body.String())
	}

	s := NewSnapshot()
	s.Gauge("x", "h", nil, 3)
	st.Set(s)
	rec = httptest.NewRecorder()
	st.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "x 3\n") {
		t.Errorf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("content-type = %q", ct)
	}
}
