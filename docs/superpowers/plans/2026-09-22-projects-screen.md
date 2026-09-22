# Projects Screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A working `/projects` page: one row per onboarded repository with its letter grade, open and blocked PRs, severity counts and links into Gitea and Woodpecker, read from the reporting service through `sidecarclient`. The sidebar's greyed-out "Projects" entry becomes a real link.

**Architecture:** A `Projects` handler takes a small `ProjectsSource` interface (satisfied by `*sidecarclient.Client`), builds a view model (grade badge class, Gitea and Woodpecker links, freshness) and renders `projects.html` inside the existing layout. When the reporting service is not configured or fails, the page says so plainly instead of erroring. No new packages.

**Tech Stack:** Go 1.22 stdlib (`html/template`), the portal's existing layout, tokens and `shell.css`.

**Spec:** `docs/superpowers/specs/2026-09-21-portal-redesign-design.md` (section "Screens: Projects", "Technical approach": sidebar entries stay visible when a feature is missing and the page says why; report notes must be visible). Depends on the completed plan `2026-09-22-aggregated-project-data.md`. The project detail page (tabs), Overview rework and Issues list are separate later plans.

## Global Constraints

- Server-rendered `html/template`, no JavaScript needed for the page, no CDN or external requests (spec: "Technical approach").
- An empty `grade` in the sidecar JSON means "could not be read": show "Unknown" and a visible "could not be read" note, NEVER a letter (aggregated-data plan ruling). A repo with zero open PRs and `unavailable: false` legitimately shows `A`.
- Grades cover findings on open pull requests only until baseline data lands: the page must say so in one visible sentence.
- If `poll_ok` is false, show a visible warning that some data may be stale or incomplete.
- Links that open in the browser use `Shell.GiteaURL` / `Shell.WoodpeckerURL` (public URLs), path segments escaped with `url.PathEscape`; external links use `target="_blank" rel="noopener"`. The Woodpecker route `/repos/<id>` is unverified in the live UI: keep it in one helper.
- Every signed-in user may see the project list (single-tenant platform; the list contains repository names and counts only, no findings text).
- Reuse existing CSS tokens (`--ssdlc-*`); no new colours outside `tokens.css`.
- Go is not installed on the dev machine. Run everything with:
  `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/src" -w /src/portal golang:1.22-alpine sh -c "go vet ./... && go test ./... && gofmt -l internal cmd main.go"`
  (gofmt lists three pre-existing files: `internal/exceptions/exceptions.go`, `internal/findings/parser.go`, `internal/handlers/prreport_test.go`; nothing else may appear.) No `-race`.
- Use the Write/Edit tools for files, not shell heredocs. Commit trailer: `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`.
- Do not touch the user's running UAT stack (`ssdlc-*` containers/volumes/network, `deploy/uat/state`); never run compose `up/down/build` against it.

---

### Task 1: Projects handler, template and grade styling

**Files:**
- Create: `portal/internal/handlers/projects.go`
- Create: `portal/web/templates/projects.html`
- Modify: `portal/web/static/shell.css` (append grade and table styles)
- Test: `portal/internal/handlers/projects_test.go`

**Interfaces:**
- Consumes: `sidecarclient.ProjectsResponse{GeneratedAt time.Time; PollOK bool; Projects []projects.Project}`, `projects.Project{Repo string; OpenPRs, BlockedPRs, Critical, High, Medium, Low, Score int; Grade string; Unavailable bool; WoodpeckerRepoID int}`, `shell.FromContext`, the layout data contract (`ActiveNav string`, `Shell shell.Shell`).
- Produces:
  - `handlers.ProjectsSource` interface: `Projects(ctx context.Context) (sidecarclient.ProjectsResponse, error)`.
  - `handlers.Projects(src ProjectsSource) http.HandlerFunc`; `src == nil` means "not configured". Never returns 500 for a sidecar failure: it renders the page with an explanatory notice (HTTP 200 when not configured; HTTP 502 when the call failed, so monitoring can see it).
  - Template names `projects.html` defining `content` and `title`.

- [ ] **Step 1: Write the failing tests** (`projects_test.go`)

