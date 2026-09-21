// portal/internal/handlers/dashboard_test.go
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/shell"
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

func dashboardLinkBody(t *testing.T, s shell.Shell) string {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/search":
			w.Write([]byte(`{"data":[{"full_name":"o/r"}]}`))
		case "/api/v1/repos/o/r/pulls":
			w.Write([]byte(`[{"number":5,"title":"T","html_url":"http://internal-gitea:3000/o/r/pulls/5","state":"open","user":{"login":"a"},"head":{"sha":"s1","ref":"b"},"created_at":"2026-09-01T10:00:00Z"}]`))
		case "/api/v1/repos/o/r/commits/s1/status":
			w.Write([]byte(`{"state":"success","statuses":[{"status":"success","context":"c"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer fake.Close()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req = req.WithContext(shell.WithContext(req.Context(), s))
	rec := httptest.NewRecorder()
	Dashboard(fake.URL)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestDashboard_LinksUsePublicGiteaURL(t *testing.T) {
	body := dashboardLinkBody(t, shell.Shell{GiteaURL: "http://192.168.1.28:3500"})
	if !strings.Contains(body, `href="http://192.168.1.28:3500/o/r/pulls/5"`) {
		t.Errorf("row link should use the public Gitea URL; body:\n%s", body)
	}
	if strings.Contains(body, "internal-gitea") {
		t.Error("the internal html_url must not be rendered")
	}
	fallback := dashboardLinkBody(t, shell.Shell{})
	if !strings.Contains(fallback, `href="http://internal-gitea:3000/o/r/pulls/5"`) {
		t.Error("without a public URL the row link falls back to html_url")
	}
}
