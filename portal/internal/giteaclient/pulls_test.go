package giteaclient

import (
	"context"
	"fmt"
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

func TestListIssueComments_Pagination(t *testing.T) {
	var pageRequests []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.URL.Path != "/api/v1/repos/o/r/issues/12/comments" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		page := r.URL.Query().Get("page")
		if page == "" {
			t.Fatal("page query parameter missing")
		}
		limit := r.URL.Query().Get("limit")
		if limit != "50" {
			t.Fatalf("limit query parameter should be 50, got %s", limit)
		}
		var pageNum int
		fmt.Sscanf(page, "%d", &pageNum)
		pageRequests = append(pageRequests, pageNum)

		// Page 1 returns 50 comments, page 2 returns 2 comments
		if pageNum == 1 {
			comments := make([]string, 50)
			for i := 0; i < 50; i++ {
				comments[i] = fmt.Sprintf(`{"id":%d,"body":"comment %d","user":{"login":"u%d"}}`, i+1, i+1, i+1)
			}
			w.Write([]byte("[" + strings.Join(comments, ",") + "]"))
		} else if pageNum == 2 {
			w.Write([]byte(`[{"id":51,"body":"comment 51","user":{"login":"u51"}},{"id":52,"body":"comment 52","user":{"login":"u52"}}]`))
		} else {
			w.Write([]byte("[]"))
		}
	}))
	defer srv.Close()

	cs, err := New(srv.URL, "tok").ListIssueComments(context.Background(), "o", "r", 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 52 {
		t.Errorf("expected 52 comments, got %d", len(cs))
	}
	if cs[0].ID != 1 || cs[50].ID != 51 || cs[51].ID != 52 {
		t.Errorf("comment order or IDs wrong: first=%d, 51st=%d, 52nd=%d", cs[0].ID, cs[50].ID, cs[51].ID)
	}
	// Check that pages 1 and 2 were requested, but not page 3
	if len(pageRequests) != 2 || pageRequests[0] != 1 || pageRequests[1] != 2 {
		t.Errorf("expected requests for pages [1, 2], got %v", pageRequests)
	}
}

func TestListIssueComments_MaxPages(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/api/v1/repos/o/r/issues/12/comments" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		// Always return 50 comments to avoid stopping early (triggers max page limit)
		comments := make([]string, 50)
		for i := 0; i < 50; i++ {
			comments[i] = fmt.Sprintf(`{"id":%d,"body":"c","user":{"login":"u"}}`, i+1)
		}
		w.Write([]byte("[" + strings.Join(comments, ",") + "]"))
	}))
	defer srv.Close()

	cs, err := New(srv.URL, "tok").ListIssueComments(context.Background(), "o", "r", 12)
	if err != nil {
		t.Fatal(err)
	}
	// Hard cap is 20 pages, so should make exactly 20 requests when every page returns 50 items
	if requestCount != 20 {
		t.Errorf("expected exactly 20 page requests (hard cap), got %d", requestCount)
	}
	// 20 pages * 50 comments per page = 1000 comments exactly
	if len(cs) != 1000 {
		t.Errorf("expected 1000 comments (20 pages * 50), got %d", len(cs))
	}
}

func TestGetPullRequestEscapesSegments(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.EscapedPath()
		w.Write([]byte(prJSON))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "tok").GetPullRequest(context.Background(), "a/b", "c d", 1); err != nil {
		t.Fatal(err)
	}
	if want := "/api/v1/repos/a%2Fb/c%20d/pulls/1"; got != want {
		t.Errorf("escaped path = %q, want %q", got, want)
	}
}

func TestGetPullRequestRejectsDotSegments(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	c := New(srv.URL, "tok")
	for _, pair := range [][2]string{{"..", "x"}, {"x", ".."}, {".", "x"}} {
		if _, err := c.GetPullRequest(context.Background(), pair[0], pair[1], 1); err == nil {
			t.Errorf("%v: expected error", pair)
		}
	}
	if called {
		t.Error("no request may be made for dot segments")
	}
}

func TestEscapeSegment(t *testing.T) {
	if s, err := escapeSegment("a b/c"); err != nil || s != "a%20b%2Fc" {
		t.Errorf("got %q, %v", s, err)
	}
	for _, bad := range []string{".", ".."} {
		if _, err := escapeSegment(bad); err == nil {
			t.Errorf("%q should error", bad)
		}
	}
}
