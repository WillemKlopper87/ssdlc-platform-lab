package sidecar

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"ssdlc-portal/internal/woodpeckerclient"
)

const agentStaleAfter = 2 * time.Minute

type HealthResult struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Detail    string `json:"detail"`
}

type HealthChecker struct {
	HTTP          *http.Client
	GiteaURL      string
	WoodpeckerURL string
	Agents        func(ctx context.Context) ([]woodpeckerclient.Agent, error)
	Now           func() time.Time
}

func (h *HealthChecker) probe(ctx context.Context, name, url string) HealthResult {
	start := time.Now()
	res := HealthResult{Name: name}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	resp, err := h.HTTP.Do(req)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.OK = resp.StatusCode == http.StatusOK
	res.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
	return res
}

func (h *HealthChecker) agent(ctx context.Context) HealthResult {
	res := HealthResult{Name: "woodpecker-agent"}
	start := time.Now()
	agents, err := h.Agents(ctx)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Detail = "cannot list agents: " + err.Error()
		return res
	}
	now := h.Now()
	for _, a := range agents {
		if now.Sub(a.LastContact) <= agentStaleAfter {
			res.OK = true
			res.Detail = fmt.Sprintf("%s seen %ds ago", a.Name, int(now.Sub(a.LastContact).Seconds()))
			return res
		}
	}
	res.Detail = fmt.Sprintf("no agent in contact within %s (%d registered)", agentStaleAfter, len(agents))
	return res
}

// Run checks Gitea, the Woodpecker server and the Woodpecker agent, in that order.
func (h *HealthChecker) Run(ctx context.Context) []HealthResult {
	return []HealthResult{
		h.probe(ctx, "gitea", h.GiteaURL+"/api/healthz"),
		h.probe(ctx, "woodpecker", h.WoodpeckerURL+"/healthz"),
		h.agent(ctx),
	}
}
