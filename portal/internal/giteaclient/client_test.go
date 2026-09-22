package giteaclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListMyPullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/user":
			w.Write([]byte(`{"login":"gateadmin"}`))
		case "/api/v1/repos/search":
			w.Write([]byte(`{"data":[{"full_name":"gateadmin/gate-demo"}]}`))
		case "/api/v1/repos/gateadmin/gate-demo/pulls":
			w.Write([]byte(`[{
				"number": 12, "title": "Add feature X", "html_url": "http://gitea/gateadmin/gate-demo/pulls/12",
				"state": "open", "user": {"login": "alice"},
				"head": {"sha": "abc123", "ref": "feature-x"}, "created_at": "2026-09-01T10:00:00Z"
			}]`))
		default:
			t.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	prs, err := c.ListMyPullRequests(context.Background())
	if err != nil {
		t.Fatalf("ListMyPullRequests: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	pr := prs[0]
	if pr.Number != 12 || pr.Title != "Add feature X" || pr.Author != "alice" ||
		pr.Repo != "gateadmin/gate-demo" || pr.HeadSHA != "abc123" || pr.HeadBranch != "feature-x" {
		t.Errorf("unexpected PR: %+v", pr)
	}
}

func TestGetCombinedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"state":"failure","statuses":[
			{"status":"failure","context":"ssdlc/security-gate/pr/woodpecker"}
		]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	statuses, err := c.GetCombinedStatus(context.Background(), "gateadmin", "gate-demo", "abc123")
	if err != nil {
		t.Fatalf("GetCombinedStatus: %v", err)
	}
	if len(statuses) != 1 || statuses[0].State != "failure" ||
		statuses[0].Context != "ssdlc/security-gate/pr/woodpecker" {
		t.Errorf("unexpected statuses: %+v", statuses)
	}
}

func TestGetFileContent_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	_, sha, err := c.GetFileContent(context.Background(), "gateadmin", "exceptions", "no-such-file.json")
	if err != nil {
		t.Fatalf("expected no error for a missing file, got %v", err)
	}
	if sha != "" {
		t.Errorf("sha = %q, want empty for a missing file", sha)
	}
}

func TestEnsureRepo_CreatesWhenMissing(t *testing.T) {
	var createCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/gateadmin/exceptions":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			createCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	if err := c.EnsureRepo(context.Background(), "gateadmin", "exceptions"); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if !createCalled {
		t.Error("expected EnsureRepo to POST /user/repos when the repo doesn't exist")
	}
}

func TestBaseURL(t *testing.T) {
	c := New("http://example.com", "fake-token")
	if c.BaseURL() != "http://example.com" {
		t.Errorf("BaseURL() = %q, want %q", c.BaseURL(), "http://example.com")
	}
}

func TestRawGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/some/custom/endpoint" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`{"field":"value"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	var out struct {
		Field string `json:"field"`
	}
	status, err := c.RawGet(context.Background(), "/api/v1/some/custom/endpoint", &out)
	if err != nil {
		t.Fatalf("RawGet: %v", err)
	}
	if status != http.StatusOK || out.Field != "value" {
		t.Errorf("status=%d out=%+v, want 200 and field=value", status, out)
	}
}

func TestListDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/gateadmin/exceptions/contents/":
			w.Write([]byte(`[
				{"path": "a1b2c3.json", "type": "file"},
				{"path": "subdir", "type": "dir"}
			]`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	paths, err := c.ListDirectory(context.Background(), "gateadmin", "exceptions", "")
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	if len(paths) != 1 || paths[0] != "a1b2c3.json" {
		t.Errorf("paths = %v, want only the file entry [a1b2c3.json] (dirs excluded)", paths)
	}
}

func TestListDirectory_MissingDirIsEmptyNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	paths, err := c.ListDirectory(context.Background(), "gateadmin", "exceptions", "")
	if err != nil {
		t.Fatalf("expected no error for a missing directory, got %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("paths = %v, want empty", paths)
	}
}
