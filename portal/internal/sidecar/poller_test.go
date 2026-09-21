package sidecar

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/report"
	"ssdlc-portal/internal/woodpeckerclient"
)

type fakeGitea struct {
	prs      map[string][]giteaclient.PRDetail // key "owner/repo"
	err      error
	failRepo string // listing this repo's PRs fails
}

func (f fakeGitea) GetPullRequest(ctx context.Context, owner, repo string, n int) (giteaclient.PRDetail, error) {
	for _, p := range f.prs[owner+"/"+repo] {
		if p.Number == n {
			return p, nil
		}
	}
	return giteaclient.PRDetail{}, errors.New("not found")
}
func (f fakeGitea) GetCombinedStatus(ctx context.Context, owner, repo, sha string) ([]giteaclient.CommitStatus, error) {
	return []giteaclient.CommitStatus{{State: "failure", Context: "ssdlc/security-gate/pr/x"}}, nil
}
func (f fakeGitea) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]giteaclient.PRDetail, error) {
	if owner+"/"+repo == f.failRepo {
		return nil, errors.New("boom")
	}
	return f.prs[owner+"/"+repo], f.err
}

type fakeWP struct {
	repos   []woodpeckerclient.Repo
	reposEr error
}

func (f fakeWP) LookupRepo(ctx context.Context, owner, repo string) (int, error) { return 7, nil }
func (f fakeWP) ListPipelines(ctx context.Context, id int) ([]woodpeckerclient.Pipeline, error) {
	return []woodpeckerclient.Pipeline{{Number: 5, Commit: "abc", Started: 1000, Finished: 1060}}, nil
}
func (f fakeWP) ListSteps(ctx context.Context, id, n int) ([]woodpeckerclient.Step, error) {
	return []woodpeckerclient.Step{{ID: 59, Name: "policy-eval-findings"}}, nil
}
func (f fakeWP) GetStepLog(ctx context.Context, id, n, s int) (string, error) {
	return "  FAIL  CRITICAL [gitleaks/aws-access-token] c.py:5 -- k\n" +
		"policy-eval: 1 finding(s) normalized -- critical=1 high=0 medium=0 low=0\n", nil
}
func (f fakeWP) ListRepos(ctx context.Context) ([]woodpeckerclient.Repo, error) {
	return f.repos, f.reposEr
}

func newPoller(g fakeGitea, w fakeWP) (*Poller, *metrics.Store) {
	st := &metrics.Store{}
	return &Poller{
		B:     Backends{Gitea: g, Woodpecker: w},
		Org:   "ssdlc",
		Store: st,
		Now:   func() time.Time { return time.Unix(2000, 0) },
		Log:   log.New(io.Discard, "", 0),
	}, st
}

func scrape(st *metrics.Store) string {
	rec := newRecorder()
	st.ServeHTTP(rec, newRequest())
	return rec.Body.String()
}

func TestPollerOncePublishesMetrics(t *testing.T) {
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{
		"ssdlc/pilot-app": {{Number: 3, Title: "t", State: "open", HeadSHA: "abc", CreatedAt: time.Unix(900, 0)}},
	}}
	w := fakeWP{repos: []woodpeckerclient.Repo{
		{ID: 7, FullName: "ssdlc/pilot-app"},
		{ID: 9, FullName: "someone-else/not-ours"}, // outside the org: ignored
	}}
	p, st := newPoller(g, w)
	if err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := scrape(st)
	for _, want := range []string{
		`ssdlc_open_pull_requests{gate="failure",repo="ssdlc/pilot-app"} 1`,
		`ssdlc_pull_requests_blocked{repo="ssdlc/pilot-app"} 1`,
		`ssdlc_open_findings{category="blocking",repo="ssdlc/pilot-app",severity="CRITICAL",tool="gitleaks"} 1`,
		`ssdlc_pr_time_to_verdict_seconds_avg{repo="ssdlc/pilot-app"} 160`,
		`ssdlc_pipeline_duration_seconds_avg{repo="ssdlc/pilot-app"} 60`,
		`ssdlc_sidecar_poll_ok 1`,
		`ssdlc_sidecar_last_poll_timestamp_seconds 2000`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "someone-else") {
		t.Errorf("repos outside the org must be ignored:\n%s", out)
	}
}

func TestPollerRepoListFailureMarksPollNotOK(t *testing.T) {
	p, st := newPoller(fakeGitea{}, fakeWP{reposEr: errors.New("woodpecker down")})
	if err := p.Once(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if out := scrape(st); !strings.Contains(out, "ssdlc_sidecar_poll_ok 0") {
		t.Errorf("out:\n%s", out)
	}
}

func TestPollerOneRepoFailingDoesNotHideTheOthers(t *testing.T) {
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{
		"ssdlc/good": {{Number: 1, HeadSHA: "abc", CreatedAt: time.Unix(900, 0)}},
	}}
	w := fakeWP{repos: []woodpeckerclient.Repo{{ID: 1, FullName: "ssdlc/broken"}, {ID: 2, FullName: "ssdlc/good"}}}
	p, st := newPoller(errOn("ssdlc/broken", g), w)
	if err := p.Once(context.Background()); err != nil {
		t.Fatalf("a single repo failing must not fail the poll: %v", err)
	}
	out := scrape(st)
	if !strings.Contains(out, `repo="ssdlc/good"`) || !strings.Contains(out, "ssdlc_sidecar_poll_ok 0") {
		t.Errorf("want good repo present and poll_ok 0:\n%s", out)
	}
}

// errOn wraps a fakeGitea so listing one repo's PRs fails.
func errOn(repo string, g fakeGitea) fakeGitea {
	g.failRepo = repo
	return g
}

func TestPollerCallsAfterReportHook(t *testing.T) {
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{"ssdlc/r": {{Number: 4, HeadSHA: "abc"}}}}
	w := fakeWP{repos: []woodpeckerclient.Repo{{ID: 1, FullName: "ssdlc/r"}}}
	p, _ := newPoller(g, w)
	var got []int
	p.AfterReport = func(ctx context.Context, r report.Report) { got = append(got, r.Number) }
	if err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 4 {
		t.Errorf("hook calls = %v", got)
	}
}
