# Portal Shell Implementation Plan (redesign step 1 of 6)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the portal its new application shell: a role-aware sidebar with badges, Quick launch links to Gitea and Woodpecker, a Ctrl+K jump palette, a Help page, deep links from existing pages into Gitea and Woodpecker, and self-hosted fonts and scripts (no external CDN), all on top of today's screens and data.

**Architecture:** A new `internal/shell` package computes a per-request `Shell` value (operator name, role, pending-approval count, public Gitea/Woodpecker URLs) from the user's own Gitea token and puts it in the request context via a middleware. Every page's template data carries `Shell`, and one rewritten `layout.html` renders the sidebar and palette from it. Help is a server-rendered page with no JavaScript needed. Screens that do not exist yet (Projects, Issues) appear as visibly disabled entries, following the portal spec.

**Tech Stack:** Go 1.22 standard library only (`html/template`, `net/http`), plain CSS, ~60 lines of vanilla JS, vendored IBM Plex fonts (OFL) and htmx 1.9.12.

**Spec:** `docs/superpowers/specs/2026-09-21-portal-redesign-design.md` (delivery step 1: "Shell"), with the visual reference in `docs/superpowers/specs/2026-09-21-portal-redesign-mockup/` (open `prototype.html`).

## Global Constraints

- Go standard library only; no new module dependencies. Server-rendered templates, no SPA build chain.
- **No external requests at page load:** no Google Fonts, no unpkg or other CDN. Fonts and scripts are served from `/static/`.
- Role comes from Gitea and is computed server-side: **Admin** = Gitea site admin; **Approver** = member of team `PORTAL_APPROVER_TEAM` in org `PORTAL_EXCEPTIONS_REPO_OWNER`; otherwise **Developer**. The role only changes what the sidebar shows; every action keeps its existing server-side authorisation (`RequireTeam`, self-approval check). Never trust a client-supplied role.
- The shell must fail soft: if a Gitea call for the shell fails, the page still renders with the developer view and no badge.
- Sidebar entries for screens that do not exist yet (Projects, Issues) are visible but disabled with a title explaining they arrive in the next step. No fake screens.
- Keep the existing dark tokens in `portal/web/static/tokens.css`; existing CSS class names keep working.
- Existing Go tests must keep passing after every task: `cd portal && go test ./...`. New and touched Go files must be `gofmt`-clean (do not reformat untouched pre-existing files).
- Source files ASCII-only except where a snippet below shows otherwise; write the arrow/dot characters used in templates as HTML entities (`&rarr;`, `&middot;`, `&#8599;`).
- The reporting service and its packages are already implemented; reuse `report`, `giteaclient`, `woodpeckerclient`. Do not change the sidecar.

## Running Go here

