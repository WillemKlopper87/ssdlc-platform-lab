// portal/internal/handlers/prreport.go
package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/findings"
	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/report"
	"ssdlc-portal/internal/shell"
	"ssdlc-portal/internal/woodpeckerclient"
)

var prReportTmpl = template.Must(template.ParseFiles(
	filepath.Join(templateDir, "layout.html"), filepath.Join(templateDir, "prreport.html"),
))

type reportFinding struct {
	findings.Finding
	SeverityClass string
}

// severityClass maps a finding's severity to the ssdlc-sev-* chip class
// suffix (see tokens.css) -- lowercase, matching the mockup's SEV map keys.
func severityClass(sev string) string {
	switch sev {
	case "CRITICAL":
		return "critical"
	case "HIGH":
		return "high"
	case "MEDIUM":
		return "medium"
	default:
		return "low"
	}
}

// toolGroup is one tool's (gitleaks/semgrep/trivy/...) findings within a
// category section, per the design spec's "findings grouped by tool"
// requirement.
type toolGroup struct {
	Tool     string
	Findings []reportFinding
}

// groupByTool buckets findings by their Tool field, preserving a stable
// alphabetical tool order so the page doesn't reshuffle between loads.
func groupByTool(list []reportFinding) []toolGroup {
	byTool := map[string][]reportFinding{}
	var tools []string
	for _, f := range list {
		if _, seen := byTool[f.Tool]; !seen {
			tools = append(tools, f.Tool)
		}
		byTool[f.Tool] = append(byTool[f.Tool], f)
	}
	sort.Strings(tools)
	groups := make([]toolGroup, 0, len(tools))
	for _, t := range tools {
		groups = append(groups, toolGroup{Tool: t, Findings: byTool[t]})
	}
	return groups
}

type prReportData struct {
	ActiveNav string
	// Operator is read by layout.html's sidebar ({{if .Operator}}); every
	// page data type rendered through "layout" needs this field or the
	// template execution fails outright rather than silently hiding the
	// sidebar note. This handler doesn't otherwise need the operator's
	// username, so it's left unset here.
	Operator     string
	Shell        shell.Shell
	RepoFullName string
	PR           struct {
		Number int
		Title  string
	}
	// Summary is the exact normalized tally policy-eval-findings.py
	// reported -- the number the gate decision was made from.
	Summary findings.Summary
	// MergeBlocked is true when at least one new (not baselined, not
	// excepted) Critical/High finding was reported -- the same rule
	// policy/severity.rego's deny rules encode.
	MergeBlocked bool
	// Active is new findings that count toward the gate decision
	// (blocking Critical/High, plus non-blocking Medium warnings),
	// grouped by tool.
	Active []toolGroup
	// Baselined is findings matched against .ssdlc/baseline.json --
	// pre-existing debt, never evaluated for blocking.
	Baselined []toolGroup
	// Excepted is findings covered by an approved, unexpired two-party
	// exception record -- also never evaluated for blocking.
	Excepted []toolGroup
}

// PRReport shows one pull request's findings, sourced from the most
// recent Woodpecker pipeline's policy-eval-findings step log — the exact
// text the gate's pass/fail decision was made from.
func PRReport(giteaBaseURL, woodpeckerBaseURL, woodpeckerToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner := r.PathValue("owner")
		repo := r.PathValue("repo")
		number, err := strconv.Atoi(r.PathValue("number"))
		if err != nil || number < 1 {
			http.Error(w, "pull request number must be a positive integer", http.StatusBadRequest)
			return
		}

		token, _ := auth.TokenFromContext(r.Context())
		gitea := giteaclient.New(giteaBaseURL, token)
		wp := woodpeckerclient.New(woodpeckerBaseURL, woodpeckerToken)

		rep, err := report.Build(r.Context(), gitea, wp, owner, repo, number, time.Now())
		if err != nil {
			http.Error(w, "could not load pull request: "+err.Error(), http.StatusBadGateway)
			return
		}
		if rep.FindingsUnavailable {
			http.Error(w, "findings unavailable: "+strings.Join(rep.Notes, "; "), http.StatusBadGateway)
			return
		}

		var active, baselined, excepted []reportFinding
		for _, f := range rep.Findings {
			rf := reportFinding{
				Finding: findings.Finding{
					Category: findings.Category(f.Category), Severity: f.Severity, Tool: f.Tool,
					RuleID: f.RuleID, Location: f.Location, Description: f.Description,
				},
				SeverityClass: severityClass(f.Severity),
			}
			switch rf.Category {
			case findings.CategoryBaselined:
				baselined = append(baselined, rf)
			case findings.CategoryExcepted:
				excepted = append(excepted, rf)
			default:
				active = append(active, rf)
			}
		}

		data := prReportData{
			ActiveNav:    "dashboard",
			Shell:        shell.FromContext(r.Context()),
			RepoFullName: owner + "/" + repo,
			Summary: findings.Summary{
				Critical: rep.Summary.Critical, High: rep.Summary.High,
				Medium: rep.Summary.Medium, Low: rep.Summary.Low,
			},
			MergeBlocked: rep.MergeBlocked,
			Active:       groupByTool(active),
			Baselined:    groupByTool(baselined),
			Excepted:     groupByTool(excepted),
		}
		data.PR.Number = rep.Number
		data.PR.Title = rep.Title

		if err := prReportTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