```go
package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssdlc-portal/internal/projects"
	"ssdlc-portal/internal/shell"
	"ssdlc-portal/internal/sidecarclient"
)

type fakeProjects struct {
	resp sidecarclient.ProjectsResponse
	err  error
}

func (f fakeProjects) Projects(ctx context.Context) (sidecarclient.ProjectsResponse, error) {
	return f.resp, f.err
}

func getProjects(t *testing.T, src ProjectsSource, s shell.Shell) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req = req.WithContext(shell.WithContext(req.Context(), s))
	rec := httptest.NewRecorder()
	Projects(src)(rec, req)
	return rec.Code, rec.Body.String()
}

var sampleShell = shell.Shell{
	Operator: "dev1", Role: shell.RoleDeveloper,
	GiteaURL: "http://192.168.1.28:3500", WoodpeckerURL: "http://192.168.1.28:8000",
}

func sampleResp() sidecarclient.ProjectsResponse {
	return sidecarclient.ProjectsResponse{
		GeneratedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC), PollOK: true,
		Projects: []projects.Project{
			{Repo: "ssdlc/pilot-app", OpenPRs: 3, BlockedPRs: 2, Critical: 1, High: 1, Score: 47, Grade: "D", WoodpeckerRepoID: 7},
			{Repo: "ssdlc/billing-api", OpenPRs: 0, Score: 100, Grade: "A"},
		},
	}
}

func TestProjects_RendersRowsGradesAndLinks(t *testing.T) {
	code, body := getProjects(t, fakeProjects{resp: sampleResp()}, sampleShell)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{
		"ssdlc/pilot-app", "ssdlc/billing-api",
		`class="ssdlc-grade ssdlc-grade-D"`, `class="ssdlc-grade ssdlc-grade-A"`,
		`href="http://192.168.1.28:3500/ssdlc/pilot-app"`,
		`href="http://192.168.1.28:8000/repos/7"`,
		"open pull requests only", // the coverage sentence
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in body", want)
		}
	}
	if strings.Contains(body, `href="http://192.168.1.28:8000/repos/0"`) {
		t.Error("no Woodpecker link when the repo id is unknown")
	}
	if strings.Contains(body, "could not be read") || strings.Contains(body, "may be stale") {
		t.Error("no warnings expected for a healthy poll")
	}
}

func TestProjects_UnreadableRepoNeverShowsAGrade(t *testing.T) {
	resp := sampleResp()
	resp.Projects = []projects.Project{{Repo: "ssdlc/billing-api", Score: 100, Grade: "", Unavailable: true}}
	_, body := getProjects(t, fakeProjects{resp: resp}, sampleShell)
	if !strings.Contains(body, "could not be read") || !strings.Contains(body, "Unknown") {
		t.Error("an unreadable repo must say so and show Unknown")
	}
	if strings.Contains(body, "ssdlc-grade-A") {
		t.Error("an unreadable repo must never show a letter")
	}
}

func TestProjects_StalePollWarns(t *testing.T) {
	resp := sampleResp()
	resp.PollOK = false
	_, body := getProjects(t, fakeProjects{resp: resp}, sampleShell)
	if !strings.Contains(body, "may be stale") {
		t.Error("poll_ok=false must show a visible warning")
	}
}

func TestProjects_NotConfigured(t *testing.T) {
	code, body := getProjects(t, nil, sampleShell)
	if code != http.StatusOK || !strings.Contains(body, "not available on this server") {
		t.Errorf("status=%d, want 200 with an explanation; body has notice: %v", code, strings.Contains(body, "not available"))
	}
}

func TestProjects_SidecarFailureIsExplainedNotRaw(t *testing.T) {
	code, body := getProjects(t, fakeProjects{err: errors.New("sidecar: status 503: no poll has completed yet")}, sampleShell)
	if code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", code)
	}
	if !strings.Contains(body, "reporting service") || strings.Contains(body, "status 503") {
		t.Error("show a friendly message; do not leak the raw error into the page")
	}
}

func TestProjects_EscapesRepoNamesInLinks(t *testing.T) {
	resp := sampleResp()
	resp.Projects = []projects.Project{{Repo: `o/a"b<c`, Score: 100, Grade: "A"}}
	_, body := getProjects(t, fakeProjects{resp: resp}, sampleShell)
	if strings.Contains(body, `a"b<c`) {
		t.Error("repo name must be escaped in the page")
	}
	if !strings.Contains(body, "/o/a%22b%3Cc") {
		t.Errorf("link path segments must be percent-escaped; body:\n%s", body)
	}
}
```

- [ ] **Step 2: Run to verify it fails.** Expected: build failure, `undefined: ProjectsSource` / `Projects`.

- [ ] **Step 3: Implement.** `projects.go`:

```go
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
```

`projects.html`:

```html
<!-- portal/web/templates/projects.html -->
{{define "title"}}Projects - SSDLC Portal{{end}}
{{define "content"}}
<h1 style="font-weight:600;">Projects</h1>
{{if .NotConfig}}
<div class="ssdlc-card">Projects are not available on this server: the reporting service is not configured. Ask your platform admin.</div>
{{else if .Failed}}
<div class="ssdlc-card ssdlc-notice">The reporting service could not be reached, so project grades cannot be shown right now. Try again in a minute; if it keeps failing, tell your platform admin.</div>
{{else}}
<p style="color:var(--ssdlc-text-3);margin:0 0 12px;">Grades count findings on open pull requests only (baseline debt is added later).{{if .Updated}} Updated <span class="ssdlc-mono">{{.Updated}}</span>.{{end}}</p>
{{if .Stale}}<div class="ssdlc-card ssdlc-notice">Some data may be stale or incomplete: the last check could not read every repository.</div>{{end}}
{{if not .Rows}}
<div class="ssdlc-card" style="color:var(--ssdlc-text-3);">No projects yet. An admin can set one up from Admin.</div>
{{else}}
<div class="ssdlc-card ssdlc-table-wrap">
<table class="ssdlc-table">
  <thead><tr><th>Project</th><th>Grade</th><th>Open PRs</th><th>Blocked</th><th>Critical</th><th>High</th><th>Medium</th><th>Low</th><th>Open in</th></tr></thead>
  <tbody>
  {{range .Rows}}
  <tr>
    <td class="ssdlc-mono">{{.Repo}}{{if .Unavailable}}<div class="ssdlc-sub">could not be read</div>{{end}}</td>
    <td><span class="ssdlc-grade ssdlc-grade-{{.GradeClass}}">{{.GradeLabel}}</span></td>
    <td>{{.OpenPRs}}</td><td>{{.BlockedPRs}}</td><td>{{.Critical}}</td><td>{{.High}}</td><td>{{.Medium}}</td><td>{{.Low}}</td>
    <td>{{if .GiteaLink}}<a class="ssdlc-ext" href="{{.GiteaLink}}" target="_blank" rel="noopener">Gitea &#8599;</a>{{end}}{{if .Woodpecker}}<a class="ssdlc-ext" href="{{.Woodpecker}}" target="_blank" rel="noopener">Woodpecker &#8599;</a>{{end}}</td>
  </tr>
  {{end}}
  </tbody>