Go is not installed on the dev machine; run it in a container from the worktree root (Git Bash needs `MSYS_NO_PATHCONV=1`):

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/src" -w /src/portal golang:1.22-alpine sh -c "go vet ./... && go test ./... && gofmt -l internal cmd"
```

Below this is written as `GOTEST`. `gofmt -l` may list pre-existing files (`internal/exceptions/exceptions.go`, `internal/findings/parser.go`); only files you create or touch must be absent from it. No `-race` (no cgo).

## File Structure

| File | Responsibility |
|---|---|
| `portal/internal/giteaclient/pulls.go` (modify) | add `CurrentUser` (login + is_admin) |
| `portal/internal/config/config.go` (modify) | optional public URLs for Gitea and Woodpecker |
| `portal/internal/exceptions/pending.go` (create) | `CountPending`: pure function over records |
| `portal/internal/shell/shell.go` (create) | `Shell`, `Role`, `Builder`, TTL cache, middleware, context helpers |
| `portal/web/static/fonts.css`, `fonts/*.woff2`, `vendor/*` (create) | self-hosted fonts and htmx |
| `portal/web/static/shell.css` (create) | sidebar, badges, quick launch, palette, help styles |
| `portal/web/static/shell.js` (create) | Ctrl+K palette |
| `portal/web/templates/layout.html` (modify) | new shell markup, no CDN |
| `portal/internal/handlers/*.go` (modify) | every page data struct carries `Shell` |
| `portal/internal/help/help.go` (create) | help topics, first steps, filtering |
| `portal/internal/handlers/help.go` (create), `portal/web/templates/help.html` (create) | Help page |
| `portal/internal/report/report.go` (modify) | `WoodpeckerRepoID` on the report |
| `portal/web/templates/dashboard.html`, `prreport.html` (modify) | deep links, "Overview" heading |
| `portal/main.go` (modify) | wire the shell, `/help`, mime type |

---

### Task 1: `CurrentUser` and public URLs

**Files:**
- Modify: `portal/internal/giteaclient/pulls.go`, `portal/internal/config/config.go`
- Create: `portal/internal/giteaclient/currentuser_test.go`
- Modify: `portal/internal/config/config_test.go`

**Interfaces:**
- Produces: `func (c *Client) CurrentUser(ctx context.Context) (login string, isAdmin bool, err error)`; `Config.GiteaPublicURL`, `Config.WoodpeckerPublicURL` (both default to the internal URL, no trailing slash).

- [ ] **Step 1: Write the failing tests**

Create `portal/internal/giteaclient/currentuser_test.go`:

```go
package giteaclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCurrentUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "token tok" {
			t.Fatalf("auth header = %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"login":"gateadmin","is_admin":true}`))
	}))
	defer srv.Close()

	login, admin, err := New(srv.URL, "tok").CurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if login != "gateadmin" || !admin {
		t.Errorf("got %q admin=%v", login, admin)
	}
}

func TestCurrentUser_NonAdminAndError(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"login":"dev2"}`))
	}))
	defer ok.Close()
	if _, admin, err := New(ok.URL, "t").CurrentUser(context.Background()); err != nil || admin {
		t.Errorf("admin=%v err=%v", admin, err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer bad.Close()
	if _, _, err := New(bad.URL, "t").CurrentUser(context.Background()); err == nil {
		t.Fatal("expected an error on 401")
	}
}
```

Append to `portal/internal/config/config_test.go`:

```go
func TestLoad_PublicURLsDefaultAndOverride(t *testing.T) {
	base := map[string]string{
		"PORTAL_GITEA_URL": "http://gitea:3500/", "PORTAL_WOODPECKER_URL": "http://woodpecker:8000",
		"PORTAL_WOODPECKER_TOKEN": "w", "PORTAL_GITEA_ADMIN_TOKEN": "g",
		"PORTAL_OAUTH_CLIENT_ID": "x", "PORTAL_OAUTH_CLIENT_SECRET": "y",
		"PORTAL_SESSION_KEY": "0123456789abcdef0123456789abcdef", "PORTAL_EXCEPTIONS_REPO_OWNER": "ssdlc",
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GiteaPublicURL != "http://gitea:3500" || cfg.WoodpeckerPublicURL != "http://woodpecker:8000" {
		t.Errorf("defaults: %q %q", cfg.GiteaPublicURL, cfg.WoodpeckerPublicURL)
	}

	t.Setenv("PORTAL_GITEA_PUBLIC_URL", "http://192.168.1.28:3500/")
	t.Setenv("PORTAL_WOODPECKER_PUBLIC_URL", "http://192.168.1.28:8000")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GiteaPublicURL != "http://192.168.1.28:3500" || cfg.WoodpeckerPublicURL != "http://192.168.1.28:8000" {
		t.Errorf("overrides: %q %q", cfg.GiteaPublicURL, cfg.WoodpeckerPublicURL)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, `CurrentUser` undefined and `GiteaPublicURL` unknown field.

- [ ] **Step 3: Implement**

Append to `portal/internal/giteaclient/pulls.go`:

```go
// CurrentUser returns the authenticated user's login and whether they are a
// Gitea site administrator.
func (c *Client) CurrentUser(ctx context.Context) (login string, isAdmin bool, err error) {
	var u struct {
		Login   string `json:"login"`
		IsAdmin bool   `json:"is_admin"`
	}
	const path = "/api/v1/user"
	status, err := c.do(ctx, http.MethodGet, path, nil, &u)
	if err != nil {
		return "", false, err
	}
	if err := statusError(http.MethodGet, path, status); err != nil {
		return "", false, err
	}
	return u.Login, u.IsAdmin, nil
}
```

In `portal/internal/config/config.go`: add `"strings"` to the imports; add two fields to `Config` after `WoodpeckerURL`:

```go
	// GiteaPublicURL / WoodpeckerPublicURL are the addresses shown to people
	// in links that open in their browser. They default to the URLs the
	// portal itself uses, and only need setting when those are internal names.
	GiteaPublicURL      string
	WoodpeckerPublicURL string
```

and at the end of `Load()`, right after the `if cfg.ListenAddr == "" {...}` block and before the session-key handling, add:

```go
	cfg.GiteaURL = strings.TrimRight(cfg.GiteaURL, "/")
	cfg.WoodpeckerURL = strings.TrimRight(cfg.WoodpeckerURL, "/")
	cfg.GiteaPublicURL = strings.TrimRight(get("PORTAL_GITEA_PUBLIC_URL"), "/")
	if cfg.GiteaPublicURL == "" {
		cfg.GiteaPublicURL = cfg.GiteaURL
	}
	cfg.WoodpeckerPublicURL = strings.TrimRight(get("PORTAL_WOODPECKER_PUBLIC_URL"), "/")
	if cfg.WoodpeckerPublicURL == "" {
		cfg.WoodpeckerPublicURL = cfg.WoodpeckerURL
	}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOTEST`
Expected: PASS (existing config tests unaffected: they compare `GiteaURL` to a value with no trailing slash).

- [ ] **Step 5: Commit**

```bash
git add portal/internal/giteaclient portal/internal/config
git commit -m "feat(portal): CurrentUser (admin flag) and public URLs for Gitea/Woodpecker links"
```

---

### Task 2: The `shell` package

**Files:**
- Create: `portal/internal/exceptions/pending.go`, `portal/internal/exceptions/pending_test.go`
- Create: `portal/internal/shell/shell.go`, `portal/internal/shell/shell_test.go`

**Interfaces:**
- Produces (exceptions): `func CountPending(records []Record, user string, now time.Time) int`
- Produces (shell):
  - `type Role string` with `RoleDeveloper`, `RoleApprover`, `RoleAdmin`
  - `type Shell struct { Operator string; Role Role; IsApprover, IsAdmin bool; PendingApprovals int; GiteaURL, WoodpeckerURL string }` and `func (s Shell) RoleLabel() string`
  - `type Identity interface { CurrentUser(ctx context.Context) (string, bool, error); IsOnTeam(ctx context.Context, org, team string) (bool, error) }`
  - `type Builder struct { NewIdentity func(token string) Identity; Records func(ctx context.Context) ([]exceptions.Record, error); Org, Team, GiteaURL, WoodpeckerURL string; TTL time.Duration; Now func() time.Time }`
  - `func (b *Builder) For(ctx context.Context, token string) Shell`
  - `func (b *Builder) Middleware(next http.HandlerFunc) http.HandlerFunc`
  - `func FromContext(ctx context.Context) Shell`, `func WithContext(ctx context.Context, s Shell) context.Context`

- [ ] **Step 1: Write the failing tests**

Create `portal/internal/exceptions/pending_test.go`:

```go
package exceptions

import (
	"testing"
	"time"
)

func TestCountPending(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	future, past := now.Add(24*time.Hour), now.Add(-24*time.Hour)
	recs := []Record{
		{Requester: "dev2", Expiry: future},                                // pending, someone else's: counts
		{Requester: "dev3", Expiry: future},                                // pending, counts
		{Requester: "dev1", Expiry: future},                                // my own request: excluded
		{Requester: "dev2", Expiry: future, Approved: true},                // decided
		{Requester: "dev2", Expiry: future, Declined: true},                // decided
		{Requester: "dev2", Expiry: past},                                  // expired request: excluded
	}
	if got := CountPending(recs, "dev1", now); got != 2 {
		t.Errorf("CountPending = %d, want 2", got)
	}
	if got := CountPending(nil, "dev1", now); got != 0 {
		t.Errorf("empty = %d", got)
	}
}
```

Create `portal/internal/shell/shell_test.go`:

```go
package shell

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/exceptions"
)

type fakeIdentity struct {
	login    string
	admin    bool
	member   bool
	userErr  error
	teamErr  error
	calls    *int
}

func (f fakeIdentity) CurrentUser(ctx context.Context) (string, bool, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.login, f.admin, f.userErr
}
func (f fakeIdentity) IsOnTeam(ctx context.Context, org, team string) (bool, error) {
	return f.member, f.teamErr
}

func builder(id fakeIdentity, recs []exceptions.Record, recErr error) *Builder {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	return &Builder{
		NewIdentity: func(token string) Identity { return id },
		Records:     func(ctx context.Context) ([]exceptions.Record, error) { return recs, recErr },
		Org:         "ssdlc", Team: "approvers",
		GiteaURL: "http://g:3500", WoodpeckerURL: "http://w:8000",
		TTL: 30 * time.Second, Now: func() time.Time { return now },
	}
}

var pendingRecs = []exceptions.Record{{Requester: "dev2", Expiry: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}}

func TestFor_Roles(t *testing.T) {
	ctx := context.Background()
	dev := builder(fakeIdentity{login: "dev2"}, pendingRecs, nil).For(ctx, "t1")
	if dev.Role != RoleDeveloper || dev.IsApprover || dev.IsAdmin || dev.PendingApprovals != 0 || dev.Operator != "dev2" {
		t.Errorf("developer = %+v", dev)
	}
	appr := builder(fakeIdentity{login: "dev1", member: true}, pendingRecs, nil).For(ctx, "t2")
	if appr.Role != RoleApprover || !appr.IsApprover || appr.IsAdmin || appr.PendingApprovals != 1 {
		t.Errorf("approver = %+v", appr)
	}
	adm := builder(fakeIdentity{login: "gateadmin", admin: true, member: true}, pendingRecs, nil).For(ctx, "t3")
	if adm.Role != RoleAdmin || !adm.IsAdmin || !adm.IsApprover || adm.RoleLabel() != "Admin" {
		t.Errorf("admin = %+v", adm)
	}
	if adm.GiteaURL != "http://g:3500" || adm.WoodpeckerURL != "http://w:8000" {
		t.Errorf("urls = %q %q", adm.GiteaURL, adm.WoodpeckerURL)
	}
}

func TestFor_FailsSoft(t *testing.T) {
	ctx := context.Background()
	s := builder(fakeIdentity{userErr: errors.New("boom")}, nil, nil).For(ctx, "t")
	if s.Role != RoleDeveloper || s.Operator != "" || s.GiteaURL == "" {
		t.Errorf("identity failure should give the developer view with links: %+v", s)
	}
	s = builder(fakeIdentity{login: "dev1", member: true}, nil, errors.New("store down")).For(ctx, "t")
	if !s.IsApprover || s.PendingApprovals != 0 {
		t.Errorf("a store failure must only lose the badge: %+v", s)
	}
	s = builder(fakeIdentity{login: "dev1", teamErr: errors.New("teams down")}, nil, nil).For(ctx, "t")
	if s.IsApprover || s.Operator != "dev1" {
		t.Errorf("a team lookup failure must not grant approver: %+v", s)
	}
}

func TestFor_CachesPerToken(t *testing.T) {
	calls := 0
	b := builder(fakeIdentity{login: "dev2", calls: &calls}, nil, nil)
	ctx := context.Background()
	b.For(ctx, "same")
	b.For(ctx, "same")
	if calls != 1 {
		t.Errorf("identity calls = %d, want 1 (cached)", calls)
	}
	b.For(ctx, "other")
	if calls != 2 {
		t.Errorf("a different token must not share the cache; calls = %d", calls)
	}
	// TTL expiry
	later := b.Now().Add(31 * time.Second)
	b.Now = func() time.Time { return later }
	b.For(ctx, "same")
	if calls != 3 {
		t.Errorf("entry should expire after the TTL; calls = %d", calls)
	}
}

func TestMiddleware_PutsShellInContext(t *testing.T) {
	b := builder(fakeIdentity{login: "dev1", member: true}, pendingRecs, nil)
	var got Shell
	h := b.Middleware(func(w http.ResponseWriter, r *http.Request) { got = FromContext(r.Context()) })
	req := httptest.NewRequest("GET", "/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyToken, "tok"))
	h(httptest.NewRecorder(), req)
	if got.Operator != "dev1" || !got.IsApprover {
		t.Errorf("shell in context = %+v", got)
	}

	// No token in the context: the request is still served, with a zero shell.
	got = Shell{Operator: "stale"}
	h(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if got.Operator != "" {
		t.Errorf("without a session the shell must be empty, got %+v", got)
	}
}

func TestWithContextRoundTrip(t *testing.T) {
	s := Shell{Operator: "x", Role: RoleAdmin}
	if FromContext(WithContext(context.Background(), s)).Operator != "x" {
		t.Error("round trip failed")
	}
	if FromContext(context.Background()).Operator != "" {
		t.Error("empty context must give a zero Shell")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, `CountPending`, package `shell` undefined.

- [ ] **Step 3: Implement**

Create `portal/internal/exceptions/pending.go`:

```go
package exceptions

import "time"

// CountPending is the number of requests waiting for the given approver:
// undecided, not expired, and not requested by that person (nobody approves
// their own request).
func CountPending(records []Record, user string, now time.Time) int {
	n := 0
	for _, r := range records {
		if r.Approved || r.Declined || r.Requester == user || !r.Expiry.After(now) {
			continue
		}
		n++
	}
	return n
}
```

Create `portal/internal/shell/shell.go`:

```go
// Package shell computes what the page chrome needs for one signed-in
// person: who they are, what role they hold, how many approvals wait for
// them, and where Gitea and Woodpecker live. The result is only used to
// decide what the sidebar shows; every action is still authorised on the
// server by its own handler.
package shell

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/exceptions"
)

type Role string

const (
	RoleDeveloper Role = "developer"
	RoleApprover  Role = "approver"
	RoleAdmin     Role = "admin"
)

type Shell struct {
	Operator         string
	Role             Role
	IsApprover       bool
	IsAdmin          bool
	PendingApprovals int
	GiteaURL         string
	WoodpeckerURL    string
}

func (s Shell) RoleLabel() string {
	switch s.Role {
	case RoleAdmin:
		return "Admin"
	case RoleApprover:
		return "Approver"
	default:
		return "Developer"
	}
}

// Identity is the slice of the Gitea client the shell needs, called with
// the signed-in user's own token.
type Identity interface {
	CurrentUser(ctx context.Context) (login string, isAdmin bool, err error)
	IsOnTeam(ctx context.Context, org, team string) (bool, error)
}

type cacheEntry struct {
	shell   Shell
	expires time.Time
}

type Builder struct {
	NewIdentity   func(token string) Identity
	Records       func(ctx context.Context) ([]exceptions.Record, error)
	Org, Team     string
	GiteaURL      string
	WoodpeckerURL string
	TTL           time.Duration
	Now           func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
}

func cacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// For builds the shell for one token. It never fails: any lookup that does
// not work simply leaves that part of the chrome out (no badge, developer
// view), so a Gitea hiccup cannot take a page down.
func (b *Builder) For(ctx context.Context, token string) Shell {
	key := cacheKey(token)
	now := b.Now()
	b.mu.Lock()
	if e, ok := b.cache[key]; ok && now.Before(e.expires) {
		b.mu.Unlock()
		return e.shell
	}
	b.mu.Unlock()

	s := Shell{Role: RoleDeveloper, GiteaURL: b.GiteaURL, WoodpeckerURL: b.WoodpeckerURL}
	id := b.NewIdentity(token)
	if login, admin, err := id.CurrentUser(ctx); err == nil {
		s.Operator, s.IsAdmin = login, admin
		if member, err := id.IsOnTeam(ctx, b.Org, b.Team); err == nil {
			s.IsApprover = member
		}
		if admin {
			s.IsApprover = true
		}
		switch {
		case s.IsAdmin:
			s.Role = RoleAdmin
		case s.IsApprover:
			s.Role = RoleApprover
		}
		if s.IsApprover {
			if recs, err := b.Records(ctx); err == nil {
				s.PendingApprovals = exceptions.CountPending(recs, login, now)
			}
		}
	}

	b.mu.Lock()
	if b.cache == nil {
		b.cache = map[string]cacheEntry{}
	}
	b.cache[key] = cacheEntry{shell: s, expires: now.Add(b.TTL)}
	b.mu.Unlock()
	return s
}

type ctxKey struct{}

func WithContext(ctx context.Context, s Shell) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext returns the shell the middleware stored, or a zero Shell
// (developer view, no links) when there is none.
func FromContext(ctx context.Context) Shell {
	s, _ := ctx.Value(ctxKey{}).(Shell)
	return s
}

// Middleware must run inside the authentication wrapper so the user's token
// is already in the request context.
func (b *Builder) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token, ok := auth.TokenFromContext(r.Context()); ok && token != "" {
			r = r.WithContext(WithContext(r.Context(), b.For(r.Context(), token)))
		}
		next(w, r)
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOTEST`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/exceptions/pending.go portal/internal/exceptions/pending_test.go portal/internal/shell
git commit -m "feat(portal): shell package - role, pending approvals and links per request"
```

---

### Task 3: Self-hosted fonts, htmx and shell styles

**Files:**
- Create: `portal/web/static/fonts.css`, `portal/web/static/fonts/*.woff2`, `portal/web/static/vendor/htmx.min.js`, `portal/web/static/vendor/htmx-sse.js`, `portal/web/static/vendor/README.md`, `portal/web/static/shell.css`

**Interfaces:**
- Produces: URLs `/static/fonts.css`, `/static/shell.css`, `/static/vendor/htmx.min.js`, `/static/vendor/htmx-sse.js`, `/static/shell.js` (Task 4 creates the last). CSS classes used by Task 4/5 templates: `.ssdlc-nav-badge`, `.ssdlc-launch`, `.ssdlc-pal-btn`, `.ssdlc-pal-bg`, `.ssdlc-pal`, `.ssdlc-pal-item`, `.ssdlc-who`, `.ssdlc-help-hero`, `.ssdlc-steps`, `.ssdlc-topic`, `.ssdlc-tag`, `.ssdlc-ext`.

- [ ] **Step 1: Download the assets**

Run from the worktree root in Git Bash:

```bash
S=portal/web/static
mkdir -p $S/fonts $S/vendor
F=https://cdn.jsdelivr.net/npm/@fontsource
for w in 400 500 600; do curl -fsSL "$F/ibm-plex-sans@5.0.8/files/ibm-plex-sans-latin-$w-normal.woff2" -o "$S/fonts/ibm-plex-sans-$w.woff2"; done
for w in 400 500; do curl -fsSL "$F/ibm-plex-mono@5.0.8/files/ibm-plex-mono-latin-$w-normal.woff2" -o "$S/fonts/ibm-plex-mono-$w.woff2"; done
curl -fsSL https://unpkg.com/htmx.org@1.9.12/dist/htmx.min.js -o $S/vendor/htmx.min.js
curl -fsSL https://unpkg.com/htmx.org@1.9.12/dist/ext/sse.js -o $S/vendor/htmx-sse.js
ls -la $S/fonts $S/vendor
sha256sum $S/fonts/*.woff2 $S/vendor/*.js
```

Expected: five `.woff2` files (each between 8 KB and 80 KB) and the two scripts (`htmx.min.js` about 48 KB), no errors. If any download fails, stop and report BLOCKED.

- [ ] **Step 2: Record provenance**

Create `portal/web/static/vendor/README.md` (fill the checksums from the `sha256sum` output above; one line per file):

```markdown
# Vendored front-end assets

Served by the portal itself so pages make no requests to a CDN.

| File | Source | Version | License |
|---|---|---|---|
| `htmx.min.js` | https://unpkg.com/htmx.org@1.9.12/dist/htmx.min.js | 1.9.12 | BSD-2-Clause |
| `htmx-sse.js` | https://unpkg.com/htmx.org@1.9.12/dist/ext/sse.js | 1.9.12 | BSD-2-Clause |
| `../fonts/ibm-plex-sans-{400,500,600}.woff2` | @fontsource/ibm-plex-sans 5.0.8 (latin subset) | 5.0.8 | SIL OFL 1.1 |
| `../fonts/ibm-plex-mono-{400,500}.woff2` | @fontsource/ibm-plex-mono 5.0.8 (latin subset) | 5.0.8 | SIL OFL 1.1 |

SHA-256 (taken when vendored):

<one `hash  path` line per file from the sha256sum output>
```

- [ ] **Step 3: Font-face and shell CSS**

Create `portal/web/static/fonts.css`:

```css
/* IBM Plex, self-hosted (SIL OFL 1.1). See vendor/README.md. */
@font-face{font-family:'IBM Plex Sans';font-weight:400;font-style:normal;font-display:swap;src:url('/static/fonts/ibm-plex-sans-400.woff2') format('woff2')}
@font-face{font-family:'IBM Plex Sans';font-weight:500;font-style:normal;font-display:swap;src:url('/static/fonts/ibm-plex-sans-500.woff2') format('woff2')}
@font-face{font-family:'IBM Plex Sans';font-weight:600;font-style:normal;font-display:swap;src:url('/static/fonts/ibm-plex-sans-600.woff2') format('woff2')}
@font-face{font-family:'IBM Plex Mono';font-weight:400;font-style:normal;font-display:swap;src:url('/static/fonts/ibm-plex-mono-400.woff2') format('woff2')}
@font-face{font-family:'IBM Plex Mono';font-weight:500;font-style:normal;font-display:swap;src:url('/static/fonts/ibm-plex-mono-500.woff2') format('woff2')}
```

Create `portal/web/static/shell.css`:

```css
/* App shell: sidebar, badges, quick launch, Ctrl+K palette, help page.
   Uses the tokens from tokens.css. */
