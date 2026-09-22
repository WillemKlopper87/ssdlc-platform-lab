package metrics

import (
	"net/http/httptest"
	"strings"
	"sync"
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

func TestWriteToDoesNotMutateSnapshot(t *testing.T) {
	s := NewSnapshot()
	// Insert samples in NON-sorted label order: z before a
	s.Gauge("m", "help", map[string]string{"repo": "z"}, 1)
	s.Gauge("m", "help", map[string]string{"repo": "a"}, 2)

	// Remember the original order before any WriteTo call
	originalFirstLabels := s.fams["m"].samples[0].labels
	originalSecondLabels := s.fams["m"].samples[1].labels

	// Call WriteTo twice, including concurrently
	var b1, b2 strings.Builder
	if _, err := s.WriteTo(&b1); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var b3, b4 strings.Builder
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.WriteTo(&b3)
	}()
	go func() {
		defer wg.Done()
		s.WriteTo(&b4)
	}()
	wg.Wait()

	if _, err := s.WriteTo(&b2); err != nil {
		t.Fatal(err)
	}

	// Assert output is sorted (a before z) and identical
	want := "# HELP m help\n# TYPE m gauge\nm{repo=\"a\"} 2\nm{repo=\"z\"} 1\n"
	if b1.String() != want {
		t.Errorf("WriteTo output 1 incorrect:\ngot:\n%s\nwant:\n%s", b1.String(), want)
	}
	if b2.String() != want {
		t.Errorf("WriteTo output 2 incorrect:\ngot:\n%s\nwant:\n%s", b2.String(), want)
	}
	if b3.String() != want {
		t.Errorf("WriteTo concurrent output 1 incorrect:\ngot:\n%s\nwant:\n%s", b3.String(), want)
	}
	if b4.String() != want {
		t.Errorf("WriteTo concurrent output 2 incorrect:\ngot:\n%s\nwant:\n%s", b4.String(), want)
	}

	// Assert the snapshot's stored order is unchanged
	if s.fams["m"].samples[0].labels != originalFirstLabels {
		t.Errorf("snapshot mutation: first sample order changed from %q to %q", originalFirstLabels, s.fams["m"].samples[0].labels)
	}
	if s.fams["m"].samples[1].labels != originalSecondLabels {
		t.Errorf("snapshot mutation: second sample order changed from %q to %q", originalSecondLabels, s.fams["m"].samples[1].labels)
	}
}
