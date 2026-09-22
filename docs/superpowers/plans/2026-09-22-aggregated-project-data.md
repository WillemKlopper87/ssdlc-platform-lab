# Aggregated Project Data Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The reporting service serves a per-project summary (open PRs, blocked PRs, severity counts, letter grade) at `GET /api/v1/projects`, and the portal has a client and config to read it, so the Projects/Overview screens (redesign step 3) have data to render.

**Architecture:** A pure `grade` package (score, letter, SLA targets) and a pure `projects` package (turn the poller's per-PR reports into one row per repository). The poller keeps its latest snapshot behind a mutex; a new authenticated sidecar endpoint serialises `projects.Aggregate` over it. The portal gets an optional `sidecarclient` that calls that endpoint with the service token.

**Tech Stack:** Go 1.22, standard library only (module `ssdlc-portal`, dir `portal/`).

**Spec:** `docs/superpowers/specs/2026-09-21-portal-redesign-design.md` (section "Data the screens need", delivery step 2). Baseline counts, SLA clocks per issue, exception joins and Woodpecker pass-rate are later plans; this plan covers only what the poller already knows (open-PR findings).

## Global Constraints

- Grade: `score = 100 - 35*critical - 18*high - 6*medium - 2*low`; A >= 90, B >= 75, C >= 60, D >= 40, else F. (Spec: "Data the screens need".) The score is not clamped at 0 in the JSON but the letter for any score < 40 is F.
- SLA targets (prototype values, configuration to be confirmed): Critical 2 days, High 7, Medium 30, Low 90.
- Standard library only; read-only endpoints; the `/api/v1/*` routes need the bearer service token (existing `auth` wrapper).
- Never present missing data as clean: a repository whose PR list or findings could not be read is `unavailable: true`, not "no issues".
- Go is not installed on the dev machine. Run everything with:
  `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/src" -w /src/portal golang:1.22-alpine sh -c "go vet ./... && go test ./... && gofmt -l internal cmd main.go"`
  (gofmt lists three pre-existing files: `internal/exceptions/exceptions.go`, `internal/findings/parser.go`, `internal/handlers/prreport_test.go`; nothing else may appear.) No `-race`.
- Use the Write/Edit tools for files, not shell heredocs. Commit trailer: `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`.
- Do not touch the user's running UAT stack (`ssdlc-*` containers/volumes/network, `deploy/uat/state`).

---

### Task 1: grade package

**Files:**
- Create: `portal/internal/grade/grade.go`
- Test: `portal/internal/grade/grade_test.go`

**Interfaces:**
- Produces: `grade.Counts{Critical, High, Medium, Low int}`; `grade.Score(Counts) int`; `grade.Letter(score int) string`; `grade.SLADays(severity string) int` (severity in `critical|high|medium|low`, case-insensitive; unknown returns 0).

- [ ] **Step 1: Write the failing test** (`grade_test.go`)

```go
package grade

import "testing"

func TestScoreAndLetter(t *testing.T) {
	for _, c := range []struct {
		name   string
		counts Counts
		score  int
		letter string
	}{
		{"clean", Counts{}, 100, "A"},
		{"one low", Counts{Low: 1}, 98, "A"},
		{"boundary A", Counts{Medium: 1, Low: 2}, 90, "A"},
		{"one high two medium", Counts{High: 1, Medium: 2}, 70, "C"},
		{"one high", Counts{High: 1}, 82, "B"},
		{"boundary B", Counts{High: 1, Medium: 1, Low: 0}, 76, "B"},
		{"one critical", Counts{Critical: 1}, 65, "C"},
		{"critical and high", Counts{Critical: 1, High: 1}, 47, "D"},
		{"boundary D", Counts{Critical: 1, High: 1, Low: 3}, 41, "D"},
		{"two critical", Counts{Critical: 2}, 30, "F"},
		{"negative stays F", Counts{Critical: 5}, -75, "F"},
	} {
		s := Score(c.counts)
		if s != c.score {
			t.Errorf("%s: Score = %d, want %d", c.name, s, c.score)
		}
		if l := Letter(s); l != c.letter {
			t.Errorf("%s: Letter(%d) = %s, want %s", c.name, s, l, c.letter)
		}
	}
}

func TestLetterBoundaries(t *testing.T) {
	for score, want := range map[int]string{90: "A", 89: "B", 75: "B", 74: "C", 60: "C", 59: "D", 40: "D", 39: "F"} {
		if got := Letter(score); got != want {
			t.Errorf("Letter(%d) = %s, want %s", score, got, want)
		}
	}
}

func TestSLADays(t *testing.T) {
	for sev, want := range map[string]int{"critical": 2, "HIGH": 7, "Medium": 30, "low": 90, "info": 0, "": 0} {
		if got := SLADays(sev); got != want {
			t.Errorf("SLADays(%q) = %d, want %d", sev, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails** (command in Global Constraints). Expected: build failure, `undefined: Counts`.

- [ ] **Step 3: Implement** (`grade.go`)

```go
// Package grade turns open-issue counts into the project score and letter,
// and holds the fix-within targets. Weights and thresholds are proposals
// from the portal redesign spec and are deliberately kept in one place.
package grade

import "strings"

type Counts struct {
	Critical, High, Medium, Low int
}

// Score is 100 minus a weight per open issue. It is not clamped so callers
// can still order two failing projects.
func Score(c Counts) int {
	return 100 - 35*c.Critical - 18*c.High - 6*c.Medium - 2*c.Low
}

// Letter maps a score to A-F.
func Letter(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	}
	return "F"
}

