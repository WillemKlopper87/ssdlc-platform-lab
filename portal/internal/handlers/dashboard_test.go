// portal/internal/handlers/dashboard_test.go
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
)

func TestDashboard_ListsMyPullRequests(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/search":
			w.Write([]byte(`{"data":[{"full_name":"gateadmin/gate-demo"}]}`))
		case "/api/v1/repos/gateadmin/gate-demo/pulls":
			w.Write([]byte(`[{"number":12,"title":"Add feature X","html_url":"http://x","state":"open","user":{"login":"alice"},"head":{"sha":"abc123","ref":"feature-x"},"created_at":"2026-09-01T10:00:00Z"}]`))
		case "/api/v1/repos/gateadmin/gate-demo/commits/abc123/status":
			w.Write([]byte(`{"state":"failure","statuses":[{"status":"failure","context":"ssdlc/security-gate/pr/woodpecker"}]}`))
		case "/api/v1/user":
			w.Write([]byte(`{"login":"gateadmin"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()

	handler := Dashboard(fakeGitea.URL)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "fake-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Add feature X") {
		t.Error("dashboard did not render the PR title")
	}
	if !strings.Contains(body, "Blocked") {
		t.Error("dashboard did not render the Blocked status badge for a failing gate")
	}
}

func TestDashboard_NoPullRequests(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/search":
			w.Write([]byte(`{"data":[]}`))
		case "/api/v1/user":
			w.Write([]byte(`{"login":"gateadmin"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()

	handler := Dashboard(fakeGitea.URL)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "fake-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "No open pull requests") {
		t.Error("dashboard did not render the empty-state message")
	}
}
