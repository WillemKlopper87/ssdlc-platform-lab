package projects

import (
	"testing"

	"ssdlc-portal/internal/report"
)

func rep(repo string, blocked bool, c, h, m, l int, unavailable bool) report.Report {
	return report.Report{
		Repo: repo, MergeBlocked: blocked, FindingsUnavailable: unavailable,
		Summary:          report.Summary{Critical: c, High: h, Medium: m, Low: l},
		WoodpeckerRepoID: 7,
	}
}

func TestAggregate_SumsPerRepoAndGrades(t *testing.T) {
	got := Aggregate(
		[]string{"ssdlc/pilot-app", "ssdlc/billing-api"}, nil,
		[]report.Report{
			rep("ssdlc/pilot-app", true, 0, 1, 2, 0, false),
			rep("ssdlc/pilot-app", false, 0, 0, 0, 1, false),
		})
	if len(got) != 2 {
		t.Fatalf("want 2 projects, got %d", len(got))
	}
	p := got[0]
	if p.Repo != "ssdlc/pilot-app" || p.OpenPRs != 2 || p.BlockedPRs != 1 {
		t.Errorf("pilot-app row wrong: %+v", p)
	}
	if p.High != 1 || p.Medium != 2 || p.Low != 1 || p.Score != 68 || p.Grade != "C" || p.WoodpeckerRepoID != 7 {
		t.Errorf("pilot-app counts/grade wrong: %+v", p)
	}
	q := got[1]
	if q.Repo != "ssdlc/billing-api" || q.OpenPRs != 0 || q.Score != 100 || q.Grade != "A" || q.Unavailable {
		t.Errorf("a repo with no PRs is a clean A, not missing: %+v", q)
	}
}

func TestAggregate_UnavailableIsNeverClean(t *testing.T) {
	got := Aggregate([]string{"o/a", "o/b"}, map[string]bool{"o/a": true},
		[]report.Report{rep("o/b", false, 0, 0, 0, 0, true)})
	for _, p := range got {
		if !p.Unavailable {
			t.Errorf("%s must be marked unavailable: %+v", p.Repo, p)
		}
	}
}

func TestAggregate_WorstFirst(t *testing.T) {
	got := Aggregate([]string{"o/good", "o/bad", "o/mid"}, nil, []report.Report{
		rep("o/bad", true, 1, 1, 0, 0, false),
		rep("o/mid", false, 0, 1, 0, 0, false),
	})
	order := []string{got[0].Repo, got[1].Repo, got[2].Repo}
	want := []string{"o/bad", "o/mid", "o/good"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestAggregate_IgnoresReportsForUnlistedRepos(t *testing.T) {
	got := Aggregate([]string{"o/a"}, nil, []report.Report{rep("o/zzz", true, 5, 0, 0, 0, false)})
	if len(got) != 1 || got[0].OpenPRs != 0 {
		t.Errorf("unlisted repo leaked in: %+v", got)
	}
}