// SLADays is the target number of days to fix an issue of the severity, or 0
// for an unknown severity (no clock).
func SLADays(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 2
	case "high":
		return 7
	case "medium":
		return 30
	case "low":
		return 90
	}
	return 0
}
```

- [ ] **Step 4: Run to verify it passes.** Expected: `ok ssdlc-portal/internal/grade`. (Check the "boundary B" row: 100-18-6 = 76, letter B; "boundary D": 100-35-18-6 = 41, letter D. If any row disagrees, the row is wrong, not the formula.)

- [ ] **Step 5: Commit**

```bash
git add portal/internal/grade
git commit -m "feat(portal): grade package (score, letter, SLA targets)"
```

---

### Task 2: projects aggregation

**Files:**
- Create: `portal/internal/projects/projects.go`
- Test: `portal/internal/projects/projects_test.go`

**Interfaces:**
- Consumes: `report.Report` (fields `Repo`, `Gate`, `MergeBlocked`, `Summary{Critical,High,Medium,Low}`, `FindingsUnavailable`, `WoodpeckerRepoID`), `grade.Counts/Score/Letter`.
- Produces:
  - `projects.Project{Repo string; OpenPRs, BlockedPRs int; Critical, High, Medium, Low int; Score int; Grade string; Unavailable bool; WoodpeckerRepoID int}` with JSON tags `repo, open_prs, blocked_prs, critical, high, medium, low, score, grade, unavailable, woodpecker_repo_id`.
  - `projects.Aggregate(repos []string, failed map[string]bool, reports []report.Report) []Project` : one row per name in `repos` (even with no PRs), worst first (lowest score, then more blocked PRs, then name).

- [ ] **Step 1: Write the failing test** (`projects_test.go`)

```go
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
```

- [ ] **Step 2: Run to verify it fails.** Expected: `undefined: Aggregate`.

- [ ] **Step 3: Implement** (`projects.go`)

```go
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
```

- [ ] **Step 4: Run to verify it passes.** Expected: `ok ssdlc-portal/internal/projects`. (pilot-app: 100-18-12-2 = 68, C.)

- [ ] **Step 5: Commit**

```bash
git add portal/internal/projects
git commit -m "feat(portal): aggregate PR reports into per-project rows with grades"
```

---

### Task 3: poller snapshot and `/api/v1/projects`

**Files:**
- Modify: `portal/internal/sidecar/poller.go` (add `Latest` type, mutex, `Latest()` method; record the snapshot in `Once`)
- Modify: `portal/internal/sidecar/server.go` (`APIDeps.Latest`, new route)
- Modify: `portal/cmd/sidecar/main.go` (pass `Latest: poller.Latest`)
- Test: `portal/internal/sidecar/poller_test.go`, `portal/internal/sidecar/server_test.go` (add to the existing files; server_test may need to be created if absent: check `ls portal/internal/sidecar` first and follow the existing test helpers)

**Interfaces:**
- Consumes: `projects.Aggregate`, `projects.Project`.
- Produces:
  - `sidecar.Latest{GeneratedAt time.Time; PollOK bool; Repos []string; Failed map[string]bool; Reports []report.Report}` and `(*Poller).Latest() Latest` (returns a copy, safe for concurrent use; zero `GeneratedAt` means no poll has completed yet).
  - `APIDeps.Latest func() Latest`.
  - Route `GET /api/v1/projects` (bearer token) returning `200 {"generated_at": <RFC3339>, "poll_ok": bool, "projects": [projects.Project...]}`; `503 "no poll has completed yet"` before the first poll.

- [ ] **Step 1: Write the failing tests.** In `poller_test.go`:

```go
func TestPollerLatestRecordsReposAndReports(t *testing.T) {
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{
		"ssdlc/pilot-app": {{Number: 3, Title: "t", State: "open", HeadSHA: "abc", CreatedAt: time.Unix(900, 0)}},
	}}
	w := fakeWP{repos: []woodpeckerclient.Repo{
		{ID: 7, FullName: "ssdlc/pilot-app"}, {ID: 8, FullName: "ssdlc/billing-api"},
		{ID: 9, FullName: "someone-else/not-ours"},
	}}
	p, _ := newPoller(g, w)
	if !p.Latest().GeneratedAt.IsZero() {
		t.Fatal("before the first poll GeneratedAt must be zero")
	}
	if err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	l := p.Latest()
	if !l.PollOK || len(l.Reports) != 1 || l.Reports[0].Repo != "ssdlc/pilot-app" {
		t.Errorf("latest wrong: %+v", l)
	}
	if len(l.Repos) != 2 || l.Repos[0] != "ssdlc/pilot-app" || l.Repos[1] != "ssdlc/billing-api" {
		t.Errorf("repos must be the org's repos in list order: %v", l.Repos)
	}
	l.Repos[0] = "mutated"
	if p.Latest().Repos[0] == "mutated" {
		t.Error("Latest must return a copy")
	}
}

