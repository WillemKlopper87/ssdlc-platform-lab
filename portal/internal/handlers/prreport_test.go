// portal/internal/handlers/prreport_test.go
package handlers

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
)

func TestPRReport_RendersFindings(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/gateadmin/gate-demo/pulls/12":
			w.Write([]byte(`{"number":12,"title":"Add feature X","head":{"sha":"abc123"}}`))
		case "/api/v1/repos/gateadmin/gate-demo":
			w.Write([]byte(`{"id":1}`))
		default:
			t.Fatalf("unexpected gitea path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()

	logLine := base64.StdEncoding.EncodeToString([]byte(
		"  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- test finding\n" +
			"policy-eval: 1 finding(s) normalized -- critical=1 high=0 medium=0 low=0\n"))
	fakeWoodpecker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			w.Write([]byte(`[{"number":5,"status":"failure","commit":"abc123","event":"pr"}]`))
		case strings.HasSuffix(r.URL.Path, "/pipelines/5"):
			w.Write([]byte(`{"workflows":[{"children":[{"id":59,"name":"policy-eval-findings","state":"failure"}]}]}`))
		case strings.HasSuffix(r.URL.Path, "/logs/5/59"):
			w.Write([]byte(`[{"data":"` + logLine + `"}]`))
		default:
			t.Fatalf("unexpected woodpecker path %s", r.URL.Path)
		}
	}))
	defer fakeWoodpecker.Close()

	handler := PRReport(fakeGitea.URL, fakeWoodpecker.URL, "wp-token")

	req := httptest.NewRequest(http.MethodGet, "/pr/gateadmin/gate-demo/12", nil)
	req.SetPathValue("owner", "gateadmin")
	req.SetPathValue("repo", "gate-demo")
	req.SetPathValue("number", "12")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "fake-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "gitleaks/aws-access-token") || !strings.Contains(body, "config.py:5") {
		t.Errorf("PR report did not render the finding; body:\n%s", body)
	}
}

func TestPRReport_GroupsByToolAndShowsBaselinedAndExcepted(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/gateadmin/gate-demo/pulls/12":
			w.Write([]byte(`{"number":12,"title":"Add feature X","head":{"sha":"abc123"}}`))
		case "/api/v1/repos/gateadmin/gate-demo":
			w.Write([]byte(`{"id":1}`))
		default:
			t.Fatalf("unexpected gitea path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()

	logLine := base64.StdEncoding.EncodeToString([]byte(
		"  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- test finding\n" +
			"  FAIL  HIGH [semgrep/sql-injection] app.py:42 -- tainted input\n" +
			"  BASELINE  [semgrep/subprocess-shell-true] service.py:118 -- shell=True\n" +
			"  EXCEPTION  [trivy/CVE-2024-6221] requirements.txt:? -- medium, logged\n" +
			"policy-eval: 4 finding(s) normalized -- critical=1 high=1 medium=1 low=0\n"))
	fakeWoodpecker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			w.Write([]byte(`[{"number":5,"status":"failure","commit":"abc123","event":"pr"}]`))
		case strings.HasSuffix(r.URL.Path, "/pipelines/5"):
			w.Write([]byte(`{"workflows":[{"children":[{"id":59,"name":"policy-eval-findings","state":"failure"}]}]}`))
		case strings.HasSuffix(r.URL.Path, "/logs/5/59"):
			w.Write([]byte(`[{"data":"` + logLine + `"}]`))
		default:
			t.Fatalf("unexpected woodpecker path %s", r.URL.Path)
		}
	}))
	defer fakeWoodpecker.Close()

	handler := PRReport(fakeGitea.URL, fakeWoodpecker.URL, "wp-token")
	req := httptest.NewRequest(http.MethodGet, "/pr/gateadmin/gate-demo/12", nil)
	req.SetPathValue("owner", "gateadmin")
	req.SetPathValue("repo", "gate-demo")
	req.SetPathValue("number", "12")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "fake-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Merging is blocked") {
		t.Error("expected the merge-blocked banner since a new Critical/High finding is present")
	}
	if !strings.Contains(body, "gitleaks") || !strings.Contains(body, "semgrep") || !strings.Contains(body, "trivy") {
		t.Errorf("expected tool group headings for gitleaks/semgrep/trivy; body:\n%s", body)
	}
	if !strings.Contains(body, "Baselined") {
		t.Error("expected a Baselined section for the BASELINE-suppressed finding")
	}
	if !strings.Contains(body, "approved exception") {
		t.Error("expected an exception section for the EXCEPTION-suppressed finding")
	}
	if !strings.Contains(body, "Request exception") {
		t.Error("expected a Request exception link on the blocking findings")
	}
}

func TestPRReport_NoMatchingPipelineIsNotAnError(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/gateadmin/gate-demo/pulls/12":
			w.Write([]byte(`{"number":12,"title":"Add feature X","head":{"sha":"zzz999"}}`))
		case "/api/v1/repos/gateadmin/gate-demo":
			w.Write([]byte(`{"id":1}`))
		default:
			t.Fatalf("unexpected gitea path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()

	fakeWoodpecker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"number":5,"status":"failure","commit":"abc123","event":"pr"}]`))
	}))
	defer fakeWoodpecker.Close()

	handler := PRReport(fakeGitea.URL, fakeWoodpecker.URL, "wp-token")
	req := httptest.NewRequest(http.MethodGet, "/pr/gateadmin/gate-demo/12", nil)
	req.SetPathValue("owner", "gateadmin")
	req.SetPathValue("repo", "gate-demo")
	req.SetPathValue("number", "12")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "fake-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "No findings") {
		t.Error("expected the no-findings empty state when no pipeline matches the PR's head SHA")
	}
}
