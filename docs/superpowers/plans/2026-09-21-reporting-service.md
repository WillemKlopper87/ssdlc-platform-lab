# Reporting Service Implementation Plan (step 1 of 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a small, stateless Go reporting service (`sidecar`) that serves PR report data and Prometheus metrics, keeps one sticky gate comment per PR, and exposes dependency health; make the portal's PR report use the same report code.

**Architecture:** A second binary (`portal/cmd/sidecar`) in the existing `ssdlc-portal` Go module, reusing `giteaclient`, `woodpeckerclient` and `findings`. A new `report` package builds a report for one PR from Gitea + Woodpecker and is shared by the sidecar API and the portal handler, so there is one implementation. A poller rebuilds metrics and syncs comments every 60 s from Gitea and Woodpecker; it stores nothing durable. It is never in the merge-decision path.

**Tech Stack:** Go 1.22, standard library only (the module has no dependencies; keep it that way), Docker, Docker Compose, PowerShell (existing setup script).

**Spec:** `docs/superpowers/specs/2026-09-21-tiered-platform-design.md` (component 1 and component 5's health endpoint). This is plan 1 of 4; tiers/Prometheus/Grafana/Loki, DefectDojo, and the export/Admin-health screens are separate plans.

**One deliberate deviation from the spec.** The spec says the portal's PR report "switches to the sidecar API". This plan has the portal call the shared `report.Build` in-process instead. Same single implementation, but the portal's PR report keeps working if the sidecar is down, which fits the spec's own invariant that nothing added here can degrade existing behaviour. Task 8 edits the spec line to match.

## Global Constraints

- Go standard library only. No new module dependencies.
- The gate is never affected: the sidecar only reads Gitea/Woodpecker and writes PR comments. It never sets a commit status, approves, or merges.
- Every network failure in the sidecar is logged and retried on the next poll; none may crash the process or block the HTTP server.
- Build containers get no new tokens. The sidecar's Gitea token is the existing `gate-reporter` account's token (`REPORTER_TOKEN` in `deploy/uat/state/uat.env`).
- The sidecar's `/api/*` endpoints require `Authorization: Bearer <SIDECAR_API_TOKEN>`. `/healthz` and `/metrics` are unauthenticated and are not published outside the Docker network.
- Woodpecker's repo ID is looked up with `GET /api/repos/lookup/{owner}/{repo}`; never reuse Gitea's repo ID.
- Existing Go tests must keep passing after every task: `cd portal && go test ./...`.

## Running Go here

Go is not installed on the dev machine; run it in a container from the repo root of the worktree:

```bash
docker run --rm -v "$(pwd -W):/src" -w /src/portal golang:1.22-alpine sh -c "go vet ./... && go test ./..."
```

Git Bash needs `MSYS_NO_PATHCONV=1` in front for the volume path. Below this is written as `GOTEST` for brevity.

## File Structure

| File | Responsibility |
|---|---|
| `portal/internal/woodpeckerclient/client.go` (modify) | Add `Started`/`Finished` to `Pipeline` |
| `portal/internal/woodpeckerclient/extra.go` (create) | `Repo`, `ListRepos`, `LookupRepo`, `Agent`, `ListAgents` |
| `portal/internal/giteaclient/pulls.go` (create) | `PRDetail`, `GetPullRequest`, `ListOpenPullRequests`, issue comment methods |
| `portal/internal/report/report.go` (create) | `Report` types, `Build`, gate-state logic |
| `portal/internal/metrics/metrics.go` (create) | Prometheus text exposition, `Snapshot`, `Store` |
| `portal/internal/sidecar/collector.go` (create) | `BuildSnapshot`: reports to metrics |
| `portal/internal/sidecar/comment.go` (create) | Comment rendering and sticky sync |
| `portal/internal/sidecar/poller.go` (create) | Poll loop tying it together |
| `portal/internal/sidecar/health.go` (create) | Dependency health checks |
| `portal/internal/sidecar/config.go` (create) | Env config |
| `portal/internal/sidecar/server.go` (create) | HTTP handlers and auth |
| `portal/cmd/sidecar/main.go` (create) | Process wiring |
| `portal/internal/handlers/prreport.go` (modify) | Use `report.Build` |
| `portal/internal/handlers/prreport_test.go` (modify) | Fixtures for the new routes |
| `deploy/uat/Dockerfile`, `docker-compose.uat.yml`, `setup-uat.ps1`, `README.md` (modify) | Build, run and document the sidecar |

---

### Task 1: Client extensions

**Files:**
- Modify: `portal/internal/woodpeckerclient/client.go` (the `Pipeline` type and `ListPipelines`)
- Create: `portal/internal/woodpeckerclient/extra.go`, `portal/internal/woodpeckerclient/extra_test.go`
- Create: `portal/internal/giteaclient/pulls.go`, `portal/internal/giteaclient/pulls_test.go`

**Interfaces:**
- Produces (woodpeckerclient):
  - `Pipeline` gains `Started int64` and `Finished int64` (unix seconds; 0 if unset)
  - `type Repo struct { ID int; FullName string }`
  - `func (c *Client) ListRepos(ctx context.Context) ([]Repo, error)`
  - `func (c *Client) LookupRepo(ctx context.Context, owner, repo string) (int, error)`
  - `type Agent struct { Name string; LastContact time.Time }`
  - `func (c *Client) ListAgents(ctx context.Context) ([]Agent, error)`
- Produces (giteaclient):
  - `type PRDetail struct { Number int; Title, State, HTMLURL, Author, HeadSHA string; CreatedAt time.Time }`
  - `func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PRDetail, error)`
  - `func (c *Client) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]PRDetail, error)`
  - `type IssueComment struct { ID int64; Body, Author string }`
  - `func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error)`
  - `func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error`
  - `func (c *Client) EditIssueComment(ctx context.Context, owner, repo string, id int64, body string) error`

- [ ] **Step 1: Write the failing Woodpecker tests**

Create `portal/internal/woodpeckerclient/extra_test.go`:

```go
package woodpeckerclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLookupRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/repos/lookup/ssdlc/pilot-app" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("auth header = %q", got)
		}
		w.Write([]byte(`{"id":7}`))
	}))
	defer srv.Close()

	id, err := New(srv.URL, "tok").LookupRepo(context.Background(), "ssdlc", "pilot-app")
	if err != nil {
		t.Fatal(err)
	}
	if id != 7 {
		t.Errorf("id = %d, want 7", id)
	}
}

func TestLookupRepo_ZeroIDIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "tok").LookupRepo(context.Background(), "o", "r"); err == nil {
		t.Fatal("expected an error for a response with no id")
	}
}

func TestListRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/repos" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`[{"id":7,"full_name":"ssdlc/pilot-app"},{"id":9,"full_name":"other/x"}]`))
	}))
	defer srv.Close()

	repos, err := New(srv.URL, "tok").ListRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].ID != 7 || repos[0].FullName != "ssdlc/pilot-app" {
		t.Errorf("repos = %+v", repos)
	}
}

func TestListAgents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`[{"name":"agent-1","last_contact":1790000000}]`))
	}))
	defer srv.Close()

	agents, err := New(srv.URL, "tok").ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Name != "agent-1" || !agents[0].LastContact.Equal(time.Unix(1790000000, 0)) {
		t.Errorf("agents = %+v", agents)
	}
}

func TestListPipelines_IncludesTimestamps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"number":5,"status":"success","commit":"abc","event":"pull_request","started":100,"finished":160}]`))
	}))
	defer srv.Close()

	ps, err := New(srv.URL, "tok").ListPipelines(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if ps[0].Started != 100 || ps[0].Finished != 160 {
		t.Errorf("pipeline = %+v", ps[0])
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST` (or `go test ./internal/woodpeckerclient/`)
Expected: FAIL, compile errors: `LookupRepo`, `ListRepos`, `ListAgents` undefined; `Started` unknown field.

- [ ] **Step 3: Implement the Woodpecker changes**

In `portal/internal/woodpeckerclient/client.go`, replace the `Pipeline` type and `ListPipelines` with:

```go
type Pipeline struct {
	Number   int
	Status   string
	Commit   string
	Event    string
	Started  int64
	Finished int64
}

func (c *Client) ListPipelines(ctx context.Context, repoID int) ([]Pipeline, error) {
	var raw []struct {
		Number   int    `json:"number"`
		Status   string `json:"status"`
		Commit   string `json:"commit"`
		Event    string `json:"event"`
		Started  int64  `json:"started"`
		Finished int64  `json:"finished"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/repos/%d/pipelines", repoID), &raw); err != nil {
		return nil, err
	}
	out := make([]Pipeline, 0, len(raw))
	for _, p := range raw {
		out = append(out, Pipeline{
			Number: p.Number, Status: p.Status, Commit: p.Commit, Event: p.Event,
			Started: p.Started, Finished: p.Finished,
		})
	}
	return out, nil
}
```

Create `portal/internal/woodpeckerclient/extra.go`:

```go
package woodpeckerclient

import (
	"context"
	"fmt"
	"time"
)

// Repo is a repository activated in Woodpecker.
type Repo struct {
	ID       int
	FullName string
}

// ListRepos returns the repositories the token's user can see that are
// active in Woodpecker.
func (c *Client) ListRepos(ctx context.Context) ([]Repo, error) {
	var raw []struct {
		ID       int    `json:"id"`
		FullName string `json:"full_name"`
	}
	if err := c.get(ctx, "/api/user/repos", &raw); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(raw))
	for _, r := range raw {
		out = append(out, Repo{ID: r.ID, FullName: r.FullName})
	}
	return out, nil
}

// LookupRepo resolves Woodpecker's own repo ID for owner/repo. Gitea's repo
// ID is a different number and must never be used here.
func (c *Client) LookupRepo(ctx context.Context, owner, repo string) (int, error) {
	var raw struct {
		ID int `json:"id"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/repos/lookup/%s/%s", owner, repo), &raw); err != nil {
		return 0, err
	}
	if raw.ID == 0 {
		return 0, fmt.Errorf("woodpecker: repo %s/%s not found or not active", owner, repo)
	}
	return raw.ID, nil
}

// Agent is a registered pipeline agent.
type Agent struct {
	Name        string
	LastContact time.Time
}

// ListAgents needs an admin token.
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	var raw []struct {
		Name        string `json:"name"`
		LastContact int64  `json:"last_contact"`
	}
	if err := c.get(ctx, "/api/agents", &raw); err != nil {
		return nil, err
	}
	out := make([]Agent, 0, len(raw))
	for _, a := range raw {
		out = append(out, Agent{Name: a.Name, LastContact: time.Unix(a.LastContact, 0)})
	}
	return out, nil
}
```

- [ ] **Step 4: Run to verify the Woodpecker tests pass**

Run: `GOTEST`
Expected: PASS for `./internal/woodpeckerclient/`; existing packages still pass.

- [ ] **Step 5: Write the failing Gitea tests**

Create `portal/internal/giteaclient/pulls_test.go`:

```go
package giteaclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const prJSON = `{"number":12,"title":"Add X","html_url":"http://g/o/r/pulls/12","state":"open",
"user":{"login":"dev2"},"head":{"sha":"abc123","ref":"x"},"created_at":"2026-09-01T10:00:00Z"}`

func TestGetPullRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/o/r/pulls/12" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(prJSON))
	}))
	defer srv.Close()

	pr, err := New(srv.URL, "tok").GetPullRequest(context.Background(), "o", "r", 12)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 12 || pr.HeadSHA != "abc123" || pr.Author != "dev2" || pr.State != "open" || pr.CreatedAt.IsZero() {
		t.Errorf("pr = %+v", pr)
	}
}