func TestPollerLatestMarksFailedRepoAndKeepsDataOnListFailure(t *testing.T) {
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{}, failRepo: "ssdlc/pilot-app"}
	w := fakeWP{repos: []woodpeckerclient.Repo{{ID: 7, FullName: "ssdlc/pilot-app"}}}
	p, _ := newPoller(g, w)
	if err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	l := p.Latest()
	if l.PollOK || !l.Failed["ssdlc/pilot-app"] {
		t.Errorf("a repo whose PR list failed must be Failed and PollOK false: %+v", l)
	}
	fail := true
	w.reposErrFn = func() error {
		if fail {
			return errors.New("down")
		}
		return nil
	}
	p2, _ := newPoller(fakeGitea{prs: g.prs}, w)
	fail = false
	_ = p2.Once(context.Background())
	before := p2.Latest()
	fail = true
	if err := p2.Once(context.Background()); err == nil {
		t.Fatal("repo-list failure must be an error")
	}
	after := p2.Latest()
	if after.PollOK || len(after.Repos) != len(before.Repos) {
		t.Errorf("list failure must keep the previous repos and clear PollOK: before=%+v after=%+v", before, after)
	}
}
```

In `server_test.go` (create if missing, `package sidecar`; look at any existing server test for how `NewServer` and the token are built and copy that setup):

```go
func TestProjectsEndpoint(t *testing.T) {
	now := time.Unix(3000, 0)
	d := &APIDeps{Token: "s3cret", Now: func() time.Time { return now }, Metrics: &metrics.Store{},
		Latest: func() Latest {
			return Latest{
				GeneratedAt: now, PollOK: true, Repos: []string{"ssdlc/pilot-app"},
				Reports: []report.Report{{Repo: "ssdlc/pilot-app", MergeBlocked: true,
					Summary: report.Summary{Critical: 1}}},
			}
		}}
	srv := NewServer(d)

	req := httptest.NewRequest("GET", "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		PollOK   bool               `json:"poll_ok"`
		Projects []projects.Project `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.PollOK || len(body.Projects) != 1 || body.Projects[0].Grade != "C" || body.Projects[0].BlockedPRs != 1 {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}

	d.Latest = func() Latest { return Latest{} }
	req = httptest.NewRequest("GET", "/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	NewServer(d).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("before the first poll want 503, got %d", rec.Code)
	}
}
```

(Add the imports each test file needs: `encoding/json`, `net/http`, `net/http/httptest`, `ssdlc-portal/internal/projects`, `ssdlc-portal/internal/report`, `ssdlc-portal/internal/metrics`.)

- [ ] **Step 2: Run to verify they fail.** Expected: `p.Latest undefined`, `unknown field Latest`.

- [ ] **Step 3: Implement.** In `poller.go` add `"sync"` to the imports, then:

```go
// Latest is the poller's most recent view, for the projects endpoint. A zero
// GeneratedAt means no poll has completed yet.
type Latest struct {
	GeneratedAt time.Time
	PollOK      bool
	Repos       []string        // the org's repositories, in Woodpecker's order
	Failed      map[string]bool // repositories whose pull requests could not be listed
	Reports     []report.Report
}
```

Add to the `Poller` struct: `mu sync.RWMutex` and `latest Latest`. Add:

```go
// Latest returns a copy of the most recent snapshot; safe for concurrent use.
func (p *Poller) Latest() Latest {
	p.mu.RLock()
	defer p.mu.RUnlock()
	l := p.latest
	l.Repos = append([]string(nil), l.Repos...)
	l.Reports = append([]report.Report(nil), l.Reports...)
	l.Failed = make(map[string]bool, len(p.latest.Failed))
	for k, v := range p.latest.Failed {
		l.Failed[k] = v
	}
	return l
}

func (p *Poller) setLatest(l Latest) {
	p.mu.Lock()
	p.latest = l
	p.mu.Unlock()
}
```

In `Once`: in the repo-list-failure branch, before returning, keep the previous data but flag the poll:

```go
		prev := p.Latest()
		prev.PollOK = false
		p.setLatest(prev)
```

In the success path, collect `var orgRepos []string` and `failed := map[string]bool{}`: append `repo.FullName` to `orgRepos` right after the prefix check; set `failed[repo.FullName] = true` in the `ListOpenPullRequests` error branch and when `report.Build` errors. After `p.lastReports, p.lastTime = reports, now` add:

```go
	p.setLatest(Latest{GeneratedAt: now, PollOK: allOK, Repos: orgRepos, Failed: failed, Reports: reports})
```

In `server.go` add `Latest func() Latest` to `APIDeps`, import `ssdlc-portal/internal/projects`, and register:

```go
	mux.HandleFunc("GET /api/v1/projects", d.auth(func(w http.ResponseWriter, r *http.Request) {
		l := d.Latest()
		if l.GeneratedAt.IsZero() {
			http.Error(w, "no poll has completed yet", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"generated_at": l.GeneratedAt.UTC().Format(time.RFC3339),
			"poll_ok":      l.PollOK,
			"projects":     projects.Aggregate(l.Repos, l.Failed, l.Reports),
		})
	}))
```

In `cmd/sidecar/main.go` add `Latest: poller.Latest,` to the `APIDeps` literal. Guard nil `d.Latest` in the handler (`if d.Latest == nil` return 503) so existing tests that build `APIDeps` without it are unaffected.

- [ ] **Step 4: Run the full suite.** Expected: everything passes, gofmt lists only the three known files.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/sidecar portal/cmd/sidecar
git commit -m "feat(sidecar): GET /api/v1/projects with per-project grades"
```

---

### Task 4: portal sidecar client and configuration

**Files:**
- Create: `portal/internal/sidecarclient/client.go`
- Test: `portal/internal/sidecarclient/client_test.go`
- Modify: `portal/internal/config/config.go`, `portal/internal/config/config_test.go`
- Modify: `deploy/uat/docker-compose.uat.yml` (portal env), `deploy/uat/README.md` (one line)

**Interfaces:**
- Consumes: `projects.Project`.
- Produces:
  - `config.Config.SidecarURL, SidecarToken string` from `PORTAL_SIDECAR_URL` (trailing slash trimmed) and `PORTAL_SIDECAR_TOKEN`; both empty is valid (feature off); exactly one set is a configuration error.
  - `sidecarclient.Client{BaseURL, Token string; HTTP *http.Client}`, `sidecarclient.New(baseURL, token string) *Client` (10 s timeout), `(*Client).Projects(ctx) (ProjectsResponse, error)`, `ProjectsResponse{GeneratedAt time.Time; PollOK bool; Projects []projects.Project}`. Non-200 returns an error containing the status code (503 included).

- [ ] **Step 1: Write the failing tests.** `client_test.go`:

```go
package sidecarclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjects_SendsTokenAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("bad request: %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"generated_at":"2026-09-22T10:00:00Z","poll_ok":true,"projects":[{"repo":"ssdlc/pilot-app","open_prs":2,"grade":"C","score":68}]}`))
	}))
	defer srv.Close()
	got, err := New(srv.URL, "tok").Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.PollOK || len(got.Projects) != 1 || got.Projects[0].Grade != "C" || got.GeneratedAt.IsZero() {
		t.Errorf("decode wrong: %+v", got)
	}
}

