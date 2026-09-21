package handlers

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/shell"
)

func TestPRReport_LinksToGiteaAndWoodpecker(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/ssdlc/pilot-app/pulls/3":
			w.Write([]byte(`{"number":3,"title":"Add login","head":{"sha":"abc123"}}`))
		case "/api/v1/repos/ssdlc/pilot-app/commits/abc123/status":
			w.Write([]byte(`{"statuses":[]}`))
		default:
			t.Fatalf("unexpected gitea path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()
	log := base64.StdEncoding.EncodeToString([]byte("policy-eval: 0 finding(s) normalized -- critical=0 high=0 medium=0 low=0\n"))
	fakeWP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/repos/lookup/ssdlc/pilot-app":
			w.Write([]byte(`{"id":7}`))
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			w.Write([]byte(`[{"number":9,"status":"success","commit":"abc123","event":"pull_request"}]`))
		case strings.HasSuffix(r.URL.Path, "/pipelines/9"):
			w.Write([]byte(`{"workflows":[{"children":[{"id":59,"name":"policy-eval-findings","state":"success"}]}]}`))
		case strings.HasSuffix(r.URL.Path, "/logs/9/59"):
			w.Write([]byte(`[{"data":"` + log + `"}]`))
		default:
			t.Fatalf("unexpected woodpecker path %s", r.URL.Path)
		}
	}))
	defer fakeWP.Close()

	req := httptest.NewRequest(http.MethodGet, "/pr/ssdlc/pilot-app/3", nil)
	req.SetPathValue("owner", "ssdlc")
	req.SetPathValue("repo", "pilot-app")
	req.SetPathValue("number", "3")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "tok")
	ctx = shell.WithContext(ctx, shell.Shell{GiteaURL: "http://192.168.1.28:3500", WoodpeckerURL: "http://192.168.1.28:8000"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	PRReport(fakeGitea.URL, fakeWP.URL, "wp")(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, body)
	}
	for _, want := range []string{
		`href="http://192.168.1.28:3500/ssdlc/pilot-app/pulls/3"`,
		`href="http://192.168.1.28:8000/repos/7/pipeline/9"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing link %s", want)
		}
	}
}

func TestPRReport_NoLinksWithoutPublicURLs(t *testing.T) {
	// The earlier PR report tests run without a shell; the page must simply
	// omit the links rather than emit broken ones.
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := prLinks(shell.FromContext(req.Context()), "o", "r", "3", 0, 0); got.Gitea != "" || got.Woodpecker != "" {
		t.Errorf("links = %+v", got)
	}
}

func TestPRLinks_EscapeAndPartial(t *testing.T) {
	s := shell.Shell{GiteaURL: "http://g", WoodpeckerURL: "http://w"}
	got := prLinks(s, "we ird", "re/po", "3", 7, 0)
	if got.Gitea != "http://g/we%20ird/re%2Fpo/pulls/3" {
		t.Errorf("gitea = %q", got.Gitea)
	}
	if got.Woodpecker != "http://w/repos/7" {
		t.Errorf("without a pipeline number the link points at the repo, got %q", got.Woodpecker)
	}
}
