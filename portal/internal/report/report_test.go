package report

import (
	"context"
	"errors"
	"testing"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/woodpeckerclient"
)

type fakeGitea struct {
	pr       giteaclient.PRDetail
	prErr    error
	statuses []giteaclient.CommitStatus
	stErr    error
}

func (f fakeGitea) GetPullRequest(ctx context.Context, owner, repo string, number int) (giteaclient.PRDetail, error) {
	return f.pr, f.prErr
}
func (f fakeGitea) GetCombinedStatus(ctx context.Context, owner, repo, sha string) ([]giteaclient.CommitStatus, error) {
	return f.statuses, f.stErr
}

type fakeWP struct {
	repoID    int
	lookupErr error
	pipelines []woodpeckerclient.Pipeline
	steps     []woodpeckerclient.Step
	log       string

	pipelinesErr, stepsErr, logErr error
}

func (f fakeWP) LookupRepo(ctx context.Context, owner, repo string) (int, error) {
	return f.repoID, f.lookupErr
}
func (f fakeWP) ListPipelines(ctx context.Context, repoID int) ([]woodpeckerclient.Pipeline, error) {
	return f.pipelines, f.pipelinesErr
}
func (f fakeWP) ListSteps(ctx context.Context, repoID, n int) ([]woodpeckerclient.Step, error) {
	return f.steps, f.stepsErr
}
func (f fakeWP) GetStepLog(ctx context.Context, repoID, n, stepID int) (string, error) {
	return f.log, f.logErr
}

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func basePR() giteaclient.PRDetail {
	return giteaclient.PRDetail{Number: 12, Title: "Add X", State: "open", HeadSHA: "abc", Author: "dev2", HTMLURL: "http://g/o/r/pulls/12"}
}

const blockedLog = "  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- test finding\n" +
	"  WARN  MEDIUM [semgrep/weak-hash] app.py:12 -- md5\n" +
	"  BASELINE  [semgrep/old] old.py:1 -- legacy\n" +
	"policy-eval: 2 finding(s) normalized -- critical=1 high=0 medium=1 low=0\n"