func TestProjects_NonOKIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no poll has completed yet", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := New(srv.URL, "tok").Projects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("want an error naming 503, got %v", err)
	}
}
```

Add to `config_test.go` (follow its existing helper for setting the required env; read the file first): a test that with all required vars set, `PORTAL_SIDECAR_URL=http://sidecar:8282/` and `PORTAL_SIDECAR_TOKEN=t` give `SidecarURL == "http://sidecar:8282"`; that neither set gives empty values and no error; and that only the URL set returns an error mentioning `PORTAL_SIDECAR_TOKEN`.

- [ ] **Step 2: Run to verify they fail.** Expected: `undefined: New`, unknown config fields.

- [ ] **Step 3: Implement** `client.go`:

```go
// Package sidecarclient reads the reporting service's aggregated endpoints
// with the service token. It is optional: the portal only builds one when
// PORTAL_SIDECAR_URL is configured.
package sidecarclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ssdlc-portal/internal/projects"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

type ProjectsResponse struct {
	GeneratedAt time.Time          `json:"generated_at"`
	PollOK      bool               `json:"poll_ok"`
	Projects    []projects.Project `json:"projects"`
}

func (c *Client) Projects(ctx context.Context) (ProjectsResponse, error) {
	var out ProjectsResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/projects", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return out, fmt.Errorf("sidecar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return out, fmt.Errorf("sidecar: status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return out, fmt.Errorf("sidecar: decode: %w", err)
	}
	return out, nil
}
```

