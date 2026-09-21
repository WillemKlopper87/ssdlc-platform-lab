package giteaclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const prJSON = `{"number":12,"title":"Add X","html_url":"http://g/o/r/pulls/12","state":"open",
"user":{"login":"dev2"},"head":{"sha":"abc123","ref":"x"},"created_at":"2026-09-01T10:00:00Z"}`

func TestGetPullRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/o/r/pulls/12" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(prJSON))
	}))
	defer srv.Close()

	pr, err := New(srv.URL, "tok").GetPullRequest(context.Background(), "o", "r", 12)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 12 || pr.HeadSHA != "abc123" || pr.Author != "dev2" || pr.State != "open" || pr.CreatedAt.IsZero() {
		t.Errorf("pr = %+v", pr)
	}
}

func TestGetPullRequest_NotFoundIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "tok").GetPullRequest(context.Background(), "o", "r", 1); err == nil {
		t.Fatal("expected an error on 404")
	}
}

func TestListOpenPullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/o/r/pulls" || r.URL.Query().Get("state") != "open" {
			t.Fatalf("unexpected request %s", r.URL.String())
		}
		w.Write([]byte("[" + prJSON + "]"))
	}))
	defer srv.Close()

	prs, err := New(srv.URL, "tok").ListOpenPullRequests(context.Background(), "o", "r")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Number != 12 {
		t.Errorf("prs = %+v", prs)
	}
}

func TestIssueComments(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		if r.Method == http.MethodGet {
			w.Write([]byte(`[{"id":5,"body":"hello","user":{"login":"gate-reporter"}}]`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	ctx := context.Background()

	cs, err := c.ListIssueComments(ctx, "o", "r", 12)
	if err != nil || len(cs) != 1 || cs[0].ID != 5 || cs[0].Author != "gate-reporter" || cs[0].Body != "hello" {
		t.Fatalf("list = %+v, %v", cs, err)
	}
	if gotPath != "/api/v1/repos/o/r/issues/12/comments" {
		t.Errorf("list path = %s", gotPath)
	}

	if err := c.CreateIssueComment(ctx, "o", "r", 12, "new"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/repos/o/r/issues/12/comments" || !strings.Contains(gotBody, `"body":"new"`) {
		t.Errorf("create = %s %s %s", gotMethod, gotPath, gotBody)
	}

	if err := c.EditIssueComment(ctx, "o", "r", 5, "edited"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/v1/repos/o/r/issues/comments/5" || !strings.Contains(gotBody, `"body":"edited"`) {
		t.Errorf("edit = %s %s %s", gotMethod, gotPath, gotBody)
	}
}
