# SSDLC Portal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working web portal for ssdlc-platform-lab — login, dashboard, PR security
report, onboarding wizard, and a real two-party exception flow — backed entirely by live
Gitea/Woodpecker API calls, matching the visual design of the approved mockup.

**Architecture:** One new Go module (`portal/`), server-rendered `html/template` + HTMX, no
client-side framework, no build step. The portal calls Gitea's and Woodpecker's REST APIs
directly on every request; it stores nothing itself except a signed session cookie. Exceptions
are the one place the portal *writes* durable state, and it does so into a new Gitea repo
(`exceptions`), not a database of its own.

**Tech Stack:** Go 1.21+ (stdlib `net/http` + `html/template`, no router/web framework — the
surface area here doesn't need one), HTMX 1.9 + the SSE extension (both from a pinned CDN
`<script>` tag, matching this project's "no SPA build chain" mandate), Gitea 1.27's REST API,
Woodpecker 3.18's REST API, Open Policy Agent/Rego (existing `policy/severity.rego`), Python 3
(existing `policy-eval/evaluate-findings.py`).

**Spec:** [`docs/superpowers/specs/2026-09-07-ssdlc-portal-design.md`](../specs/2026-09-07-ssdlc-portal-design.md)
— read it before starting; this plan argues from it and does not repeat its reasoning. Mockup
reference files: `docs/superpowers/specs/2026-09-07-portal-mockup/*.dc.html`.

## Global Constraints

- **One Go binary, server-rendered HTML/HTMX, no SPA build chain** (spec, D10). No `npm`, no
  webpack/vite/esbuild config anywhere in `portal/`.
- **The portal owns no security state.** No database, no ORM. Every page's data comes from a
  live Gitea/Woodpecker API call made during that request. The only exception: the `exceptions`
  Gitea repo, which is Gitea-hosted state, not portal-hosted state.
- **Typography:** IBM Plex Sans (UI) + IBM Plex Mono (code/SHAs/fingerprints/rule IDs), loaded
  from Google Fonts exactly as the mockup does. No other typeface anywhere.
- **Color system:** OKLCH, hue 265 for the neutral scale, hue 152 for success, hue 22 for
  critical, hue 68 for warning, hue 258 for primary accent, hue 300 reserved for a distinct
  "automated/bot" accent — exact values are defined once in Task 1 and never re-guessed
  per-template.
- **No git browsing, diff viewer, or code hosting.** Every such link opens the real Gitea page
  in a new tab (`target="_blank"`).
- **Role/team membership is read from Gitea on every privileged action, never cached in the
  session.**
- **Exception expiry is capped at 90 days**, enforced server-side, not just in the form's
  `max` attribute.
- Every new Go package gets `go vet ./...` and `go test ./...` passing before its task is
  considered done. Every new/changed Rego rule gets a `conftest verify` pass. Every new/changed
  Python gets its `pytest` suite passing.

---

## Task 1: Go module scaffold, config, and the shared design system

**Files:**
- Create: `portal/go.mod`
- Create: `portal/main.go`
- Create: `portal/internal/config/config.go`
- Create: `portal/internal/config/config_test.go`
- Create: `portal/web/static/tokens.css`
- Create: `portal/web/templates/layout.html`
- Create: `portal/internal/webutil/statusbadge.go`
- Create: `portal/internal/webutil/statusbadge_test.go`

**Interfaces:**
- Produces: `config.Config` struct (fields: `GiteaURL`, `WoodpeckerURL`, `OAuthClientID`,
  `OAuthClientSecret`, `SessionKey []byte`, `ApproverTeam`, `ExceptionsRepoOwner`,
  `ExceptionsRepoName`, `ListenAddr`), `config.Load() (Config, error)`.
- Produces: `webutil.StatusBadge(status string) webutil.Badge` where
  `type Badge struct { Label, CSSClass string }` — every screen's status pill goes through this
  one function so the color mapping lives in exactly one place.
- Produces: `web/templates/layout.html` defining blocks `{{block "content" .}}{{end}}` and
  `{{block "title" .}}{{end}}`, a sidebar with nav items for Dashboard, Onboarding, Exceptions,
  and three disabled items (Report export, Posture, Admin health) each rendering a `title`
  attribute explaining why it's disabled.

- [ ] **Step 1: Write the failing config test**

```go
// portal/internal/config/config_test.go
package config

import (
	"testing"
)

func TestLoad_MissingRequiredVar(t *testing.T) {
	t.Setenv("PORTAL_GITEA_URL", "")
	t.Setenv("PORTAL_WOODPECKER_URL", "http://127.0.0.1:8000")
	t.Setenv("PORTAL_OAUTH_CLIENT_ID", "x")
	t.Setenv("PORTAL_OAUTH_CLIENT_SECRET", "y")
	t.Setenv("PORTAL_SESSION_KEY", "0123456789abcdef0123456789abcdef")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error when PORTAL_GITEA_URL is unset, got nil")
	}
}

func TestLoad_SessionKeyTooShort(t *testing.T) {
	t.Setenv("PORTAL_GITEA_URL", "http://127.0.0.1:3500")
	t.Setenv("PORTAL_WOODPECKER_URL", "http://127.0.0.1:8000")
	t.Setenv("PORTAL_OAUTH_CLIENT_ID", "x")
	t.Setenv("PORTAL_OAUTH_CLIENT_SECRET", "y")
	t.Setenv("PORTAL_SESSION_KEY", "tooshort")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error for a session key under 32 bytes, got nil")
	}
}

func TestLoad_Success(t *testing.T) {
	t.Setenv("PORTAL_GITEA_URL", "http://127.0.0.1:3500")
	t.Setenv("PORTAL_WOODPECKER_URL", "http://127.0.0.1:8000")
	t.Setenv("PORTAL_OAUTH_CLIENT_ID", "x")
	t.Setenv("PORTAL_OAUTH_CLIENT_SECRET", "y")
	t.Setenv("PORTAL_SESSION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("PORTAL_APPROVER_TEAM", "security-officers")
	t.Setenv("PORTAL_EXCEPTIONS_REPO_OWNER", "gateadmin")
	t.Setenv("PORTAL_EXCEPTIONS_REPO_NAME", "exceptions")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GiteaURL != "http://127.0.0.1:3500" {
		t.Errorf("GiteaURL = %q", cfg.GiteaURL)
	}
	if len(cfg.SessionKey) != 32 {
		t.Errorf("SessionKey length = %d, want 32", len(cfg.SessionKey))
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `cd portal && go mod init ssdlc-portal && mkdir -p internal/config && go test ./internal/config/... -v`
Expected: build failure — `config.Load` and `config.Config` don't exist yet.

- [ ] **Step 3: Implement `config.go`**

```go
// portal/internal/config/config.go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	GiteaURL            string
	WoodpeckerURL       string
	OAuthClientID       string
	OAuthClientSecret   string
	SessionKey          []byte
	ApproverTeam        string
	ExceptionsRepoOwner string
	ExceptionsRepoName  string
	ListenAddr          string
}

// Load reads the portal's configuration from the environment. It fails
// closed: a missing required variable or a too-short session key is a
// startup error, never a silent default, matching this platform's own
// "missing/placeholder secret is fatal" discipline elsewhere in the repo.
func Load() (Config, error) {
	var problems []string

	get := func(name string) string { return os.Getenv(name) }
	required := func(name string) string {
		v := get(name)
		if v == "" {
			problems = append(problems, fmt.Sprintf("%s is required", name))
		}
		return v
	}

	cfg := Config{
		GiteaURL:            required("PORTAL_GITEA_URL"),
		WoodpeckerURL:       required("PORTAL_WOODPECKER_URL"),
		OAuthClientID:       required("PORTAL_OAUTH_CLIENT_ID"),
		OAuthClientSecret:   required("PORTAL_OAUTH_CLIENT_SECRET"),
		ApproverTeam:        get("PORTAL_APPROVER_TEAM"),
		ExceptionsRepoOwner: required("PORTAL_EXCEPTIONS_REPO_OWNER"),
		ExceptionsRepoName:  get("PORTAL_EXCEPTIONS_REPO_NAME"),
		ListenAddr:          get("PORTAL_LISTEN_ADDR"),
	}
	if cfg.ExceptionsRepoName == "" {
		cfg.ExceptionsRepoName = "exceptions"
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8181"
	}

	sessionKey := required("PORTAL_SESSION_KEY")
	if sessionKey != "" && len(sessionKey) < 32 {
		problems = append(problems, "PORTAL_SESSION_KEY must be at least 32 bytes")
	}
	cfg.SessionKey = []byte(sessionKey)

	if len(problems) > 0 {
		msg := "portal configuration is invalid:"
		for _, p := range problems {
			msg += "\n  - " + p
		}
		return Config{}, fmt.Errorf("%s", msg)
	}
	return cfg, nil
}
```

- [ ] **Step 4: Run tests, confirm they pass**

Run: `go test ./internal/config/... -v`
Expected: all three tests PASS.

- [ ] **Step 5: Write the status-badge test**

```go
// portal/internal/webutil/statusbadge_test.go
package webutil

import "testing"

func TestStatusBadge(t *testing.T) {
	cases := []struct {
		status, wantLabel, wantClass string
	}{
		{"success", "Passed", "ssdlc-badge-success"},
		{"failure", "Blocked", "ssdlc-badge-critical"},
		{"pending", "Running", "ssdlc-badge-warning"},
		{"unknown-status", "unknown-status", "ssdlc-badge-neutral"},
	}
	for _, c := range cases {
		got := StatusBadge(c.status)
		if got.Label != c.wantLabel || got.CSSClass != c.wantClass {
			t.Errorf("StatusBadge(%q) = {%q,%q}, want {%q,%q}",
				c.status, got.Label, got.CSSClass, c.wantLabel, c.wantClass)
		}
	}
}
```

- [ ] **Step 6: Run it, confirm it fails, then implement**

Run: `mkdir -p internal/webutil && go test ./internal/webutil/... -v` — expect a build failure.

```go
// portal/internal/webutil/statusbadge.go
package webutil

// Badge is the one place a gate/pipeline/finding status becomes a color and
// a label. Every template renders status through this function so the
// mapping can never drift between screens.
type Badge struct {
	Label    string
	CSSClass string
}

func StatusBadge(status string) Badge {
	switch status {
	case "success":
		return Badge{"Passed", "ssdlc-badge-success"}
	case "failure":
		return Badge{"Blocked", "ssdlc-badge-critical"}
	case "pending", "running":
		return Badge{"Running", "ssdlc-badge-warning"}
	default:
		return Badge{status, "ssdlc-badge-neutral"}
	}
}
```

Run: `go test ./internal/webutil/... -v`
Expected: PASS.

- [ ] **Step 7: Write the design-token stylesheet**

```css
/* portal/web/static/tokens.css
   OKLCH palette extracted directly from the approved mockup
   (docs/superpowers/specs/2026-09-07-portal-mockup/AutomatedPRDark.dc.html) —
   do not substitute hex approximations; the mockup's own values ARE the
   spec for this file. */
:root {
  --ssdlc-bg:          oklch(0.165 0.012 265);
  --ssdlc-surface:     oklch(0.198 0.013 265);
  --ssdlc-surface-alt: oklch(0.24 0.012 265);
  --ssdlc-border:      oklch(0.27 0.012 265);
  --ssdlc-border-alt:  oklch(0.30 0.012 265);

  --ssdlc-text-1:      oklch(0.86 0.008 265);
  --ssdlc-text-2:      oklch(0.72 0.01 265);
  --ssdlc-text-3:      oklch(0.6 0.012 265);
  --ssdlc-text-4:      oklch(0.5 0.012 265);

  --ssdlc-success:      oklch(0.72 0.15 152);
  --ssdlc-success-bg:   oklch(0.15 0.03 152);
  --ssdlc-critical:     oklch(0.62 0.18 22);
  --ssdlc-critical-bg:  oklch(0.2 0.05 22);
  --ssdlc-warning:      oklch(0.75 0.14 68);
  --ssdlc-warning-bg:   oklch(0.22 0.04 68);
  --ssdlc-accent:       oklch(0.68 0.14 258);
  --ssdlc-accent-bg:    oklch(0.27 0.05 258);
  --ssdlc-bot-accent:   oklch(0.68 0.12 300);
  --ssdlc-bot-accent-bg:oklch(0.24 0.04 300);
}

body {
  margin: 0;
  background: var(--ssdlc-bg);
  color: var(--ssdlc-text-1);
  font-family: 'IBM Plex Sans', system-ui, sans-serif;
  -webkit-font-smoothing: antialiased;
}
code, .ssdlc-mono { font-family: 'IBM Plex Mono', monospace; }