In `config.go` add `SidecarURL, SidecarToken string` (comment: optional; both or neither), read `get("PORTAL_SIDECAR_URL")` (trim trailing `/`) and `get("PORTAL_SIDECAR_TOKEN")`, and after the required-variable checks:

```go
	if (cfg.SidecarURL == "") != (cfg.SidecarToken == "") {
		problems = append(problems, "PORTAL_SIDECAR_URL and PORTAL_SIDECAR_TOKEN must be set together")
	}
```

In `docker-compose.uat.yml`, under the portal `environment:` add:

```yaml
      # Reporting service: source of the Projects and Overview numbers.
      - PORTAL_SIDECAR_URL=http://sidecar:8282
      - PORTAL_SIDECAR_TOKEN=${SIDECAR_API_TOKEN:?SIDECAR_API_TOKEN must be set}
```

and add `- sidecar` to the portal `depends_on`. In `deploy/uat/README.md` add one sentence next to the sidecar description: "The portal reads project grades from the reporting service over the Docker network with `SIDECAR_API_TOKEN`."

Validate the compose file: write a throwaway env file in the scratchpad containing every variable the compose files reference with dummy values (`SERVER_IP=192.168.1.28`, `REPO_ROOT`, `REPO_VM`, `SIDECAR_API_TOKEN=x`, etc.) and run `docker compose --env-file <tmp> -f compose/minimal/docker-compose.yml -f deploy/uat/docker-compose.uat.yml config --quiet`. This only parses; it must not start anything. If a variable is reported missing, add it to the temp file.

- [ ] **Step 4: Run the full suite and the compose parse.** Expected: all `ok`, gofmt clean apart from the three known files, `config --quiet` exits 0.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/sidecarclient portal/internal/config deploy/uat
git commit -m "feat(portal): sidecar client and PORTAL_SIDECAR_* configuration"
```

---

## Self-review notes

- Spec coverage for this step: projects endpoint and grades (Tasks 1-3), portal access to the service (Task 4). Explicitly deferred to later plans: baseline counts and burn-down, per-issue SLA clocks and status flags, exception joins, Woodpecker pass rate, Prometheus trend, the screens themselves (redesign step 3).
- Types used across tasks: `grade.Counts`, `projects.Project`, `projects.Aggregate(repos, failed, reports)`, `sidecar.Latest`, `sidecarclient.ProjectsResponse` are defined once and reused with identical names.
- The score is over open-PR findings only until baseline data lands; the Projects screen must say so.