.ssdlc-sidebar{display:flex;flex-direction:column;position:sticky;top:0;height:100vh;box-sizing:border-box;overflow-y:auto}
.ssdlc-brand{display:flex;align-items:center;gap:10px;padding:0 20px 14px;font-weight:600;font-size:16px;color:var(--ssdlc-text-1)}
.ssdlc-brand i{width:26px;height:26px;border-radius:7px;background:linear-gradient(135deg,var(--ssdlc-accent),var(--ssdlc-bot-accent));display:inline-block}
.ssdlc-pal-btn{margin:0 16px 12px;display:flex;justify-content:space-between;align-items:center;background:var(--ssdlc-bg);border:1px solid var(--ssdlc-border);border-radius:8px;padding:8px 10px;color:var(--ssdlc-text-3);font:inherit;font-size:13px;cursor:pointer}
.ssdlc-pal-btn:hover{border-color:var(--ssdlc-border-alt);color:var(--ssdlc-text-2)}
.ssdlc-pal-btn kbd,.ssdlc-kbd{border:1px solid var(--ssdlc-border-alt);border-radius:5px;padding:0 6px;font-size:11px;font-family:inherit;color:var(--ssdlc-text-3)}
.ssdlc-sidebar a{display:flex;align-items:center;gap:8px}
.ssdlc-nav-badge{margin-left:auto;background:var(--ssdlc-accent);color:var(--ssdlc-bg);border-radius:10px;font-size:11px;font-weight:600;padding:1px 7px}
.ssdlc-launch{margin-top:auto;padding:12px 0 0;border-top:1px solid var(--ssdlc-border)}
.ssdlc-launch .lbl,.ssdlc-who .lbl{display:block;padding:0 20px 4px;font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--ssdlc-text-3)}
.ssdlc-launch a{justify-content:space-between;padding:7px 20px}
.ssdlc-who{padding:12px 20px 4px;border-top:1px solid var(--ssdlc-border);color:var(--ssdlc-text-3);font-size:13px}
.ssdlc-who .lbl{padding:0}
.ssdlc-who b{color:var(--ssdlc-text-1);font-weight:500}
.ssdlc-who a{display:inline;color:var(--ssdlc-text-3);text-decoration:underline;padding:0}
.ssdlc-ext{display:inline-block;color:var(--ssdlc-accent);text-decoration:none;font-size:12px;white-space:nowrap;border:1px solid var(--ssdlc-border-alt);border-radius:6px;padding:2px 7px;margin-right:4px}
.ssdlc-ext:hover{background:var(--ssdlc-accent-bg);border-color:var(--ssdlc-accent)}
/* palette */
.ssdlc-pal-bg{position:fixed;inset:0;background:#000a;display:none;align-items:flex-start;justify-content:center;padding-top:14vh;z-index:50}
.ssdlc-pal-bg.on{display:flex}
.ssdlc-pal{width:min(560px,92vw);background:var(--ssdlc-surface);border:1px solid var(--ssdlc-border-alt);border-radius:12px;overflow:hidden}
.ssdlc-pal input{width:100%;box-sizing:border-box;border:0;border-bottom:1px solid var(--ssdlc-border);background:transparent;color:var(--ssdlc-text-1);padding:14px 16px;font:inherit;font-size:15px;outline:none}
.ssdlc-pal-item{display:flex;justify-content:space-between;gap:10px;padding:10px 16px;color:var(--ssdlc-text-1);text-decoration:none;font-size:14px}
.ssdlc-pal-item span{color:var(--ssdlc-text-3);font-size:12px}
.ssdlc-pal-item.on,.ssdlc-pal-item:hover{background:var(--ssdlc-surface-alt)}
/* help */
.ssdlc-help-hero{background:linear-gradient(135deg,var(--ssdlc-accent-bg),var(--ssdlc-surface));border:1px solid var(--ssdlc-border-alt);border-radius:14px;padding:24px 26px;margin-bottom:16px}
.ssdlc-help-hero form{display:flex;gap:8px;margin-top:12px;max-width:560px}
.ssdlc-help-hero input[type=search]{flex:1;background:var(--ssdlc-bg);border:1px solid var(--ssdlc-border-alt);border-radius:8px;color:var(--ssdlc-text-1);padding:10px 12px;font:inherit}
.ssdlc-steps{list-style:none;margin:10px 0 0;padding:0;counter-reset:s}
.ssdlc-steps li{counter-increment:s;display:flex;gap:10px;align-items:flex-start;padding:5px 0;color:var(--ssdlc-text-2);line-height:1.5}
.ssdlc-steps li::before{content:counter(s);flex:none;width:24px;height:24px;border-radius:50%;background:var(--ssdlc-accent);color:var(--ssdlc-bg);display:inline-flex;align-items:center;justify-content:center;font-size:12px;font-weight:600}
.ssdlc-topic{border:1px solid var(--ssdlc-border);border-radius:10px;background:var(--ssdlc-surface);margin-bottom:8px}
.ssdlc-topic summary{display:flex;gap:10px;align-items:center;padding:13px 16px;cursor:pointer;font-weight:500;list-style:none}
.ssdlc-topic summary::-webkit-details-marker{display:none}
.ssdlc-topic summary::after{content:"\203A";margin-left:auto;color:var(--ssdlc-text-3);transition:transform .2s}
.ssdlc-topic[open] summary::after{transform:rotate(90deg)}
.ssdlc-topic .a{padding:0 16px 14px;color:var(--ssdlc-text-2);line-height:1.65}
.ssdlc-tag{font-size:11px;border:1px solid var(--ssdlc-border-alt);border-radius:5px;padding:0 6px;color:var(--ssdlc-text-3);font-weight:400}
```

Also change the `body` rule's font stack is already `'IBM Plex Sans'` in `tokens.css`; make no change there.

- [ ] **Step 4: Verify the files are servable (Go check)**

Run: `docker run --rm -v "$(pwd -W):/src" -w /src alpine sh -c "ls -la portal/web/static portal/web/static/fonts portal/web/static/vendor && head -c 120 portal/web/static/vendor/htmx.min.js"` with `MSYS_NO_PATHCONV=1`.
Expected: files present, and the htmx file starts with JavaScript (not an HTML error page).

- [ ] **Step 5: Commit**

```bash
git add portal/web/static
git commit -m "feat(portal): self-hosted IBM Plex, htmx and shell styles (no CDN)"
```

---

### Task 4: New layout, `Shell` in every page, and the Ctrl+K palette

**Files:**
- Modify: `portal/web/templates/layout.html`
- Create: `portal/web/static/shell.js`, `portal/internal/handlers/layout_test.go`
- Modify: `portal/internal/handlers/dashboard.go`, `prreport.go`, `exceptions.go`, `onboarding.go`

**Interfaces:**
- Consumes: `shell.Shell`, `shell.FromContext`, `shell.WithContext`, the CSS classes and URLs from Task 3.
- Produces: `layout.html` renders `.Shell.*` and `.ActiveNav`; every data struct passed to `ExecuteTemplate(w, "layout", data)` must have a `Shell shell.Shell` field. `.ActiveNav` values: `dashboard`, `exceptions`, `onboarding`, `help`.

- [ ] **Step 1: Write the failing tests**

Create `portal/internal/handlers/layout_test.go`:

```go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/shell"
)