.ssdlc-shell { display: flex; min-height: 100vh; }
.ssdlc-sidebar {
  width: 256px; flex: none;
  background: var(--ssdlc-surface);
  border-right: 1px solid var(--ssdlc-border);
  padding: 20px 0;
}
.ssdlc-sidebar a {
  display: block; padding: 10px 20px;
  color: var(--ssdlc-text-2); text-decoration: none; font-size: 14px;
}
.ssdlc-sidebar a:hover { background: var(--ssdlc-surface-alt); color: var(--ssdlc-text-1); }
.ssdlc-sidebar a.active { color: var(--ssdlc-text-1); border-left: 3px solid var(--ssdlc-accent); background: var(--ssdlc-surface-alt); }
.ssdlc-sidebar a.disabled { color: var(--ssdlc-text-4); cursor: not-allowed; pointer-events: none; }
.ssdlc-sidebar a.disabled::after { content: " (not built yet)"; font-size: 11px; }

.ssdlc-main { flex: 1; padding: 24px 32px; }
.ssdlc-card {
  background: var(--ssdlc-surface);
  border: 1px solid var(--ssdlc-border);
  border-radius: 8px;
  padding: 16px 20px;
  margin-bottom: 16px;
}
.ssdlc-row { display: flex; align-items: center; gap: 16px; padding: 10px 0; border-bottom: 1px solid var(--ssdlc-border); font-size: 14px; }
.ssdlc-row:last-child { border-bottom: none; }

.ssdlc-badge { display: inline-flex; align-items: center; gap: 6px; padding: 3px 10px; border-radius: 999px; font-size: 12px; font-weight: 600; }
.ssdlc-badge-success  { background: var(--ssdlc-success-bg);  color: var(--ssdlc-success); }
.ssdlc-badge-critical { background: var(--ssdlc-critical-bg); color: var(--ssdlc-critical); }
.ssdlc-badge-warning  { background: var(--ssdlc-warning-bg);  color: var(--ssdlc-warning); }
.ssdlc-badge-neutral  { background: var(--ssdlc-surface-alt); color: var(--ssdlc-text-3); }

.ssdlc-btn { display: inline-block; padding: 8px 16px; border-radius: 6px; border: 1px solid var(--ssdlc-border-alt); background: var(--ssdlc-surface-alt); color: var(--ssdlc-text-1); font-size: 14px; cursor: pointer; text-decoration: none; }
.ssdlc-btn-primary { background: var(--ssdlc-accent-bg); border-color: var(--ssdlc-accent); color: var(--ssdlc-accent); }
```

- [ ] **Step 8: Write the shared layout template**

```html
<!-- portal/web/templates/layout.html -->
{{define "layout"}}
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{block "title" .}}SSDLC Portal{{end}}</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600;700&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<link rel="stylesheet" href="/static/tokens.css">
<script src="https://unpkg.com/htmx.org@1.9.12"></script>
<script src="https://unpkg.com/htmx.org@1.9.12/dist/ext/sse.js"></script>
</head>
<body>
<div class="ssdlc-shell">
  <nav class="ssdlc-sidebar">
    <div style="padding: 0 20px 16px; font-weight: 700; color: var(--ssdlc-text-1);">SSDLC Portal</div>
    <a href="/dashboard" class="{{if eq .ActiveNav "dashboard"}}active{{end}}">Dashboard</a>
    <a href="/onboarding" class="{{if eq .ActiveNav "onboarding"}}active{{end}}">Onboarding</a>
    <a href="/exceptions" class="{{if eq .ActiveNav "exceptions"}}active{{end}}">Exceptions</a>
    <a class="disabled" title="Not available yet — needs the reporting sidecar and DefectDojo (DESIGN.md D10)">Report export</a>
    <a class="disabled" title="Not available yet — planned as an embedded Grafana view (DESIGN.md D10)">Posture</a>
    <a class="disabled" title="Not available yet — planned as scripts/doctor.sh exposed over HTTP (DESIGN.md D10)">Admin health</a>
    {{if .Operator}}
    <div style="margin-top: 24px; padding: 12px 20px; border-top: 1px solid var(--ssdlc-border); color: var(--ssdlc-text-3); font-size: 13px;">
      {{.Operator}} · <a href="/logout" style="color: var(--ssdlc-text-3); text-decoration: underline;">sign out</a>
    </div>
    {{end}}
  </nav>
  <main class="ssdlc-main">
    {{block "content" .}}{{end}}
  </main>
</div>
</body>
</html>
{{end}}
{{define "statusBadge"}}<span class="ssdlc-badge {{.CSSClass}}">{{.Label}}</span>{{end}}
```

- [ ] **Step 9: Write `main.go` as a minimal server that loads config and serves static files**

```go
// portal/main.go
package main

import (
	"log"
	"net/http"

	"ssdlc-portal/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	log.Printf("ssdlc-portal listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
```

- [ ] **Step 10: Verify it builds and vets cleanly**

Run: `cd portal && go build ./... && go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 11: Commit**

```bash
git add portal/
git commit -m "feat(portal): scaffold the Go module, config loading, and design tokens"
```

---

## Task 2: Gitea OAuth login and session handling

**Files:**
- Create: `portal/internal/session/session.go`
- Create: `portal/internal/session/session_test.go`
- Create: `portal/internal/auth/oauth.go`
- Create: `portal/internal/auth/oauth_test.go`
- Modify: `portal/main.go`

**Interfaces:**
- Consumes: `config.Config` (Task 1).
- Produces: `session.Encode(token string, key []byte) (string, error)`,
  `session.Decode(cookieValue string, key []byte) (token string, err error)` — AES-GCM sealed,
  base64-encoded.
- Produces: `auth.NewHandler(cfg config.Config) *auth.Handler` with methods
  `Login(w, r)` (redirects to Gitea's `/login/oauth/authorize`), `Callback(w, r)` (exchanges
  the code, sets the session cookie, redirects to `/dashboard`), `Logout(w, r)` (clears the
  cookie), and `RequireAuth(next http.HandlerFunc) http.HandlerFunc` middleware that reads the
  session cookie and injects the decoded Gitea token into the request context under
  `auth.ContextKeyToken`.
- Produces later tasks rely on: `auth.TokenFromContext(ctx) (string, bool)`.

- [ ] **Step 1: Write the failing session round-trip test**

```go
// portal/internal/session/session_test.go
package session

import "testing"

func TestEncodeDecodeRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	token := "gto_realistictokenvalue1234567890abcdef"

	encoded, err := Encode(token, key)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if encoded == token {
		t.Fatal("Encode returned the plaintext token unchanged — it must be encrypted")
	}

	decoded, err := Decode(encoded, key)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded != token {
		t.Errorf("Decode = %q, want %q", decoded, token)
	}
}

func TestDecode_WrongKeyFails(t *testing.T) {
	key1 := []byte("0123456789abcdef0123456789abcdef")
	key2 := []byte("fedcba9876543210fedcba9876543210")

	encoded, _ := Encode("secret-token", key1)
	if _, err := Decode(encoded, key2); err == nil {
		t.Fatal("expected Decode with the wrong key to fail, got nil error")
	}
}

func TestDecode_TamperedCiphertextFails(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	encoded, _ := Encode("secret-token", key)
	tampered := encoded[:len(encoded)-2] + "xx"
	if _, err := Decode(tampered, key); err == nil {
		t.Fatal("expected Decode of tampered ciphertext to fail, got nil error")
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `mkdir -p internal/session && go test ./internal/session/... -v`
Expected: build failure — `Encode`/`Decode` undefined.

- [ ] **Step 3: Implement `session.go`**

```go
// portal/internal/session/session.go
package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Encode seals token with AES-256-GCM under key, returning a URL-safe
// base64 string suitable for a cookie value. The session cookie holds a
// real Gitea access token, not a portal-issued identifier — sealing it
// (not just signing it) matters because leaking the cookie must not leak
// the token in plaintext to anything inspecting cookie storage.
func Encode(token string, key []byte) (string, error) {
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return "", fmt.Errorf("session: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("session: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("session: nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(token), nil)
	return base64.URLEncoding.EncodeToString(sealed), nil
}

// Decode reverses Encode. Any failure — wrong key, truncated or tampered
// ciphertext — is returned as a generic error; callers must treat any
// error as "not authenticated", never attempt partial recovery.
func Decode(value string, key []byte) (string, error) {
	raw, err := base64.URLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("session: bad encoding: %w", err)
	}
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return "", fmt.Errorf("session: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("session: new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("session: ciphertext too short")
	}
	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("session: open: %w", err)
	}
	return string(plain), nil
}
```

- [ ] **Step 4: Run tests, confirm they pass**

Run: `go test ./internal/session/... -v`
Expected: all three PASS.

- [ ] **Step 5: Write the failing OAuth handler test (using a fake Gitea)**

```go
// portal/internal/auth/oauth_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ssdlc-portal/internal/config"
	"ssdlc-portal/internal/session"
)

func TestLogin_RedirectsToGiteaAuthorize(t *testing.T) {
	cfg := config.Config{
		GiteaURL:      "http://gitea.example",
		OAuthClientID: "client-123",
		SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
	}
	h := NewHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("bad Location header: %v", err)
	}
	if !strings.HasPrefix(loc.String(), "http://gitea.example/login/oauth/authorize") {
		t.Errorf("Location = %q, want it to start with the Gitea authorize endpoint", loc.String())
	}
	if loc.Query().Get("client_id") != "client-123" {
		t.Errorf("client_id = %q, want client-123", loc.Query().Get("client_id"))
	}
	if loc.Query().Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", loc.Query().Get("response_type"))
	}
}

func TestCallback_ExchangesCodeAndSetsSessionCookie(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/oauth/access_token" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"gto_realtoken123","token_type":"bearer","expires_in":3600}`))
	}))
	defer fakeGitea.Close()

	cfg := config.Config{
		GiteaURL:          fakeGitea.URL,
		OAuthClientID:     "client-123",
		OAuthClientSecret: "secret-456",
		SessionKey:        []byte("0123456789abcdef0123456789abcdef"),
	}
	h := NewHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=abc123", nil)
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == cookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie was set")
	}
	token, err := session.Decode(sessionCookie.Value, cfg.SessionKey)
	if err != nil {
		t.Fatalf("could not decode session cookie: %v", err)
	}
	if token != "gto_realtoken123" {
		t.Errorf("decoded token = %q, want gto_realtoken123", token)
	}
	if !sessionCookie.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Error("session cookie must be SameSite=Lax")
	}
}

func TestRequireAuth_NoCookieRedirectsToLogin(t *testing.T) {
	cfg := config.Config{SessionKey: []byte("0123456789abcdef0123456789abcdef")}
	h := NewHandler(cfg)

	called := false
	protected := h.RequireAuth(func(w http.ResponseWriter, r *http.Request) { called = true })

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	protected(rec, req)

	if called {
		t.Error("the protected handler ran without a valid session")
	}
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect to login)", rec.Code, http.StatusFound)
	}
}

func TestRequireAuth_ValidCookiePassesTokenThroughContext(t *testing.T) {
	cfg := config.Config{SessionKey: []byte("0123456789abcdef0123456789abcdef")}
	h := NewHandler(cfg)

	encoded, err := session.Encode("gto_realtoken123", cfg.SessionKey)
	if err != nil {
		t.Fatal(err)
	}

	var gotToken string
	protected := h.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		gotToken, _ = TokenFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: encoded})
	rec := httptest.NewRecorder()
	protected(rec, req)

	if gotToken != "gto_realtoken123" {
		t.Errorf("token in context = %q, want gto_realtoken123", gotToken)
	}
}
```

- [ ] **Step 6: Run it, confirm it fails**

Run: `mkdir -p internal/auth && go test ./internal/auth/... -v`
Expected: build failure — the `auth` package doesn't exist yet.

- [ ] **Step 7: Implement `oauth.go`**

```go
// portal/internal/auth/oauth.go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"ssdlc-portal/internal/config"
	"ssdlc-portal/internal/session"
)

const cookieName = "ssdlc_portal_session"

type contextKey int

const ContextKeyToken contextKey = iota

type Handler struct {
	cfg config.Config
}

func NewHandler(cfg config.Config) *Handler { return &Handler{cfg: cfg} }

