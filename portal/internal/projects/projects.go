// Package projects folds the poller's per-pull-request reports into one row
// per repository: open and blocked PRs, severity counts, grade. Counts cover
// findings on open pull requests only; baseline debt is added by a later step.
package projects

import (
	"sort"

	"ssdlc-portal/internal/grade"
	"ssdlc-portal/internal/report"
)

type Project struct {
	Repo             string `json:"repo"`
	OpenPRs          int    `json:"open_prs"`
	BlockedPRs       int    `json:"blocked_prs"`
	Critical         int    `json:"critical"`
	High             int    `json:"high"`
	Medium           int    `json:"medium"`
	Low              int    `json:"low"`
	Score            int    `json:"score"`
	Grade            string `json:"grade"`
	Unavailable      bool   `json:"unavailable"`
	WoodpeckerRepoID int    `json:"woodpecker_repo_id"`
}

// Aggregate returns one Project per name in repos, worst first. failed marks
// repositories whose pull requests could not be listed; a report with
// FindingsUnavailable also marks its repository, so an unread repository is
// never shown as clean.
func Aggregate(repos []string, failed map[string]bool, reports []report.Report) []Project {
	byRepo := make(map[string]*Project, len(repos))
	out := make([]*Project, 0, len(repos))
	for _, name := range repos {
		if _, dup := byRepo[name]; dup {
			continue
		}
		p := &Project{Repo: name, Unavailable: failed[name]}
		byRepo[name] = p
		out = append(out, p)
	}
	for _, r := range reports {
		p := byRepo[r.Repo]
		if p == nil {
			continue
		}
		p.OpenPRs++
		if r.MergeBlocked {
			p.BlockedPRs++
		}
		if r.FindingsUnavailable {
			p.Unavailable = true
		}
		p.Critical += r.Summary.Critical
		p.High += r.Summary.High
		p.Medium += r.Summary.Medium
		p.Low += r.Summary.Low
		if r.WoodpeckerRepoID > 0 {
			p.WoodpeckerRepoID = r.WoodpeckerRepoID
		}
	}
	res := make([]Project, 0, len(out))
	for _, p := range out {
		p.Score = grade.Score(grade.Counts{Critical: p.Critical, High: p.High, Medium: p.Medium, Low: p.Low})
		p.Grade = grade.Letter(p.Score)
		res = append(res, *p)
	}
	sort.SliceStable(res, func(i, j int) bool {
		a, b := res[i], res[j]
		if a.Score != b.Score {
			return a.Score < b.Score
		}
		if a.BlockedPRs != b.BlockedPRs {
			return a.BlockedPRs > b.BlockedPRs
		}
		return a.Repo < b.Repo
	})
	return res
}