func renderDashboardAs(t *testing.T, s shell.Shell) string {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/user":
			w.Write([]byte(`{"login":"` + s.Operator + `"}`))
		case "/api/v1/repos/search":
			w.Write([]byte(`{"data":[]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer fake.Close()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req = req.WithContext(shell.WithContext(req.Context(), s))
	rec := httptest.NewRecorder()
	Dashboard(fake.URL)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestLayout_DeveloperSidebar(t *testing.T) {
	body := renderDashboardAs(t, shell.Shell{
		Operator: "dev2", Role: shell.RoleDeveloper,
		GiteaURL: "http://192.168.1.28:3500", WoodpeckerURL: "http://192.168.1.28:8000",
	})
	for _, want := range []string{
		`href="/dashboard"`, "Overview", `href="/exceptions"`, `href="/help"`,
		`href="http://192.168.1.28:3500"`, `href="http://192.168.1.28:8000"`,
		"Quick launch", "dev2", "Developer", "Projects", "Issues",
		`/static/shell.js`, `/static/fonts.css`, `/static/vendor/htmx.min.js`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{`href="/onboarding"`, "fonts.googleapis.com", "unpkg.com", "ssdlc-nav-badge"} {
		if strings.Contains(body, bad) {
			t.Errorf("developer view must not contain %q", bad)
		}
	}
	// Screens that do not exist yet are visible but not links.
	if strings.Contains(body, `href="/projects"`) || strings.Contains(body, `href="/issues"`) {
		t.Error("Projects and Issues must be disabled placeholders, not links")
	}
}

func TestLayout_ApproverBadgeAndAdminEntry(t *testing.T) {
	approver := renderDashboardAs(t, shell.Shell{Operator: "dev1", Role: shell.RoleApprover, IsApprover: true, PendingApprovals: 2, GiteaURL: "http://g", WoodpeckerURL: "http://w"})
	if !strings.Contains(approver, `ssdlc-nav-badge">2<`) {
		t.Errorf("approver should see the pending badge; body:\n%s", approver)
	}
	if strings.Contains(approver, `href="/onboarding"`) {
		t.Error("an approver who is not an admin must not see Admin")
	}

	admin := renderDashboardAs(t, shell.Shell{Operator: "gateadmin", Role: shell.RoleAdmin, IsApprover: true, IsAdmin: true, GiteaURL: "http://g", WoodpeckerURL: "http://w"})
	if !strings.Contains(admin, `href="/onboarding"`) || !strings.Contains(admin, "Admin") {
		t.Error("an admin should see the Admin entry")
	}
	if strings.Contains(admin, "ssdlc-nav-badge") {
		t.Error("no badge when nothing is pending")
	}
}

func TestLayout_NoShellStillRenders(t *testing.T) {
	body := renderDashboardAs(t, shell.Shell{})
	if !strings.Contains(body, "Overview") {
		t.Error("the page must render with an empty shell (developer view, no links)")
	}
	if strings.Contains(body, "Quick launch") {
		t.Error("no Quick launch block without URLs")
	}
}

func TestLayout_LinksAreEscaped(t *testing.T) {
	body := renderDashboardAs(t, shell.Shell{Operator: `<script>x</script>`, GiteaURL: "http://g", WoodpeckerURL: "http://w"})
	if strings.Contains(body, "<script>x</script>") {
		t.Error("operator name must be HTML-escaped")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, `Shell` unknown/undefined in the handlers and old sidebar markup.

- [ ] **Step 3: Rewrite the layout**

Replace the whole content of `portal/web/templates/layout.html` with:

```html
<!-- portal/web/templates/layout.html -->
{{define "layout"}}
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{block "title" .}}SSDLC Portal{{end}}</title>
<link rel="stylesheet" href="/static/fonts.css">
<link rel="stylesheet" href="/static/tokens.css">
<link rel="stylesheet" href="/static/shell.css">
<script src="/static/vendor/htmx.min.js"></script>
<script src="/static/vendor/htmx-sse.js"></script>
<script src="/static/shell.js" defer></script>
</head>
<body>
<div class="ssdlc-shell">
  <nav class="ssdlc-sidebar" aria-label="Main">
    <div class="ssdlc-brand"><i></i>SSDLC</div>
    <button type="button" id="pal-open" class="ssdlc-pal-btn">Jump to&hellip; <kbd>Ctrl K</kbd></button>
    <a href="/dashboard" data-pal-item data-pal-sub="page" class="{{if eq .ActiveNav "dashboard"}}active{{end}}">Overview</a>
    <a class="disabled" title="Arrives in the next step of the portal redesign">Projects</a>
    <a class="disabled" title="Arrives in the next step of the portal redesign">Issues</a>
    <a href="/exceptions" data-pal-item data-pal-sub="page" class="{{if eq .ActiveNav "exceptions"}}active{{end}}">Exceptions{{if and .Shell.IsApprover (gt .Shell.PendingApprovals 0)}}<span class="ssdlc-nav-badge">{{.Shell.PendingApprovals}}</span>{{end}}</a>
    {{if .Shell.IsAdmin}}<a href="/onboarding" data-pal-item data-pal-sub="page" class="{{if eq .ActiveNav "onboarding"}}active{{end}}">Admin</a>{{end}}
    <a href="/help" data-pal-item data-pal-sub="page" class="{{if eq .ActiveNav "help"}}active{{end}}">Help</a>
    {{if .Shell.GiteaURL}}
    <div class="ssdlc-launch"><span class="lbl">Quick launch</span>
      <a href="{{.Shell.GiteaURL}}" target="_blank" rel="noopener" data-pal-item data-pal-label="Open Gitea" data-pal-sub="new tab">Gitea (code, PRs) <span>&#8599;</span></a>
      <a href="{{.Shell.WoodpeckerURL}}" target="_blank" rel="noopener" data-pal-item data-pal-label="Open Woodpecker" data-pal-sub="new tab">Woodpecker (CI) <span>&#8599;</span></a>
    </div>
    {{end}}
    {{if .Shell.Operator}}
    <div class="ssdlc-who"><span class="lbl">Signed in</span><b>{{.Shell.Operator}}</b> &middot; {{.Shell.RoleLabel}}<br><a href="/logout">sign out</a></div>
    {{end}}
  </nav>
  <main class="ssdlc-main">
    {{block "content" .}}{{end}}
  </main>
</div>
<div class="ssdlc-pal-bg" id="pal-bg"><div class="ssdlc-pal"><input id="pal-input" type="text" placeholder="Jump to a page or open Gitea or Woodpecker..." autocomplete="off"><div id="pal-list"></div></div></div>
</body>
</html>
{{end}}
{{define "statusBadge"}}<span class="ssdlc-badge {{.CSSClass}}">{{.Label}}</span>{{end}}
```

- [ ] **Step 4: The palette script**

Create `portal/web/static/shell.js`:

```js
// Ctrl+K jump palette. Progressive enhancement: the sidebar links work
// without it. Items are read from the page itself (elements marked
// data-pal-item), and results are built with textContent, never innerHTML.
(function () {
  var bg = document.getElementById('pal-bg');
  var input = document.getElementById('pal-input');
  var list = document.getElementById('pal-list');
  if (!bg || !input || !list) return;

  var items = [].slice.call(document.querySelectorAll('[data-pal-item]')).map(function (a) {
    var label = a.getAttribute('data-pal-label') || a.textContent.replace(/↗/g, '').replace(/\d+$/, '').trim();
    return { label: label, sub: a.getAttribute('data-pal-sub') || '', href: a.getAttribute('href'), ext: a.target === '_blank' };
  });
  var sel = 0;
  var shown = [];

  function render() {
    var q = input.value.toLowerCase();
    shown = items.filter(function (i) { return !q || (i.label + ' ' + i.sub).toLowerCase().indexOf(q) > -1; }).slice(0, 9);
    if (sel >= shown.length) sel = Math.max(0, shown.length - 1);
    list.innerHTML = '';
    shown.forEach(function (i, n) {
      var a = document.createElement('a');
      a.className = 'ssdlc-pal-item' + (n === sel ? ' on' : '');
      a.href = i.href;
      if (i.ext) { a.target = '_blank'; a.rel = 'noopener'; }
      a.textContent = i.label;
      var s = document.createElement('span');
      s.textContent = i.sub;
      a.appendChild(s);
      list.appendChild(a);
    });
  }
  function open() { bg.classList.add('on'); input.value = ''; sel = 0; render(); input.focus(); }
  function close() { bg.classList.remove('on'); }

  document.addEventListener('keydown', function (e) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); open(); return; }
    if (!bg.classList.contains('on')) return;
    if (e.key === 'Escape') { close(); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); sel = Math.min(sel + 1, shown.length - 1); render(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); sel = Math.max(sel - 1, 0); render(); }
    else if (e.key === 'Enter' && list.children[sel]) { e.preventDefault(); list.children[sel].click(); }
  });
  input.addEventListener('input', function () { sel = 0; render(); });
  bg.addEventListener('mousedown', function (e) { if (e.target === bg) close(); });
  var btn = document.getElementById('pal-open');
  if (btn) btn.addEventListener('click', open);
})();
```

- [ ] **Step 5: Give every page a `Shell`**

Add the import `"ssdlc-portal/internal/shell"` to each file below and make these edits.

`portal/internal/handlers/dashboard.go`: in the anonymous struct inside `Dashboard` add a field and set it. Replace

```go
			ActiveNav    string
			Operator     string
```
with
```go
			ActiveNav    string
			Operator     string
			Shell        shell.Shell
```
and replace `}{ActiveNav: "dashboard", PullRequests: rows}` with `}{ActiveNav: "dashboard", PullRequests: rows, Shell: shell.FromContext(r.Context())}`.

`portal/internal/handlers/prreport.go`: in `prReportData` add `Shell shell.Shell` directly under the `Operator string` field, and in the `data := prReportData{...}` literal add `Shell: shell.FromContext(r.Context()),` as the last field.

`portal/internal/handlers/onboarding.go`: replace `data := struct{ ActiveNav, Operator string }{ActiveNav: "onboarding"}` with

```go
		data := struct {
			ActiveNav, Operator string
			Shell               shell.Shell
		}{ActiveNav: "onboarding", Shell: shell.FromContext(r.Context())}
```

`portal/internal/handlers/exceptions.go`: two places. In `ExceptionRequestForm`, add `Shell shell.Shell` to the anonymous struct next to `Operator string` and `Shell: shell.FromContext(r.Context()),` to its literal. In `ExceptionsQueue`, do the same: add `Shell shell.Shell` to the struct next to `Operator  string` and change the literal `{ActiveNav: "exceptions", Operator: operator, Pending: pending, Active: active, Expired: expired, Declined: declined}` to end with `, Shell: shell.FromContext(r.Context())}`.

- [ ] **Step 6: Run to verify pass**

Run: `GOTEST` then `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/src" node:20-alpine node --check /src/portal/web/static/shell.js && echo JS-OK`
Expected: all Go tests PASS (including the four `TestLayout_*` and every existing handler test), `JS-OK`. If an existing handler test now fails with "can't evaluate field Shell", a data struct was missed: add the field there too.

- [ ] **Step 7: Commit**

```bash
git add portal/web portal/internal/handlers
git commit -m "feat(portal): new app shell - role-aware sidebar, badges, quick launch, Ctrl+K palette"
```

---

### Task 5: The Help page

**Files:**
- Create: `portal/internal/help/help.go`, `portal/internal/help/help_test.go`, `portal/internal/handlers/help.go`, `portal/internal/handlers/help_test.go`, `portal/web/templates/help.html`

**Interfaces:**
- Produces: `help.Topic{ID, Question string; Answer template.HTML; Roles []string; GoTo, GoLabel string}`, `help.Topics() []help.Topic`, `help.Filter(topics []Topic, q string) []Topic`, `help.FirstSteps(role shell.Role) []string`, `handlers.Help() http.HandlerFunc`.

- [ ] **Step 1: Write the failing tests**

Create `portal/internal/help/help_test.go`:

```go
package help

import (
	"strings"
	"testing"

	"ssdlc-portal/internal/shell"
)

func TestTopicsAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, tp := range Topics() {
		if tp.ID == "" || tp.Question == "" || tp.Answer == "" || len(tp.Roles) == 0 {
			t.Errorf("incomplete topic %+v", tp)
		}
		if seen[tp.ID] {
			t.Errorf("duplicate id %q", tp.ID)
		}
		seen[tp.ID] = true
		if tp.GoTo != "" && !strings.HasPrefix(tp.GoTo, "/") {
			t.Errorf("topic %q GoTo must be a same-site path, got %q", tp.ID, tp.GoTo)
		}
	}
	for _, id := range []string{"blocked", "exception", "approve", "grade", "sla", "baseline", "signin", "shortcuts"} {
		if !seen[id] {
			t.Errorf("missing topic %q", id)
		}
	}
}

func TestFilter(t *testing.T) {
	all := Topics()
	if got := Filter(all, ""); len(got) != len(all) {
		t.Errorf("empty query should keep everything: %d vs %d", len(got), len(all))
	}
	got := Filter(all, "  BLOCKED ")
	if len(got) == 0 || got[0].ID != "blocked" {
		t.Errorf("search should be trimmed and case-insensitive; got %+v", got)
	}
	if got := Filter(all, "zzzz-no-such-word"); len(got) != 0 {
		t.Errorf("expected no results, got %d", len(got))
	}
	// answers are searched too, not just questions
	if got := Filter(all, "second person"); len(got) == 0 {
		t.Error("answer text should be searchable")
	}
}

func TestFirstSteps(t *testing.T) {
	for _, r := range []shell.Role{shell.RoleDeveloper, shell.RoleApprover, shell.RoleAdmin} {
		if steps := FirstSteps(r); len(steps) < 3 {
			t.Errorf("%s: want at least 3 first steps, got %v", r, steps)
		}
	}
	if FirstSteps("") == nil || len(FirstSteps("")) == 0 {
		t.Error("an unknown role falls back to the developer steps")
	}
}
```

Create `portal/internal/handlers/help_test.go`:

```go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/shell"
)

func getHelp(t *testing.T, target string, s shell.Shell) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(shell.WithContext(req.Context(), s))
	rec := httptest.NewRecorder()
	Help()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	return rec.Body.String()
}

func TestHelp_RoleSpecificFirstSteps(t *testing.T) {
	dev := getHelp(t, "/help", shell.Shell{Operator: "dev2", Role: shell.RoleDeveloper})
	if !strings.Contains(dev, "first steps as a developer") || !strings.Contains(dev, "Why was my pull request blocked?") {
		t.Error("developer help page incomplete")
	}
	appr := getHelp(t, "/help", shell.Shell{Operator: "dev1", Role: shell.RoleApprover, IsApprover: true})
	if !strings.Contains(appr, "first steps as an approver") {
		t.Error("approver page should say 'an approver'")
	}
	adm := getHelp(t, "/help", shell.Shell{Operator: "gateadmin", Role: shell.RoleAdmin, IsAdmin: true})
	if !strings.Contains(adm, "first steps as an admin") {
		t.Error("admin page should say 'an admin'")
	}
}

func TestHelp_SearchFiltersAndEmptyState(t *testing.T) {
	body := getHelp(t, "/help?q=baseline", shell.Shell{Role: shell.RoleDeveloper})
	if !strings.Contains(body, "What is the baseline?") {
		t.Error("expected the baseline topic")
	}
	if strings.Contains(body, "Keyboard shortcuts") {
		t.Error("unrelated topics must be filtered out")
	}
	none := getHelp(t, "/help?q=zzzz-nothing", shell.Shell{Role: shell.RoleDeveloper})
	if !strings.Contains(none, "No topics match") {
		t.Error("expected the empty state")
	}
}

func TestHelp_QueryIsEscaped(t *testing.T) {
	body := getHelp(t, "/help?q=%3Cscript%3Ealert(1)%3C/script%3E", shell.Shell{Role: shell.RoleDeveloper})
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("the search term must be HTML-escaped")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, package `help` and `Help` undefined.

- [ ] **Step 3: Implement the content package**

Create `portal/internal/help/help.go`:

```go
// Package help holds the portal's Help content. It is fixed text owned by
// the code (safe to render as HTML), filtered on the server so the page
// needs no JavaScript.
package help

import (
	"html/template"
	"strings"

	"ssdlc-portal/internal/shell"
)

type Topic struct {
	ID       string
	Question string
	Answer   template.HTML
	Roles    []string
	GoTo     string // same-site path, or empty
	GoLabel  string
}

var all = []string{"Developer", "Approver", "Admin"}

func Topics() []Topic {
	return []Topic{
		{ID: "blocked", Question: "Why was my pull request blocked?", Roles: []string{"Developer"},
			Answer: "The gate found at least one <b>new Critical or High</b> issue in the code you changed. Open the pull request to see each issue, why it matters and how to fix it. Existing problems in the <b>baseline</b> never block you. The pull request unblocks once the issues are fixed or an exception is approved."},
		{ID: "fix", Question: "How do I fix an issue?", Roles: []string{"Developer"},
			Answer: "Read the explanation and the suggested change, fix the code and push to your branch. The gate runs again in about a minute. If a fix is not possible right now, request an exception instead."},
		{ID: "exception", Question: "When should I request an exception?", Roles: []string{"Developer"},
			Answer:  "Only when an issue is real but acceptable for now, for example test-only code, or a control that exists elsewhere. Explain <b>why</b>, choose an expiry (at most 90 days) and send it. It needs one approver who is not you, and it always expires, after which the issue blocks again.",
			GoTo:    "/exceptions", GoLabel: "Open Exceptions"},
		{ID: "approve", Question: "Who can approve my exception?", Roles: []string{"Developer", "Approver"},
			Answer: "Anyone in the approvers team except the person who asked. You can never approve your own request: it needs a second person. Approvers see a badge next to Exceptions in the sidebar."},
		{ID: "review", Question: "How do I review an exception?", Roles: []string{"Approver"},
			Answer:  "Open <b>Exceptions</b>. Read the reason and the expiry. Approve if the risk is understood and temporary, decline if it should be fixed instead.",
			GoTo:    "/exceptions", GoLabel: "Open Exceptions"},
		{ID: "setup", Question: "How do I add a project to the gate?", Roles: []string{"Admin"},
			Answer:  "Open <b>Admin</b>, enter the repository owner and name, and start setup. The portal activates the repository in the CI system, records today's findings as the baseline, adds the gate pipeline and turns on branch protection.",
			GoTo:    "/onboarding", GoLabel: "Set up a project"},
		{ID: "grade", Question: "What do the letter grades mean?", Roles: all,
			Answer: "Each project gets a grade from its open issues, weighted by severity: <b>A</b> is 90 or above, <b>B</b> 75, <b>C</b> 60, <b>D</b> 40 and <b>F</b> below 40. One open Critical issue is enough to drop a project to a D or F, on purpose. (Grades arrive with the Projects screens.)"},
		{ID: "sla", Question: "What is the \"fix within\" countdown?", Roles: all,
			Answer: "Each severity has a target: Critical 2 days, High 7, Medium 30, Low 90. The clock starts when the issue is first found and pauses while an approved exception is in force. Baseline issues have no clock. (The countdown arrives with the Issues screens.)"},
		{ID: "baseline", Question: "What is the baseline?", Roles: all,
			Answer: "When a project joins the gate, everything already in the code is recorded as the baseline. It never blocks merges, and it can only shrink: nothing can be added to it. Secrets are never baselined."},
		{ID: "signin", Question: "Do I need to sign in again for Gitea or Woodpecker?", Roles: all,
			Answer: "No separate account is needed: it is the same Gitea account. The Quick launch links open in a new tab. If Woodpecker asks you to authorise the first time, click <b>Authorize</b> once. After that it signs you in automatically."},
		{ID: "shortcuts", Question: "Keyboard shortcuts", Roles: all,
			Answer: "<kbd class=\"ssdlc-kbd\">Ctrl</kbd> <kbd class=\"ssdlc-kbd\">K</kbd> opens the jump palette to reach any page, or open Gitea or Woodpecker. <kbd class=\"ssdlc-kbd\">Esc</kbd> closes it."},
	}
}

// Filter keeps the topics whose question or answer contains q
// (case-insensitive, surrounding spaces ignored). An empty q keeps all.
func Filter(topics []Topic, q string) []Topic {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return topics
	}
	var out []Topic
	for _, t := range topics {
		if strings.Contains(strings.ToLower(t.Question+" "+string(t.Answer)), q) {
			out = append(out, t)
		}
	}
	return out
}

// FirstSteps is the short checklist shown at the top of the page for a role.
func FirstSteps(role shell.Role) []string {
	switch role {
	case shell.RoleApprover:
		return []string{
			"Open <b>Exceptions</b> when the badge shows a number",
			"Read the reason and the expiry, and judge the risk",
			"Approve or decline (never your own requests)",
			"Use the Overview to spot anything that is overdue",
		}
	case shell.RoleAdmin:
		return []string{
			"Open <b>Admin</b> to set up a project",
			"Enter the repository owner and name and run setup",
			"Tell the team the project is now behind the gate",
			"Watch for exceptions waiting in the sidebar badge",
		}
	default:
		return []string{
			"Open a pull request as usual in Gitea",
			"Watch the gate result on the pull request or in the Overview",
			"If it is blocked, fix the issue or request an exception",
			"Merge once the gate passes and a second person approves",
		}
	}
}
```

Note: `FirstSteps` returns HTML fragments (`<b>`) authored in code; the template renders them with `safeHTML`-equivalent by converting in the handler (Step 4).

- [ ] **Step 4: The handler and template**

Create `portal/internal/handlers/help.go`:

```go
package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"

	"ssdlc-portal/internal/help"
	"ssdlc-portal/internal/shell"
)

var helpTmpl = template.Must(template.ParseFiles(
	filepath.Join(templateDir, "layout.html"), filepath.Join(templateDir, "help.html"),
))

// Help renders the role-aware help page. Searching is a plain GET (?q=), so
// it works without JavaScript.
func Help() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := shell.FromContext(r.Context())
		q := r.URL.Query().Get("q")

		steps := make([]template.HTML, 0, 4)
		for _, step := range help.FirstSteps(s.Role) {
			steps = append(steps, template.HTML(step)) // fixed text owned by the code
		}
		article := "a"
		if s.Role == shell.RoleApprover || s.Role == shell.RoleAdmin {
			article = "an"
		}
		roleWord := "developer"
		switch s.Role {
		case shell.RoleApprover:
			roleWord = "approver"
		case shell.RoleAdmin:
			roleWord = "admin"
		}

		topics := help.Filter(help.Topics(), q)
		data := struct {
			ActiveNav string
			Operator  string
			Shell     shell.Shell
			Query     string
			Article   string
			RoleWord  string
			Steps     []template.HTML
			Topics    []help.Topic
			Open      bool
		}{"help", s.Operator, s, q, article, roleWord, steps, topics, q != ""}
		if err := helpTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
```

Create `portal/web/templates/help.html`:

```html
<!-- portal/web/templates/help.html -->
{{define "content"}}
<h1 style="font-weight:600;">Help</h1>
<div class="ssdlc-help-hero">
  <b style="font-size:18px;">How can we help?</b>
  <div style="color:var(--ssdlc-text-3);margin-top:2px;">Showing guidance for the {{.RoleWord}} role.</div>
  <form method="get" action="/help">
    <input type="search" name="q" value="{{.Query}}" placeholder="Search: blocked, exception, baseline...">
    <button type="submit" class="ssdlc-btn ssdlc-btn-primary">Search</button>
  </form>
</div>

<div class="ssdlc-card">
  <b>Your first steps as {{.Article}} {{.RoleWord}}</b>
  <ol class="ssdlc-steps">{{range .Steps}}<li><span>{{.}}</span></li>{{end}}</ol>
</div>

{{if .Shell.GiteaURL}}
<div class="ssdlc-card">
  <b>Need something else?</b>
  <div style="color:var(--ssdlc-text-3);margin:6px 0 10px;">Ask your platform admin, or go straight to the source:</div>
  <a class="ssdlc-ext" href="{{.Shell.GiteaURL}}" target="_blank" rel="noopener">Gitea &#8599;</a>
  <a class="ssdlc-ext" href="{{.Shell.WoodpeckerURL}}" target="_blank" rel="noopener">Woodpecker &#8599;</a>
  <div style="color:var(--ssdlc-text-3);margin-top:10px;">Tip: press <kbd class="ssdlc-kbd">Ctrl</kbd> <kbd class="ssdlc-kbd">K</kbd> to jump anywhere.</div>
</div>
{{end}}

<h2 style="font-size:15px;margin:22px 0 10px;">{{if .Query}}Results for &ldquo;{{.Query}}&rdquo;{{else}}Common questions{{end}} <span style="color:var(--ssdlc-text-3);font-weight:400;">{{len .Topics}}</span></h2>
{{range .Topics}}
<details class="ssdlc-topic" id="{{.ID}}" {{if $.Open}}open{{end}}>
  <summary>{{.Question}}{{range .Roles}}<span class="ssdlc-tag">{{.}}</span>{{end}}</summary>
  <div class="a">{{.Answer}}{{if .GoTo}}<div style="margin-top:10px;"><a class="ssdlc-btn ssdlc-btn-primary" href="{{.GoTo}}">{{.GoLabel}} &rarr;</a></div>{{end}}</div>
</details>
{{else}}
<div class="ssdlc-card" style="text-align:center;color:var(--ssdlc-text-2);">No topics match &ldquo;{{.Query}}&rdquo;. Try a shorter word, or ask your platform admin.</div>
{{end}}
{{end}}
```

- [ ] **Step 5: Run to verify pass**

Run: `GOTEST`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add portal/internal/help portal/internal/handlers/help.go portal/internal/handlers/help_test.go portal/web/templates/help.html
git commit -m "feat(portal): Help page - role-aware first steps and searchable questions"
```

---

### Task 6: Deep links into Gitea and Woodpecker

**Files:**
- Modify: `portal/internal/report/report.go`, `portal/internal/report/report_test.go`
- Modify: `portal/internal/handlers/dashboard.go`, `portal/internal/handlers/prreport.go`, `portal/web/templates/dashboard.html`, `portal/web/templates/prreport.html`
- Create: `portal/internal/handlers/links_test.go`

**Interfaces:**
- Produces: `report.Report.WoodpeckerRepoID int` (`json:"woodpecker_repo_id"`), set whenever Build resolved the Woodpecker repo; PR report page gets header links; dashboard rows get a link to the PR in Gitea (`giteaclient.PullRequest.HTMLURL`, already fetched).

- [ ] **Step 1: Write the failing tests**

Append to `portal/internal/report/report_test.go`:

```go
func TestBuild_ExposesWoodpeckerRepoID(t *testing.T) {
	g := fakeGitea{pr: basePR()}
	w := fakeWP{repoID: 42, pipelines: []woodpeckerclient.Pipeline{{Number: 5, Commit: "abc"}}}
	r, err := Build(context.Background(), g, w, "o", "r", 12, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.WoodpeckerRepoID != 42 || r.PipelineNumber != 5 {
		t.Errorf("repo id = %d pipeline = %d", r.WoodpeckerRepoID, r.PipelineNumber)
	}
	failed, _ := Build(context.Background(), fakeGitea{pr: basePR()}, fakeWP{lookupErr: errors.New("x")}, "o", "r", 12, now)
	if failed.WoodpeckerRepoID != 0 {
		t.Errorf("no repo id when the lookup fails, got %d", failed.WoodpeckerRepoID)
	}
}
```

Create `portal/internal/handlers/links_test.go`:

```go
package handlers

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/shell"
)

func TestPRReport_LinksToGiteaAndWoodpecker(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/ssdlc/pilot-app/pulls/3":
			w.Write([]byte(`{"number":3,"title":"Add login","head":{"sha":"abc123"}}`))
		case "/api/v1/repos/ssdlc/pilot-app/commits/abc123/status":
			w.Write([]byte(`{"statuses":[]}`))
		default:
			t.Fatalf("unexpected gitea path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()
	log := base64.StdEncoding.EncodeToString([]byte("policy-eval: 0 finding(s) normalized -- critical=0 high=0 medium=0 low=0\n"))
	fakeWP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/repos/lookup/ssdlc/pilot-app":
			w.Write([]byte(`{"id":7}`))
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			w.Write([]byte(`[{"number":9,"status":"success","commit":"abc123","event":"pull_request"}]`))
		case strings.HasSuffix(r.URL.Path, "/pipelines/9"):
			w.Write([]byte(`{"workflows":[{"children":[{"id":59,"name":"policy-eval-findings","state":"success"}]}]}`))
		case strings.HasSuffix(r.URL.Path, "/logs/9/59"):
			w.Write([]byte(`[{"data":"` + log + `"}]`))
		default:
			t.Fatalf("unexpected woodpecker path %s", r.URL.Path)
		}
	}))
	defer fakeWP.Close()

	req := httptest.NewRequest(http.MethodGet, "/pr/ssdlc/pilot-app/3", nil)
	req.SetPathValue("owner", "ssdlc")
	req.SetPathValue("repo", "pilot-app")
	req.SetPathValue("number", "3")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "tok")
	ctx = shell.WithContext(ctx, shell.Shell{GiteaURL: "http://192.168.1.28:3500", WoodpeckerURL: "http://192.168.1.28:8000"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	PRReport(fakeGitea.URL, fakeWP.URL, "wp")(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, body)
	}
	for _, want := range []string{
		`href="http://192.168.1.28:3500/ssdlc/pilot-app/pulls/3"`,
		`href="http://192.168.1.28:8000/repos/7/pipeline/9"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing link %s", want)
		}
	}
}

