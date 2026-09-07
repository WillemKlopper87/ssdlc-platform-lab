// portal/internal/handlers/prreport.go
package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"

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
	IsBlocking    bool
}

func severityClass(sev string) string {
	switch sev {
	case "CRITICAL":
		return "critical"
	case "HIGH":
		return "warning"
	default:
		return "neutral"
	}
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
	Findings []reportFinding
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

		var reportFindings []reportFinding
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
				parsed, _, err := findings.Parse(log)
				if err != nil {
					continue
				}
				for _, f := range parsed {
					reportFindings = append(reportFindings, reportFinding{
						Finding:       f,
						SeverityClass: severityClass(f.Severity),
						IsBlocking:    f.Severity == "CRITICAL" || f.Severity == "HIGH",
					})
				}
			}
			break
		}

		data := prReportData{
			ActiveNav:    "dashboard",
			RepoFullName: owner + "/" + repo,
			Findings:     reportFindings,
		}
		data.PR.Number = pr.Number
		data.PR.Title = pr.Title

		if err := prReportTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
