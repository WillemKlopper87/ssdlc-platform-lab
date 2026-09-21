// portal/internal/handlers/prreport.go
package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"
	"sort"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/findings"
	"ssdlc-portal/internal/giteaclient"
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
		number := r.PathValue("number")

		token, _ := auth.TokenFromContext(r.Context())
		gitea := giteaclient.New(giteaBaseURL, token)

		var pr struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Head   struct {
				SHA string `json:"sha"`
			} `json:"head"`
		}
		if _, err := gitea.RawGet(r.Context(), "/api/v1/repos/"+owner+"/"+repo+"/pulls/"+number, &pr); err != nil {
			http.Error(w, "could not load pull request: "+err.Error(), http.StatusBadGateway)
			return
		}

		var repoInfo struct {
			ID int `json:"id"`
		}
		if _, err := gitea.RawGet(r.Context(), "/api/v1/repos/"+owner+"/"+repo, &repoInfo); err != nil {
			http.Error(w, "could not resolve repo id: "+err.Error(), http.StatusBadGateway)
			return
		}

		wp := woodpeckerclient.New(woodpeckerBaseURL, woodpeckerToken)
		pipelines, err := wp.ListPipelines(r.Context(), repoInfo.ID)
		if err != nil {
			http.Error(w, "could not load pipelines: "+err.Error(), http.StatusBadGateway)
			return
		}

		var active, baselined, excepted []reportFinding
		var summary findings.Summary
		for _, p := range pipelines {
			if p.Commit != pr.Head.SHA {
				continue
			}
			steps, err := wp.ListSteps(r.Context(), repoInfo.ID, p.Number)
			if err != nil {
				continue
			}
			for _, s := range steps {
				if s.Name != "policy-eval-findings" {
					continue
				}
				log, err := wp.GetStepLog(r.Context(), repoInfo.ID, p.Number, s.ID)
				if err != nil {
					continue
				}
				parsed, parsedSummary, err := findings.Parse(log)
				if err != nil {
					continue
				}
				summary = parsedSummary
				for _, f := range parsed {
					rf := reportFinding{Finding: f, SeverityClass: severityClass(f.Severity)}
					switch f.Category {
					case findings.CategoryBaselined:
						baselined = append(baselined, rf)
					case findings.CategoryExcepted:
						excepted = append(excepted, rf)
					default:
						active = append(active, rf)
					}
				}
			}
			break
		}

		mergeBlocked := false
		for _, f := range active {
			if f.Category == findings.CategoryBlocking {
				mergeBlocked = true
				break
			}
		}

		data := prReportData{
			ActiveNav:    "dashboard",
			RepoFullName: owner + "/" + repo,
			Summary:      summary,
			MergeBlocked: mergeBlocked,
			Active:       groupByTool(active),
			Baselined:    groupByTool(baselined),
			Excepted:     groupByTool(excepted),
		}
		data.PR.Number = pr.Number
		data.PR.Title = pr.Title

		if err := prReportTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