func TestPRReport_NoLinksWithoutPublicURLs(t *testing.T) {
	// The earlier PR report tests run without a shell; the page must simply
	// omit the links rather than emit broken ones.
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := prLinks(shell.FromContext(req.Context()), "o", "r", "3", 0, 0); got.Gitea != "" || got.Woodpecker != "" {
		t.Errorf("links = %+v", got)
	}
}

func TestPRLinks_EscapeAndPartial(t *testing.T) {
	s := shell.Shell{GiteaURL: "http://g", WoodpeckerURL: "http://w"}
	got := prLinks(s, "we ird", "re/po", "3", 7, 0)
	if got.Gitea != "http://g/we%20ird/re%2Fpo/pulls/3" {
		t.Errorf("gitea = %q", got.Gitea)
	}
	if got.Woodpecker != "http://w/repos/7" {
		t.Errorf("without a pipeline number the link points at the repo, got %q", got.Woodpecker)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOTEST`
Expected: FAIL, `WoodpeckerRepoID` and `prLinks` undefined.

- [ ] **Step 3: Implement**

`portal/internal/report/report.go`: add to `Report` (next to `PipelineNumber`):

```go
	WoodpeckerRepoID int `json:"woodpecker_repo_id"`
```

and in `Build`, immediately after the successful `repoID, err := w.LookupRepo(...)` error check (i.e. once the lookup has succeeded), add `r.WoodpeckerRepoID = repoID`. (Keep the field alignment `gofmt`-clean by running gofmt on the file, which this plan created in an earlier task.)

`portal/internal/handlers/prreport.go`: add `"net/url"` to the imports; add to `prReportData`:

```go
	// Links open the pull request and its pipeline run in Gitea and
	// Woodpecker; empty when the public URLs are unknown.
	Links prLinkSet
```

and above `PRReport` add:

```go
type prLinkSet struct{ Gitea, Woodpecker string }

// prLinks builds the "open in" links. Woodpecker's web UI addresses a
// repository by its numeric id, which the report already carries.
func prLinks(s shell.Shell, owner, repo, number string, woodpeckerRepoID, pipeline int) prLinkSet {
	var l prLinkSet
	if s.GiteaURL != "" {
		l.Gitea = s.GiteaURL + "/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls/" + url.PathEscape(number)
	}
	if s.WoodpeckerURL != "" && woodpeckerRepoID > 0 {
		l.Woodpecker = fmt.Sprintf("%s/repos/%d", s.WoodpeckerURL, woodpeckerRepoID)
		if pipeline > 0 {
			l.Woodpecker += fmt.Sprintf("/pipeline/%d", pipeline)
		}
	}
	return l
}
```

(add `"fmt"` to the imports too). In `PRReport`, in the `data := prReportData{...}` literal add `Links: prLinks(shell.FromContext(r.Context()), owner, repo, strconv.Itoa(number), rep.WoodpeckerRepoID, rep.PipelineNumber),`.

`portal/web/templates/prreport.html`: directly after the page's first `<h1 ...>...</h1>` line add:

```html
{{if or .Links.Gitea .Links.Woodpecker}}
<div style="margin:-6px 0 14px;">
  {{if .Links.Gitea}}<a class="ssdlc-ext" href="{{.Links.Gitea}}" target="_blank" rel="noopener">Pull request in Gitea &#8599;</a>{{end}}
  {{if .Links.Woodpecker}}<a class="ssdlc-ext" href="{{.Links.Woodpecker}}" target="_blank" rel="noopener">Pipeline in Woodpecker &#8599;</a>{{end}}
</div>
{{end}}
```

`portal/internal/handlers/dashboard.go`: the row type already embeds `giteaclient.PullRequest` (which has `HTMLURL`), so no Go change is needed for the dashboard link. In `portal/web/templates/dashboard.html`: change the heading `<h1 style="font-weight:600;">Dashboard</h1>` to `<h1 style="font-weight:600;">Overview</h1>`, and inside each `.ssdlc-row`, after the `statusBadge` template call, add:

```html
      {{if .HTMLURL}}<a class="ssdlc-ext" href="{{.HTMLURL}}" target="_blank" rel="noopener">Gitea &#8599;</a>{{end}}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOTEST`
Expected: PASS, including the earlier PR report tests (their shell is empty, so no links appear and nothing else changes).

- [ ] **Step 5: Commit**

```bash
git add portal
git commit -m "feat(portal): deep links from the PR report and Overview into Gitea and Woodpecker"
```

---

### Task 7: Wire it up, document, verify

**Files:**
- Modify: `portal/main.go`
- Modify: `deploy/uat/docker-compose.uat.yml`, `deploy/uat/README.md`

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Wire `main.go`**

Read `portal/main.go`. Add the imports `"mime"`, `"time"` (if absent), `"ssdlc-portal/internal/shell"`. Directly after the line that creates `authHandler := auth.NewHandler(cfg)` add:

```go
	// Fonts are served by the portal itself; the minimal runtime image has no
	// system mime database, so register the type explicitly.
	mime.AddExtensionType(".woff2", "font/woff2")

	shellBuilder := &shell.Builder{
		NewIdentity: func(token string) shell.Identity { return giteaclient.New(cfg.GiteaURL, token) },
		Records:     exceptionsStore.List,
		Org:         cfg.ExceptionsRepoOwner,
		Team:        cfg.ApproverTeam,
		GiteaURL:    cfg.GiteaPublicURL,
		WoodpeckerURL: cfg.WoodpeckerPublicURL,
		TTL:         30 * time.Second,
		Now:         time.Now,
	}
	// authed = require a signed-in user, then compute the page chrome for them.
	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return authHandler.RequireAuth(shellBuilder.Middleware(h))
	}
```

Then replace every `authHandler.RequireAuth(` used to register a page or action route with `authed(` (same argument, same closing parenthesis; the routes are `/dashboard`, `/pr/...`, `/onboarding`, `/onboarding/start`, `/exceptions`, `/exceptions/request`, `/exceptions/submit`, `/exceptions/approve`, `/exceptions/decline`). Add the new route next to `/dashboard`:

```go
	mux.HandleFunc("/help", authed(handlers.Help()))
```

Run `gofmt -w portal/main.go` only if `gofmt -l` reported it before your edit was clean; otherwise fix only your own added lines' alignment by hand (do not reformat unrelated pre-existing code).

- [ ] **Step 2: Compose and docs**

In `deploy/uat/docker-compose.uat.yml`, in the `portal` service `environment:` list add (the portal's Gitea and Woodpecker URLs are already the server address, so these are only documented overrides; do NOT add active lines):

```yaml
      # Optional: addresses shown in links that open in people's browsers.
      # They default to PORTAL_GITEA_URL / PORTAL_WOODPECKER_URL, which are
      # already the server's LAN address in this deployment.
      # - PORTAL_GITEA_PUBLIC_URL=http://192.168.1.28:3500
      # - PORTAL_WOODPECKER_PUBLIC_URL=http://192.168.1.28:8000
```

In `deploy/uat/README.md` under "Operations" add:

```markdown
- **Portal navigation:** Overview, Exceptions (approvers see a badge with how many requests wait for them), Admin
  (Gitea admins only: project setup), Help, and Quick launch links that open Gitea and Woodpecker in a new tab.
  `Ctrl+K` opens a jump palette. Projects and Issues appear greyed out until the next portal step lands.
```

- [ ] **Step 3: Full verification**

Run:

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/src" -w /src/portal golang:1.22-alpine sh -c "go vet ./... && go test ./... && gofmt -l cmd internal main.go"
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/src" node:20-alpine node --check /src/portal/web/static/shell.js && echo JS-OK
R="$(pwd -W)"; T="$(mktemp -d)"; printf 'SERVER_IP=1.2.3.4\nREPO_ROOT=%s\nREPO_VM=/x\nPOSTGRES_PASSWORD=x\nWOODPECKER_AGENT_SECRET=x\nWOODPECKER_GRPC_SECRET=x\nGITEA_OAUTH_CLIENT_ID=x\nGITEA_OAUTH_CLIENT_SECRET=x\nSIDECAR_API_TOKEN=0123456789abcdef0123\n' "$R" > "$T/e"; docker compose -p t --env-file "$T/e" -f compose/minimal/docker-compose.yml -f deploy/uat/docker-compose.uat.yml config --quiet && echo compose-ok
docker build -q -f deploy/uat/Dockerfile --target portal -t ssdlc-uat-portal-test . && docker run --rm ssdlc-uat-portal-test 2>&1 | head -3; docker rmi ssdlc-uat-portal-test >/dev/null
```

Expected: vet and tests pass; `gofmt -l` lists no file you created or touched (pre-existing `internal/exceptions/exceptions.go` and `internal/findings/parser.go` may still be listed); `JS-OK`; `compose-ok`; the portal image builds and, run with no environment, prints `portal configuration is invalid:` (it starts, loads the templates, and fails closed on config).

- [ ] **Step 4: Render the pages**

The implementer cannot log in through Gitea, so render the shell directly: write a throwaway Go program is NOT needed. Instead render each new page with a Go test-style request the same way the layout tests do and save the HTML, then screenshot it with headless Edge. From the worktree root in Git Bash:

```bash
mkdir -p .superpowers/shots
# Uses the tests' rendering path: run a tiny test that writes HTML files.
cat > portal/internal/handlers/zz_render_test.go <<'EOF'
package handlers

import (
	"os"
	"testing"

	"ssdlc-portal/internal/shell"
)

// Renders sample pages to disk for a visual check. Skipped unless RENDER_DIR is set.
func TestRenderSamples(t *testing.T) {
	dir := os.Getenv("RENDER_DIR")
	if dir == "" {
		t.Skip("RENDER_DIR not set")
	}
	write := func(name, body string) {
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	admin := shell.Shell{Operator: "gateadmin", Role: shell.RoleAdmin, IsAdmin: true, IsApprover: true, PendingApprovals: 2,
		GiteaURL: "http://192.168.1.28:3500", WoodpeckerURL: "http://192.168.1.28:8000"}
	write("overview-admin.html", renderDashboardAs(t, admin))
	write("help-approver.html", getHelp(t, "/help", shell.Shell{Operator: "dev1", Role: shell.RoleApprover, IsApprover: true, PendingApprovals: 2,
		GiteaURL: "http://192.168.1.28:3500", WoodpeckerURL: "http://192.168.1.28:8000"}))
}
EOF
MSYS_NO_PATHCONV=1 docker run --rm -e RENDER_DIR=/src/.superpowers/shots -v "$(pwd -W):/src" -w /src/portal golang:1.22-alpine sh -c "go test ./internal/handlers/ -run TestRenderSamples -v"
```

Then take screenshots of the two saved files with headless Edge (the CSS and fonts load from `portal/web/static`, so rewrite the paths first):

```bash
sed -i 's#="/static/#="../../portal/web/static/#g; s#url(/static/#url(../../portal/web/static/#g' .superpowers/shots/*.html
EDGE="/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe"; ABS="$(pwd -W)"
for f in overview-admin help-approver; do "$EDGE" --headless=new --disable-gpu --hide-scrollbars --window-size=1440,900 --virtual-time-budget=4000 --screenshot="$ABS/.superpowers/shots/$f.png" "file:///$ABS/.superpowers/shots/$f.html" >/dev/null 2>&1; done
ls -la .superpowers/shots
rm portal/internal/handlers/zz_render_test.go
```

Expected: two PNGs. Look at them (Read tool): the sidebar shows Overview, greyed Projects and Issues, Exceptions with the badge "2", Admin, Help, the Quick launch block and the signed-in block; the Help page shows the role's first steps and the questions. Note: `.superpowers/` is git-ignored scratch; the temporary test file must be deleted (last command) and not committed.

- [ ] **Step 5: Commit**

```bash
git add portal/main.go deploy/uat
git commit -m "feat(portal): wire the shell, /help and font mime type; document navigation"
git push origin worktree-ssdlc-portal
```

---

## Self-Review

**Spec coverage (delivery step 1: "Shell"):**
- New layout and role-aware sidebar with badges: Tasks 2 and 4. Roles from Gitea (admin flag, team membership); approver badge from pending exceptions.
- Help page with role-aware first steps and searchable questions: Task 5 (server-side search, works without JS; "Why?" link on blocked PRs is deferred with the Overview rework since the attention cards arrive in step 3).
- `Ctrl K` palette: Task 4 (items read from the page; includes Quick launch links).
- Gitea and Woodpecker deep links and Quick launch: Tasks 4 and 6 (repo/PR links on Overview rows, PR and pipeline links on the PR report). File-and-line links come with the Issue page (step 4).
- Vendored fonts and scripts, no CDN: Task 3, asserted by `TestLayout_DeveloperSidebar`.
- Disabled placeholders for unbuilt screens (Projects, Issues): Task 4, asserted.
- Reuses today's data only: yes; the only new Gitea calls are `/user` (admin flag) and team lookup, cached 30 s per user.

**Placeholder scan:** none; the only "fill in" is the sha256 list in `vendor/README.md`, produced by the command run in the same task.

**Type consistency:** `shell.Shell` fields used by templates (`Operator`, `IsAdmin`, `IsApprover`, `PendingApprovals`, `GiteaURL`, `WoodpeckerURL`, `RoleLabel`) match Task 2's definition; `giteaclient.Client` satisfies `shell.Identity` (`CurrentUser` from Task 1, existing `IsOnTeam`); `exceptionsStore.List` matches `Builder.Records`; `prLinks` signature is identical in its definition and both test uses; `report.Report.WoodpeckerRepoID` is set in `Build` and read in the handler.

**Known limits to watch during execution:**
- Woodpecker's UI route `/repos/<id>/pipeline/<n>` is taken from Woodpecker 3.x behaviour and could not be checked in this environment; the live check (below) confirms it.
- The layout test only exercises `Dashboard`; the other pages (exceptions, onboarding, PR report, Help) are covered by their own existing/new handler tests failing loudly ("can't evaluate field Shell") if a struct is missed.
- Live verification on the running stack is not part of this plan (it would alter the user's UAT stack); after pulling the branch and re-running `setup-uat.ps1`, open the portal as a developer and as `gateadmin` and confirm: sidebar per role, Quick launch links open in new tabs, `Ctrl K` works, Help renders, the PR report shows the two links and the Woodpecker one lands on the pipeline run.