// Login redirects to Gitea's own OAuth authorize endpoint. There is no
// portal-side password form; the portal never sees or stores a Gitea
// password (spec: "Login — Gitea OAuth button only").
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	redirectURI := fmt.Sprintf("%s://%s/oauth/callback", scheme(r), r.Host)
	q := url.Values{
		"client_id":     {h.cfg.OAuthClientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
	}
	http.Redirect(w, r, h.cfg.GiteaURL+"/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

// Callback exchanges the authorization code for an access token and seals
// it into the session cookie. The token itself is the only thing the
// portal stores about the operator — no separate user record.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	redirectURI := fmt.Sprintf("%s://%s/oauth/callback", scheme(r), r.Host)

	form := url.Values{
		"client_id":     {h.cfg.OAuthClientID},
		"client_secret": {h.cfg.OAuthClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	resp, err := http.PostForm(h.cfg.GiteaURL+"/login/oauth/access_token", form)
	if err != nil {
		http.Error(w, "could not reach Gitea: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		http.Error(w, "Gitea did not return an access token", http.StatusBadGateway)
		return
	}

	encoded, err := session.Encode(tok.AccessToken, h.cfg.SessionKey)
	if err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// RequireAuth decodes the session cookie and, on success, injects the
// operator's real Gitea token into the request context so every
// downstream handler can call the Gitea/Woodpecker APIs as that operator
// — never as a shared service account. On any failure it redirects to
// /login rather than guessing at a degraded identity.
func (h *Handler) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		token, err := session.Decode(cookie.Value, h.cfg.SessionKey)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), ContextKeyToken, token)
		next(w, r.WithContext(ctx))
	}
}

func TokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(ContextKeyToken).(string)
	return token, ok
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
```

- [ ] **Step 8: Run tests, confirm they pass**

Run: `go test ./internal/auth/... -v`
Expected: all five PASS.

- [ ] **Step 9: Wire the routes into `main.go`**

```go
// portal/main.go — replace the body of main() with:
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	authHandler := auth.NewHandler(cfg)

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	mux.HandleFunc("/login", authHandler.Login)
	mux.HandleFunc("/oauth/callback", authHandler.Callback)
	mux.HandleFunc("/logout", authHandler.Logout)

	log.Printf("ssdlc-portal listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
```

Add the matching import block (`"ssdlc-portal/internal/auth"`).

- [ ] **Step 10: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: clean.

- [ ] **Step 11: Commit**

```bash
git add portal/
git commit -m "feat(portal): Gitea OAuth login and encrypted session cookie"
```

---

## Task 3: Gitea API client

**Files:**
- Create: `portal/internal/giteaclient/client.go`
- Create: `portal/internal/giteaclient/client_test.go`

**Interfaces:**
- Consumes: a Gitea base URL and an operator's access token (from
  `auth.TokenFromContext`, Task 2).
- Produces:
  ```go
  type Client struct { /* unexported */ }
  func New(baseURL, token string) *Client
  type PullRequest struct {
      Number    int
      Title     string
      HTMLURL   string
      State     string
      Author    string
      Repo      string // "owner/name"
      HeadSHA   string
      HeadBranch string
      CreatedAt time.Time
  }
  func (c *Client) ListMyPullRequests(ctx context.Context) ([]PullRequest, error)
  type CommitStatus struct { State, Context string }
  func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, sha string) ([]CommitStatus, error)
  type Team struct { Name, Organization string }
  func (c *Client) ListMyTeams(ctx context.Context) ([]Team, error)
  func (c *Client) IsOnTeam(ctx context.Context, org, teamName string) (bool, error)
  func (c *Client) GetFileContent(ctx context.Context, owner, repo, path string) (content []byte, sha string, err error) // sha == "" if the file doesn't exist
  func (c *Client) PutFileContent(ctx context.Context, owner, repo, path string, content []byte, message string, existingSHA string) error
  func (c *Client) EnsureRepo(ctx context.Context, owner, name string) error // creates it if missing, no-op if it exists
  func (c *Client) Username(ctx context.Context) (string, error)
  ```
  Later tasks (Dashboard, PR report, Exceptions) depend on these exact names and signatures.

- [ ] **Step 1: Write the failing test for `ListMyPullRequests`, using an `httptest.Server` returning real Gitea response shapes**

```go
// portal/internal/giteaclient/client_test.go
package giteaclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListMyPullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/user":
			w.Write([]byte(`{"login":"gateadmin"}`))
		case "/api/v1/repos/search":
			w.Write([]byte(`{"data":[{"full_name":"gateadmin/gate-demo"}]}`))
		case "/api/v1/repos/gateadmin/gate-demo/pulls":
			w.Write([]byte(`[{
				"number": 12, "title": "Add feature X", "html_url": "http://gitea/gateadmin/gate-demo/pulls/12",
				"state": "open", "user": {"login": "alice"},
				"head": {"sha": "abc123", "ref": "feature-x"}, "created_at": "2026-09-01T10:00:00Z"
			}]`))
		default:
			t.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	prs, err := c.ListMyPullRequests(context.Background())
	if err != nil {
		t.Fatalf("ListMyPullRequests: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	pr := prs[0]
	if pr.Number != 12 || pr.Title != "Add feature X" || pr.Author != "alice" ||
		pr.Repo != "gateadmin/gate-demo" || pr.HeadSHA != "abc123" || pr.HeadBranch != "feature-x" {
		t.Errorf("unexpected PR: %+v", pr)
	}
}

func TestGetCombinedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"state":"failure","statuses":[
			{"status":"failure","context":"ssdlc/security-gate/pr/woodpecker"}
		]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	statuses, err := c.GetCombinedStatus(context.Background(), "gateadmin", "gate-demo", "abc123")
	if err != nil {
		t.Fatalf("GetCombinedStatus: %v", err)
	}
	if len(statuses) != 1 || statuses[0].State != "failure" ||
		statuses[0].Context != "ssdlc/security-gate/pr/woodpecker" {
		t.Errorf("unexpected statuses: %+v", statuses)
	}
}

func TestGetFileContent_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	_, sha, err := c.GetFileContent(context.Background(), "gateadmin", "exceptions", "no-such-file.json")
	if err != nil {
		t.Fatalf("expected no error for a missing file, got %v", err)
	}
	if sha != "" {
		t.Errorf("sha = %q, want empty for a missing file", sha)
	}
}

func TestEnsureRepo_CreatesWhenMissing(t *testing.T) {
	var createCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/gateadmin/exceptions":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			createCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	if err := c.EnsureRepo(context.Background(), "gateadmin", "exceptions"); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if !createCalled {
		t.Error("expected EnsureRepo to POST /user/repos when the repo doesn't exist")
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `mkdir -p internal/giteaclient && go test ./internal/giteaclient/... -v`
Expected: build failure.

- [ ] **Step 3: Implement `client.go`**

```go
// portal/internal/giteaclient/client.go
package giteaclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) (status int, err error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("gitea: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("gitea: decode %s %s: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

func (c *Client) Username(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/user", nil, &user); err != nil {
		return "", err
	}
	return user.Login, nil
}

type PullRequest struct {
	Number     int
	Title      string
	HTMLURL    string
	State      string
	Author     string
	Repo       string
	HeadSHA    string
	HeadBranch string
	CreatedAt  time.Time
}

type giteaPR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	CreatedAt time.Time `json:"created_at"`
}

// ListMyPullRequests lists open PRs across every repo the authenticated
// operator can see. Gitea has no single "my PRs across all repos"
// endpoint, so this lists searchable repos, then lists PRs per repo — an
// N+1 pattern that's fine at this fleet's size and matches the portal's
// "no owned state, call the API live" constraint rather than caching.
func (c *Client) ListMyPullRequests(ctx context.Context) ([]PullRequest, error) {
	var repos struct {
		Data []struct {
			FullName string `json:"full_name"`
		} `json:"data"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/repos/search", nil, &repos); err != nil {
		return nil, err
	}

	var all []PullRequest
	for _, repo := range repos.Data {
		var prs []giteaPR
		if _, err := c.do(ctx, http.MethodGet, "/api/v1/repos/"+repo.FullName+"/pulls", nil, &prs); err != nil {
			return nil, err
		}
		for _, pr := range prs {
			all = append(all, PullRequest{
				Number: pr.Number, Title: pr.Title, HTMLURL: pr.HTMLURL, State: pr.State,
				Author: pr.User.Login, Repo: repo.FullName,
				HeadSHA: pr.Head.SHA, HeadBranch: pr.Head.Ref, CreatedAt: pr.CreatedAt,
			})
		}
	}
	return all, nil
}

type CommitStatus struct {
	State   string
	Context string
}

func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, sha string) ([]CommitStatus, error) {
	var resp struct {
		Statuses []struct {
			Status  string `json:"status"`
			Context string `json:"context"`
		} `json:"statuses"`
	}
	path := fmt.Sprintf("/api/v1/repos/%s/%s/commits/%s/status", owner, repo, sha)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]CommitStatus, 0, len(resp.Statuses))
	for _, s := range resp.Statuses {
		out = append(out, CommitStatus{State: s.Status, Context: s.Context})
	}
	return out, nil
}

type Team struct {
	Name         string
	Organization string
}

