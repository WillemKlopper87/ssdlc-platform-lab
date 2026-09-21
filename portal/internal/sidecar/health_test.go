package sidecar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssdlc-portal/internal/woodpeckerclient"
)

func healthServer(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
}

func TestHealthCheckerAllHealthy(t *testing.T) {
	g, w := healthServer(200), healthServer(200)
	defer g.Close()
	defer w.Close()
	now := time.Unix(10_000, 0)
	h := &HealthChecker{
		HTTP: http.DefaultClient, GiteaURL: g.URL, WoodpeckerURL: w.URL,
		Now: func() time.Time { return now },
		Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) {
			return []woodpeckerclient.Agent{{Name: "a1", LastContact: now.Add(-30 * time.Second)}}, nil
		},
	}
	res := h.Run(context.Background())
	if len(res) != 3 || res[0].Name != "gitea" || res[1].Name != "woodpecker" || res[2].Name != "woodpecker-agent" {
		t.Fatalf("results = %+v", res)
	}
	for _, r := range res {
		if !r.OK {
			t.Errorf("%s not ok: %s", r.Name, r.Detail)
		}
	}
	if res[0].Detail != "HTTP 200" {
		t.Errorf("gitea detail = %q, want %q", res[0].Detail, "HTTP 200")
	}
	if res[1].Detail != "HTTP 200" {
		t.Errorf("woodpecker detail = %q, want %q", res[1].Detail, "HTTP 200")
	}
	if !strings.Contains(res[2].Detail, "a1 seen 30s ago") {
		t.Errorf("agent detail = %q, does not contain %q", res[2].Detail, "a1 seen 30s ago")
	}
}

func TestHealthCheckerReportsFailures(t *testing.T) {
	g, w := healthServer(503), healthServer(200)
	defer g.Close()
	defer w.Close()
	now := time.Unix(10_000, 0)
	h := &HealthChecker{
		HTTP: http.DefaultClient, GiteaURL: g.URL, WoodpeckerURL: w.URL,
		Now: func() time.Time { return now },
		Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) {
			return []woodpeckerclient.Agent{{Name: "a1", LastContact: now.Add(-10 * time.Minute)}}, nil
		},
	}
	res := h.Run(context.Background())
	if res[0].OK {
		t.Error("gitea returned 503: must not be ok")
	}
	if !res[1].OK {
		t.Error("woodpecker returned 200: must be ok")
	}
	if res[2].OK {
		t.Error("agent last seen 10 minutes ago: must not be ok")
	}
	if res[0].Detail != "HTTP 503" {
		t.Errorf("gitea detail = %q, want %q", res[0].Detail, "HTTP 503")
	}
	if !strings.Contains(res[2].Detail, "no agent in contact") {
		t.Errorf("agent detail = %q, does not contain %q", res[2].Detail, "no agent in contact")
	}
	if !strings.Contains(res[2].Detail, "(1 registered)") {
		t.Errorf("agent detail = %q, does not contain %q", res[2].Detail, "(1 registered)")
	}
}

func TestHealthCheckerAgentLookupError(t *testing.T) {
	g, w := healthServer(200), healthServer(200)
	defer g.Close()
	defer w.Close()
	h := &HealthChecker{
		HTTP: http.DefaultClient, GiteaURL: g.URL, WoodpeckerURL: w.URL, Now: time.Now,
		Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) { return nil, errors.New("forbidden") },
	}
	if res := h.Run(context.Background()); res[2].OK || res[2].Detail == "" {
		t.Errorf("agent result = %+v", res[2])
	}
}

