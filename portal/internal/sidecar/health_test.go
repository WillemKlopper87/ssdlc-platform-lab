package sidecar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