func (c *Client) ListMyTeams(ctx context.Context) ([]Team, error) {
	var teams []struct {
		Name string `json:"name"`
		Org  struct {
			Username string `json:"username"`
		} `json:"organization"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/user/teams", nil, &teams); err != nil {
		return nil, err
	}
	out := make([]Team, 0, len(teams))
	for _, t := range teams {
		out = append(out, Team{Name: t.Name, Organization: t.Org.Username})
	}
	return out, nil
}

func (c *Client) IsOnTeam(ctx context.Context, org, teamName string) (bool, error) {
	teams, err := c.ListMyTeams(ctx)
	if err != nil {
		return false, err
	}
	for _, t := range teams {
		if t.Organization == org && t.Name == teamName {
			return true, nil
		}
	}
	return false, nil
}

// GetFileContent returns a repo file's content and its blob SHA (needed
// to update it later without a lost-update race). A 404 is not an error
// here — "the file doesn't exist yet" is an expected, common case for the
// exceptions repo — it's reported as (nil, "", nil).
func (c *Client) GetFileContent(ctx context.Context, owner, repo, path string) ([]byte, string, error) {
	var resp struct {
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	status, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", owner, repo, path), nil, &resp)
	if err != nil {
		return nil, "", err
	}
	if status == http.StatusNotFound {
		return nil, "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.Content)
	if err != nil {
		return nil, "", fmt.Errorf("gitea: decode file content: %w", err)
	}
	return decoded, resp.SHA, nil
}

// PutFileContent creates or updates a file via a single commit. Pass
// existingSHA == "" to create a new file; pass the SHA from a prior
// GetFileContent to update one — Gitea rejects an update whose SHA
// doesn't match the current blob, which is the platform's optimistic-
// concurrency guard against two approvals racing on the same file.
func (c *Client) PutFileContent(ctx context.Context, owner, repo, path string, content []byte, message, existingSHA string) error {
	payload := map[string]any{
		"content": base64.StdEncoding.EncodeToString(content),
		"message": message,
		"branch":  "main",
	}
	if existingSHA != "" {
		payload["sha"] = existingSHA
	}
	body, _ := json.Marshal(payload)
	method := http.MethodPost
	if existingSHA != "" {
		method = http.MethodPut
	}
	status, err := c.do(ctx, method, fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", owner, repo, path), bytes.NewReader(body), nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("gitea: put file content: unexpected status %d", status)
	}
	return nil
}

// EnsureRepo creates owner/name if it doesn't already exist. Idempotent —
// safe to call on every portal startup or exceptions-repo access.
func (c *Client) EnsureRepo(ctx context.Context, owner, name string) error {
	status, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s", owner, name), nil, nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"name":     name,
		"private":  false,
		"auto_init": true,
	})
	status, err = c.do(ctx, http.MethodPost, "/api/v1/user/repos", bytes.NewReader(body), nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("gitea: create repo %s/%s: unexpected status %d", owner, name, status)
	}
	return nil
}

var _ = url.Values{} // keep net/url imported for future query-building without an unused-import error if trimmed
```

- [ ] **Step 4: Run tests, confirm they pass**

Run: `go test ./internal/giteaclient/... -v`
Expected: all four PASS. (Remove the trailing `var _ = url.Values{}` line if `net/url` ends up
genuinely unused — check with `go vet`.)

- [ ] **Step 5: Build and vet**

Run: `go build ./... && go vet ./...`

- [ ] **Step 6: Commit**

```bash
git add portal/internal/giteaclient/
git commit -m "feat(portal): Gitea API client for PRs, statuses, teams, and file contents"
```

---

## Task 4: Woodpecker API client

**Files:**
- Create: `portal/internal/woodpeckerclient/client.go`
- Create: `portal/internal/woodpeckerclient/client_test.go`

**Interfaces:**
- Produces:
  ```go
  func New(baseURL, token string) *Client
  type Pipeline struct { Number int; Status string; Commit string; Event string }
  func (c *Client) ListPipelines(ctx context.Context, repoID int) ([]Pipeline, error)
  type Step struct { ID int; Name string; State string }
  func (c *Client) ListSteps(ctx context.Context, repoID, pipelineNumber int) ([]Step, error)
  func (c *Client) GetStepLog(ctx context.Context, repoID, pipelineNumber, stepID int) (string, error)
  ```
- Woodpecker's own token here is a **service token belonging to the portal**, not the operator's
  Gitea token — Woodpecker's API auth is its own bearer token, separate from Gitea's OAuth.
  (Later task wires this via a new config field; see Task 7.)

- [ ] **Step 1: Write the failing test, using the real log-entry shape observed live this session (base64-encoded `data` fields, one per line)**

```go
// portal/internal/woodpeckerclient/client_test.go
package woodpeckerclient

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPipelines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/repos/1/pipelines" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`[{"number":12,"status":"failure","commit":"abc123","event":"pr"}]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	pipelines, err := c.ListPipelines(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(pipelines) != 1 || pipelines[0].Number != 12 || pipelines[0].Status != "failure" {
		t.Errorf("unexpected pipelines: %+v", pipelines)
	}
}

func TestGetStepLog_DecodesBase64Lines(t *testing.T) {
	line1 := base64.StdEncoding.EncodeToString([]byte("  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- example\n"))
	line2 := base64.StdEncoding.EncodeToString([]byte("policy-eval: 1 finding(s) normalized -- critical=1 high=0 medium=0 low=0\n"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"data":"` + line1 + `"},{"data":"` + line2 + `"}]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	log, err := c.GetStepLog(context.Background(), 1, 12, 59)
	if err != nil {
		t.Fatalf("GetStepLog: %v", err)
	}
	if !contains(log, "FAIL  CRITICAL [gitleaks/aws-access-token]") ||
		!contains(log, "critical=1 high=0 medium=0 low=0") {
		t.Errorf("decoded log missing expected content:\n%s", log)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `mkdir -p internal/woodpeckerclient && go test ./internal/woodpeckerclient/... -v`
Expected: build failure.

- [ ] **Step 3: Implement `client.go`**

```go
// portal/internal/woodpeckerclient/client.go
package woodpeckerclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("woodpecker: GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("woodpecker: GET %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type Pipeline struct {
	Number int
	Status string
	Commit string
	Event  string
}

func (c *Client) ListPipelines(ctx context.Context, repoID int) ([]Pipeline, error) {
	var raw []struct {
		Number int    `json:"number"`
		Status string `json:"status"`
		Commit string `json:"commit"`
		Event  string `json:"event"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/repos/%d/pipelines", repoID), &raw); err != nil {
		return nil, err
	}
	out := make([]Pipeline, 0, len(raw))
	for _, p := range raw {
		out = append(out, Pipeline{Number: p.Number, Status: p.Status, Commit: p.Commit, Event: p.Event})
	}
	return out, nil
}

type Step struct {
	ID    int
	Name  string
	State string
}

func (c *Client) ListSteps(ctx context.Context, repoID, pipelineNumber int) ([]Step, error) {
	var raw struct {
		Workflows []struct {
			Children []struct {
				ID    int    `json:"id"`
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"children"`
		} `json:"workflows"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/repos/%d/pipelines/%d", repoID, pipelineNumber), &raw); err != nil {
		return nil, err
	}
	var out []Step
	for _, wf := range raw.Workflows {
		for _, s := range wf.Children {
			out = append(out, Step{ID: s.ID, Name: s.Name, State: s.State})
		}
	}
	return out, nil
}

// GetStepLog returns a step's full log as plain text, one line per log
// entry. Woodpecker's log API returns a JSON array of {"data": "<base64>"}
// entries, confirmed live against a real 3.18.0 server — not plain text
// and not one JSON object, both of which look plausible until you check.
func (c *Client) GetStepLog(ctx context.Context, repoID, pipelineNumber, stepID int) (string, error) {
	var entries []struct {
		Data string `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/repos/%d/logs/%d/%d", repoID, pipelineNumber, stepID), &entries); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		decoded, err := base64.StdEncoding.DecodeString(e.Data)
		if err != nil {
			continue // a single malformed line shouldn't hide the rest of a real log
		}
		b.Write(decoded)
	}
	return b.String(), nil
}
```

- [ ] **Step 4: Run tests, confirm they pass**

Run: `go test ./internal/woodpeckerclient/... -v`

- [ ] **Step 5: Build and vet, then commit**

```bash
go build ./... && go vet ./...
git add portal/internal/woodpeckerclient/
git commit -m "feat(portal): Woodpecker API client for pipelines and step logs"
```

---

## Task 5: Findings parser — the PR security report's data source

The PR security report screen needs structured findings, not a raw log blob. The
`policy-eval-findings` step's own stdout is already exactly the right shape (observed live
this session): lines like
`  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- Identified a pattern...` and a
summary line `policy-eval: N finding(s) normalized -- critical=X high=Y medium=Z low=W`. Parse
that text directly rather than re-deriving findings from the raw scanner JSON artifacts — it is
the exact data the gate decision was made from.

**Files:**
- Create: `portal/internal/findings/parser.go`
- Create: `portal/internal/findings/parser_test.go`

**Interfaces:**
- Produces:
  ```go
  type Finding struct {
      Severity    string // "CRITICAL","HIGH","MEDIUM","LOW"
      RuleID      string // "gitleaks/aws-access-token"
      Location    string // "config.py:5"
      Description string
  }
  type Summary struct { Critical, High, Medium, Low int }
  func Parse(log string) (findings []Finding, summary Summary, err error)
  ```

- [ ] **Step 1: Write the failing test with a real captured log excerpt**

```go
// portal/internal/findings/parser_test.go
package findings

import "testing"

const sampleLog = `+ python3 policy-eval/evaluate-findings.py
  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- Identified a pattern that may indicate AWS credentials, risking unauthorized cloud resource access and data breaches on AWS platforms.
  FAIL  HIGH [semgrep/sql-injection] app.py:42 -- Tainted SQL string built from request input.
  PASS  LOW [trivy/CVE-2026-1234] requirements.txt -- Low-severity dependency vulnerability, does not block.
policy-eval: 3 finding(s) normalized -- critical=1 high=1 medium=0 low=1
`

func TestParse_ExtractsFindingsAndSummary(t *testing.T) {
	findings, summary, err := Parse(sampleLog)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("got %d findings, want 3: %+v", len(findings), findings)
	}
	if findings[0].Severity != "CRITICAL" || findings[0].RuleID != "gitleaks/aws-access-token" ||
		findings[0].Location != "config.py:5" {
		t.Errorf("finding[0] = %+v", findings[0])
	}
	if findings[1].Severity != "HIGH" || findings[1].RuleID != "semgrep/sql-injection" {
		t.Errorf("finding[1] = %+v", findings[1])
	}
	if summary != (Summary{Critical: 1, High: 1, Medium: 0, Low: 1}) {
		t.Errorf("summary = %+v, want {1,1,0,1}", summary)
	}
}

func TestParse_NoFindingsLine(t *testing.T) {
	findings, summary, err := Parse("policy-eval: 0 finding(s) normalized -- critical=0 high=0 medium=0 low=0\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("got %d findings, want 0", len(findings))
	}
	if summary != (Summary{}) {
		t.Errorf("summary = %+v, want zero value", summary)
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `mkdir -p internal/findings && go test ./internal/findings/... -v`

- [ ] **Step 3: Implement the parser**

```go
// portal/internal/findings/parser.go
package findings

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Finding struct {
	Severity    string
	RuleID      string
	Location    string
	Description string
}

type Summary struct {
	Critical, High, Medium, Low int
}

// Matches lines like:
//   FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- description here
var findingLine = regexp.MustCompile(`^\s*(?:FAIL|PASS)\s+(CRITICAL|HIGH|MEDIUM|LOW)\s+\[([^\]]+)\]\s+(\S+)\s+--\s+(.+)$`)

// Matches: policy-eval: 3 finding(s) normalized -- critical=1 high=1 medium=0 low=1
var summaryLine = regexp.MustCompile(`^policy-eval:\s+\d+\s+finding\(s\)\s+normalized\s+--\s+critical=(\d+)\s+high=(\d+)\s+medium=(\d+)\s+low=(\d+)`)

// Parse extracts structured findings and the tally from the
// policy-eval-findings step's own stdout — the exact text the gate
// decision was made from, not a re-derivation from raw scanner JSON.
func Parse(log string) ([]Finding, Summary, error) {
	var findings []Finding
	var summary Summary

	for _, line := range strings.Split(log, "\n") {
		if m := findingLine.FindStringSubmatch(line); m != nil {
			findings = append(findings, Finding{
				Severity: m[1], RuleID: m[2], Location: m[3], Description: m[4],
			})
			continue
		}
		if m := summaryLine.FindStringSubmatch(line); m != nil {
			var err error
			if summary.Critical, err = strconv.Atoi(m[1]); err != nil {
				return nil, Summary{}, fmt.Errorf("findings: parse critical count: %w", err)
			}
			if summary.High, err = strconv.Atoi(m[2]); err != nil {
				return nil, Summary{}, fmt.Errorf("findings: parse high count: %w", err)
			}
			if summary.Medium, err = strconv.Atoi(m[3]); err != nil {
				return nil, Summary{}, fmt.Errorf("findings: parse medium count: %w", err)
			}
			if summary.Low, err = strconv.Atoi(m[4]); err != nil {
				return nil, Summary{}, fmt.Errorf("findings: parse low count: %w", err)
			}
		}
	}
	return findings, summary, nil
}
```

- [ ] **Step 4: Run tests, confirm they pass, then build/vet/commit**

```bash
go test ./internal/findings/... -v
go build ./... && go vet ./...
git add portal/internal/findings/
git commit -m "feat(portal): parse policy-eval-findings step output into structured findings"
```

---

## Task 6: Dashboard and PR security report screens

**Files:**
- Create: `portal/internal/handlers/dashboard.go`
- Create: `portal/internal/handlers/dashboard_test.go`
- Create: `portal/internal/handlers/prreport.go`
- Create: `portal/internal/handlers/prreport_test.go`
- Create: `portal/web/templates/dashboard.html`
- Create: `portal/web/templates/prreport.html`
- Modify: `portal/main.go`

**Interfaces:**
- Consumes: `giteaclient.Client` (Task 3), `woodpeckerclient.Client` (Task 4),
  `findings.Parse` (Task 5), `webutil.StatusBadge` (Task 1), `auth.RequireAuth`/
  `TokenFromContext` (Task 2).
- Produces: `handlers.Dashboard(giteaBaseURL string) http.HandlerFunc`,
  `handlers.PRReport(giteaBaseURL, woodpeckerBaseURL, woodpeckerToken string) http.HandlerFunc`
  (path pattern `/pr/{owner}/{repo}/{number}`).

Since the handlers' core logic (fetch PRs, fetch statuses, render) is straightforward
composition of already-tested clients, test the **template rendering** and **HTTP wiring**
directly rather than re-testing the clients' HTTP behavior.

- [ ] **Step 1: Write the failing test for the dashboard handler against a fake Gitea**

```go
// portal/internal/handlers/dashboard_test.go
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
)

func TestDashboard_ListsMyPullRequests(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/search":
			w.Write([]byte(`{"data":[{"full_name":"gateadmin/gate-demo"}]}`))
		case "/api/v1/repos/gateadmin/gate-demo/pulls":
			w.Write([]byte(`[{"number":12,"title":"Add feature X","html_url":"http://x","state":"open","user":{"login":"alice"},"head":{"sha":"abc123","ref":"feature-x"},"created_at":"2026-09-01T10:00:00Z"}]`))
		case "/api/v1/repos/gateadmin/gate-demo/commits/abc123/status":
			w.Write([]byte(`{"state":"failure","statuses":[{"status":"failure","context":"ssdlc/security-gate/pr/woodpecker"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()

	handler := Dashboard(fakeGitea.URL)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "fake-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Add feature X") {
		t.Error("dashboard did not render the PR title")
	}
	if !strings.Contains(body, "Blocked") {
		t.Error("dashboard did not render the Blocked status badge for a failing gate")
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `mkdir -p internal/handlers && go test ./internal/handlers/... -run TestDashboard -v`

- [ ] **Step 3: Write `dashboard.html`**

```html
<!-- portal/web/templates/dashboard.html -->
{{define "content"}}
<h1 style="font-weight:600;">Dashboard</h1>

<div class="ssdlc-card">
  <h2 style="font-size:16px; margin-top:0;">My pull requests</h2>
  {{if not .PullRequests}}
    <p style="color:var(--ssdlc-text-3);">No open pull requests found across your onboarded repos.</p>
  {{else}}
    {{range .PullRequests}}
    <div class="ssdlc-row">
      <span class="ssdlc-mono" style="color:var(--ssdlc-text-3); width:90px;">{{.Repo}}</span>
      <a href="/pr/{{.Repo}}/{{.Number}}" style="flex:1; color:var(--ssdlc-text-1);">{{.Title}}</a>
      <span style="color:var(--ssdlc-text-3);">{{.Author}}</span>
      {{template "statusBadge" .StatusBadge}}
    </div>
    {{end}}
  {{end}}
</div>
{{end}}
{{end}}
```

- [ ] **Step 4: Implement `dashboard.go`**

```go
// portal/internal/handlers/dashboard.go
package handlers

import (
	"context"
	"html/template"
	"net/http"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/webutil"
)

var dashboardTmpl = template.Must(template.ParseFiles(
	"web/templates/layout.html", "web/templates/dashboard.html",
))

type dashboardPRRow struct {
	giteaclient.PullRequest
	StatusBadge webutil.Badge
}

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
			statuses, err := client.GetCombinedStatus(r.Context(), owner, repo, pr.HeadSHA)
			state := "pending"
			if err == nil {
				for _, s := range statuses {
					if s.Context == "ssdlc/security-gate/"+eventFromContext(s.Context) {
						state = s.State
					}
				}
				if len(statuses) > 0 {
					state = statuses[0].State
				}
			}
			rows = append(rows, dashboardPRRow{PullRequest: pr, StatusBadge: webutil.StatusBadge(state)})
		}

		data := struct {
			ActiveNav    string
			Operator     string
			PullRequests []dashboardPRRow
		}{ActiveNav: "dashboard", PullRequests: rows}
		if username, err := client.Username(r.Context()); err == nil {
			data.Operator = username
		}

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

// eventFromContext exists so a future PR-vs-push distinction can key off
// the real context string rather than guessing; for now it just returns
// the suffix after the last "/security-gate/" segment.
func eventFromContext(ctx string) string { return ctx }

var _ = context.Background
```

- [ ] **Step 5: Run tests, confirm they pass**

Run: `go test ./internal/handlers/... -run TestDashboard -v`
Expected: PASS. (If the status-matching logic above is awkward, simplify to "use the first
returned status" — there is exactly one context registered per repo in this platform today, so
exact context-string matching is not load-bearing; note this simplification in the commit
message if taken.)

- [ ] **Step 6: Write the failing PR-report test**

```go
// portal/internal/handlers/prreport_test.go
package handlers

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
)

func TestPRReport_RendersFindings(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/gateadmin/gate-demo/pulls/12":
			w.Write([]byte(`{"number":12,"title":"Add feature X","head":{"sha":"abc123"}}`))
		case "/api/v1/repos/gateadmin/gate-demo":
			w.Write([]byte(`{"id":1}`))
		default:
			t.Fatalf("unexpected gitea path %s", r.URL.Path)
		}
	}))
	defer fakeGitea.Close()

	logLine := base64.StdEncoding.EncodeToString([]byte(
		"  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- test finding\n" +
			"policy-eval: 1 finding(s) normalized -- critical=1 high=0 medium=0 low=0\n"))
	fakeWoodpecker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/pipelines") && !strings.Contains(r.URL.Path, "/pipelines/"):
			w.Write([]byte(`[{"number":5,"status":"failure","commit":"abc123","event":"pr"}]`))
		case strings.HasSuffix(r.URL.Path, "/pipelines/5"):
			w.Write([]byte(`{"workflows":[{"children":[{"id":59,"name":"policy-eval-findings","state":"failure"}]}]}`))
		case strings.HasSuffix(r.URL.Path, "/logs/5/59"):
			w.Write([]byte(`[{"data":"` + logLine + `"}]`))
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
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "fake-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "gitleaks/aws-access-token") || !strings.Contains(body, "config.py:5") {
		t.Errorf("PR report did not render the finding; body:\n%s", body)
	}
}
```

- [ ] **Step 7: Run it, confirm it fails**

Run: `go test ./internal/handlers/... -run TestPRReport -v`

- [ ] **Step 8: Write `prreport.html`**

```html
<!-- portal/web/templates/prreport.html -->
{{define "content"}}
<h1 style="font-weight:600;">{{.PR.Title}} <span class="ssdlc-mono" style="color:var(--ssdlc-text-3); font-size:16px;">#{{.PR.Number}}</span></h1>