func TestBuild_BlockedPRWithFindings(t *testing.T) {
	g := fakeGitea{pr: basePR(), statuses: []giteaclient.CommitStatus{{State: "failure", Context: "ssdlc/security-gate/pr/x"}}}
	w := fakeWP{
		repoID:    7,
		pipelines: []woodpeckerclient.Pipeline{{Number: 5, Commit: "abc", Started: 100, Finished: 160}},
		steps:     []woodpeckerclient.Step{{ID: 59, Name: "policy-eval-findings"}},
		log:       blockedLog,
	}
	r, err := Build(context.Background(), g, w, "o", "r", 12, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Gate != "failure" || !r.MergeBlocked {
		t.Errorf("gate=%q blocked=%v", r.Gate, r.MergeBlocked)
	}
	if r.Summary.Critical != 1 || r.Summary.Medium != 1 {
		t.Errorf("summary = %+v", r.Summary)
	}
	if len(r.Findings) != 3 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	// blocking Critical first, then the warning, then the baselined one (no severity)
	if r.Findings[0].Severity != "CRITICAL" || r.Findings[1].Severity != "MEDIUM" || r.Findings[2].Category != "baselined" {
		t.Errorf("order = %+v", r.Findings)
	}
	if r.PipelineNumber != 5 || r.PipelineStarted != 100 || r.PipelineFinished != 160 {
		t.Errorf("pipeline fields = %d %d %d", r.PipelineNumber, r.PipelineStarted, r.PipelineFinished)
	}
	if r.Repo != "o/r" || r.HeadSHA != "abc" || r.GeneratedAt != now {
		t.Errorf("identity = %+v", r)
	}
}

func TestBuild_NoPipelineForHeadCommitIsNotAnError(t *testing.T) {
	g := fakeGitea{pr: basePR()}
	w := fakeWP{repoID: 7, pipelines: []woodpeckerclient.Pipeline{{Number: 5, Commit: "other"}}}
	r, err := Build(context.Background(), g, w, "o", "r", 12, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Gate != "none" || r.MergeBlocked || len(r.Findings) != 0 {
		t.Errorf("report = %+v", r)
	}
	if len(r.Notes) == 0 {
		t.Error("expected a note explaining that no pipeline was found")
	}
}

func TestBuild_WoodpeckerLookupFailureBecomesANote(t *testing.T) {
	g := fakeGitea{pr: basePR()}
	w := fakeWP{lookupErr: errors.New("boom")}
	r, err := Build(context.Background(), g, w, "o", "r", 12, now)
	if err != nil {
		t.Fatalf("a Woodpecker failure must not fail the report: %v", err)
	}
	if len(r.Notes) == 0 || r.Findings == nil {
		t.Errorf("report = %+v", r)
	}
	if !r.FindingsUnavailable {
		t.Error("FindingsUnavailable must be true when the repo lookup fails")
	}
}

func TestBuild_FindingsUnavailableFlag(t *testing.T) {
	pl := []woodpeckerclient.Pipeline{{Number: 5, Commit: "abc"}}
	st := []woodpeckerclient.Step{{ID: 59, Name: "policy-eval-findings"}}
	boom := errors.New("boom")
	cases := []struct {
		name string
		w    fakeWP
		want bool
	}{
		{"pipelines failure", fakeWP{repoID: 7, pipelinesErr: boom}, true},
		{"steps failure", fakeWP{repoID: 7, pipelines: pl, stepsErr: boom}, true},
		{"log failure", fakeWP{repoID: 7, pipelines: pl, steps: st, logErr: boom}, true},
		{"unparseable log", fakeWP{repoID: 7, pipelines: pl, steps: st, log: "policy-eval: 1 finding(s) normalized -- critical=99999999999999999999 high=0 medium=0 low=0\n"}, true},
		{"no pipeline for commit", fakeWP{repoID: 7, pipelines: []woodpeckerclient.Pipeline{{Number: 5, Commit: "other"}}}, false},
		{"blocked normal run", fakeWP{repoID: 7, pipelines: pl, steps: st, log: blockedLog}, false},
	}
	for _, c := range cases {
		r, err := Build(context.Background(), fakeGitea{pr: basePR()}, c.w, "o", "r", 12, now)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if r.FindingsUnavailable != c.want {
			t.Errorf("%s: FindingsUnavailable = %v, want %v (notes %v)", c.name, r.FindingsUnavailable, c.want, r.Notes)
		}
	}
}

func TestBuild_PRLookupFailureIsAnError(t *testing.T) {
	g := fakeGitea{prErr: errors.New("gone")}
	if _, err := Build(context.Background(), g, fakeWP{}, "o", "r", 12, now); err == nil {
		t.Fatal("expected an error when the pull request cannot be loaded")
	}
}

func TestGateState(t *testing.T) {
	gate := func(state string) giteaclient.CommitStatus {
		return giteaclient.CommitStatus{State: state, Context: "ssdlc/security-gate/pr/x"}
	}
	other := giteaclient.CommitStatus{State: "failure", Context: "ci/other"}
	cases := []struct {
		name string
		in   []giteaclient.CommitStatus
		want string
	}{
		{"empty", nil, "none"},
		{"unrelated context ignored", []giteaclient.CommitStatus{other}, "none"},
		{"success", []giteaclient.CommitStatus{gate("success")}, "success"},
		{"pending beats success", []giteaclient.CommitStatus{gate("success"), gate("pending")}, "pending"},
		{"failure beats all", []giteaclient.CommitStatus{gate("success"), gate("pending"), gate("failure")}, "failure"},
		{"error counts as failure", []giteaclient.CommitStatus{gate("error")}, "failure"},
	}
	for _, c := range cases {
		if got := GateState(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBuild_ExposesWoodpeckerRepoID(t *testing.T) {
	g := fakeGitea{pr: basePR()}
	w := fakeWP{repoID: 42, pipelines: []woodpeckerclient.Pipeline{{Number: 5, Commit: "abc"}}}
	r, err := Build(context.Background(), g, w, "o", "r", 12, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.WoodpeckerRepoID != 42 || r.PipelineNumber != 5 {
		t.Errorf("repo id = %d pipeline = %d", r.WoodpeckerRepoID, r.PipelineNumber)
	}
	failed, _ := Build(context.Background(), fakeGitea{pr: basePR()}, fakeWP{lookupErr: errors.New("x")}, "o", "r", 12, now)
	if failed.WoodpeckerRepoID != 0 {
		t.Errorf("no repo id when the lookup fails, got %d", failed.WoodpeckerRepoID)
	}
}
