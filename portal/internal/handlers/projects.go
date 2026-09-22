// portal/internal/handlers/projects.go
package handlers

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ssdlc-portal/internal/projects"
	"ssdlc-portal/internal/shell"
	"ssdlc-portal/internal/sidecarclient"
)

var projectsTmpl = template.Must(template.ParseFiles(
	filepath.Join(templateDir, "layout.html"), filepath.Join(templateDir, "projects.html"),
))

// ProjectsSource is the reporting service as the Projects page sees it.
type ProjectsSource interface {
	Projects(ctx context.Context) (sidecarclient.ProjectsResponse, error)
}

type projectRow struct {
	projects.Project
	GradeLabel string // "A".."F", or "Unknown" when the repo could not be read
	GradeClass string // CSS suffix: A..F or unknown
	GiteaLink  string
	Woodpecker string
}

// projectLinks builds the browser links for one repository. Woodpecker
// addresses a repository by its numeric id (unverified route, kept here).
func projectLinks(s shell.Shell, repo string, woodpeckerID int) (gitea, woodpecker string) {
	if s.GiteaURL != "" {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			gitea = strings.TrimRight(s.GiteaURL, "/") + "/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
		}
	}
	if s.WoodpeckerURL != "" && woodpeckerID > 0 {
		woodpecker = strings.TrimRight(s.WoodpeckerURL, "/") + "/repos/" + strconv.Itoa(woodpeckerID)
	}
	return gitea, woodpecker
}

// Projects renders the project list. src == nil means the reporting service
// is not configured on this server.
func Projects(src ProjectsSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := shell.FromContext(r.Context())
		data := struct {
			ActiveNav string
			Shell     shell.Shell
			NotConfig bool
			Failed    bool
			Stale     bool
			Updated   string
			Rows      []projectRow
		}{ActiveNav: "projects", Shell: s}

		status := http.StatusOK
		if src == nil {
			data.NotConfig = true
		} else if resp, err := src.Projects(r.Context()); err != nil {
			data.Failed = true
			status = http.StatusBadGateway
		} else {
			data.Stale = !resp.PollOK
			if !resp.GeneratedAt.IsZero() {
				data.Updated = resp.GeneratedAt.UTC().Format(time.RFC3339)
			}
			for _, p := range resp.Projects {
				row := projectRow{Project: p, GradeLabel: p.Grade, GradeClass: p.Grade}
				if p.Unavailable || p.Grade == "" {
					row.GradeLabel, row.GradeClass = "Unknown", "unknown"
				}
				row.GiteaLink, row.Woodpecker = projectLinks(s, p.Repo, p.WoodpeckerRepoID)
				data.Rows = append(data.Rows, row)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if err := projectsTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