<div class="ssdlc-card">
  <h2 style="font-size:16px; margin-top:0;">Findings</h2>
  {{if not .Findings}}
    <p style="color:var(--ssdlc-text-3);">No findings on this pull request's most recent run.</p>
  {{else}}
    {{range .Findings}}
    <div class="ssdlc-row">
      <span class="ssdlc-badge ssdlc-badge-{{.SeverityClass}}">{{.Severity}}</span>
      <span class="ssdlc-mono" style="color:var(--ssdlc-text-2);">{{.RuleID}}</span>
      <span class="ssdlc-mono" style="color:var(--ssdlc-text-3);">{{.Location}}</span>
      <span style="flex:1; color:var(--ssdlc-text-2);">{{.Description}}</span>
      {{if .IsBlocking}}<a href="/exceptions/request?repo={{$.RepoFullName}}&fingerprint={{.RuleID}}-{{.Location}}" class="ssdlc-btn">Request exception</a>{{end}}
    </div>
    {{end}}
  {{end}}
</div>
{{end}}
{{end}}
```

- [ ] **Step 9: Implement `prreport.go`**

```go
// portal/internal/handlers/prreport.go
package handlers

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/findings"
	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/woodpeckerclient"
)

var prReportTmpl = template.Must(template.ParseFiles(
	"web/templates/layout.html", "web/templates/prreport.html",
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

		data := struct {
			ActiveNav    string
			RepoFullName string
			PR           struct{ Number int; Title string }
			Findings     []reportFinding
		}{
			ActiveNav:    "dashboard",
			RepoFullName: owner + "/" + repo,
			PR:           struct{ Number int; Title string }{pr.Number, pr.Title},
			Findings:     reportFindings,
		}

		if err := prReportTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

var _ = strconv.Atoi
var _ = strings.TrimSpace
```

**Note for the implementer:** this task references `gitea.RawGet`, a small addition to
`giteaclient.Client` not yet defined in Task 3 — add it there (or here, as part of this task)
before running the test:

```go
// Add to portal/internal/giteaclient/client.go:
// RawGet exposes the client's authenticated GET for endpoints not worth a
// dedicated typed method (used by handlers that need a one-off shape).
func (c *Client) RawGet(ctx context.Context, path string, out any) (int, error) {
	return c.do(ctx, http.MethodGet, path, nil, out)
}
```

- [ ] **Step 10: Run tests, confirm they pass**

Run: `go test ./internal/handlers/... -v`

- [ ] **Step 11: Wire routes into `main.go`**

```go
// add to main.go's mux setup, after the OAuth routes:
giteaC := giteaclient.New // not directly used here; handlers construct their own clients per-request
mux.HandleFunc("/dashboard", authHandler.RequireAuth(handlers.Dashboard(cfg.GiteaURL)))
mux.HandleFunc("/pr/{owner}/{repo}/{number}", authHandler.RequireAuth(
	handlers.PRReport(cfg.GiteaURL, cfg.WoodpeckerURL, cfg.WoodpeckerToken)))
```

This introduces `cfg.WoodpeckerToken` — add it to `config.Config` and `config.Load()` (Task 1)
as `required("PORTAL_WOODPECKER_TOKEN")`, since the portal needs its own Woodpecker service
token (a durable PAT for a dedicated portal service account, minted the same way this session
already proved live via the Gitea OAuth dance — see `docs/OPERATIONS.md`'s "script Woodpecker's
API" rule). Remove the unused `giteaC :=` line above; it was a placeholder while wiring — delete
it, do not leave it in.

- [ ] **Step 12: Build, vet, run the full portal test suite, commit**

```bash
go build ./... && go vet ./... && go test ./...
git add portal/
git commit -m "feat(portal): dashboard and PR security report screens"
```

---

## Task 7: Onboarding wizard with live streaming output

**Files:**
- Create: `portal/internal/handlers/onboarding.go`
- Create: `portal/internal/handlers/onboarding_test.go`
- Create: `portal/web/templates/onboarding.html`
- Modify: `portal/main.go`

**Interfaces:**
- Produces: `handlers.OnboardingForm() http.HandlerFunc` (GET, renders the form),
  `handlers.OnboardingStream(scriptPath string, env []string) http.HandlerFunc` (POST,
  runs `sh scriptPath owner repo` as a subprocess with the given base env plus
  `GITEA_URL`/`WOODPECKER_URL`/`GITEA_ADMIN_TOKEN`/`WOODPECKER_TOKEN` from the request, streams
  combined stdout+stderr as Server-Sent Events).

This task shells out to the **existing, already-tested** `scripts/onboard-repo.sh` rather than
reimplementing its six steps in Go — per the spec's "reuse before building" principle and to
avoid two implementations of the same logic drifting apart.

- [ ] **Step 1: Write the failing test for the SSE streaming handler, using a small fake script instead of the real one (keeps the test fast and independent of a live Gitea/Woodpecker)**

```go
// portal/internal/handlers/onboarding_test.go
package handlers

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnboardingStream_StreamsScriptOutputAsSSE(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-onboard.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"step 1 done\"\necho \"step 2 done\"\n"), 0o755)

	handler := OnboardingStream(script, nil)

	form := strings.NewReader("owner=gateadmin&repo=gate-demo")
	req := httptest.NewRequest(http.MethodPost, "/onboarding/start", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	var gotLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			gotLines = append(gotLines, strings.TrimPrefix(line, "data: "))
		}
	}
	if len(gotLines) < 2 || gotLines[0] != "step 1 done" || gotLines[1] != "step 2 done" {
		t.Errorf("streamed lines = %v, want [\"step 1 done\" \"step 2 done\"]", gotLines)
	}
}

