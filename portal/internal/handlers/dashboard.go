// portal/internal/handlers/dashboard.go
package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/shell"
	"ssdlc-portal/internal/webutil"
)

// templateDir resolves to portal/web/templates regardless of the process's
// working directory: `go test` runs with the working directory set to this
// package's own source directory rather than the portal module root, so a
// bare relative "web/templates/..." (which works fine for the compiled
// server run from portal/) would fail to load under `go test`.
var templateDir = func() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "web", "templates")
}()

var dashboardTmpl = template.Must(template.ParseFiles(
	filepath.Join(templateDir, "layout.html"), filepath.Join(templateDir, "dashboard.html"),
))

type dashboardPRRow struct {
	giteaclient.PullRequest
	StatusBadge webutil.Badge
	Link        string // browser-reachable pull request URL
}

// Dashboard lists the operator's open pull requests with each one's most
// recent gate status. This platform registers exactly one commit-status
// context per repo (the security gate), so the first entry returned by
// GetCombinedStatus is unambiguous — no context-string matching needed.
func Dashboard(giteaBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := auth.TokenFromContext(r.Context())
		client := giteaclient.New(giteaBaseURL, token)

		prs, err := client.ListMyPullRequests(r.Context())
		if err != nil {
			http.Error(w, "could not load pull requests from Gitea: "+err.Error(), http.StatusBadGateway)
			return
		}

		rows := make([]dashboardPRRow, 0, len(prs))
		for _, pr := range prs {
			owner, repo := splitRepo(pr.Repo)
			state := "pending"
			if statuses, err := client.GetCombinedStatus(r.Context(), owner, repo, pr.HeadSHA); err == nil && len(statuses) > 0 {
				state = statuses[0].State
			}
			rows = append(rows, dashboardPRRow{PullRequest: pr, StatusBadge: webutil.StatusBadge(state), Link: pr.HTMLURL})
		}

		// Gitea's html_url is built from its own ROOT_URL, which may be an
		// internal name; prefer the public URL the browser can reach.
		sh := shell.FromContext(r.Context())
		if sh.GiteaURL != "" {
			base := strings.TrimRight(sh.GiteaURL, "/")
			for i := range rows {
				owner, repo := splitRepo(rows[i].Repo)
				rows[i].Link = fmt.Sprintf("%s/%s/%s/pulls/%d", base, url.PathEscape(owner), url.PathEscape(repo), rows[i].Number)
			}
		}

		data := struct {
			ActiveNav    string
			Shell        shell.Shell
			PullRequests []dashboardPRRow
		}{ActiveNav: "dashboard", PullRequests: rows, Shell: sh}

		if err := dashboardTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func splitRepo(fullName string) (owner, repo string) {
	for i := 0; i < len(fullName); i++ {
		if fullName[i] == '/' {
			return fullName[:i], fullName[i+1:]
		}
	}
	return fullName, ""
}