func TestGetPullRequest_NotFoundIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "tok").GetPullRequest(context.Background(), "o", "r", 1); err == nil {
		t.Fatal("expected an error on 404")
	}
}

func TestListOpenPullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/o/r/pulls" || r.URL.Query().Get("state") != "open" {
			t.Fatalf("unexpected request %s", r.URL.String())
		}
		w.Write([]byte("[" + prJSON + "]"))
	}))
	defer srv.Close()

	prs, err := New(srv.URL, "tok").ListOpenPullRequests(context.Background(), "o", "r")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Number != 12 {
		t.Errorf("prs = %+v", prs)
	}
}

func TestIssueComments(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		if r.Method == http.MethodGet {
			w.Write([]byte(`[{"id":5,"body":"hello","user":{"login":"gate-reporter"}}]`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	ctx := context.Background()

	cs, err := c.ListIssueComments(ctx, "o", "r", 12)
	if err != nil || len(cs) != 1 || cs[0].ID != 5 || cs[0].Author != "gate-reporter" || cs[0].Body != "hello" {
		t.Fatalf("list = %+v, %v", cs, err)
	}
	if gotPath != "/api/v1/repos/o/r/issues/12/comments" {
		t.Errorf("list path = %s", gotPath)
	}

	if err := c.CreateIssueComment(ctx, "o", "r", 12, "new"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/repos/o/r/issues/12/comments" || !strings.Contains(gotBody, `"body":"new"`) {
		t.Errorf("create = %s %s %s", gotMethod, gotPath, gotBody)
	}

	if err := c.EditIssueComment(ctx, "o", "r", 5, "edited"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/v1/repos/o/r/issues/comments/5" || !strings.Contains(gotBody, `"body":"edited"`) {
		t.Errorf("edit = %s %s %s", gotMethod, gotPath, gotBody)
	}
}
```

- [ ] **Step 6: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, `GetPullRequest`, `ListOpenPullRequests`, `ListIssueComments` etc. undefined.

- [ ] **Step 7: Implement the Gitea additions**

Create `portal/internal/giteaclient/pulls.go`:

```go
package giteaclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PRDetail is one pull request's identity and head commit.
type PRDetail struct {
	Number    int
	Title     string
	State     string
	HTMLURL   string
	Author    string
	HeadSHA   string
	CreatedAt time.Time
}

func toDetail(p giteaPR) PRDetail {
	return PRDetail{
		Number: p.Number, Title: p.Title, State: p.State, HTMLURL: p.HTMLURL,
		Author: p.User.Login, HeadSHA: p.Head.SHA, CreatedAt: p.CreatedAt,
	}
}

func statusError(method, path string, status int) error {
	if status >= 300 {
		return fmt.Errorf("gitea: %s %s: status %d", method, path, status)
	}
	return nil
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PRDetail, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d", owner, repo, number)
	var pr giteaPR
	status, err := c.do(ctx, http.MethodGet, path, nil, &pr)
	if err != nil {
		return PRDetail{}, err
	}
	if err := statusError(http.MethodGet, path, status); err != nil {
		return PRDetail{}, err
	}
	return toDetail(pr), nil
}

func (c *Client) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]PRDetail, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls?state=open&limit=50", owner, repo)
	var prs []giteaPR
	status, err := c.do(ctx, http.MethodGet, path, nil, &prs)
	if err != nil {
		return nil, err
	}
	if err := statusError(http.MethodGet, path, status); err != nil {
		return nil, err
	}
	out := make([]PRDetail, 0, len(prs))
	for _, p := range prs {
		out = append(out, toDetail(p))
	}
	return out, nil
}

// IssueComment is a comment on an issue or pull request.
type IssueComment struct {
	ID     int64
	Body   string
	Author string
}

func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, number)
	var raw []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	status, err := c.do(ctx, http.MethodGet, path, nil, &raw)
	if err != nil {
		return nil, err
	}
	if err := statusError(http.MethodGet, path, status); err != nil {
		return nil, err
	}
	out := make([]IssueComment, 0, len(raw))
	for _, r := range raw {
		out = append(out, IssueComment{ID: r.ID, Body: r.Body, Author: r.User.Login})
	}
	return out, nil
}

func (c *Client) sendBody(ctx context.Context, method, path, body string) error {
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}
	status, err := c.do(ctx, method, path, bytes.NewReader(payload), nil)
	if err != nil {
		return err
	}
	return statusError(method, path, status)
}

func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	return c.sendBody(ctx, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, number), body)
}