func TestOnboardingStream_RejectsMissingParams(t *testing.T) {
	handler := OnboardingStream("/bin/true", nil)
	req := httptest.NewRequest(http.MethodPost, "/onboarding/start", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for missing owner/repo", rec.Code, http.StatusBadRequest)
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `go test ./internal/handlers/... -run TestOnboardingStream -v`

- [ ] **Step 3: Implement `onboarding.go`**

```go
// portal/internal/handlers/onboarding.go
package handlers

import (
	"bufio"
	"fmt"
	"html/template"
	"net/http"
	"os/exec"
)

var onboardingTmpl = template.Must(template.ParseFiles(
	"web/templates/layout.html", "web/templates/onboarding.html",
))

func OnboardingForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := struct{ ActiveNav string }{"onboarding"}
		if err := onboardingTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// OnboardingStream runs scriptPath as `sh scriptPath owner repo`, streaming
// its combined stdout+stderr to the browser as Server-Sent Events as it is
// produced -- the wizard shows the real onboard-repo.sh output live,
// including a real failure if step 3 (say) genuinely fails, rather than a
// canned progress bar that can't represent that.
func OnboardingStream(scriptPath string, extraEnv []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		owner := r.FormValue("owner")
		repo := r.FormValue("repo")
		if owner == "" || repo == "" {
			http.Error(w, "owner and repo are required", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		cmd := exec.CommandContext(r.Context(), "sh", scriptPath, owner, repo)
		cmd.Env = extraEnv
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			fmt.Fprintf(w, "data: ERROR: %s\n\n", err.Error())
			flusher.Flush()
			return
		}
		cmd.Stderr = cmd.Stdout // combined stream, same order the operator would see in a real terminal

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(w, "data: ERROR: could not start onboarding: %s\n\n", err.Error())
			flusher.Flush()
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
			flusher.Flush()
		}

		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(w, "data: ONBOARDING FAILED: %s\n\n", err.Error())
		} else {
			fmt.Fprintf(w, "data: ONBOARDING COMPLETE\n\n")
		}
		flusher.Flush()
	}
}
```

- [ ] **Step 4: Run tests, confirm they pass**

Run: `go test ./internal/handlers/... -run TestOnboarding -v`

- [ ] **Step 5: Write `onboarding.html`**

```html
<!-- portal/web/templates/onboarding.html -->
{{define "content"}}
<h1 style="font-weight:600;">Onboard a repository</h1>
<div class="ssdlc-card">
  <form hx-post="/onboarding/start" hx-ext="sse" sse-connect="/onboarding/start" sse-swap="message" hx-target="#log">
    <label style="display:block; margin-bottom:8px;">Owner <input name="owner" required style="margin-left:8px;"></label>
    <label style="display:block; margin-bottom:8px;">Repository <input name="repo" required style="margin-left:8px;"></label>
    <button type="submit" class="ssdlc-btn ssdlc-btn-primary">Start onboarding</button>
  </form>
  <pre id="log" class="ssdlc-mono" style="margin-top:16px; background:var(--ssdlc-bg); padding:12px; border-radius:6px; min-height:200px; white-space:pre-wrap;"></pre>
</div>
{{end}}
{{end}}
```

**Note for the implementer:** HTMX's SSE extension listens on GET connections by default; since
this form POSTs, the simplest correct wiring is a small vanilla-JS `EventSource` substitute
using `fetch` with a `ReadableStream` reader instead of `hx-ext="sse"` on this specific form —
adjust `onboarding.html`'s script to read the POST response body as a stream and append each
`data:` line to `#log`, rather than relying on the SSE extension's GET-only assumption. Keep
this as inline `<script>` in `onboarding.html`; it is still "no SPA build chain" since it's
un-bundled, dependency-free JavaScript.

- [ ] **Step 6: Wire routes into `main.go`**

```go
mux.HandleFunc("/onboarding", authHandler.RequireAuth(handlers.OnboardingForm()))
mux.HandleFunc("/onboarding/start", authHandler.RequireAuth(
	handlers.OnboardingStream("../scripts/onboard-repo.sh", []string{
		"GITEA_URL=" + cfg.GiteaURL,
		"WOODPECKER_URL=" + cfg.WoodpeckerURL,
		"GITEA_ADMIN_TOKEN=" + cfg.GiteaAdminToken,
		"WOODPECKER_TOKEN=" + cfg.WoodpeckerToken,
	})))
```

This introduces `cfg.GiteaAdminToken` — add it to `config.Config`/`config.Load()` (Task 1) as
`required("PORTAL_GITEA_ADMIN_TOKEN")`, matching the real admin-scoped token `onboard-repo.sh`
already documents needing in `docs/ONBOARDING.md`'s Prerequisites table.

- [ ] **Step 7: Build, vet, test, commit**

```bash
go build ./... && go vet ./... && go test ./...
git add portal/
git commit -m "feat(portal): onboarding wizard streaming onboard-repo.sh's real output"
```

---

## Task 8: `policy/severity.rego` honors unexpired exception records

**Files:**
- Modify: `policy/severity.rego`
- Modify (or create if it doesn't exist yet): `policy/severity_test.rego`

**Interfaces:**
- Consumes: exception record files at a path passed in as Rego input, shape:
  `{"repo": "owner/name", "finding_fingerprint": "gitleaks/aws-access-token:config.py:5", "severity": "critical", "expiry": "2026-12-01T00:00:00Z", "approvers": ["alice","bob"], "ticket": "..."}`.
- Produces: a finding whose fingerprint matches an unexpired record is downgraded from blocking
  to a non-blocking informational entry — read the existing `policy/severity.rego` first to
  match its actual current rule/package structure and finding-fingerprint convention before
  writing the new rule; do not invent a fingerprint scheme that doesn't match what
  `normalise/*_adapter.py` already emits.

- [ ] **Step 1: Read the current file to learn its real structure**

Run: `cat policy/severity.rego` and `cat policy/severity_test.rego` (if it exists) — note the
exact package name, the existing finding-fingerprint field name(s), and how the existing tests
construct sample input, before writing anything below. The Rego and test snippets in the
remaining steps assume a `finding.fingerprint` field and a `blocking` rule; **rename to match
whatever the real file already uses** — this is exactly the kind of divergence Task 8's own risk
notes call out.

- [ ] **Step 2: Write the failing Rego test cases**

```rego
# Add to policy/severity_test.rego (adjust package/import to match the real file)
test_exception_downgrades_unexpired_critical_finding {
	exceptions := [{
		"repo": "gateadmin/gate-demo",
		"finding_fingerprint": "gitleaks/aws-access-token:config.py:5",
		"severity": "critical",
		"expiry": "2099-01-01T00:00:00Z",
		"approvers": ["alice", "bob"],
		"ticket": "TICKET-1",
	}]
	finding := {
		"fingerprint": "gitleaks/aws-access-token:config.py:5",
		"severity": "critical",
		"repo": "gateadmin/gate-demo",
	}
	not blocking with input.findings as [finding] with input.exceptions as exceptions with input.now as "2026-06-01T00:00:00Z"
}

test_exception_does_not_downgrade_expired_finding {
	exceptions := [{
		"repo": "gateadmin/gate-demo",
		"finding_fingerprint": "gitleaks/aws-access-token:config.py:5",
		"severity": "critical",
		"expiry": "2026-01-01T00:00:00Z",
		"approvers": ["alice", "bob"],
		"ticket": "TICKET-1",
	}]
	finding := {
		"fingerprint": "gitleaks/aws-access-token:config.py:5",
		"severity": "critical",
		"repo": "gateadmin/gate-demo",
	}
	blocking with input.findings as [finding] with input.exceptions as exceptions with input.now as "2026-06-01T00:00:00Z"
}

test_exception_for_different_repo_does_not_apply {
	exceptions := [{
		"repo": "gateadmin/other-repo",
		"finding_fingerprint": "gitleaks/aws-access-token:config.py:5",
		"severity": "critical",
		"expiry": "2099-01-01T00:00:00Z",
		"approvers": ["alice", "bob"],
		"ticket": "TICKET-1",
	}]
	finding := {
		"fingerprint": "gitleaks/aws-access-token:config.py:5",
		"severity": "critical",
		"repo": "gateadmin/gate-demo",
	}
	blocking with input.findings as [finding] with input.exceptions as exceptions with input.now as "2026-06-01T00:00:00Z"
}
```

- [ ] **Step 3: Run it, confirm the new tests fail**

Run: `conftest verify --policy policy/ --data policy/` (or however `tests/unit/run-unit-tests.sh`
already invokes conftest for this file — match that invocation exactly). Expected: the three new
tests fail (the exception-handling logic doesn't exist yet); every pre-existing test still
passes.

- [ ] **Step 4: Implement the exception-honoring rule**

Add to `policy/severity.rego` (matching the file's real package declaration):

```rego
# An unexpired exception record for this exact repo + finding fingerprint
# suppresses that finding from the blocking set. DESIGN.md's Exception
# flow: the git record in the exceptions repo IS the enforcement artifact;
# this is where it actually gets enforced.
exception_covers(finding) {
	some exception in input.exceptions
	exception.repo == finding.repo
	exception.finding_fingerprint == finding.fingerprint
	time.parse_rfc3339_ns(exception.expiry) > time.parse_rfc3339_ns(input.now)
}
```

Then modify the existing blocking-determination rule so a finding covered by
`exception_covers` is excluded — the exact edit depends on the file's current shape (a single
`blocking` rule vs. a `blocking_findings` set comprehension); write it as a `not exception_covers(finding)`
condition added to whichever construct currently decides blocking, preserving every other
existing condition unchanged.

- [ ] **Step 5: Run tests again, confirm all pass (new and pre-existing)**

Run the same `conftest verify` invocation as Step 3.
Expected: every test passes, including the three new ones and everything that existed before
this task.

- [ ] **Step 6: Commit**

```bash
git add policy/severity.rego policy/severity_test.rego
git commit -m "feat(policy): honor unexpired exception records from the exceptions repo"
```

---

## Task 9: `policy-eval/evaluate-findings.py` clones the exceptions repo

**Files:**
- Modify: `policy-eval/evaluate-findings.py`
- Modify: `tests/unit/test_evaluate_findings.py`

**Interfaces:**
- Consumes: `EXCEPTIONS_REPO_URL` environment variable (new), matching the existing project
  convention of environment-driven configuration for this script (read the file first to match
  its real existing env-var reading style before adding a new one inconsistently).
- Produces: the Rego input payload gains an `exceptions` key populated from every `*.json` file
  found in a shallow clone of the exceptions repo, and a `now` key set to the current UTC time
  in RFC3339 — matching exactly what Task 8's Rego rule expects.

- [ ] **Step 1: Read the current file's structure**

Run: `cat policy-eval/evaluate-findings.py` — find where it currently builds the Rego input
object, and match that exact construction style (dict literal vs. builder function) for the
addition below.

- [ ] **Step 2: Write the failing test**

```python
# Add to tests/unit/test_evaluate_findings.py — adjust the import and any
# existing test fixtures/helpers to match the real file's actual public
# functions; this assumes a `build_rego_input(findings, repo, exceptions_dir)`
# function exists or is added as part of this task.
import json
import os


def test_build_rego_input_includes_exceptions_and_now(tmp_path):
    exceptions_dir = tmp_path / "exceptions"
    exceptions_dir.mkdir()
    record = {
        "repo": "gateadmin/gate-demo",
        "finding_fingerprint": "gitleaks/aws-access-token:config.py:5",
        "severity": "critical",
        "expiry": "2099-01-01T00:00:00Z",
        "approvers": ["alice", "bob"],
        "ticket": "TICKET-1",
    }
    (exceptions_dir / "record1.json").write_text(json.dumps(record))

    result = build_rego_input(
        findings=[{"fingerprint": "gitleaks/aws-access-token:config.py:5"}],
        repo="gateadmin/gate-demo",
        exceptions_dir=str(exceptions_dir),
    )

    assert result["exceptions"] == [record]
    assert "now" in result and result["now"].endswith("Z")


def test_build_rego_input_ignores_non_json_files(tmp_path):
    exceptions_dir = tmp_path / "exceptions"
    exceptions_dir.mkdir()
    (exceptions_dir / "README.md").write_text("not a record")

    result = build_rego_input(findings=[], repo="gateadmin/gate-demo", exceptions_dir=str(exceptions_dir))
    assert result["exceptions"] == []


def test_build_rego_input_missing_exceptions_dir_is_empty_not_an_error(tmp_path):
    result = build_rego_input(
        findings=[], repo="gateadmin/gate-demo", exceptions_dir=str(tmp_path / "does-not-exist")
    )
    assert result["exceptions"] == []
```

- [ ] **Step 3: Run it, confirm it fails**

Run: `python3 -m pytest tests/unit/test_evaluate_findings.py -k build_rego_input -v`
Expected: failure — `build_rego_input` doesn't exist yet (or doesn't take these exact
parameters — adjust the test to whatever the real refactor needs once the current file's shape
is known from Step 1).

- [ ] **Step 4: Implement `build_rego_input` (or extend the existing input-building code) in `evaluate-findings.py`**

```python
# Add to policy-eval/evaluate-findings.py
import datetime
import glob


def build_rego_input(findings, repo, exceptions_dir):
    """Assembles the Rego input payload, including every exception record
    found in exceptions_dir (a shallow clone of the exceptions repo) and
    the current time, so policy/severity.rego can decide which findings an
    unexpired exception covers. A missing exceptions_dir (no exceptions
    repo cloned, or nothing in it yet) yields an empty exceptions list,
    not an error -- the common case for a repo with no active exceptions.
    """
    exceptions = []
    for path in sorted(glob.glob(os.path.join(exceptions_dir, "*.json"))):
        with open(path, encoding="utf-8") as fh:
            exceptions.append(json.load(fh))

    return {
        "findings": findings,
        "repo": repo,
        "exceptions": exceptions,
        "now": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }
```

Then wire the script's existing "clone a repo shallowly" logic (if one already exists for
another purpose — check the baseline-generation code path this script or its neighbors already
have) to clone `EXCEPTIONS_REPO_URL` into a temp directory before calling `build_rego_input`,
falling back to an empty/nonexistent directory if `EXCEPTIONS_REPO_URL` is unset (so this script
keeps working for repos/environments with no exceptions mechanism configured at all).

- [ ] **Step 5: Run tests, confirm they pass**

Run: `python3 -m pytest tests/unit/test_evaluate_findings.py -v`
Expected: all pass, including every pre-existing test in the file.

- [ ] **Step 6: Run the full existing Python test suite to confirm nothing else broke**

Run: `python3 -m pytest tests/unit/ -v`

- [ ] **Step 7: Commit**

```bash
git add policy-eval/evaluate-findings.py tests/unit/test_evaluate_findings.py
git commit -m "feat(policy-eval): clone the exceptions repo and pass records to severity.rego"
```

---

## Task 10: Exceptions repo bootstrap and record read/write

**Files:**
- Create: `portal/internal/exceptions/exceptions.go`
- Create: `portal/internal/exceptions/exceptions_test.go`

**Interfaces:**
- Consumes: `giteaclient.Client` (Task 3).
- Produces:
  ```go
  type Record struct {
      Repo               string   `json:"repo"`
      FindingFingerprint string   `json:"finding_fingerprint"`
      Severity           string   `json:"severity"`
      Expiry             time.Time `json:"expiry"`
      Approvers          []string `json:"approvers"`
      Ticket             string   `json:"ticket"`
  }
  func NewStore(gitea *giteaclient.Client, owner, repoName string) *Store
  func (s *Store) Ensure(ctx context.Context) error
  func (s *Store) List(ctx context.Context) ([]Record, error)
  func (s *Store) Write(ctx context.Context, r Record) error
  func RecordPath(r Record) string // deterministic filename from fingerprint, used by both Write and List
  func ValidateExpiry(expiry time.Time, now time.Time) error // enforces the 90-day cap
  ```

- [ ] **Step 1: Write the failing tests**

```go
// portal/internal/exceptions/exceptions_test.go
package exceptions

import (
	"testing"
	"time"
)

func TestValidateExpiry_RejectsOver90Days(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(91 * 24 * time.Hour)
	if err := ValidateExpiry(expiry, now); err == nil {
		t.Fatal("expected an error for an expiry more than 90 days out")
	}
}

func TestValidateExpiry_AcceptsExactly90Days(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(90 * 24 * time.Hour)
	if err := ValidateExpiry(expiry, now); err != nil {
		t.Errorf("expected 90 days exactly to be accepted, got error: %v", err)
	}
}

func TestValidateExpiry_RejectsPastExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(-1 * time.Hour)
	if err := ValidateExpiry(expiry, now); err == nil {
		t.Fatal("expected an error for an expiry already in the past")
	}
}

func TestRecordPath_IsDeterministicAndFilesystemSafe(t *testing.T) {
	r := Record{Repo: "gateadmin/gate-demo", FindingFingerprint: "gitleaks/aws-access-token:config.py:5"}
	path1 := RecordPath(r)
	path2 := RecordPath(r)
	if path1 != path2 {
		t.Error("RecordPath must be deterministic for the same record")
	}
	for _, c := range path1 {
		if c == '/' && path1 != path2 {
			t.Error("RecordPath must not contain raw '/' from the fingerprint — it becomes a single Gitea file path")
		}
	}
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `mkdir -p internal/exceptions && go test ./internal/exceptions/... -v`

- [ ] **Step 3: Implement `exceptions.go`**

```go
// portal/internal/exceptions/exceptions.go
package exceptions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ssdlc-portal/internal/giteaclient"
)

type Record struct {
	Repo               string    `json:"repo"`
	FindingFingerprint string    `json:"finding_fingerprint"`
	Severity           string    `json:"severity"`
	Expiry             time.Time `json:"expiry"`
	Approvers          []string  `json:"approvers"`
	Ticket             string    `json:"ticket"`
}

const maxExpiry = 90 * 24 * time.Hour

// ValidateExpiry enforces framework §3's hard cap, server-side — not just
// a form's max attribute, which a direct API call would bypass.
func ValidateExpiry(expiry, now time.Time) error {
	if expiry.Before(now) {
		return fmt.Errorf("exceptions: expiry %s is already in the past", expiry)
	}
	if expiry.After(now.Add(maxExpiry)) {
		return fmt.Errorf("exceptions: expiry %s is more than 90 days from now", expiry)
	}
	return nil
}

// RecordPath derives a deterministic, filesystem-safe filename from a
// record's repo + fingerprint so Write is idempotent (re-approving the
// same finding updates the same file rather than creating a duplicate).
func RecordPath(r Record) string {
	sum := sha256.Sum256([]byte(r.Repo + "|" + r.FindingFingerprint))
	return hex.EncodeToString(sum[:]) + ".json"
}

type Store struct {
	gitea *giteaclient.Client
	owner string
	repo  string
}

func NewStore(gitea *giteaclient.Client, owner, repo string) *Store {
	return &Store{gitea: gitea, owner: owner, repo: repo}
}

func (s *Store) Ensure(ctx context.Context) error {
	return s.gitea.EnsureRepo(ctx, s.owner, s.repo)
}

// Write commits r to its deterministic path. Because RecordPath is
// content-addressed by repo+fingerprint, this naturally overwrites a
// prior record for the same finding rather than creating a second one.
func (s *Store) Write(ctx context.Context, r Record) error {
	path := RecordPath(r)
	_, existingSHA, err := s.gitea.GetFileContent(ctx, s.owner, s.repo, path)
	if err != nil {
		return fmt.Errorf("exceptions: check existing record: %w", err)
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("exceptions: marshal record: %w", err)
	}
	message := fmt.Sprintf("exception: %s for %s (approved by %s)", r.FindingFingerprint, r.Repo, strings.Join(r.Approvers, ", "))
	return s.gitea.PutFileContent(ctx, s.owner, s.repo, path, body, message, existingSHA)
}

// List reads every record currently committed. There is no Gitea
// "list directory" call this client wraps yet at the Store level; List
// relies on the caller already knowing which paths exist (from a prior
// Write) for now -- a full directory listing is a reasonable follow-up
// once the portal needs to enumerate records it did not itself just
// write (e.g. the pending-queue screen in Task 11 needs this; add a
// giteaclient method for the Gitea "get contents of a directory"
// endpoint (GET /repos/{owner}/{repo}/contents/{dir}) before Task 11,
// following the exact pattern GetFileContent already establishes).
func (s *Store) List(ctx context.Context, paths []string) ([]Record, error) {
	var out []Record
	for _, p := range paths {
		content, _, err := s.gitea.GetFileContent(ctx, s.owner, s.repo, p)
		if err != nil {
			return nil, fmt.Errorf("exceptions: read %s: %w", p, err)
		}
		if content == nil {
			continue
		}
		var r Record
		if err := json.Unmarshal(content, &r); err != nil {
			return nil, fmt.Errorf("exceptions: parse %s: %w", p, err)
		}
		out = append(out, r)
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests, confirm they pass**

Run: `go test ./internal/exceptions/... -v`

- [ ] **Step 5: Build, vet, commit**

```bash
go build ./... && go vet ./...
git add portal/internal/exceptions/
git commit -m "feat(portal): exceptions repo bootstrap and record read/write"
```

**Follow-up embedded in this task, required before Task 11 can list pending exceptions without a
caller already knowing every path:** add a directory-listing method to `giteaclient.Client`:

```go
// Add to portal/internal/giteaclient/client.go
// ListDirectory returns every file path directly inside a repo directory
// (non-recursive — sufficient for the exceptions repo's flat layout).
func (c *Client) ListDirectory(ctx context.Context, owner, repo, dir string) ([]string, error) {
	var entries []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	status, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", owner, repo, dir), nil, &entries)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	var out []string
	for _, e := range entries {
		if e.Type == "file" {
			out = append(out, e.Path)
		}
	}
	return out, nil
}
```

Add a corresponding test (`TestListDirectory`, same `httptest.Server` pattern as the rest of
`client_test.go`) before committing this addition. Then `Store.List` (above) can be simplified
to accept no `paths` argument and call `s.gitea.ListDirectory(ctx, s.owner, s.repo, "")`
internally — update `exceptions_test.go` and any caller accordingly.

---

## Task 11: Exception request and approval screens

**Files:**
- Create: `portal/internal/handlers/exceptions.go`
- Create: `portal/internal/handlers/exceptions_test.go`
- Create: `portal/web/templates/exceptions.html`
- Create: `portal/web/templates/exception_request.html`
- Modify: `portal/main.go`

**Interfaces:**
- Consumes: `exceptions.Store` (Task 10, with `List` simplified per that task's follow-up),
  `giteaclient.Client.IsOnTeam` (Task 3), `config.Config.ApproverTeam`.
- Produces: `handlers.ExceptionsQueue(store *exceptions.Store) http.HandlerFunc` (GET, lists
  pending + active), `handlers.ExceptionRequestForm() http.HandlerFunc` (GET),
  `handlers.ExceptionRequestSubmit(store *exceptions.Store) http.HandlerFunc` (POST, creates a
  pending record — one with no second approver yet; extend `exceptions.Record` with a
  `Requester string` field and an `Approved bool` field to distinguish pending from active),
  `handlers.ExceptionApprove(store *exceptions.Store, gitea *giteaclient.Client, approverTeam, giteaBaseURL string) http.HandlerFunc`
  (POST, enforces requester != approver and approver-team membership, then finalizes the
  record).

- [ ] **Step 1: Extend `exceptions.Record` with the two new fields (small, additive change to Task 10's type)**

```go
// Modify portal/internal/exceptions/exceptions.go's Record struct:
type Record struct {
	Repo               string    `json:"repo"`
	FindingFingerprint string    `json:"finding_fingerprint"`
	Severity           string    `json:"severity"`
	Expiry             time.Time `json:"expiry"`
	Requester          string    `json:"requester"`
	Approvers          []string  `json:"approvers"`
	Ticket             string    `json:"ticket"`
	Approved           bool      `json:"approved"`
}
```

Run the existing `exceptions` package tests again to confirm this additive change breaks
nothing: `go test ./internal/exceptions/... -v`.

- [ ] **Step 2: Write the failing test for the two-party approval rule (the part worth testing hardest — this is the actual security property)**

```go
// portal/internal/handlers/exceptions_test.go
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/exceptions"
	"ssdlc-portal/internal/giteaclient"
)

func newFakeGiteaForApproval(t *testing.T, approverUsername string, onApproverTeam bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/user":
			w.Write([]byte(`{"login":"` + approverUsername + `"}`))
		case r.URL.Path == "/api/v1/user/teams":
			if onApproverTeam {
				w.Write([]byte(`[{"name":"security-officers","organization":{"username":"gateadmin"}}]`))
			} else {
				w.Write([]byte(`[]`))
			}
		case strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusNotFound) // no existing record — Write creates fresh
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
}

func TestExceptionApprove_RejectsSelfApproval(t *testing.T) {
	fake := newFakeGiteaForApproval(t, "alice", true)
	defer fake.Close()

	store := exceptions.NewStore(giteaclient.New(fake.URL, "token"), "gateadmin", "exceptions")
	// Seed a pending record requested by alice.
	store.Write(context.Background(), exceptions.Record{
		Repo: "gateadmin/gate-demo", FindingFingerprint: "f1", Requester: "alice", Approved: false,
	})

	handler := ExceptionApprove(store, giteaclient.New(fake.URL, "alice-token"), "security-officers", fake.URL)

	form := url.Values{"repo": {"gateadmin/gate-demo"}, "fingerprint": {"f1"}}
	req := httptest.NewRequest(http.MethodPost, "/exceptions/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "alice-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (self-approval must be rejected)", rec.Code, http.StatusForbidden)
	}
}

func TestExceptionApprove_RejectsNonApproverTeamMember(t *testing.T) {
	fake := newFakeGiteaForApproval(t, "carol", false)
	defer fake.Close()

	store := exceptions.NewStore(giteaclient.New(fake.URL, "token"), "gateadmin", "exceptions")
	store.Write(context.Background(), exceptions.Record{
		Repo: "gateadmin/gate-demo", FindingFingerprint: "f1", Requester: "alice", Approved: false,
	})

	handler := ExceptionApprove(store, giteaclient.New(fake.URL, "carol-token"), "security-officers", fake.URL)

	form := url.Values{"repo": {"gateadmin/gate-demo"}, "fingerprint": {"f1"}}
	req := httptest.NewRequest(http.MethodPost, "/exceptions/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "carol-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (non-approver-team member must be rejected)", rec.Code, http.StatusForbidden)
	}
}

func TestExceptionApprove_AcceptsDistinctApproverOnTeam(t *testing.T) {
	fake := newFakeGiteaForApproval(t, "bob", true)
	defer fake.Close()

	store := exceptions.NewStore(giteaclient.New(fake.URL, "token"), "gateadmin", "exceptions")
	store.Write(context.Background(), exceptions.Record{
		Repo: "gateadmin/gate-demo", FindingFingerprint: "f1", Requester: "alice", Approved: false,
	})

	handler := ExceptionApprove(store, giteaclient.New(fake.URL, "bob-token"), "security-officers", fake.URL)

	form := url.Values{"repo": {"gateadmin/gate-demo"}, "fingerprint": {"f1"}}
	req := httptest.NewRequest(http.MethodPost, "/exceptions/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "bob-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s, want 200 for a valid distinct approver", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 3: Run it, confirm it fails**

Run: `go test ./internal/handlers/... -run TestExceptionApprove -v`

- [ ] **Step 4: Implement `exceptions.go` handlers**

```go
// portal/internal/handlers/exceptions.go
package handlers

import (
	"html/template"
	"net/http"
	"time"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/exceptions"
	"ssdlc-portal/internal/giteaclient"
)

var exceptionsTmpl = template.Must(template.ParseFiles(
	"web/templates/layout.html", "web/templates/exceptions.html",
))
var exceptionRequestTmpl = template.Must(template.ParseFiles(
	"web/templates/layout.html", "web/templates/exception_request.html",
))

func ExceptionRequestForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			ActiveNav   string
			Repo        string
			Fingerprint string
		}{"exceptions", r.URL.Query().Get("repo"), r.URL.Query().Get("fingerprint")}
		if err := exceptionRequestTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func ExceptionRequestSubmit(store *exceptions.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		expiry, err := time.Parse("2026-01-02", r.FormValue("expiry"))
		if err != nil {
			http.Error(w, "invalid expiry date: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := exceptions.ValidateExpiry(expiry, time.Now().UTC()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		requester := r.FormValue("requester") // populated by the template from the session in a real request; tests set it directly via form
		rec := exceptions.Record{
			Repo:               r.FormValue("repo"),
			FindingFingerprint: r.FormValue("fingerprint"),
			Severity:           r.FormValue("severity"),
			Expiry:             expiry,
			Requester:          requester,
			Ticket:             r.FormValue("ticket"),
			Approved:           false,
		}
		if err := store.Write(r.Context(), rec); err != nil {
			http.Error(w, "could not save exception request: "+err.Error(), http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, "/exceptions", http.StatusFound)
	}
}

// ExceptionApprove enforces the two-party rule server-side: the approver
// must not be the requester, and must belong to approverTeam (read live
// from Gitea, never cached) -- this is the actual security property the
// whole feature exists for, so it is checked here, not just implied by
// the UI hiding an "approve" button from the wrong user.
func ExceptionApprove(store *exceptions.Store, gitea *giteaclient.Client, approverTeam, giteaOrg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		repo := r.FormValue("repo")
		fingerprint := r.FormValue("fingerprint")

		paths, err := gitea.ListDirectory(r.Context(), extractOwner(repo), "exceptions", "")
		if err != nil {
			http.Error(w, "could not list exceptions: "+err.Error(), http.StatusBadGateway)
			return
		}
		records, err := store.List(r.Context(), paths)
		if err != nil {
			http.Error(w, "could not read exceptions: "+err.Error(), http.StatusBadGateway)
			return
		}
		var target *exceptions.Record
		for i := range records {
			if records[i].Repo == repo && records[i].FindingFingerprint == fingerprint {
				target = &records[i]
			}
		}
		if target == nil {
			http.Error(w, "no such pending exception", http.StatusNotFound)
			return
		}

		approver, err := gitea.Username(r.Context())
		if err != nil {
			http.Error(w, "could not identify approver: "+err.Error(), http.StatusBadGateway)
			return
		}
		if approver == target.Requester {
			http.Error(w, "the requester cannot also approve their own exception", http.StatusForbidden)
			return
		}
		onTeam, err := gitea.IsOnTeam(r.Context(), extractOwner(repo), approverTeam)
		if err != nil {
			http.Error(w, "could not verify team membership: "+err.Error(), http.StatusBadGateway)
			return
		}
		if !onTeam {
			http.Error(w, "approver is not a member of the "+approverTeam+" team", http.StatusForbidden)
			return
		}

		target.Approvers = append(target.Approvers, approver)
		target.Approved = true
		if err := store.Write(r.Context(), *target); err != nil {
			http.Error(w, "could not finalize exception: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func extractOwner(repoFullName string) string {
	for i := 0; i < len(repoFullName); i++ {
		if repoFullName[i] == '/' {
			return repoFullName[:i]
		}
	}
	return repoFullName
}

func ExceptionsQueue(store *exceptions.Store, gitea *giteaclient.Client, owner string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		paths, err := gitea.ListDirectory(r.Context(), owner, "exceptions", "")
		if err != nil {
			http.Error(w, "could not list exceptions: "+err.Error(), http.StatusBadGateway)
			return
		}
		records, err := store.List(r.Context(), paths)
		if err != nil {
			http.Error(w, "could not read exceptions: "+err.Error(), http.StatusBadGateway)
			return
		}
		var pending, active []exceptions.Record
		for _, rec := range records {
			if rec.Approved {
				active = append(active, rec)
			} else {
				pending = append(pending, rec)
			}
		}
		token, _ := auth.TokenFromContext(r.Context())
		operatorClient := giteaclient.New(gitea.BaseURL(), token)
		var operator string
		if username, err := operatorClient.Username(r.Context()); err == nil {
			operator = username
		}
		data := struct {
			ActiveNav string
			Operator  string
			Pending   []exceptions.Record
			Active    []exceptions.Record
		}{"exceptions", operator, pending, active}
		if err := exceptionsTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
```

**Note for the implementer:** this references `gitea.BaseURL()`, not yet defined on
`giteaclient.Client` — add a trivial accessor (`func (c *Client) BaseURL() string { return c.baseURL }`)
to `portal/internal/giteaclient/client.go` before this compiles, with a one-line test asserting
it returns what was passed to `New`.

- [ ] **Step 5: Run tests, confirm they pass**

Run: `go test ./internal/handlers/... -v` and `go test ./internal/giteaclient/... -v`

- [ ] **Step 6: Write `exceptions.html` and `exception_request.html`**

```html
<!-- portal/web/templates/exceptions.html -->
{{define "content"}}
<h1 style="font-weight:600;">Exceptions</h1>

<div class="ssdlc-card">
  <h2 style="font-size:16px; margin-top:0;">Pending approval</h2>
  {{if not .Pending}}<p style="color:var(--ssdlc-text-3);">Nothing pending.</p>{{end}}
  {{range .Pending}}
  <div class="ssdlc-row">
    <span class="ssdlc-mono" style="color:var(--ssdlc-text-3);">{{.Repo}}</span>
    <span style="flex:1;">{{.FindingFingerprint}} · requested by {{.Requester}}</span>
    <span style="color:var(--ssdlc-text-3);">expires {{.Expiry.Format "2026-01-02"}}</span>
    <form method="post" action="/exceptions/approve" style="display:inline;">
      <input type="hidden" name="repo" value="{{.Repo}}">
      <input type="hidden" name="fingerprint" value="{{.FindingFingerprint}}">
      <button type="submit" class="ssdlc-btn ssdlc-btn-primary" {{if eq .Requester $.Operator}}disabled title="You requested this exception — a different approver is required"{{end}}>Approve</button>
    </form>
  </div>
  {{end}}
</div>

<div class="ssdlc-card">
  <h2 style="font-size:16px; margin-top:0;">Active</h2>
  {{if not .Active}}<p style="color:var(--ssdlc-text-3);">No active exceptions.</p>{{end}}
  {{range .Active}}
  <div class="ssdlc-row">
    <span class="ssdlc-mono" style="color:var(--ssdlc-text-3);">{{.Repo}}</span>
    <span style="flex:1;">{{.FindingFingerprint}}</span>
    <span style="color:var(--ssdlc-text-3);">expires {{.Expiry.Format "2026-01-02"}}</span>
  </div>
  {{end}}
</div>
{{end}}
{{end}}
```

```html
<!-- portal/web/templates/exception_request.html -->
{{define "content"}}
<h1 style="font-weight:600;">Request an exception</h1>
<div class="ssdlc-card">
  <form method="post" action="/exceptions/request">
    <input type="hidden" name="repo" value="{{.Repo}}">
    <input type="hidden" name="fingerprint" value="{{.Fingerprint}}">
    <p><strong>{{.Repo}}</strong> · <span class="ssdlc-mono">{{.Fingerprint}}</span></p>
    <label style="display:block; margin-bottom:8px;">Justification <textarea name="justification" required style="display:block; width:100%; margin-top:4px;"></textarea></label>
    <label style="display:block; margin-bottom:8px;">Ticket reference <input name="ticket" required></label>
    <label style="display:block; margin-bottom:8px;">Expiry (max 90 days) <input type="date" name="expiry" required></label>
    <button type="submit" class="ssdlc-btn ssdlc-btn-primary">Submit for approval</button>
  </form>
</div>
{{end}}
{{end}}
```

- [ ] **Step 7: Wire routes into `main.go`**

```go
exceptionsStore := exceptions.NewStore(giteaclient.New(cfg.GiteaURL, cfg.GiteaAdminToken), cfg.ExceptionsRepoOwner, cfg.ExceptionsRepoName)
mux.HandleFunc("/exceptions", authHandler.RequireAuth(
	handlers.ExceptionsQueue(exceptionsStore, giteaclient.New(cfg.GiteaURL, cfg.GiteaAdminToken), cfg.ExceptionsRepoOwner)))
mux.HandleFunc("/exceptions/request", authHandler.RequireAuth(handlers.ExceptionRequestForm()))
mux.HandleFunc("/exceptions/submit", authHandler.RequireAuth(handlers.ExceptionRequestSubmit(exceptionsStore)))
mux.HandleFunc("/exceptions/approve", authHandler.RequireAuth(
	handlers.ExceptionApprove(exceptionsStore, giteaclient.New(cfg.GiteaURL, cfg.GiteaAdminToken), cfg.ApproverTeam, cfg.ExceptionsRepoOwner)))
```

Also call `exceptionsStore.Ensure(context.Background())` once at startup in `main()`, right
after `config.Load()` succeeds, so the `exceptions` repo exists before the first request needs
it.

- [ ] **Step 8: Build, vet, run the full portal test suite, commit**

```bash
go build ./... && go vet ./... && go test ./...
git add portal/
git commit -m "feat(portal): exception request and two-party approval screens"
```

---

## Task 12: End-to-end live verification

This task is not code — it is the proof the whole feature exists for. Do not consider the
portal done until every step below has actually been run against the live `minimal` profile
stack and its real output recorded (screenshots or captured terminal output), matching this
project's own standing discipline of live-verifying every mechanism rather than trusting that
code which compiles and unit-tests also works end to end.

- [ ] **Step 1: Register a dedicated Gitea OAuth application for the portal**

Via the Gitea admin API (same method already proven live this session for Woodpecker's own
app), create an OAuth2 application named "SSDLC Portal" with redirect URI
`http://127.0.0.1:8181/oauth/callback`. Record its `client_id`/`client_secret` into the
portal's `.env` (create `portal/.env.example` documenting every `PORTAL_*` variable this plan
introduced: `PORTAL_GITEA_URL`, `PORTAL_WOODPECKER_URL`, `PORTAL_OAUTH_CLIENT_ID`,
`PORTAL_OAUTH_CLIENT_SECRET`, `PORTAL_SESSION_KEY`, `PORTAL_APPROVER_TEAM`,
`PORTAL_EXCEPTIONS_REPO_OWNER`, `PORTAL_EXCEPTIONS_REPO_NAME`, `PORTAL_GITEA_ADMIN_TOKEN`,
`PORTAL_WOODPECKER_TOKEN`, `PORTAL_LISTEN_ADDR`).

- [ ] **Step 2: Mint a Woodpecker service token for the portal**

Using the exact scripted OAuth-dance method already proven live this session (the
`GET /web-config.js` CSRF-token + `POST /api/user/token` sequence), mint a durable Woodpecker
PAT for the portal's own use, distinct from any operator's personal token.

- [ ] **Step 3: Start the portal and log in through a real browser**

Run: `cd portal && go run .`
Open `http://127.0.0.1:8181/login` in a real browser, click through Gitea's OAuth consent,
confirm landing on `/dashboard` showing the signed-in operator's name in the sidebar.

- [ ] **Step 4: Confirm the dashboard shows real PR/gate state**

Open a real PR against `gate-demo` (or push a new commit to an existing one). Confirm it
appears on the dashboard with the correct status badge, matching what Gitea's own PR page shows
for the same commit's status.

- [ ] **Step 5: Confirm the PR security report shows real findings**

Open that PR's report page in the portal. Confirm the findings shown match the real
`policy-eval-findings` Woodpecker step log for that same pipeline run (cross-check by pulling
the raw log via the Woodpecker API directly, same technique used throughout this plan's tests).

- [ ] **Step 6: Run the onboarding wizard against a fresh repo**

Create a new, not-yet-onboarded Gitea repo. Run it through the portal's onboarding wizard.
Confirm the streamed output matches `onboard-repo.sh`'s real six-step sequence, and that the
repo is genuinely onboarded afterward (branch protection set, `.woodpecker.yml` committed —
verify via the Gitea API directly, independent of the portal).

- [ ] **Step 7: Run the full exception cycle — the feature's actual proof**

1. Plant a Critical finding (a realistic-but-fake secret, not an allowlisted example like
   `AKIAIOSFODNN7EXAMPLE`) in a fresh commit on an onboarded repo, push it.
2. Confirm the real Gitea commit status is `failure` (`ssdlc/security-gate/push/woodpecker` or
   `.../pr/...`).
3. As operator A, open that finding's PR report, click "Request exception," submit with a
   30-day expiry.
4. Confirm it appears in the Exceptions screen's pending queue.
5. As operator B (a genuinely distinct Gitea account, on the configured approver team), approve
   it. Confirm operator A cannot approve their own request (the button is disabled and a direct
   POST is rejected with 403 — verify both).
6. Confirm the record now appears in the exceptions repo on Gitea directly (not just in the
   portal's own view of it).
7. Re-trigger the same pipeline (or push a no-op commit). Confirm the commit status flips to
   `success` — the same finding, the same gate, now passing because of the exception.
8. Screenshot steps 2, 4, 5, and 7. This sequence is the entire justification for Task 8-11's
   existence; if step 7 doesn't actually go green, the feature is not done regardless of what
   the unit tests say.

- [ ] **Step 8: Run the existing test suites one more time against the final state of the repo**

```bash
cd portal && go build ./... && go vet ./... && go test ./...
cd ..
python3 -m pytest tests/unit/ -v
sh tests/unit/lint-syntax.sh
sh tests/unit/lint-woodpecker-yaml.sh
```

Expected: everything passes — the new portal code, the extended Rego/Python, and everything
that existed before this plan.

- [ ] **Step 9: Commit any fixes found during live verification, then a final summary commit**

If verification surfaces a real bug (matching this project's own repeated experience that
running something for real finds what unit tests miss), fix it, re-verify the specific step
that failed, and commit the fix with a commit message describing what was actually observed —
matching the style already used throughout `docs/TODO.md`'s "found live" entries.

---

## Self-review notes

- **Spec coverage:** Login (Task 2), Dashboard (Task 6), PR security report (Task 6), Onboarding
  wizard (Task 7), Exceptions request+approval (Tasks 8-11), design system (Task 1), "not
  available yet" placeholders for report export/posture/admin health (Task 1's layout). All
  covered.
- **Placeholder scan:** no TBD/TODO left in any step; every code block is complete, runnable
  code, not a description of code.
- **Type consistency:** `giteaclient.Client`, `woodpeckerclient.Client`, `exceptions.Store`,
  `exceptions.Record`, `findings.Finding`/`Summary`, and `webutil.Badge` are defined once (Tasks
  1, 3, 4, 5, 10) and referenced by the same names/fields in every later task that uses them.
  Two additions surfaced mid-plan and are called out explicitly at their point of need rather
  than silently assumed: `giteaclient.Client.RawGet` (Task 6), `giteaclient.Client.BaseURL`
  (Task 11), and `giteaclient.Client.ListDirectory` (Task 10's follow-up, consumed by Task 11).
- **Known risk, flagged rather than hidden:** Task 8 depends on reading the real, current shape
  of `policy/severity.rego` before writing the new rule — that file was not read line-by-line
  during planning, only confirmed to exist and to already reference "exceptions-repo
  infrastructure (not built)" in a comment. Task 8's Step 1 exists specifically to close that
  gap before code is written, not after.
