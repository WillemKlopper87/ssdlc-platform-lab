package sidecarclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjects_SendsTokenAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("bad request: %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"generated_at":"2026-09-22T10:00:00Z","poll_ok":true,"projects":[{"repo":"ssdlc/pilot-app","open_prs":2,"grade":"C","score":68}]}`))
	}))
	defer srv.Close()
	got, err := New(srv.URL, "tok").Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.PollOK || len(got.Projects) != 1 || got.Projects[0].Grade != "C" || got.GeneratedAt.IsZero() {
		t.Errorf("decode wrong: %+v", got)
	}
}

func TestProjects_NonOKIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no poll has completed yet", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := New(srv.URL, "tok").Projects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("want an error naming 503, got %v", err)
	}
}
