package woodpeckerclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLookupRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/repos/lookup/ssdlc/pilot-app" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("auth header = %q", got)
		}
		w.Write([]byte(`{"id":7}`))
	}))
	defer srv.Close()

	id, err := New(srv.URL, "tok").LookupRepo(context.Background(), "ssdlc", "pilot-app")
	if err != nil {
		t.Fatal(err)
	}
	if id != 7 {
		t.Errorf("id = %d, want 7", id)
	}
}

func TestLookupRepo_ZeroIDIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "tok").LookupRepo(context.Background(), "o", "r"); err == nil {
		t.Fatal("expected an error for a response with no id")
	}
}

func TestListRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/repos" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`[{"id":7,"full_name":"ssdlc/pilot-app"},{"id":9,"full_name":"other/x"}]`))
	}))
	defer srv.Close()

	repos, err := New(srv.URL, "tok").ListRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].ID != 7 || repos[0].FullName != "ssdlc/pilot-app" {
		t.Errorf("repos = %+v", repos)
	}
}

func TestListAgents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`[{"name":"agent-1","last_contact":1790000000}]`))
	}))
	defer srv.Close()

	agents, err := New(srv.URL, "tok").ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Name != "agent-1" || !agents[0].LastContact.Equal(time.Unix(1790000000, 0)) {
		t.Errorf("agents = %+v", agents)
	}
}

func TestListPipelines_IncludesTimestamps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"number":5,"status":"success","commit":"abc","event":"pull_request","started":100,"finished":160}]`))
	}))
	defer srv.Close()

	ps, err := New(srv.URL, "tok").ListPipelines(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if ps[0].Started != 100 || ps[0].Finished != 160 {
		t.Errorf("pipeline = %+v", ps[0])
	}
}
