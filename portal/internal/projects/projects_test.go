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
		if p.Grade != "" {
			t.Errorf("%s unavailable must have empty grade, got %q", p.Repo, p.Grade)
		}
	}
	got = Aggregate([]string{"o/clean", "o/a", "o/b"}, map[string]bool{"o/a": true},
		[]report.Report{rep("o/b", false, 0, 0, 0, 0, true)})
	if !got[0].Unavailable || !got[1].Unavailable || got[2].Repo != "o/clean" || got[2].Grade != "A" {
		t.Errorf("unavailable rows must sort before clean: %+v", got)
	}
}

func TestAggregate_UnavailableSortsFirstEvenWithBetterScore(t *testing.T) {
	got := Aggregate([]string{"o/bad", "o/unk"}, map[string]bool{"o/unk": true},
		[]report.Report{rep("o/bad", true, 3, 0, 0, 0, false)})
	if got[0].Repo != "o/unk" {
		t.Errorf("unavailable must come first: %+v", got)
	}
}

func TestAggregate_BlockedDescendingTieBreak(t *testing.T) {
	got := Aggregate([]string{"o/a", "o/b"}, nil, []report.Report{
		rep("o/a", false, 0, 1, 0, 0, false),
		rep("o/b", true, 0, 1, 0, 0, false),
	})
	if got[0].Score != got[1].Score || got[0].Repo != "o/b" {
		t.Errorf("more blocked first on equal score: %+v", got)
	}
}

func TestAggregate_NameAscendingTieBreak(t *testing.T) {
	got := Aggregate([]string{"o/c", "o/a", "o/b"}, nil, nil)
	want := []string{"o/a", "o/b", "o/c"}
	for i := range want {
		if got[i].Repo != want[i] {
			t.Fatalf("order = %+v, want %v", got, want)
		}
	}
}

func TestAggregate_DuplicateRepoNamesOneRow(t *testing.T) {
	got := Aggregate([]string{"o/a", "o/a"}, nil, []report.Report{rep("o/a", true, 0, 1, 0, 0, false)})
	if len(got) != 1 || got[0].OpenPRs != 1 || got[0].BlockedPRs != 1 || got[0].High != 1 {
		t.Errorf("duplicates must yield one row without double counting: %+v", got)
	}
}

func TestAggregate_UnavailableWithoutReportsStillListed(t *testing.T) {
	got := Aggregate([]string{"o/a"}, map[string]bool{"o/a": true}, nil)
	if len(got) != 1 || !got[0].Unavailable || got[0].Grade != "" {
		t.Errorf("failed repo with no reports needs a row: %+v", got)
	}
}

func TestAggregate_UnlistedReportsDoNotAffectListed(t *testing.T) {
	got := Aggregate([]string{"o/a"}, nil, []report.Report{
		rep("o/a", false, 0, 0, 0, 1, false),
		rep("o/zzz", true, 5, 5, 0, 0, false),
	})
	if len(got) != 1 || got[0].OpenPRs != 1 || got[0].BlockedPRs != 0 || got[0].Critical != 0 || got[0].Low != 1 {
		t.Errorf("unlisted report leaked: %+v", got)
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