</table>
</div>
{{end}}
{{end}}
{{end}}
```

Note: the test for an unreadable repo expects the word "Unknown" and "could not be read" in the same page; the "Some data may be stale" banner uses different wording so the healthy-poll test's `not contains "could not be read"` holds only when no row is unavailable.

Append to `shell.css`:

```css
/* projects table and grades */
.ssdlc-table-wrap{overflow-x:auto;padding:0}
.ssdlc-table{border-collapse:collapse;width:100%;font-size:13px}
.ssdlc-table th{text-align:left;font-weight:500;font-size:11px;text-transform:uppercase;letter-spacing:.05em;color:var(--ssdlc-text-3);background:var(--ssdlc-surface-alt)}
.ssdlc-table th,.ssdlc-table td{padding:10px 14px;border-bottom:1px solid var(--ssdlc-border);white-space:nowrap}
.ssdlc-table tbody tr:last-child td{border-bottom:0}
.ssdlc-sub{color:var(--ssdlc-text-3);font-family:'IBM Plex Sans',system-ui,sans-serif;font-size:11px}
.ssdlc-grade{display:inline-block;min-width:26px;text-align:center;border-radius:6px;padding:1px 8px;font-weight:600;font-size:13px;border:1px solid var(--ssdlc-border-alt)}
.ssdlc-grade-A{color:var(--ssdlc-success);border-color:var(--ssdlc-success);background:var(--ssdlc-success-bg)}
.ssdlc-grade-B{color:var(--ssdlc-accent);border-color:var(--ssdlc-accent);background:var(--ssdlc-accent-bg)}
.ssdlc-grade-C{color:var(--ssdlc-warning);border-color:var(--ssdlc-warning);background:var(--ssdlc-warning-bg)}
.ssdlc-grade-D,.ssdlc-grade-F{color:var(--ssdlc-critical);border-color:var(--ssdlc-critical);background:var(--ssdlc-critical-bg)}
.ssdlc-grade-unknown{color:var(--ssdlc-text-3);border-style:dashed}
.ssdlc-notice{border-color:var(--ssdlc-warning);color:var(--ssdlc-text-1)}
```

(If `.ssdlc-card` is defined only in `tokens.css`, that is fine: both stylesheets are loaded by the layout.)

- [ ] **Step 4: Run to verify it passes.** Expected: `ok ssdlc-portal/internal/handlers`; whole suite green; gofmt clean apart from the three known files.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/handlers/projects.go portal/internal/handlers/projects_test.go portal/web/templates/projects.html portal/web/static/shell.css
git commit -m "feat(portal): Projects page with grades and deep links"
```

