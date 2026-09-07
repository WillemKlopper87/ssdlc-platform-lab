// portal/internal/handlers/onboarding_test.go
package handlers

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