func (c *Client) EditIssueComment(ctx context.Context, owner, repo string, id int64, body string) error {
	return c.sendBody(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/repos/%s/%s/issues/comments/%d", owner, repo, id), body)
}
```

- [ ] **Step 8: Run to verify everything passes**

Run: `GOTEST`
Expected: PASS for all packages (`go vet` clean).

- [ ] **Step 9: Commit**

```bash
git add portal/internal/woodpeckerclient portal/internal/giteaclient
git commit -m "feat(portal): client methods for repo lookup, agents, PRs and issue comments"
```

---

### Task 2: The `report` package

**Files:**
- Create: `portal/internal/report/report.go`, `portal/internal/report/report_test.go`

**Interfaces:**
- Consumes: Task 1 (`PRDetail`, `LookupRepo`, `Pipeline.Started/Finished`), existing `findings.Parse`, `giteaclient.GetCombinedStatus`, `woodpeckerclient.ListPipelines/ListSteps/GetStepLog`
- Produces:
  - `type Gitea interface { GetPullRequest(ctx, owner, repo string, number int) (giteaclient.PRDetail, error); GetCombinedStatus(ctx, owner, repo, sha string) ([]giteaclient.CommitStatus, error) }`
  - `type Woodpecker interface { LookupRepo(ctx, owner, repo string) (int, error); ListPipelines(ctx, repoID int) ([]woodpeckerclient.Pipeline, error); ListSteps(ctx, repoID, pipelineNumber int) ([]woodpeckerclient.Step, error); GetStepLog(ctx, repoID, pipelineNumber, stepID int) (string, error) }`
  - `type Summary struct { Critical, High, Medium, Low int }` (json: `critical`, `high`, `medium`, `low`)
  - `type Finding struct { Category, Severity, Tool, RuleID, Location, Description string }` (json snake_case)
  - `type Report struct { Repo string; Number int; Title, State, HTMLURL, Author, HeadSHA string; CreatedAt time.Time; Gate string; MergeBlocked bool; Summary Summary; Findings []Finding; PipelineNumber int; PipelineStarted, PipelineFinished int64; Notes []string; GeneratedAt time.Time }`
  - `func Build(ctx context.Context, g Gitea, w Woodpecker, owner, repo string, number int, now time.Time) (Report, error)`
  - `func GateState(statuses []giteaclient.CommitStatus) string` returning `"failure"`, `"pending"`, `"success"` or `"none"`

- [ ] **Step 1: Write the failing tests**

Create `portal/internal/report/report_test.go`:

```go
package report

import (
	"context"
	"errors"
	"testing"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/woodpeckerclient"
)

type fakeGitea struct {
	pr       giteaclient.PRDetail
	prErr    error
	statuses []giteaclient.CommitStatus
	stErr    error
}

func (f fakeGitea) GetPullRequest(ctx context.Context, owner, repo string, number int) (giteaclient.PRDetail, error) {
	return f.pr, f.prErr
}
func (f fakeGitea) GetCombinedStatus(ctx context.Context, owner, repo, sha string) ([]giteaclient.CommitStatus, error) {
	return f.statuses, f.stErr
}

type fakeWP struct {
	repoID    int
	lookupErr error
	pipelines []woodpeckerclient.Pipeline
	steps     []woodpeckerclient.Step
	log       string
}

func (f fakeWP) LookupRepo(ctx context.Context, owner, repo string) (int, error) {
	return f.repoID, f.lookupErr
}
func (f fakeWP) ListPipelines(ctx context.Context, repoID int) ([]woodpeckerclient.Pipeline, error) {
	return f.pipelines, nil
}
func (f fakeWP) ListSteps(ctx context.Context, repoID, n int) ([]woodpeckerclient.Step, error) {
	return f.steps, nil
}
func (f fakeWP) GetStepLog(ctx context.Context, repoID, n, stepID int) (string, error) {
	return f.log, nil
}

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func basePR() giteaclient.PRDetail {
	return giteaclient.PRDetail{Number: 12, Title: "Add X", State: "open", HeadSHA: "abc", Author: "dev2", HTMLURL: "http://g/o/r/pulls/12"}
}

const blockedLog = "  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- test finding\n" +
	"  WARN  MEDIUM [semgrep/weak-hash] app.py:12 -- md5\n" +
	"  BASELINE  [semgrep/old] old.py:1 -- legacy\n" +
	"policy-eval: 2 finding(s) normalized -- critical=1 high=0 medium=1 low=0\n"

func TestBuild_BlockedPRWithFindings(t *testing.T) {
	g := fakeGitea{pr: basePR(), statuses: []giteaclient.CommitStatus{{State: "failure", Context: "ssdlc/security-gate/pr/x"}}}
	w := fakeWP{
		repoID:    7,
		pipelines: []woodpeckerclient.Pipeline{{Number: 5, Commit: "abc", Started: 100, Finished: 160}},
		steps:     []woodpeckerclient.Step{{ID: 59, Name: "policy-eval-findings"}},
		log:       blockedLog,
	}
	r, err := Build(context.Background(), g, w, "o", "r", 12, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Gate != "failure" || !r.MergeBlocked {
		t.Errorf("gate=%q blocked=%v", r.Gate, r.MergeBlocked)
	}
	if r.Summary.Critical != 1 || r.Summary.Medium != 1 {
		t.Errorf("summary = %+v", r.Summary)
	}
	if len(r.Findings) != 3 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	// blocking Critical first, then the warning, then the baselined one (no severity)
	if r.Findings[0].Severity != "CRITICAL" || r.Findings[1].Severity != "MEDIUM" || r.Findings[2].Category != "baselined" {
		t.Errorf("order = %+v", r.Findings)
	}
	if r.PipelineNumber != 5 || r.PipelineStarted != 100 || r.PipelineFinished != 160 {
		t.Errorf("pipeline fields = %d %d %d", r.PipelineNumber, r.PipelineStarted, r.PipelineFinished)
	}
	if r.Repo != "o/r" || r.HeadSHA != "abc" || r.GeneratedAt != now {
		t.Errorf("identity = %+v", r)
	}
}

func TestBuild_NoPipelineForHeadCommitIsNotAnError(t *testing.T) {
	g := fakeGitea{pr: basePR()}
	w := fakeWP{repoID: 7, pipelines: []woodpeckerclient.Pipeline{{Number: 5, Commit: "other"}}}
	r, err := Build(context.Background(), g, w, "o", "r", 12, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Gate != "none" || r.MergeBlocked || len(r.Findings) != 0 {
		t.Errorf("report = %+v", r)
	}
	if len(r.Notes) == 0 {
		t.Error("expected a note explaining that no pipeline was found")
	}
}

func TestBuild_WoodpeckerLookupFailureBecomesANote(t *testing.T) {
	g := fakeGitea{pr: basePR()}
	w := fakeWP{lookupErr: errors.New("boom")}
	r, err := Build(context.Background(), g, w, "o", "r", 12, now)
	if err != nil {
		t.Fatalf("a Woodpecker failure must not fail the report: %v", err)
	}
	if len(r.Notes) == 0 || r.Findings == nil {
		t.Errorf("report = %+v", r)
	}
}

func TestBuild_PRLookupFailureIsAnError(t *testing.T) {
	g := fakeGitea{prErr: errors.New("gone")}
	if _, err := Build(context.Background(), g, fakeWP{}, "o", "r", 12, now); err == nil {
		t.Fatal("expected an error when the pull request cannot be loaded")
	}
}

func TestGateState(t *testing.T) {
	gate := func(state string) giteaclient.CommitStatus {
		return giteaclient.CommitStatus{State: state, Context: "ssdlc/security-gate/pr/x"}
	}
	other := giteaclient.CommitStatus{State: "failure", Context: "ci/other"}
	cases := []struct {
		name string
		in   []giteaclient.CommitStatus
		want string
	}{
		{"empty", nil, "none"},
		{"unrelated context ignored", []giteaclient.CommitStatus{other}, "none"},
		{"success", []giteaclient.CommitStatus{gate("success")}, "success"},
		{"pending beats success", []giteaclient.CommitStatus{gate("success"), gate("pending")}, "pending"},
		{"failure beats all", []giteaclient.CommitStatus{gate("success"), gate("pending"), gate("failure")}, "failure"},
		{"error counts as failure", []giteaclient.CommitStatus{gate("error")}, "failure"},
	}
	for _, c := range cases {
		if got := GateState(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, package `report` has no non-test files / `Build` undefined.

- [ ] **Step 3: Implement**

Create `portal/internal/report/report.go`:

```go
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
	GeneratedAt      time.Time `json:"generated_at"`
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
		return r, nil
	}
	pipelines, err := w.ListPipelines(ctx, repoID)
	if err != nil {
		r.Notes = append(r.Notes, "findings unavailable: "+err.Error())
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
			break
		}
		for _, s := range steps {
			if s.Name != findingsStep {
				continue
			}
			log, err := w.GetStepLog(ctx, repoID, p.Number, s.ID)
			if err != nil {
				r.Notes = append(r.Notes, "findings log unavailable: "+err.Error())
				continue
			}
			parsed, sum, err := findings.Parse(log)
			if err != nil {
				r.Notes = append(r.Notes, "findings log unreadable: "+err.Error())
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
```

- [ ] **Step 4: Run to verify pass**

Run: `GOTEST`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/report
git commit -m "feat(portal): shared report package (gate state, findings, pipeline timing)"
```

---

### Task 3: Metrics exposition

**Files:**
- Create: `portal/internal/metrics/metrics.go`, `portal/internal/metrics/metrics_test.go`

**Interfaces:**
- Produces:
  - `func NewSnapshot() *Snapshot`
  - `func (s *Snapshot) Gauge(name, help string, labels map[string]string, value float64)`
  - `func (s *Snapshot) WriteTo(w io.Writer) (int64, error)`
  - `type Store struct` with `func (s *Store) Set(*Snapshot)` and `func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request)`

- [ ] **Step 1: Write the failing test**

Create `portal/internal/metrics/metrics_test.go`:

```go
package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSnapshotExposition(t *testing.T) {
	s := NewSnapshot()
	s.Gauge("ssdlc_open_pull_requests", "Open pull requests.", map[string]string{"repo": "o/r", "gate": "failure"}, 2)
	s.Gauge("ssdlc_open_pull_requests", "Open pull requests.", map[string]string{"repo": "o/a", "gate": "success"}, 1)
	s.Gauge("ssdlc_sidecar_poll_ok", "1 when the last poll succeeded.", nil, 1)

	var b strings.Builder
	if _, err := s.WriteTo(&b); err != nil {
		t.Fatal(err)
	}
	want := "# HELP ssdlc_open_pull_requests Open pull requests.\n" +
		"# TYPE ssdlc_open_pull_requests gauge\n" +
		"ssdlc_open_pull_requests{gate=\"failure\",repo=\"o/r\"} 2\n" +
		"ssdlc_open_pull_requests{gate=\"success\",repo=\"o/a\"} 1\n" +
		"# HELP ssdlc_sidecar_poll_ok 1 when the last poll succeeded.\n" +
		"# TYPE ssdlc_sidecar_poll_ok gauge\n" +
		"ssdlc_sidecar_poll_ok 1\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestLabelValuesAreEscaped(t *testing.T) {
	s := NewSnapshot()
	s.Gauge("m", "h", map[string]string{"k": "a\"b\\c\nd"}, 1)
	var b strings.Builder
	s.WriteTo(&b)
	if !strings.Contains(b.String(), `m{k="a\"b\\c\nd"} 1`) {
		t.Errorf("escaping wrong: %q", b.String())
	}
}

func TestStoreServesLatestSnapshot(t *testing.T) {
	var st Store
	rec := httptest.NewRecorder()
	st.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("empty store: %d %q", rec.Code, rec.Body.String())
	}

	s := NewSnapshot()
	s.Gauge("x", "h", nil, 3)
	st.Set(s)
	rec = httptest.NewRecorder()
	st.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "x 3\n") {
		t.Errorf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("content-type = %q", ct)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, `NewSnapshot` undefined.

- [ ] **Step 3: Implement**

Create `portal/internal/metrics/metrics.go`:

```go
// Package metrics writes the Prometheus text exposition format for gauges.
// It is deliberately tiny so the module keeps zero dependencies; the sidecar
// only ever exports gauges rebuilt from scratch on every poll.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type sample struct {
	labels string // rendered, sorted: {a="b",c="d"} or ""
	value  float64
}

type family struct {
	help    string
	samples []sample
}

type Snapshot struct {
	fams map[string]*family
}

func NewSnapshot() *Snapshot { return &Snapshot{fams: map[string]*family{}} }

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func renderLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+`="`+labelEscaper.Replace(labels[k])+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (s *Snapshot) Gauge(name, help string, labels map[string]string, value float64) {
	f, ok := s.fams[name]
	if !ok {
		f = &family{help: help}
		s.fams[name] = f
	}
	f.samples = append(f.samples, sample{labels: renderLabels(labels), value: value})
}

func (s *Snapshot) WriteTo(w io.Writer) (int64, error) {
	names := make([]string, 0, len(s.fams))
	for n := range s.fams {
		names = append(names, n)
	}
	sort.Strings(names)
	var total int64
	for _, n := range names {
		f := s.fams[n]
		sort.SliceStable(f.samples, func(i, j int) bool { return f.samples[i].labels < f.samples[j].labels })
		c, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", n, f.help, n)
		total += int64(c)
		if err != nil {
			return total, err
		}
		for _, sm := range f.samples {
			c, err := fmt.Fprintf(w, "%s%s %s\n", n, sm.labels, strconv.FormatFloat(sm.value, 'g', -1, 64))
			total += int64(c)
			if err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

// Store holds the most recent Snapshot and serves it.
type Store struct {
	mu   sync.RWMutex
	snap *Snapshot
}

func (s *Store) Set(snap *Snapshot) {
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
}

func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	snap := s.snap
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if snap != nil {
		snap.WriteTo(w)
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOTEST`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/metrics
git commit -m "feat(portal): minimal Prometheus text exposition"
```

---

### Task 4: Collector and poller

**Files:**
- Create: `portal/internal/sidecar/collector.go`, `portal/internal/sidecar/poller.go`, `portal/internal/sidecar/poller_test.go`

**Interfaces:**
- Consumes: Tasks 1-3
- Produces:
  - `func BuildSnapshot(reports []report.Report, now time.Time, pollOK bool) *metrics.Snapshot`
  - `type Backends struct { Gitea interface { report.Gitea; ListOpenPullRequests(ctx context.Context, owner, repo string) ([]giteaclient.PRDetail, error) }; Woodpecker interface { report.Woodpecker; ListRepos(ctx context.Context) ([]woodpeckerclient.Repo, error) } }`
  - `type Poller struct { B Backends; Org string; Store *metrics.Store; Now func() time.Time; Log *log.Logger; AfterReport func(ctx context.Context, r report.Report) }` (`AfterReport` is a hook Task 5 fills in; nil means none)
  - `func (p *Poller) Once(ctx context.Context) error`
  - `func (p *Poller) Run(ctx context.Context, interval time.Duration)`

- [ ] **Step 1: Write the failing test**

Create `portal/internal/sidecar/poller_test.go`:

```go
package sidecar

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/report"
	"ssdlc-portal/internal/woodpeckerclient"
)

type fakeGitea struct {
	prs      map[string][]giteaclient.PRDetail // key "owner/repo"
	err      error
	failRepo string // listing this repo's PRs fails
}

func (f fakeGitea) GetPullRequest(ctx context.Context, owner, repo string, n int) (giteaclient.PRDetail, error) {
	for _, p := range f.prs[owner+"/"+repo] {
		if p.Number == n {
			return p, nil
		}
	}
	return giteaclient.PRDetail{}, errors.New("not found")
}
func (f fakeGitea) GetCombinedStatus(ctx context.Context, owner, repo, sha string) ([]giteaclient.CommitStatus, error) {
	return []giteaclient.CommitStatus{{State: "failure", Context: "ssdlc/security-gate/pr/x"}}, nil
}
func (f fakeGitea) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]giteaclient.PRDetail, error) {
	if owner+"/"+repo == f.failRepo {
		return nil, errors.New("boom")
	}
	return f.prs[owner+"/"+repo], f.err
}

type fakeWP struct {
	repos   []woodpeckerclient.Repo
	reposEr error
}

func (f fakeWP) LookupRepo(ctx context.Context, owner, repo string) (int, error) { return 7, nil }
func (f fakeWP) ListPipelines(ctx context.Context, id int) ([]woodpeckerclient.Pipeline, error) {
	return []woodpeckerclient.Pipeline{{Number: 5, Commit: "abc", Started: 1000, Finished: 1060}}, nil
}
func (f fakeWP) ListSteps(ctx context.Context, id, n int) ([]woodpeckerclient.Step, error) {
	return []woodpeckerclient.Step{{ID: 59, Name: "policy-eval-findings"}}, nil
}
func (f fakeWP) GetStepLog(ctx context.Context, id, n, s int) (string, error) {
	return "  FAIL  CRITICAL [gitleaks/aws-access-token] c.py:5 -- k\n" +
		"policy-eval: 1 finding(s) normalized -- critical=1 high=0 medium=0 low=0\n", nil
}
func (f fakeWP) ListRepos(ctx context.Context) ([]woodpeckerclient.Repo, error) {
	return f.repos, f.reposEr
}

func newPoller(g fakeGitea, w fakeWP) (*Poller, *metrics.Store) {
	st := &metrics.Store{}
	return &Poller{
		B:     Backends{Gitea: g, Woodpecker: w},
		Org:   "ssdlc",
		Store: st,
		Now:   func() time.Time { return time.Unix(2000, 0) },
		Log:   log.New(io.Discard, "", 0),
	}, st
}

func scrape(st *metrics.Store) string {
	rec := newRecorder()
	st.ServeHTTP(rec, newRequest())
	return rec.Body.String()
}

func TestPollerOncePublishesMetrics(t *testing.T) {
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{
		"ssdlc/pilot-app": {{Number: 3, Title: "t", State: "open", HeadSHA: "abc", CreatedAt: time.Unix(900, 0)}},
	}}
	w := fakeWP{repos: []woodpeckerclient.Repo{
		{ID: 7, FullName: "ssdlc/pilot-app"},
		{ID: 9, FullName: "someone-else/not-ours"}, // outside the org: ignored
	}}
	p, st := newPoller(g, w)
	if err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := scrape(st)
	for _, want := range []string{
		`ssdlc_open_pull_requests{gate="failure",repo="ssdlc/pilot-app"} 1`,
		`ssdlc_pull_requests_blocked{repo="ssdlc/pilot-app"} 1`,
		`ssdlc_open_findings{category="blocking",repo="ssdlc/pilot-app",severity="CRITICAL",tool="gitleaks"} 1`,
		`ssdlc_pr_time_to_verdict_seconds_avg{repo="ssdlc/pilot-app"} 160`,
		`ssdlc_pipeline_duration_seconds_avg{repo="ssdlc/pilot-app"} 60`,
		`ssdlc_sidecar_poll_ok 1`,
		`ssdlc_sidecar_last_poll_timestamp_seconds 2000`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "someone-else") {
		t.Errorf("repos outside the org must be ignored:\n%s", out)
	}
}

func TestPollerRepoListFailureMarksPollNotOK(t *testing.T) {
	p, st := newPoller(fakeGitea{}, fakeWP{reposEr: errors.New("woodpecker down")})
	if err := p.Once(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if out := scrape(st); !strings.Contains(out, "ssdlc_sidecar_poll_ok 0") {
		t.Errorf("out:\n%s", out)
	}
}

func TestPollerOneRepoFailingDoesNotHideTheOthers(t *testing.T) {
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{
		"ssdlc/good": {{Number: 1, HeadSHA: "abc", CreatedAt: time.Unix(900, 0)}},
	}}
	w := fakeWP{repos: []woodpeckerclient.Repo{{ID: 1, FullName: "ssdlc/broken"}, {ID: 2, FullName: "ssdlc/good"}}}
	p, st := newPoller(errOn("ssdlc/broken", g), w)
	if err := p.Once(context.Background()); err != nil {
		t.Fatalf("a single repo failing must not fail the poll: %v", err)
	}
	out := scrape(st)
	if !strings.Contains(out, `repo="ssdlc/good"`) || !strings.Contains(out, "ssdlc_sidecar_poll_ok 0") {
		t.Errorf("want good repo present and poll_ok 0:\n%s", out)
	}
}

// errOn wraps a fakeGitea so listing one repo's PRs fails.
func errOn(repo string, g fakeGitea) fakeGitea {
	g.failRepo = repo
	return g
}

func TestPollerCallsAfterReportHook(t *testing.T) {
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{"ssdlc/r": {{Number: 4, HeadSHA: "abc"}}}}
	w := fakeWP{repos: []woodpeckerclient.Repo{{ID: 1, FullName: "ssdlc/r"}}}
	p, _ := newPoller(g, w)
	var got []int
	p.AfterReport = func(ctx context.Context, r report.Report) { got = append(got, r.Number) }
	if err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 4 {
		t.Errorf("hook calls = %v", got)
	}
}
```

The test helpers `newRecorder()` and `newRequest()` live in a small file in the same package:

Create `portal/internal/sidecar/helpers_test.go`:

```go
package sidecar

import (
	"net/http"
	"net/http/httptest"
)

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
func newRequest() *http.Request              { return httptest.NewRequest("GET", "/metrics", nil) }
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, `Poller`, `Backends` undefined.

- [ ] **Step 3: Implement the collector**

Create `portal/internal/sidecar/collector.go`:

```go
package sidecar

import (
	"time"

	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/report"
)

// BuildSnapshot turns the current open-PR reports into gauges. Everything is
// recomputed from Gitea/Woodpecker on each poll, so the service holds no state
// that can be lost across a restart.
func BuildSnapshot(reports []report.Report, now time.Time, pollOK bool) *metrics.Snapshot {
	s := metrics.NewSnapshot()

	type key struct{ repo, gate string }
	open := map[key]float64{}
	blocked := map[string]float64{}
	type fkey struct{ repo, category, severity, tool string }
	fcount := map[fkey]float64{}
	verdictSum, verdictN := map[string]float64{}, map[string]float64{}
	durSum, durN := map[string]float64{}, map[string]float64{}

	for _, r := range reports {
		open[key{r.Repo, r.Gate}]++
		if _, ok := blocked[r.Repo]; !ok {
			blocked[r.Repo] = 0
		}
		if r.MergeBlocked {
			blocked[r.Repo]++
		}
		for _, f := range r.Findings {
			sev := f.Severity
			if sev == "" {
				sev = "n/a"
			}
			fcount[fkey{r.Repo, f.Category, sev, f.Tool}]++
		}
		if r.PipelineFinished > 0 {
			if v := float64(r.PipelineFinished - r.CreatedAt.Unix()); v >= 0 {
				verdictSum[r.Repo] += v
				verdictN[r.Repo]++
			}
			if r.PipelineStarted > 0 && r.PipelineFinished >= r.PipelineStarted {
				durSum[r.Repo] += float64(r.PipelineFinished - r.PipelineStarted)
				durN[r.Repo]++
			}
		}
	}

	for k, n := range open {
		s.Gauge("ssdlc_open_pull_requests", "Open pull requests by repository and gate result.",
			map[string]string{"repo": k.repo, "gate": k.gate}, n)
	}
	for repo, n := range blocked {
		s.Gauge("ssdlc_pull_requests_blocked", "Open pull requests the gate is blocking.",
			map[string]string{"repo": repo}, n)
	}
	for k, n := range fcount {
		s.Gauge("ssdlc_open_findings", "Findings on open pull requests.",
			map[string]string{"repo": k.repo, "category": k.category, "severity": k.severity, "tool": k.tool}, n)
	}
	for repo, sum := range verdictSum {
		s.Gauge("ssdlc_pr_time_to_verdict_seconds_avg", "Average seconds from PR creation to a finished gate run.",
			map[string]string{"repo": repo}, sum/verdictN[repo])
	}
	for repo, sum := range durSum {
		s.Gauge("ssdlc_pipeline_duration_seconds_avg", "Average gate pipeline duration for open pull requests.",
			map[string]string{"repo": repo}, sum/durN[repo])
	}

	ok := 0.0
	if pollOK {
		ok = 1
	}
	s.Gauge("ssdlc_sidecar_poll_ok", "1 when every repository was read successfully in the last poll.", nil, ok)
	s.Gauge("ssdlc_sidecar_last_poll_timestamp_seconds", "Unix time of the last completed poll.", nil, float64(now.Unix()))
	return s
}
```

- [ ] **Step 4: Implement the poller**

Create `portal/internal/sidecar/poller.go`:

```go
package sidecar

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/report"
	"ssdlc-portal/internal/woodpeckerclient"
)

type Backends struct {
	Gitea interface {
		report.Gitea
		ListOpenPullRequests(ctx context.Context, owner, repo string) ([]giteaclient.PRDetail, error)
	}
	Woodpecker interface {
		report.Woodpecker
		ListRepos(ctx context.Context) ([]woodpeckerclient.Repo, error)
	}
}

type Poller struct {
	B     Backends
	Org   string
	Store *metrics.Store
	Now   func() time.Time
	Log   *log.Logger
	// AfterReport, when set, is called for every successfully built report
	// (the sticky-comment sync hooks in here). Its failures are its own to log.
	AfterReport func(ctx context.Context, r report.Report)
}

// Once performs a single poll. It returns an error only when the repository
// list itself cannot be read; a single repository failing is logged, marks
// ssdlc_sidecar_poll_ok 0 and leaves the others intact.
func (p *Poller) Once(ctx context.Context) error {
	now := p.Now()
	repos, err := p.B.Woodpecker.ListRepos(ctx)
	if err != nil {
		p.Store.Set(BuildSnapshot(nil, now, false))
		return fmt.Errorf("sidecar: list repositories: %w", err)
	}

	var reports []report.Report
	allOK := true
	prefix := p.Org + "/"
	for _, repo := range repos {
		if !strings.HasPrefix(repo.FullName, prefix) {
			continue
		}
		owner, name, _ := strings.Cut(repo.FullName, "/")
		prs, err := p.B.Gitea.ListOpenPullRequests(ctx, owner, name)
		if err != nil {
			p.Log.Printf("poll: list PRs for %s: %v", repo.FullName, err)
			allOK = false
			continue
		}
		for _, pr := range prs {
			r, err := report.Build(ctx, p.B.Gitea, p.B.Woodpecker, owner, name, pr.Number, now)
			if err != nil {
				p.Log.Printf("poll: report for %s#%d: %v", repo.FullName, pr.Number, err)
				allOK = false
				continue
			}
			reports = append(reports, r)
			if p.AfterReport != nil {
				p.AfterReport(ctx, r)
			}
		}
	}
	p.Store.Set(BuildSnapshot(reports, now, allOK))
	return nil
}

// Run polls immediately and then every interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	for {
		if err := p.Once(ctx); err != nil {
			p.Log.Print(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
```

- [ ] **Step 5: Run to verify pass**

Run: `GOTEST`
Expected: PASS (all sidecar tests, plus everything from Tasks 1-3).

- [ ] **Step 6: Commit**

```bash
git add portal/internal/sidecar
git commit -m "feat(portal): sidecar metrics collector and poller"
```

---

### Task 5: Sticky PR comment

**Files:**
- Create: `portal/internal/sidecar/comment.go`, `portal/internal/sidecar/comment_test.go`

**Interfaces:**
- Consumes: Task 1 comment methods, Task 2 `report.Report`
- Produces:
  - `const CommentMarker = "<!-- ssdlc-gate-report -->"`
  - `func RenderComment(r report.Report, portalURL string) string`
  - `type CommentAPI interface { ListIssueComments(ctx, owner, repo string, number int) ([]giteaclient.IssueComment, error); CreateIssueComment(ctx, owner, repo string, number int, body string) error; EditIssueComment(ctx, owner, repo string, id int64, body string) error }`
  - `func SyncComment(ctx context.Context, api CommentAPI, botLogin, owner, repo string, number int, body string) error`
  - `func CommentHook(api CommentAPI, botLogin, portalURL string, log *log.Logger) func(ctx context.Context, r report.Report)` (the value assigned to `Poller.AfterReport`)

- [ ] **Step 1: Write the failing tests**

Create `portal/internal/sidecar/comment_test.go`:

```go
package sidecar

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/report"
)

type fakeCommentAPI struct {
	comments []giteaclient.IssueComment
	created  []string
	edited   map[int64]string
	failList bool
}

func (f *fakeCommentAPI) ListIssueComments(ctx context.Context, o, r string, n int) ([]giteaclient.IssueComment, error) {
	if f.failList {
		return nil, errors.New("boom")
	}
	return f.comments, nil
}
func (f *fakeCommentAPI) CreateIssueComment(ctx context.Context, o, r string, n int, body string) error {
	f.created = append(f.created, body)
	return nil
}
func (f *fakeCommentAPI) EditIssueComment(ctx context.Context, o, r string, id int64, body string) error {
	if f.edited == nil {
		f.edited = map[int64]string{}
	}
	f.edited[id] = body
	return nil
}

func blockedReport() report.Report {
	return report.Report{
		Repo: "ssdlc/pilot-app", Number: 3, HeadSHA: "abcdef0123456789", Gate: "failure", MergeBlocked: true,
		Summary: report.Summary{Critical: 1, Medium: 1},
		Findings: []report.Finding{
			{Category: "blocking", Severity: "CRITICAL", Tool: "gitleaks", RuleID: "gitleaks/aws-access-token", Location: "config.py:5", Description: "AWS key"},
			{Category: "warning", Severity: "MEDIUM", Tool: "semgrep", RuleID: "semgrep/weak-hash", Location: "app.py:12", Description: "md5"},
		},
	}
}

func TestRenderComment(t *testing.T) {
	body := RenderComment(blockedReport(), "http://192.168.1.28:8181")
	for _, want := range []string{
		CommentMarker,
		"blocked",
		"abcdef0",
		"critical 1",
		"gitleaks/aws-access-token",
		"config.py:5",
		"http://192.168.1.28:8181/pr/ssdlc/pilot-app/3",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("comment missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "semgrep/weak-hash") {
		t.Errorf("only blocking findings are listed in the comment:\n%s", body)
	}
}

func TestRenderCommentIsDeterministic(t *testing.T) {
	if RenderComment(blockedReport(), "u") != RenderComment(blockedReport(), "u") {
		t.Fatal("comment must not embed timestamps or other varying data")
	}
}

func TestRenderCommentWithoutPortalURLOmitsLink(t *testing.T) {
	if strings.Contains(RenderComment(blockedReport(), ""), "/pr/") {
		t.Error("no portal URL configured: no link expected")
	}
}

func TestSyncCommentCreatesWhenNoneExists(t *testing.T) {
	api := &fakeCommentAPI{}
	if err := SyncComment(context.Background(), api, "gate-reporter", "o", "r", 1, CommentMarker+"\nhi"); err != nil {
		t.Fatal(err)
	}
	if len(api.created) != 1 {
		t.Errorf("created = %v", api.created)
	}
}

func TestSyncCommentEditsOwnCommentWhenChanged(t *testing.T) {
	api := &fakeCommentAPI{comments: []giteaclient.IssueComment{
		{ID: 9, Body: CommentMarker + "\nold", Author: "gate-reporter"},
	}}
	if err := SyncComment(context.Background(), api, "gate-reporter", "o", "r", 1, CommentMarker+"\nnew"); err != nil {
		t.Fatal(err)
	}
	if api.edited[9] != CommentMarker+"\nnew" || len(api.created) != 0 {
		t.Errorf("edited=%v created=%v", api.edited, api.created)
	}
}

func TestSyncCommentSkipsWhenUnchanged(t *testing.T) {
	body := CommentMarker + "\nsame"
	api := &fakeCommentAPI{comments: []giteaclient.IssueComment{{ID: 9, Body: body, Author: "gate-reporter"}}}
	if err := SyncComment(context.Background(), api, "gate-reporter", "o", "r", 1, body); err != nil {
		t.Fatal(err)
	}
	if len(api.edited) != 0 || len(api.created) != 0 {
		t.Errorf("unchanged body must not write: edited=%v created=%v", api.edited, api.created)
	}
}

func TestSyncCommentIgnoresMarkerFromAnotherAuthor(t *testing.T) {
	api := &fakeCommentAPI{comments: []giteaclient.IssueComment{
		{ID: 4, Body: CommentMarker + "\nspoof", Author: "dev2"},
	}}
	if err := SyncComment(context.Background(), api, "gate-reporter", "o", "r", 1, CommentMarker+"\nreal"); err != nil {
		t.Fatal(err)
	}
	if len(api.edited) != 0 || len(api.created) != 1 {
		t.Errorf("a marker pasted by someone else must not be edited: edited=%v created=%v", api.edited, api.created)
	}
}

func TestCommentHookNeverPanicsOrPropagatesErrors(t *testing.T) {
	api := &fakeCommentAPI{failList: true}
	hook := CommentHook(api, "gate-reporter", "", log.New(io.Discard, "", 0))
	hook(context.Background(), blockedReport()) // must simply log and return
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, `RenderComment` undefined.

- [ ] **Step 3: Implement**

Create `portal/internal/sidecar/comment.go`:

```go
package sidecar

import (
	"context"
	"fmt"
	"log"
	"strings"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/report"
)

// CommentMarker identifies the one comment this service maintains per PR.
const CommentMarker = "<!-- ssdlc-gate-report -->"

const maxCommentFindings = 10

type CommentAPI interface {
	ListIssueComments(ctx context.Context, owner, repo string, number int) ([]giteaclient.IssueComment, error)
	CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error
	EditIssueComment(ctx context.Context, owner, repo string, id int64, body string) error
}

// RenderComment is deterministic (no timestamps) so an unchanged result never
// causes an edit.
func RenderComment(r report.Report, portalURL string) string {
	var b strings.Builder
	b.WriteString(CommentMarker + "\n")
	verdict := map[string]string{
		"success": "passed", "failure": "blocked", "pending": "running", "none": "no result yet",
	}[r.Gate]
	if verdict == "" {
		verdict = "no result yet"
	}
	sha := r.HeadSHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	fmt.Fprintf(&b, "**SSDLC gate: %s** for `%s`\n\n", verdict, sha)
	fmt.Fprintf(&b, "Findings: critical %d, high %d, medium %d, low %d\n", r.Summary.Critical, r.Summary.High, r.Summary.Medium, r.Summary.Low)

	shown := 0
	for _, f := range r.Findings {
		if f.Category != "blocking" {
			continue
		}
		if shown == 0 {
			b.WriteString("\nBlocking:\n")
		}
		if shown == maxCommentFindings {
			b.WriteString("- ...and more (see the full report)\n")
			break
		}
		fmt.Fprintf(&b, "- **%s** `%s` at `%s`: %s\n", f.Severity, f.RuleID, f.Location, f.Description)
		shown++
	}
	if portalURL != "" {
		fmt.Fprintf(&b, "\n[Full report](%s/pr/%s/%d)\n", strings.TrimRight(portalURL, "/"), r.Repo, r.Number)
	}
	return b.String()
}

// SyncComment creates the sticky comment, or edits the one this bot already
// wrote. A marker pasted by another user is ignored: only comments by
// botLogin are ever edited.
func SyncComment(ctx context.Context, api CommentAPI, botLogin, owner, repo string, number int, body string) error {
	comments, err := api.ListIssueComments(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	for _, c := range comments {
		if c.Author == botLogin && strings.Contains(c.Body, CommentMarker) {
			if strings.TrimSpace(c.Body) == strings.TrimSpace(body) {
				return nil
			}
			return api.EditIssueComment(ctx, owner, repo, c.ID, body)
		}
	}
	return api.CreateIssueComment(ctx, owner, repo, number, body)
}

// CommentHook returns a Poller.AfterReport that keeps each PR's sticky
// comment current. Failures are logged and never propagate.
func CommentHook(api CommentAPI, botLogin, portalURL string, lg *log.Logger) func(ctx context.Context, r report.Report) {
	return func(ctx context.Context, r report.Report) {
		owner, repo, _ := strings.Cut(r.Repo, "/")
		if err := SyncComment(ctx, api, botLogin, owner, repo, r.Number, RenderComment(r, portalURL)); err != nil {
			lg.Printf("comment: %s#%d: %v", r.Repo, r.Number, err)
		}
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOTEST`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/sidecar
git commit -m "feat(portal): sticky gate comment on pull requests"
```

---

### Task 6: Dependency health

**Files:**
- Create: `portal/internal/sidecar/health.go`, `portal/internal/sidecar/health_test.go`

**Interfaces:**
- Consumes: `woodpeckerclient.Agent`
- Produces:
  - `type HealthResult struct { Name string; OK bool; LatencyMS int64; Detail string }` (json: `name`, `ok`, `latency_ms`, `detail`)
  - `type HealthChecker struct { HTTP *http.Client; GiteaURL, WoodpeckerURL string; Agents func(ctx context.Context) ([]woodpeckerclient.Agent, error); Now func() time.Time }`
  - `func (h *HealthChecker) Run(ctx context.Context) []HealthResult` returning exactly three results in the order `gitea`, `woodpecker`, `woodpecker-agent`

- [ ] **Step 1: Write the failing test**

Create `portal/internal/sidecar/health_test.go`:

```go
package sidecar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ssdlc-portal/internal/woodpeckerclient"
)

func healthServer(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
}

func TestHealthCheckerAllHealthy(t *testing.T) {
	g, w := healthServer(200), healthServer(200)
	defer g.Close()
	defer w.Close()
	now := time.Unix(10_000, 0)
	h := &HealthChecker{
		HTTP: http.DefaultClient, GiteaURL: g.URL, WoodpeckerURL: w.URL,
		Now: func() time.Time { return now },
		Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) {
			return []woodpeckerclient.Agent{{Name: "a1", LastContact: now.Add(-30 * time.Second)}}, nil
		},
	}
	res := h.Run(context.Background())
	if len(res) != 3 || res[0].Name != "gitea" || res[1].Name != "woodpecker" || res[2].Name != "woodpecker-agent" {
		t.Fatalf("results = %+v", res)
	}
	for _, r := range res {
		if !r.OK {
			t.Errorf("%s not ok: %s", r.Name, r.Detail)
		}
	}
}

func TestHealthCheckerReportsFailures(t *testing.T) {
	g, w := healthServer(503), healthServer(200)
	defer g.Close()
	defer w.Close()
	now := time.Unix(10_000, 0)
	h := &HealthChecker{
		HTTP: http.DefaultClient, GiteaURL: g.URL, WoodpeckerURL: w.URL,
		Now: func() time.Time { return now },
		Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) {
			return []woodpeckerclient.Agent{{Name: "a1", LastContact: now.Add(-10 * time.Minute)}}, nil
		},
	}
	res := h.Run(context.Background())
	if res[0].OK {
		t.Error("gitea returned 503: must not be ok")
	}
	if !res[1].OK {
		t.Error("woodpecker returned 200: must be ok")
	}
	if res[2].OK {
		t.Error("agent last seen 10 minutes ago: must not be ok")
	}
}

func TestHealthCheckerAgentLookupError(t *testing.T) {
	g, w := healthServer(200), healthServer(200)
	defer g.Close()
	defer w.Close()
	h := &HealthChecker{
		HTTP: http.DefaultClient, GiteaURL: g.URL, WoodpeckerURL: w.URL, Now: time.Now,
		Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) { return nil, errors.New("forbidden") },
	}
	if res := h.Run(context.Background()); res[2].OK || res[2].Detail == "" {
		t.Errorf("agent result = %+v", res[2])
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, `HealthChecker` undefined.

- [ ] **Step 3: Implement**

Create `portal/internal/sidecar/health.go`:

```go
package sidecar

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"ssdlc-portal/internal/woodpeckerclient"
)

const agentStaleAfter = 2 * time.Minute

type HealthResult struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Detail    string `json:"detail"`
}

type HealthChecker struct {
	HTTP          *http.Client
	GiteaURL      string
	WoodpeckerURL string
	Agents        func(ctx context.Context) ([]woodpeckerclient.Agent, error)
	Now           func() time.Time
}

func (h *HealthChecker) probe(ctx context.Context, name, url string) HealthResult {
	start := time.Now()
	res := HealthResult{Name: name}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	resp, err := h.HTTP.Do(req)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.OK = resp.StatusCode == http.StatusOK
	res.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
	return res
}

func (h *HealthChecker) agent(ctx context.Context) HealthResult {
	res := HealthResult{Name: "woodpecker-agent"}
	start := time.Now()
	agents, err := h.Agents(ctx)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Detail = "cannot list agents: " + err.Error()
		return res
	}
	now := h.Now()
	for _, a := range agents {
		if now.Sub(a.LastContact) <= agentStaleAfter {
			res.OK = true
			res.Detail = fmt.Sprintf("%s seen %ds ago", a.Name, int(now.Sub(a.LastContact).Seconds()))
			return res
		}
	}
	res.Detail = fmt.Sprintf("no agent in contact within %s (%d registered)", agentStaleAfter, len(agents))
	return res
}

// Run checks Gitea, the Woodpecker server and the Woodpecker agent, in that order.
func (h *HealthChecker) Run(ctx context.Context) []HealthResult {
	return []HealthResult{
		h.probe(ctx, "gitea", h.GiteaURL+"/api/healthz"),
		h.probe(ctx, "woodpecker", h.WoodpeckerURL+"/healthz"),
		h.agent(ctx),
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOTEST`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/sidecar
git commit -m "feat(portal): sidecar dependency health checks"
```

---

### Task 7: Config, HTTP server and `main`

**Files:**
- Create: `portal/internal/sidecar/config.go`, `portal/internal/sidecar/config_test.go`, `portal/internal/sidecar/server.go`, `portal/internal/sidecar/server_test.go`, `portal/cmd/sidecar/main.go`

**Interfaces:**
- Consumes: Tasks 1-6
- Produces:
  - `type Config struct { GiteaURL, GiteaToken, WoodpeckerURL, WoodpeckerToken, Org, ListenAddr, APIToken, PortalURL, CommentUser string; PollInterval time.Duration; Comments bool }`
  - `func LoadConfig(getenv func(string) string) (Config, error)`
  - `func NewServer(api *APIDeps) http.Handler` with `type APIDeps struct { Token string; Metrics *metrics.Store; Gitea report.Gitea; Woodpecker report.Woodpecker; Health *HealthChecker; Now func() time.Time }`
  - Routes: `GET /healthz` (open), `GET /metrics` (open), `GET /api/v1/reports/{owner}/{repo}/{number}` and `GET /api/v1/health` (bearer token)

- [ ] **Step 1: Write the failing config test**

Create `portal/internal/sidecar/config_test.go`:

```go
package sidecar

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func validEnv() map[string]string {
	return map[string]string{
		"SIDECAR_GITEA_URL": "http://g:3500", "SIDECAR_GITEA_TOKEN": "t1",
		"SIDECAR_WOODPECKER_URL": "http://w:8000", "SIDECAR_WOODPECKER_TOKEN": "t2",
		"SIDECAR_API_TOKEN": "0123456789abcdef0123",
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(env(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Org != "ssdlc" || c.ListenAddr != ":8282" || c.PollInterval != 60*time.Second || !c.Comments || c.CommentUser != "gate-reporter" {
		t.Errorf("defaults wrong: %+v", c)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	m := validEnv()
	m["SIDECAR_ORG"], m["SIDECAR_POLL_INTERVAL"], m["SIDECAR_COMMENTS"] = "acme", "30s", "false"
	c, err := LoadConfig(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.Org != "acme" || c.PollInterval != 30*time.Second || c.Comments {
		t.Errorf("overrides wrong: %+v", c)
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	m := validEnv()
	delete(m, "SIDECAR_GITEA_TOKEN")
	m["SIDECAR_API_TOKEN"] = "short"
	m["SIDECAR_POLL_INTERVAL"] = "1s"
	_, err := LoadConfig(env(m))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"SIDECAR_GITEA_TOKEN", "SIDECAR_API_TOKEN", "SIDECAR_POLL_INTERVAL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure, implement config**

Run: `GOTEST` → FAIL (`LoadConfig` undefined). Then create `portal/internal/sidecar/config.go`:

```go
package sidecar

import (
	"fmt"
	"strings"
	"time"
)

type Config struct {
	GiteaURL, GiteaToken               string
	WoodpeckerURL, WoodpeckerToken     string
	Org, ListenAddr, APIToken          string
	PortalURL, CommentUser             string
	PollInterval                       time.Duration
	Comments                           bool
}

// LoadConfig fails closed: a missing or weak value is a startup error, never
// a silent default, matching the portal's own configuration rules.
func LoadConfig(getenv func(string) string) (Config, error) {
	var problems []string
	required := func(name string) string {
		v := getenv(name)
		if v == "" {
			problems = append(problems, name+" is required")
		}
		return v
	}
	orDefault := func(name, def string) string {
		if v := getenv(name); v != "" {
			return v
		}
		return def
	}

	c := Config{
		GiteaURL:        strings.TrimRight(required("SIDECAR_GITEA_URL"), "/"),
		GiteaToken:      required("SIDECAR_GITEA_TOKEN"),
		WoodpeckerURL:   strings.TrimRight(required("SIDECAR_WOODPECKER_URL"), "/"),
		WoodpeckerToken: required("SIDECAR_WOODPECKER_TOKEN"),
		APIToken:        required("SIDECAR_API_TOKEN"),
		Org:             orDefault("SIDECAR_ORG", "ssdlc"),
		ListenAddr:      orDefault("SIDECAR_LISTEN_ADDR", ":8282"),
		PortalURL:       strings.TrimRight(getenv("SIDECAR_PORTAL_URL"), "/"),
		CommentUser:     orDefault("SIDECAR_COMMENT_USER", "gate-reporter"),
		Comments:        orDefault("SIDECAR_COMMENTS", "true") != "false",
	}
	if c.APIToken != "" && len(c.APIToken) < 16 {
		problems = append(problems, "SIDECAR_API_TOKEN must be at least 16 characters")
	}
	interval, err := time.ParseDuration(orDefault("SIDECAR_POLL_INTERVAL", "60s"))
	if err != nil || interval < 5*time.Second {
		problems = append(problems, "SIDECAR_POLL_INTERVAL must be a duration of at least 5s")
	}
	c.PollInterval = interval

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("sidecar configuration is invalid:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return c, nil
}
```

Run: `GOTEST` → config tests PASS.

- [ ] **Step 3: Write the failing server test**

Create `portal/internal/sidecar/server_test.go`:

```go
package sidecar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/woodpeckerclient"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	g := fakeGitea{prs: map[string][]giteaclient.PRDetail{
		"o/r": {{Number: 3, Title: "t", State: "open", HeadSHA: "abc", CreatedAt: time.Unix(900, 0)}},
	}}
	st := &metrics.Store{}
	snap := metrics.NewSnapshot()
	snap.Gauge("ssdlc_sidecar_poll_ok", "h", nil, 1)
	st.Set(snap)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(up.Close)
	return NewServer(&APIDeps{
		Token: "0123456789abcdef0123", Metrics: st, Gitea: g, Woodpecker: fakeWP{},
		Health: &HealthChecker{
			HTTP: http.DefaultClient, GiteaURL: up.URL, WoodpeckerURL: up.URL, Now: time.Now,
			Agents: func(ctx context.Context) ([]woodpeckerclient.Agent, error) { return nil, nil },
		},
		Now: func() time.Time { return time.Unix(2000, 0) },
	})
}

func do(h http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestServerOpenEndpoints(t *testing.T) {
	h := testServer(t)
	if rec := do(h, "/healthz", ""); rec.Code != 200 {
		t.Errorf("/healthz = %d", rec.Code)
	}
	rec := do(h, "/metrics", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ssdlc_sidecar_poll_ok 1") {
		t.Errorf("/metrics = %d %q", rec.Code, rec.Body.String())
	}
}

func TestServerAPIRequiresBearerToken(t *testing.T) {
	h := testServer(t)
	for _, path := range []string{"/api/v1/reports/o/r/3", "/api/v1/health"} {
		if rec := do(h, path, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without token = %d", path, rec.Code)
		}
		if rec := do(h, path, "wrong-token-wrong-token"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with wrong token = %d", path, rec.Code)
		}
	}
}

func TestServerReportEndpoint(t *testing.T) {
	h := testServer(t)
	rec := do(h, "/api/v1/reports/o/r/3", "0123456789abcdef0123")
	if rec.Code != 200 {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Repo         string `json:"repo"`
		Number       int    `json:"number"`
		Gate         string `json:"gate"`
		MergeBlocked bool   `json:"merge_blocked"`
		Findings     []struct {
			RuleID string `json:"rule_id"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Repo != "o/r" || got.Number != 3 || got.Gate != "failure" || !got.MergeBlocked || len(got.Findings) != 1 {
		t.Errorf("report = %+v", got)
	}
}

func TestServerReportEndpointRejectsBadNumberAndMissingPR(t *testing.T) {
	h := testServer(t)
	if rec := do(h, "/api/v1/reports/o/r/abc", "0123456789abcdef0123"); rec.Code != http.StatusBadRequest {
		t.Errorf("non-numeric PR = %d", rec.Code)
	}
	if rec := do(h, "/api/v1/reports/o/r/99", "0123456789abcdef0123"); rec.Code != http.StatusBadGateway {
		t.Errorf("unknown PR = %d", rec.Code)
	}
}

func TestServerHealthEndpoint(t *testing.T) {
	h := testServer(t)
	rec := do(h, "/api/v1/health", "0123456789abcdef0123")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var res []HealthResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || len(res) != 3 {
		t.Errorf("health = %v %v", res, err)
	}
}
```

Run: `GOTEST` → FAIL (`NewServer`, `APIDeps` undefined).

- [ ] **Step 4: Implement the server**

Create `portal/internal/sidecar/server.go`:

```go
package sidecar

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/report"
)

type APIDeps struct {
	Token      string
	Metrics    *metrics.Store
	Gitea      report.Gitea
	Woodpecker report.Woodpecker
	Health     *HealthChecker
	Now        func() time.Time
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (d *APIDeps) auth(next http.HandlerFunc) http.HandlerFunc {
	want := []byte("Bearer " + d.Token)
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// NewServer wires the HTTP routes. /healthz and /metrics are open because the
// port is not published outside the Docker network; /api/* needs the token.
func NewServer(d *APIDeps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /metrics", d.Metrics)
	mux.HandleFunc("GET /api/v1/reports/{owner}/{repo}/{number}", d.auth(func(w http.ResponseWriter, r *http.Request) {
		number, err := strconv.Atoi(r.PathValue("number"))
		if err != nil || number < 1 {
			http.Error(w, "pull request number must be a positive integer", http.StatusBadRequest)
			return
		}
		rep, err := report.Build(r.Context(), d.Gitea, d.Woodpecker, r.PathValue("owner"), r.PathValue("repo"), number, d.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, rep)
	}))
	mux.HandleFunc("GET /api/v1/health", d.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, d.Health.Run(r.Context()))
	}))
	return mux
}
```

Run: `GOTEST` → PASS.

- [ ] **Step 5: Create `main`**

Create `portal/cmd/sidecar/main.go`:

```go
// Command sidecar is the SSDLC reporting service: PR report data, Prometheus
// metrics, one sticky gate comment per PR, and dependency health. It is not in
// the merge-decision path and holds no durable state.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/sidecar"
	"ssdlc-portal/internal/woodpeckerclient"
)

func main() {
	cfg, err := sidecar.LoadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	lg := log.New(os.Stderr, "sidecar: ", log.LstdFlags)

	gitea := giteaclient.New(cfg.GiteaURL, cfg.GiteaToken)
	wp := woodpeckerclient.New(cfg.WoodpeckerURL, cfg.WoodpeckerToken)
	store := &metrics.Store{}

	poller := &sidecar.Poller{
		B:     sidecar.Backends{Gitea: gitea, Woodpecker: wp},
		Org:   cfg.Org,
		Store: store,
		Now:   time.Now,
		Log:   lg,
	}
	if cfg.Comments {
		poller.AfterReport = sidecar.CommentHook(gitea, cfg.CommentUser, cfg.PortalURL, lg)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go poller.Run(ctx, cfg.PollInterval)

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: sidecar.NewServer(&sidecar.APIDeps{
			Token: cfg.APIToken, Metrics: store, Gitea: gitea, Woodpecker: wp, Now: time.Now,
			Health: &sidecar.HealthChecker{
				HTTP:          &http.Client{Timeout: 5 * time.Second},
				GiteaURL:      cfg.GiteaURL,
				WoodpeckerURL: cfg.WoodpeckerURL,
				Agents:        wp.ListAgents,
				Now:           time.Now,
			},
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	lg.Printf("listening on %s (org %q, poll every %s, comments %v)", cfg.ListenAddr, cfg.Org, cfg.PollInterval, cfg.Comments)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
```

- [ ] **Step 6: Build everything**

Run: `GOTEST` then `docker run --rm -v "$(pwd -W):/src" -w /src/portal golang:1.22-alpine go build -o /dev/null ./cmd/sidecar`
Expected: tests PASS, build succeeds.

- [ ] **Step 7: Commit**

```bash
git add portal/internal/sidecar portal/cmd
git commit -m "feat(portal): sidecar HTTP API, config and entrypoint"
```

---

### Task 8: Portal PR report uses `report.Build`

**Files:**
- Modify: `portal/internal/handlers/prreport.go` (the `PRReport` function body)
- Modify: `portal/internal/handlers/prreport_test.go` (three fixtures)
- Modify: `docs/superpowers/specs/2026-09-21-tiered-platform-design.md` (the "Report data" bullet)

**Interfaces:**
- Consumes: `report.Build`, `report.Report`, existing `reportFinding`, `groupByTool`, `severityClass`, `prReportData`, `prReportTmpl`
- Produces: unchanged `PRReport(giteaBaseURL, woodpeckerBaseURL, woodpeckerToken string) http.HandlerFunc`

- [ ] **Step 1: Update the three test fixtures first (they should FAIL against the old handler)**

In `portal/internal/handlers/prreport_test.go`, in **each** of the three tests:

a. In the fake Gitea `switch`, delete the case for `"/api/v1/repos/gateadmin/gate-demo"` (the `{"id":1}` one) and add, before `default:`:

```go
		case "/api/v1/repos/gateadmin/gate-demo/commits/abc123/status":
			w.Write([]byte(`{"statuses":[]}`))
```

In the third test (head SHA `zzz999`) use `zzz999` in that path instead of `abc123`.

b. In the first two tests' fake Woodpecker `switch`, add before the `strings.HasSuffix(r.URL.Path, "/pipelines")` case:

```go
		case r.URL.Path == "/api/repos/lookup/gateadmin/gate-demo":
			w.Write([]byte(`{"id":1}`))
```

c. In the third test, replace the fake Woodpecker handler body with:

```go
		if r.URL.Path == "/api/repos/lookup/gateadmin/gate-demo" {
			w.Write([]byte(`{"id":1}`))
			return
		}
		w.Write([]byte(`[{"number":5,"status":"failure","commit":"abc123","event":"pr"}]`))
```

Also add this new test at the end of the file, which proves the Woodpecker ID is looked up rather than assumed equal to Gitea's:

```go
func TestPRReport_UsesWoodpeckerRepoIDNotGiteasRepoID(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/gateadmin/gate-demo/pulls/12":
			w.Write([]byte(`{"number":12,"title":"t","head":{"sha":"abc123"}}`))
		case "/api/v1/repos/gateadmin/gate-demo/commits/abc123/status":
			w.Write([]byte(`{"statuses":[]}`))
		default:
			t.Fatalf("unexpected gitea path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()

	var pipelineIDs []string
	fakeWoodpecker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/repos/lookup/gateadmin/gate-demo":
			w.Write([]byte(`{"id":42}`))
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			pipelineIDs = append(pipelineIDs, r.URL.Path)
			w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected woodpecker path %s", r.URL.Path)
		}
	}))
	defer fakeWoodpecker.Close()

	handler := PRReport(fakeGitea.URL, fakeWoodpecker.URL, "wp-token")
	req := httptest.NewRequest(http.MethodGet, "/pr/gateadmin/gate-demo/12", nil)
	req.SetPathValue("owner", "gateadmin")
	req.SetPathValue("repo", "gate-demo")
	req.SetPathValue("number", "12")
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyToken, "fake-token"))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(pipelineIDs) != 1 || pipelineIDs[0] != "/api/repos/42/pipelines" {
		t.Errorf("pipelines were requested at %v, want /api/repos/42/pipelines", pipelineIDs)
	}
}
```

- [ ] **Step 2: Run to verify the tests fail**

Run: `GOTEST`
Expected: FAIL in `internal/handlers` (the old handler still calls `/api/v1/repos/gateadmin/gate-demo` and uses the wrong ID).

- [ ] **Step 3: Replace the handler body**

In `portal/internal/handlers/prreport.go`, replace everything from the line `wp := woodpeckerclient.New(woodpeckerBaseURL, woodpeckerToken)` down to the end of `PRReport` (keep the earlier lines that read `owner`, `repo`, `number`, `token` and create `gitea`), and delete the now-unused `var pr struct {...}` and `var repoInfo struct {...}` blocks and their two `gitea.RawGet` calls. The function becomes:

```go
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
```

Fix the file's imports to exactly:

```go
import (
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/findings"
	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/report"
	"ssdlc-portal/internal/woodpeckerclient"
)
```

- [ ] **Step 4: Run to verify all pass**

Run: `GOTEST`
Expected: PASS everywhere, including the four `TestPRReport_*` tests and the untouched dashboard/exceptions/onboarding tests.

- [ ] **Step 5: Update the spec's deviation line**

In `docs/superpowers/specs/2026-09-21-tiered-platform-design.md`, in component 1's "Report data" bullet replace `The portal's PR report page switches to this API so there is one implementation.` with `The portal's PR report page calls the same shared report.Build function in-process, so there is one implementation and the page keeps working if the sidecar is down.`

- [ ] **Step 6: Commit**

```bash
git add portal docs/superpowers/specs/2026-09-21-tiered-platform-design.md
git commit -m "refactor(portal): PR report uses the shared report package (fixes Woodpecker repo ID lookup)"
```

---

### Task 9: Deploy the sidecar and verify it live

**Files:**
- Modify: `deploy/uat/Dockerfile`, `deploy/uat/docker-compose.uat.yml`, `deploy/uat/setup-uat.ps1`, `deploy/uat/README.md`

**Interfaces:**
- Consumes: the `sidecar` binary; `REPORTER_TOKEN`, `WOODPECKER_TOKEN`, `SERVER_IP` from `state/uat.env`
- Produces: a running `ssdlc-uat-sidecar` container on the `ssdlc-minimal` network, port 8282 **not published**; `SIDECAR_API_TOKEN` in `state/uat.env`

- [ ] **Step 1: Add the image target**

In `deploy/uat/Dockerfile`, in the `build` stage after `RUN CGO_ENABLED=0 go build -o /out/portal .` add:

```dockerfile
RUN CGO_ENABLED=0 go build -o /out/sidecar ./cmd/sidecar
```

and append at the end of the file:

```dockerfile

# Small runtime for the reporting service: no shell tooling, non-root.
FROM gcr.io/distroless/static-debian12:nonroot AS sidecar
COPY --from=build /out/sidecar /sidecar
USER nonroot
ENTRYPOINT ["/sidecar"]
```

- [ ] **Step 2: Add the compose service**

In `deploy/uat/docker-compose.uat.yml`, add under `services:`:

```yaml
  # Reporting service (plan 1 of the tiered platform): PR report data,
  # metrics, sticky PR comment, dependency health. Runs at every tier. Its
  # port is deliberately not published; the portal and Prometheus reach it
  # over the ssdlc-minimal network.
  sidecar:
    build:
      context: ${REPO_ROOT:?}
      dockerfile: deploy/uat/Dockerfile
      target: sidecar
    image: ssdlc-uat-sidecar
    container_name: ssdlc-uat-sidecar
    restart: unless-stopped
    mem_limit: 128m
    depends_on:
      - gitea
      - woodpecker-server
    environment:
      - SIDECAR_GITEA_URL=http://${SERVER_IP}:3500
      - SIDECAR_GITEA_TOKEN=${REPORTER_TOKEN:-unset}
      - SIDECAR_WOODPECKER_URL=http://${SERVER_IP}:${WOODPECKER_PORT:-8000}
      - SIDECAR_WOODPECKER_TOKEN=${WOODPECKER_TOKEN:-unset}
      - SIDECAR_API_TOKEN=${SIDECAR_API_TOKEN:-unsetunsetunsetunset}
      - SIDECAR_ORG=ssdlc
      - SIDECAR_PORTAL_URL=http://${SERVER_IP}:8181
      - SIDECAR_COMMENT_USER=gate-reporter
```

(The address goes through the hairpin redirect, the same way the portal and bot reach Gitea.)

- [ ] **Step 3: Generate the API token and start the service**

In `deploy/uat/setup-uat.ps1`:

- In the "Secrets and configuration" section, next to `Ensure-Cfg 'PORTAL_SESSION_KEY' { New-Hex 32 }`, add: `Ensure-Cfg 'SIDECAR_API_TOKEN'        { New-Hex 32 }`
- Change the final `Compose up -d --build portal bot-approver` to `Compose up -d --build portal bot-approver sidecar`.

- [ ] **Step 4: Validate the config and build the image**

Run:

```bash
R="$(pwd -W)"; T="$(mktemp -d)"
printf 'SERVER_IP=1.2.3.4\nREPO_ROOT=%s\nREPO_VM=/x\nPOSTGRES_PASSWORD=x\nWOODPECKER_AGENT_SECRET=x\nWOODPECKER_GRPC_SECRET=x\nGITEA_OAUTH_CLIENT_ID=x\nGITEA_OAUTH_CLIENT_SECRET=x\n' "$R" > "$T/e"
docker compose -p t --env-file "$T/e" -f compose/minimal/docker-compose.yml -f deploy/uat/docker-compose.uat.yml config --quiet && echo config-ok
docker build -q -f deploy/uat/Dockerfile --target sidecar -t ssdlc-uat-sidecar-test .
docker run --rm ssdlc-uat-sidecar-test 2>&1 | head -5
```

Expected: `config-ok`; the build succeeds; running it with no environment prints `sidecar configuration is invalid:` followed by the required-variable list (proves the binary starts and fails closed).

- [ ] **Step 5: Live verification on the running stack**

On a machine with the stack running (setup already completed), from the repo root:

```powershell
git pull origin worktree-ssdlc-portal
.\deploy\uat\setup-uat.ps1
```

Then check, using the token from `state\uat.env`:

```powershell
$tok = (Select-String .\deploy\uat\state\uat.env -Pattern '^SIDECAR_API_TOKEN=').Line.Split('=')[1]
docker exec ssdlc-uat-portal sh -c "curl -s http://sidecar:8282/healthz"
docker exec ssdlc-uat-portal sh -c "curl -s http://sidecar:8282/metrics | head -20"
docker exec ssdlc-uat-portal sh -c "curl -s -H 'Authorization: Bearer $tok' http://sidecar:8282/api/v1/health"
```

Expected: `{"status":"ok"}`; metrics containing `ssdlc_sidecar_poll_ok 1`; health JSON with `gitea`, `woodpecker`, `woodpecker-agent` all `"ok":true`.

Then open a pull request on `ssdlc/pilot-app` (use the deliberately bad `app.py` from the README's first checks) and, within about 60 seconds, confirm:
- the PR has one comment from `gate-reporter` titled "SSDLC gate: blocked" listing the finding;
- pushing another commit **edits** that comment rather than adding a second;
- `docker exec ssdlc-uat-portal sh -c "curl -s http://sidecar:8282/metrics | grep blocked"` shows `ssdlc_pull_requests_blocked{repo="ssdlc/pilot-app"} 1`;
- `http://<server>:8181/pr/ssdlc/pilot-app/<n>` still renders the report (now via `report.Build`);
- `docker stop ssdlc-uat-sidecar`, then confirm a new PR still passes/blocks exactly as before and the portal PR report still loads; `docker start ssdlc-uat-sidecar` afterwards.

- [ ] **Step 6: Document and commit**

Add to `deploy/uat/README.md` under "Operations":

```markdown
- **Reporting service** (`ssdlc-uat-sidecar`): posts one edited-in-place comment per PR, serves report data and
  Prometheus metrics on port 8282 (internal only, not published), and reports dependency health. It is never in
  the merge decision: stopping it changes nothing about which PRs pass or block. Logs: `docker logs ssdlc-uat-sidecar`.
```

```bash
git add deploy docs
git commit -m "feat(deploy): run the reporting service at every tier"
git push origin worktree-ssdlc-portal
```

---

## Self-Review

**Spec coverage (component 1 and component 5's health endpoint):**
- Report data endpoint, JSON, Woodpecker log parser reuse: Tasks 2 and 7.
- Metrics (gate outcomes, blocking findings by severity and tool, pipeline duration, time to verdict, rebuilt from Gitea/Woodpecker): Tasks 3 and 4.
- Sticky comment, dedicated token, best effort, never a gate failure: Task 5, Task 7 wiring.
- Health endpoint checking Gitea, Woodpecker, agent: Task 6 and 7. Postgres and the hairpin container are not checked here (no database driver in a zero-dependency module; the hairpin has no HTTP surface); the Admin health screen plan can add Docker-API-based checks for those. Called out so it is not mistaken for a gap.
- Runs at every tier, small image, 128 MB cap: Task 9.
- Stateless: nothing is persisted; metrics rebuilt each poll.
- Invariants: read-only toward Gitea/Woodpecker except PR comments; `/api/*` token-protected; port not published; no new tokens in build containers (uses the existing `gate-reporter` token).
- Out of scope here by design: tier detection, Prometheus/Loki/Grafana, DefectDojo, export, Admin health screen (plans 2-4).

**Placeholder scan:** none; every code step contains the code.

**Type consistency checked:** `report.Gitea`/`report.Woodpecker` method signatures match the concrete clients (`GetPullRequest` returns `giteaclient.PRDetail`; `LookupRepo` returns `(int, error)`); `Backends` embeds those interfaces plus `ListOpenPullRequests`/`ListRepos`; `Poller.AfterReport` type matches `CommentHook`'s return; `BuildSnapshot(reports, now, pollOK)` matches its call in `Once`; `HealthChecker.Agents` matches `woodpeckerclient.Client.ListAgents`; `APIDeps.Gitea` accepts `*giteaclient.Client`.

**Known fragile points to watch during execution:**
- Woodpecker's `/api/user/repos`, `/api/repos/lookup/...` and `/api/agents` response shapes are taken from the existing setup code and Woodpecker 3.x behaviour; Task 9 step 5 is the live confirmation. If `ListRepos` returns too little, switch it to `/api/user/repos?all=true` and re-run.
- Gitea's `pulls?state=open&limit=50` returns at most 50 PRs per repo; fine for UAT, note if it grows.
