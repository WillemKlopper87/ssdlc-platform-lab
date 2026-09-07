// portal/internal/handlers/onboarding_test.go
package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/giteaclient"
)

func TestOnboardingStream_StreamsScriptOutputAsSSE(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-onboard.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"step 1 done\"\necho \"step 2 done\"\n"), 0o755)

	handler := OnboardingStream(script, nil)

	form := strings.NewReader("owner=gateadmin&repo=gate-demo")
	req := httptest.NewRequest(http.MethodPost, "/onboarding/start", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	var gotLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			gotLines = append(gotLines, strings.TrimPrefix(line, "data: "))
		}
	}
	if len(gotLines) < 2 || gotLines[0] != "step 1 done" || gotLines[1] != "step 2 done" {
		t.Errorf("streamed lines = %v, want [\"step 1 done\" \"step 2 done\"]", gotLines)
	}
}

func TestOnboardingStream_RejectsMissingParams(t *testing.T) {
	handler := OnboardingStream("/bin/true", nil)
	req := httptest.NewRequest(http.MethodPost, "/onboarding/start", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for missing owner/repo", rec.Code, http.StatusBadRequest)
	}
}

func TestOnboardingStream_InheritsParentEnvironment(t *testing.T) {
	// Regression test for the controller ruling: the subprocess must see
	// PATH (and everything else the parent process has), not just the
	// extra vars passed in. A shell script that depends on PATH to find
	// "echo" (or any external command) would fail silently otherwise on
	// some platforms; here we assert more directly by having the fake
	// script print an inherited environment variable that OnboardingStream
	// did NOT explicitly pass through its extraEnv argument.
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-onboard.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"HOME_SEEN=$ONBOARD_TEST_INHERITED_VAR\"\n"), 0o755)

	os.Setenv("ONBOARD_TEST_INHERITED_VAR", "inherited-value-123")
	defer os.Unsetenv("ONBOARD_TEST_INHERITED_VAR")

	handler := OnboardingStream(script, []string{"EXTRA_ONLY_VAR=extra-value"})

	form := strings.NewReader("owner=gateadmin&repo=gate-demo")
	req := httptest.NewRequest(http.MethodPost, "/onboarding/start", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !strings.Contains(rec.Body.String(), "inherited-value-123") {
		t.Errorf("subprocess did not see the parent's environment; body:\n%s", rec.Body.String())
	}
}

func TestOnboardingStream_RejectsMalformedOwnerOrRepo(t *testing.T) {
	// Point at a script that would fail loudly (nonexistent path) so that
	// if validation is accidentally skipped, the test fails via "script
	// executed" evidence rather than silently passing.
	handler := OnboardingStream(filepath.Join(t.TempDir(), "does-not-exist.sh"), nil)

	cases := []struct{ owner, repo string }{
		{"../evil", "repo"},
		{"owner", "foo/bar"},
		{"owner", ".."},
		{"", "repo"}, // still must be BadRequest, not reach the regex path oddly
	}
	for _, c := range cases {
		form := strings.NewReader("owner=" + c.owner + "&repo=" + c.repo)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/start", form)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("owner=%q repo=%q: status = %d, want %d", c.owner, c.repo, rec.Code, http.StatusBadRequest)
		}
		if strings.Contains(rec.Body.String(), "data:") {
			t.Errorf("owner=%q repo=%q: subprocess appears to have run; body:\n%s", c.owner, c.repo, rec.Body.String())
		}
	}
}

func TestOnboardingStream_AcceptsWellFormedOwnerAndRepo(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-onboard.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"ok $1 $2\"\n"), 0o755)

	handler := OnboardingStream(script, nil)

	form := strings.NewReader("owner=gate-admin.1&repo=gate_demo-2")
	req := httptest.NewRequest(http.MethodPost, "/onboarding/start", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ok gate-admin.1 gate_demo-2") {
		t.Errorf("subprocess did not run with the validated owner/repo args; body:\n%s", rec.Body.String())
	}
}

// fakeTeamGitea returns an httptest.Server implementing just enough of the
// Gitea API (GET /api/v1/user/teams) for IsOnTeam, so RequireTeam can be
// tested without a live Gitea instance.
func fakeTeamGitea(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/teams" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newRequireTeamRequest(token string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	if token != "" {
		ctx := context.WithValue(req.Context(), auth.ContextKeyToken, token)
		req = req.WithContext(ctx)
	}
	return httptest.NewRecorder(), req
}

func TestRequireTeam_MemberPassesThrough(t *testing.T) {
	srv := fakeTeamGitea(t, `[{"name":"approvers","organization":{"username":"acme"}}]`)
	gitea := giteaclient.New(srv.URL, "")

	called := false
	next := func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusOK) }

	handler := RequireTeam(gitea, "acme", "approvers", next)
	rec, req := newRequireTeamRequest("caller-token")
	handler(rec, req)

	if !called {
		t.Error("next was not called for a caller on the required team")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireTeam_NonMemberIsForbidden(t *testing.T) {
	srv := fakeTeamGitea(t, `[{"name":"engineers","organization":{"username":"acme"}}]`)
	gitea := giteaclient.New(srv.URL, "")

	called := false
	next := func(w http.ResponseWriter, r *http.Request) { called = true }

	handler := RequireTeam(gitea, "acme", "approvers", next)
	rec, req := newRequireTeamRequest("caller-token")
	handler(rec, req)

	if called {
		t.Error("next was called for a caller NOT on the required team")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireTeam_APIErrorFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	gitea := giteaclient.New(srv.URL, "")

	called := false
	next := func(w http.ResponseWriter, r *http.Request) { called = true }

	handler := RequireTeam(gitea, "acme", "approvers", next)
	rec, req := newRequireTeamRequest("caller-token")
	handler(rec, req)

	if called {
		t.Error("next was called despite a Gitea API error checking team membership")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (fail closed on API error)", rec.Code, http.StatusForbidden)
	}
}

func TestRequireTeam_NoTokenIsForbidden(t *testing.T) {
	srv := fakeTeamGitea(t, `[]`)
	gitea := giteaclient.New(srv.URL, "")

	called := false
	next := func(w http.ResponseWriter, r *http.Request) { called = true }

	handler := RequireTeam(gitea, "acme", "approvers", next)
	rec, req := newRequireTeamRequest("")
	handler(rec, req)

	if called {
		t.Error("next was called for a request with no auth token in context")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