func TestHealthCheckerUnreachableServer(t *testing.T) {
	g := healthServer(200)
	g.Close()
	w := healthServer(200)
	defer w.Close()
	now := time.Unix(10_000, 0)
	h := &HealthChecker{
		HTTP: http.DefaultClient, GiteaURL: g.URL, WoodpeckerURL: w.URL,
		Now: func() time.Time { return now },
		Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) {
			return []woodpeckerclient.Agent{{Name: "a1", LastContact: now.Add(-30 * time.Second)}}, nil
		},
	}
	res := h.Run(context.Background())
	if len(res) != 3 {
		t.Fatalf("want 3 results, got %d", len(res))
	}
	if res[0].OK {
		t.Error("gitea (closed server) must not be ok")
	}
	if res[0].Detail == "" {
		t.Error("gitea detail must not be empty for connection error")
	}
	if !res[1].OK {
		t.Error("woodpecker must still be ok")
	}
	if !res[2].OK {
		t.Error("agent must still be ok")
	}
}

func TestHealthCheckerAgentStalenessBoundary(t *testing.T) {
	tests := []struct {
		name     string
		agents   []woodpeckerclient.Agent
		wantOK   bool
		wantInfo string
	}{
		{
			name:     "empty agent list",
			agents:   []woodpeckerclient.Agent{},
			wantOK:   false,
			wantInfo: "(0 registered)",
		},
		{
			name: "agent exactly at boundary (now-2m, <=)",
			agents: []woodpeckerclient.Agent{
				{Name: "boundary", LastContact: time.Unix(10_000, 0).Add(-2 * time.Minute)},
			},
			wantOK:   true,
			wantInfo: "boundary seen 120s ago",
		},
		{
			name: "agent just past boundary (now-2m-1s)",
			agents: []woodpeckerclient.Agent{
				{Name: "stale", LastContact: time.Unix(10_000, 0).Add(-2*time.Minute - 1*time.Second)},
			},
			wantOK:   false,
			wantInfo: "no agent in contact",
		},
		{
			name: "two agents, first stale, second fresh",
			agents: []woodpeckerclient.Agent{
				{Name: "stale", LastContact: time.Unix(10_000, 0).Add(-10 * time.Minute)},
				{Name: "fresh", LastContact: time.Unix(10_000, 0).Add(-5 * time.Second)},
			},
			wantOK:   true,
			wantInfo: "fresh seen 5s ago",
		},
	}
	now := time.Unix(10_000, 0)
	g, w := healthServer(200), healthServer(200)
	defer g.Close()
	defer w.Close()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := tt.agents
			h := &HealthChecker{
				HTTP: http.DefaultClient, GiteaURL: g.URL, WoodpeckerURL: w.URL,
				Now: func() time.Time { return now },
				Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) {
					return agents, nil
				},
			}
			res := h.Run(context.Background())
			if res[2].OK != tt.wantOK {
				t.Errorf("OK = %v, want %v", res[2].OK, tt.wantOK)
			}
			if !strings.Contains(res[2].Detail, tt.wantInfo) {
				t.Errorf("Detail = %q, does not contain %q", res[2].Detail, tt.wantInfo)
			}
		})
	}
}

func TestHealthCheckerOrderAndIndependence(t *testing.T) {
	g, w := healthServer(503), healthServer(200)
	defer g.Close()
	defer w.Close()
	now := time.Unix(10_000, 0)
	h := &HealthChecker{
		HTTP: http.DefaultClient, GiteaURL: g.URL, WoodpeckerURL: w.URL,
		Now: func() time.Time { return now },
		Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) {
			return []woodpeckerclient.Agent{{Name: "a1", LastContact: now.Add(-30 * time.Second)}}, nil
		},
	}
	res := h.Run(context.Background())
	if len(res) != 3 {
		t.Fatalf("want 3 results, got %d", len(res))
	}
	if res[0].Name != "gitea" || res[0].OK {
		t.Errorf("gitea result = %+v, want unhealthy", res[0])
	}
	if res[1].Name != "woodpecker" || !res[1].OK {
		t.Errorf("woodpecker result = %+v, want healthy", res[1])
	}
	if res[2].Name != "woodpecker-agent" || !res[2].OK {
		t.Errorf("agent result = %+v, want healthy", res[2])
	}
}