---

### Task 2: Wire the route, enable the sidebar entry, validate the sidecar URL

**Files:**
- Modify: `portal/main.go` (build the client, register `/projects`)
- Modify: `portal/web/templates/layout.html` (Projects entry)
- Modify: `portal/internal/config/config.go`, `portal/internal/config/config_test.go` (URL scheme check)
- Modify: `portal/internal/handlers/layout_test.go` (nav assertions)
- Modify: `portal/internal/help/help.go` (grade topic text), `portal/internal/help/help_test.go` only if a test pins the old text
- Modify: `deploy/uat/README.md` (one sentence)

**Interfaces:**
- Consumes: `handlers.Projects(src ProjectsSource) http.HandlerFunc`, `sidecarclient.New(baseURL, token) *sidecarclient.Client`, `cfg.SidecarURL/SidecarToken`.
- Produces: route `GET /projects` behind `authed(...)`; sidebar link to `/projects` with `data-pal-item` so Ctrl+K lists it; `config.Load` rejects a `PORTAL_SIDECAR_URL` that does not start with `http://` or `https://`.

- [ ] **Step 1: Write the failing tests.**
  - `config_test.go`: with the required env set (use the file's `setRequiredEnv` helper) and `PORTAL_SIDECAR_TOKEN=t`, `PORTAL_SIDECAR_URL=sidecar:8282` must return an error mentioning `PORTAL_SIDECAR_URL` and `http`; `http://sidecar:8282` and `https://x` must load.
  - `layout_test.go` (follow its existing setup for rendering a page with a given Shell): the rendered layout contains `href="/projects"`, does NOT contain the text `class="disabled" title="Arrives in the next step of the portal redesign">Projects`, and marks the Projects link `active` when `ActiveNav == "projects"`. Keep the existing assertion that Issues is still disabled.

- [ ] **Step 2: Run to verify they fail.**

- [ ] **Step 3: Implement.**
  - `layout.html`: replace the Projects line with
    `<a href="/projects" data-pal-item data-pal-sub="page" class="{{if eq .ActiveNav "projects"}}active{{end}}">Projects</a>`.
  - `config.go`: after the both-or-neither check add
    ```go
    if cfg.SidecarURL != "" && !strings.HasPrefix(cfg.SidecarURL, "http://") && !strings.HasPrefix(cfg.SidecarURL, "https://") {
    	problems = append(problems, "PORTAL_SIDECAR_URL must start with http:// or https://")
    }
    ```
  - `main.go`: after the shell builder:
    ```go
    var projectsSource handlers.ProjectsSource // nil = reporting service not configured
    if cfg.SidecarURL != "" {
    	projectsSource = sidecarclient.New(cfg.SidecarURL, cfg.SidecarToken)
    }
    ```
    (import `ssdlc-portal/internal/sidecarclient`; a typed-nil pitfall is avoided because the variable is only assigned a non-nil pointer inside the `if`) and register next to the other pages:
    `mux.HandleFunc("/projects", authed(handlers.Projects(projectsSource)))`.
  - `help.go`, topic `grade`: change the answer's opening `<b>Coming with the Projects screens.</b>` to `<b>Shown on the Projects page.</b>` and add the sentence "Right now it counts findings on open pull requests only." keeping the rest (still "proposals, not enforced yet"). Update any test that pins the old wording.
  - `deploy/uat/README.md`: extend the "Portal navigation" bullet with "Projects lists every onboarded repository with its grade, read from the reporting service."

- [ ] **Step 4: Run the full suite.** Expected: all `ok`, gofmt clean apart from the three known files.

- [ ] **Step 5: Commit**

```bash
git add portal/main.go portal/web/templates/layout.html portal/internal/config portal/internal/handlers/layout_test.go portal/internal/help deploy/uat/README.md
git commit -m "feat(portal): route /projects, enable the sidebar entry, validate PORTAL_SIDECAR_URL"
```

---

## Self-review notes

- Spec coverage: the Projects list (grade, gate/blocked, counts, links to Gitea and Woodpecker), feature-missing explanation, report-note visibility (`unavailable`, `poll_ok`). Deferred to later plans: health ring, project detail tabs (Pull requests, Issues, Baseline, Settings), sorting/filtering, Overview rework.
- Types: `ProjectsSource`, `projectRow`, `projectLinks` are defined in Task 1 and only consumed by Task 2 through `handlers.Projects`.
- `TestProjects_NotConfigured` passes a nil interface value: `getProjects(t, nil, ...)` with parameter type `ProjectsSource` is a true nil interface, so `src == nil` holds; `main.go` keeps the same guarantee by only assigning inside the `if`.
