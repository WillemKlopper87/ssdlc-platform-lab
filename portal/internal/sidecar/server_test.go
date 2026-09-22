package sidecar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/projects"
	"ssdlc-portal/internal/report"
	"ssdlc-portal/internal/woodpeckerclient"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{
		"o/r": {{Number: 3, Title: "t", State: "open", HeadSHA: "abc", CreatedAt: time.Unix(900, 0)}},
	}}
	st := &metrics.Store{}
	snap := metrics.NewSnapshot()
	snap.Gauge("ssdlc_sidecar_poll_ok", "h", nil, 1)
	st.Set(snap)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(up.Close)
	return NewServer(&APIDeps{
		Token: "0123456789abcdef0123", Metrics: st, Gitea: g, Woodpecker: fakeWP{},
		Health: &HealthChecker{
			HTTP: http.DefaultClient, GiteaURL: up.URL, WoodpeckerURL: up.URL, Now: time.Now,
			Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) { return nil, nil },
		},
		Now: func() time.Time { return time.Unix(2000, 0) },
	})
}

func do(h http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestServerOpenEndpoints(t *testing.T) {
	h := testServer(t)
	if rec := do(h, "/healthz", ""); rec.Code != 200 {
		t.Errorf("/healthz = %d", rec.Code)
	}
	rec := do(h, "/metrics", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ssdlc_sidecar_poll_ok 1") {
		t.Errorf("/metrics = %d %q", rec.Code, rec.Body.String())
	}
}

func TestServerAPIRequiresBearerToken(t *testing.T) {
	h := testServer(t)
	for _, path := range []string{"/api/v1/reports/o/r/3", "/api/v1/health"} {
		if rec := do(h, path, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without token = %d", path, rec.Code)
		}
		if rec := do(h, path, "wrong-token-wrong-token"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with wrong token = %d", path, rec.Code)
		}
	}
}

func TestServerReportEndpoint(t *testing.T) {
	h := testServer(t)
	rec := do(h, "/api/v1/reports/o/r/3", "0123456789abcdef0123")
	if rec.Code != 200 {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Repo         string `json:"repo"`
		Number       int    `json:"number"`
		Gate         string `json:"gate"`
		MergeBlocked bool   `json:"merge_blocked"`
		Findings     []struct {
			RuleID string `json:"rule_id"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Repo != "o/r" || got.Number != 3 || got.Gate != "failure" || !got.MergeBlocked || len(got.Findings) != 1 {
		t.Errorf("report = %+v", got)
	}
}

func TestServerReportEndpointRejectsBadNumberAndMissingPR(t *testing.T) {
	h := testServer(t)
	if rec := do(h, "/api/v1/reports/o/r/abc", "0123456789abcdef0123"); rec.Code != http.StatusBadRequest {
		t.Errorf("non-numeric PR = %d", rec.Code)
	}
	if rec := do(h, "/api/v1/reports/o/r/99", "0123456789abcdef0123"); rec.Code != http.StatusBadGateway {
		t.Errorf("unknown PR = %d", rec.Code)
	}
}

func TestServerHealthEndpoint(t *testing.T) {
	h := testServer(t)
	rec := do(h, "/api/v1/health", "0123456789abcdef0123")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var res []HealthResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || len(res) != 3 {
		t.Errorf("health = %v %v", res, err)
	}
}

func TestProjectsEndpoint(t *testing.T) {
	now := time.Unix(3000, 0)
	d := &APIDeps{Token: "s3cret", Now: func() time.Time { return now }, Metrics: &metrics.Store{},
		Latest: func() Latest {
			return Latest{
				GeneratedAt: now, PollOK: true, Repos: []string{"ssdlc/pilot-app"},
				Reports: []report.Report{{Repo: "ssdlc/pilot-app", MergeBlocked: true,
					Summary: report.Summary{Critical: 1}}},
			}
		}}
	srv := NewServer(d)

	if rec := do(srv, "/api/v1/projects", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status %d", rec.Code)
	}
	rec := do(srv, "/api/v1/projects", "s3cret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		PollOK   bool               `json:"poll_ok"`
		Projects []projects.Project `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.PollOK || len(body.Projects) != 1 || body.Projects[0].Grade != "C" || body.Projects[0].BlockedPRs != 1 {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}

	d.Latest = func() Latest { return Latest{} }
	if rec := do(NewServer(d), "/api/v1/projects", "s3cret"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("before the first poll want 503, got %d", rec.Code)
	}
	d.Latest = nil
	if rec := do(NewServer(d), "/api/v1/projects", "s3cret"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil Latest want 503, got %d", rec.Code)
	}
}
