package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssdlc-portal/internal/projects"
	"ssdlc-portal/internal/shell"
	"ssdlc-portal/internal/sidecarclient"
)

type fakeProjects struct {
	resp sidecarclient.ProjectsResponse
	err  error
}

func (f fakeProjects) Projects(ctx context.Context) (sidecarclient.ProjectsResponse, error) {
	return f.resp, f.err
}

func getProjects(t *testing.T, src ProjectsSource, s shell.Shell) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req = req.WithContext(shell.WithContext(req.Context(), s))
	rec := httptest.NewRecorder()
	Projects(src)(rec, req)
	return rec.Code, rec.Body.String()
}

var sampleShell = shell.Shell{
	Operator: "dev1", Role: shell.RoleDeveloper,
	GiteaURL: "http://192.168.1.28:3500", WoodpeckerURL: "http://192.168.1.28:8000",
}

func sampleResp() sidecarclient.ProjectsResponse {
	return sidecarclient.ProjectsResponse{
		GeneratedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC), PollOK: true,
		Projects: []projects.Project{
			{Repo: "ssdlc/pilot-app", OpenPRs: 3, BlockedPRs: 2, Critical: 1, High: 1, Score: 47, Grade: "D", WoodpeckerRepoID: 7},
			{Repo: "ssdlc/billing-api", OpenPRs: 0, Score: 100, Grade: "A"},
		},
	}
}

func TestProjects_RendersRowsGradesAndLinks(t *testing.T) {
	code, body := getProjects(t, fakeProjects{resp: sampleResp()}, sampleShell)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{
		"ssdlc/pilot-app", "ssdlc/billing-api",
		`class="ssdlc-grade ssdlc-grade-D"`, `class="ssdlc-grade ssdlc-grade-A"`,
		`href="http://192.168.1.28:3500/ssdlc/pilot-app"`,
		`href="http://192.168.1.28:8000/repos/7"`,
		"open pull requests only", // the coverage sentence
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in body", want)
		}
	}
	if strings.Contains(body, `href="http://192.168.1.28:8000/repos/0"`) {
		t.Error("no Woodpecker link when the repo id is unknown")
	}
	if strings.Contains(body, "could not be read") || strings.Contains(body, "may be stale") {
		t.Error("no warnings expected for a healthy poll")
	}
}

func TestProjects_UnreadableRepoNeverShowsAGrade(t *testing.T) {
	resp := sampleResp()
	resp.Projects = []projects.Project{{Repo: "ssdlc/billing-api", Score: 100, Grade: "", Unavailable: true}}
	_, body := getProjects(t, fakeProjects{resp: resp}, sampleShell)
	if !strings.Contains(body, "could not be read") || !strings.Contains(body, "Unknown") {
		t.Error("an unreadable repo must say so and show Unknown")
	}
	if strings.Contains(body, "ssdlc-grade-A") {
		t.Error("an unreadable repo must never show a letter")
	}
}

func TestProjects_StalePollWarns(t *testing.T) {
	resp := sampleResp()
	resp.PollOK = false
	_, body := getProjects(t, fakeProjects{resp: resp}, sampleShell)
	if !strings.Contains(body, "may be stale") {
		t.Error("poll_ok=false must show a visible warning")
	}
}

func TestProjects_NotConfigured(t *testing.T) {
	code, body := getProjects(t, nil, sampleShell)
	if code != http.StatusOK || !strings.Contains(body, "not available on this server") {
		t.Errorf("status=%d, want 200 with an explanation; body has notice: %v", code, strings.Contains(body, "not available"))
	}
}

func TestProjects_SidecarFailureIsExplainedNotRaw(t *testing.T) {
	code, body := getProjects(t, fakeProjects{err: errors.New("sidecar: status 503: no poll has completed yet")}, sampleShell)
	if code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", code)
	}
	if !strings.Contains(body, "reporting service") || strings.Contains(body, "status 503") {
		t.Error("show a friendly message; do not leak the raw error into the page")
	}
}

func TestProjects_EscapesRepoNamesInLinks(t *testing.T) {
	resp := sampleResp()
	resp.Projects = []projects.Project{{Repo: `o/a"b<c`, Score: 100, Grade: "A"}}
	_, body := getProjects(t, fakeProjects{resp: resp}, sampleShell)
	if strings.Contains(body, `a"b<c`) {
		t.Error("repo name must be escaped in the page")
	}
	if !strings.Contains(body, "/o/a%22b%3Cc") {
		t.Errorf("link path segments must be percent-escaped; body:\n%s", body)
	}
}
