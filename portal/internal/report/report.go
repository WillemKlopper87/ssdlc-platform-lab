// Package report builds the security report for one pull request from
// Gitea and Woodpecker. It is shared by the sidecar API and the portal's PR
// report page, so there is a single implementation.
package report

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"ssdlc-portal/internal/findings"
	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/woodpeckerclient"
)

const (
	gateContextPrefix = "ssdlc/security-gate"
	findingsStep      = "policy-eval-findings"
)

type Gitea interface {
	GetPullRequest(ctx context.Context, owner, repo string, number int) (giteaclient.PRDetail, error)
	GetCombinedStatus(ctx context.Context, owner, repo, sha string) ([]giteaclient.CommitStatus, error)
}

type Woodpecker interface {
	LookupRepo(ctx context.Context, owner, repo string) (int, error)
	ListPipelines(ctx context.Context, repoID int) ([]woodpeckerclient.Pipeline, error)
	ListSteps(ctx context.Context, repoID, pipelineNumber int) ([]woodpeckerclient.Step, error)
	GetStepLog(ctx context.Context, repoID, pipelineNumber, stepID int) (string, error)
}

type Summary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}

type Finding struct {
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Tool        string `json:"tool"`
	RuleID      string `json:"rule_id"`
	Location    string `json:"location"`
	Description string `json:"description"`
}

type Report struct {
	Repo             string    `json:"repo"`
	Number           int       `json:"number"`
	Title            string    `json:"title"`
	State            string    `json:"state"`
	HTMLURL          string    `json:"html_url"`
	Author           string    `json:"author"`
	HeadSHA          string    `json:"head_sha"`
	CreatedAt        time.Time `json:"created_at"`
	Gate             string    `json:"gate"`
	MergeBlocked     bool      `json:"merge_blocked"`
	Summary          Summary   `json:"summary"`
	Findings         []Finding `json:"findings"`
	PipelineNumber   int       `json:"pipeline_number"`
	PipelineStarted  int64     `json:"pipeline_started"`
	PipelineFinished int64     `json:"pipeline_finished"`
	Notes            []string  `json:"notes"`
	// FindingsUnavailable is true when Woodpecker could not be read, so an
	// empty Findings list must not be taken to mean "clean".
	FindingsUnavailable bool      `json:"findings_unavailable"`
	GeneratedAt         time.Time `json:"generated_at"`
}

// GateState collapses the security-gate commit statuses into one value.
// Worst wins: failure, then pending, then success.
func GateState(statuses []giteaclient.CommitStatus) string {
	state := "none"
	for _, s := range statuses {
		if !strings.HasPrefix(s.Context, gateContextPrefix) {
			continue
		}
		switch s.State {
		case "failure", "error":
			return "failure"
		case "pending":
			state = "pending"
		case "success":
			if state == "none" {
				state = "success"
			}
		}
	}
	return state
}

var severityRank = map[string]int{"CRITICAL": 0, "HIGH": 1, "MEDIUM": 2, "LOW": 3}

func rank(sev string) int {
	if r, ok := severityRank[sev]; ok {
		return r
	}
	return 4
}

// Build assembles the report. Only a failure to load the pull request itself
// is an error; problems reaching Woodpecker are recorded in Notes so a report
// is still produced.
func Build(ctx context.Context, g Gitea, w Woodpecker, owner, repo string, number int, now time.Time) (Report, error) {
	pr, err := g.GetPullRequest(ctx, owner, repo, number)
	if err != nil {
		return Report{}, fmt.Errorf("report: load pull request: %w", err)
	}
	r := Report{
		Repo: owner + "/" + repo, Number: pr.Number, Title: pr.Title, State: pr.State,
		HTMLURL: pr.HTMLURL, Author: pr.Author, HeadSHA: pr.HeadSHA, CreatedAt: pr.CreatedAt,
		Gate: "none", Findings: []Finding{}, Notes: []string{}, GeneratedAt: now,
	}

	if statuses, err := g.GetCombinedStatus(ctx, owner, repo, pr.HeadSHA); err != nil {
		r.Notes = append(r.Notes, "gate status unavailable: "+err.Error())
	} else {
		r.Gate = GateState(statuses)
	}

	repoID, err := w.LookupRepo(ctx, owner, repo)
	if err != nil {
		r.Notes = append(r.Notes, "findings unavailable: "+err.Error())
		r.FindingsUnavailable = true
		return r, nil
	}
	pipelines, err := w.ListPipelines(ctx, repoID)
	if err != nil {
		r.Notes = append(r.Notes, "findings unavailable: "+err.Error())
		r.FindingsUnavailable = true
		return r, nil
	}

	found := false
	for _, p := range pipelines {
		if p.Commit != pr.HeadSHA {
			continue
		}
		found = true
		r.PipelineNumber, r.PipelineStarted, r.PipelineFinished = p.Number, p.Started, p.Finished
		steps, err := w.ListSteps(ctx, repoID, p.Number)
		if err != nil {
			r.Notes = append(r.Notes, "pipeline steps unavailable: "+err.Error())
			r.FindingsUnavailable = true
			break
		}
		for _, s := range steps {
			if s.Name != findingsStep {
				continue
			}
			log, err := w.GetStepLog(ctx, repoID, p.Number, s.ID)
			if err != nil {
				r.Notes = append(r.Notes, "findings log unavailable: "+err.Error())
				r.FindingsUnavailable = true
				continue
			}
			parsed, sum, err := findings.Parse(log)
			if err != nil {
				r.Notes = append(r.Notes, "findings log unreadable: "+err.Error())
				r.FindingsUnavailable = true
				continue
			}
			r.Summary = Summary{Critical: sum.Critical, High: sum.High, Medium: sum.Medium, Low: sum.Low}
			for _, f := range parsed {
				r.Findings = append(r.Findings, Finding{
					Category: string(f.Category), Severity: f.Severity, Tool: f.Tool,
					RuleID: f.RuleID, Location: f.Location, Description: f.Description,
				})
				if f.Category == findings.CategoryBlocking {
					r.MergeBlocked = true
				}
			}
		}
		break // pipelines are newest-first; the first match is the latest run
	}
	if !found {
		r.Notes = append(r.Notes, "no pipeline has run for the head commit yet")
	}

	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if rank(a.Severity) != rank(b.Severity) {
			return rank(a.Severity) < rank(b.Severity)
		}
		if a.Tool != b.Tool {
			return a.Tool < b.Tool
		}
		return a.Location < b.Location
	})
	return r, nil
}
