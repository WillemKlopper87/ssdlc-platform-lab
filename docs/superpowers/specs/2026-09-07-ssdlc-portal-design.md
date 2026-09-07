# SSDLC Portal — design (D10 MVP + real exceptions)

**Status:** approved for implementation planning
**Implements:** [`DESIGN.md` D10](../DESIGN.md) ("The SSDLC Portal: one front door, no security
state"), scoped to what's buildable against the services actually running in the `minimal`
profile today.
**Visual reference:** a Claude Design canvas artifact titled "SSDLC Platform Portal" — three
design directions (V1, V2, V3), V3 ("selected direction," dark theme) is the one this spec
follows. Its four artboard files are saved under `docs/superpowers/specs/2026-09-07-portal-mockup/`
for reference during implementation (`Main.dc.html`, `PRReport.dc.html`, `Exceptions.dc.html`,
`Onboarding.dc.html`, `AutomatedPR.dc.html`, `AutomatedPRDark.dc.html`, `canvas.json`).

## Why this scope, not the full D10 scope

D10 lists seven screens across a 4-week milestone: dashboard, PR security report, **report
export** (signed HTML/PDF), **exception** request + two-party approval, onboarding wizard,
**posture** (embedded Grafana), **admin health**. Three of those seven depend on infrastructure
that does not exist in this environment, confirmed live:

- The **reporting sidecar** D10 says the portal reuses ("one Go service reuses the sidecar's API
  clients") does not exist as code anywhere in the repo (`grep -r sidecar` finds only design-doc
  prose, no `sidecar/` directory, no Go source at all in the project — the only `.go` files in
  the repo are Semgrep test fixtures under `policy/vendored-rules/`).
- **DefectDojo** is explicitly not deployed (`README.md`: "DefectDojo/Dependency-Track ... arrive
  in later milestones"; `compose/minimal` is "deliberately just the CI loop").
- The **exceptions repo mechanism** (a dedicated Gitea repo holding signed risk-acceptance
  records, per `DESIGN.md`'s Exception flow section) does not exist. `docs/EXCEPTIONS.md` states
  outright: *"Status: not built."* `policy/severity.rego` has a comment confirming the same:
  exception-honouring "depends on the exceptions-repo infrastructure (Milestone 4, not built)."

Building the full seven-screen scope means building the sidecar and standing up DefectDojo
first — a genuinely separate, multi-week piece of work. This spec instead covers:

1. The four screens fully buildable against what's live today: **Login**, **Dashboard**, **PR
   security report**, **Onboarding wizard** — every one backed by real Gitea/Woodpecker API
   calls, no mock data.
2. A **real, working exceptions flow** — narrower than D10's DefectDojo-backed version, but not a
   stub: the portal takes over the one specific write path DESIGN.md already specifies for the
   sidecar (a signed record committed to a dedicated `exceptions` Gitea repo), and
   `policy/severity.rego` + `policy-eval/evaluate-findings.py` are extended to actually honour
   those records. An approved exception must visibly turn a red gate green on the next push —
   that is the acceptance bar, not "a form that writes JSON somewhere."
3. **Report export, embedded Grafana posture, and admin health are out of scope for this build.**
   Each gets a visible, honest "not available yet" state in the UI — never a fake screen, never
   silently omitted from the nav (an operator should be able to see D10's full intended shape,
   just not use the unbuilt parts).

## Visual design — match the mockup, not just its spirit

The user's explicit ask: the portal should look very similar to the V3 mockup, because that
density of information (findings, gate state, developer context all visible at once) is the
actual point of building this. Treat the mockup as load-bearing spec, not loose inspiration.

**Typography:** IBM Plex Sans (UI text) + IBM Plex Mono (code, commit SHAs, fingerprints, rule
IDs) — loaded from Google Fonts exactly as the mockup does. No other typeface.

**Color system:** OKLCH, not hex — extracted directly from the mockup's actual CSS
(`grep -o 'oklch([^)]*)' AutomatedPRDark.dc.html`, tallied by hue):

| Hue | Role | Sample values seen |
|---|---|---|
| 265 (~262-265) | Neutral scale — backgrounds, borders, body text, at lightness 0.16 (page background) through 0.86 (near-white text) | `oklch(0.165 0.012 265)` page bg, `oklch(0.27 0.012 265)` card/border, `oklch(0.82 0.008 265)` primary text |
| 152 | Success / gate passed | `oklch(0.72 0.15 152)` accent, `oklch(0.15 0.03 152)` tinted background |
| 22 | Critical / error | matched at lightness ~0.5-0.6 for text, ~0.2 for tinted backgrounds |
| 68 | Warning / High-but-not-blocking | amber family |
| 258 | Primary accent — links, primary buttons, active nav | `oklch(0.68 0.14 258)`, `oklch(0.82 0.09 258)` |
| 300 | Secondary accent — reserved for a distinct role (e.g. "automated fix" badges, distinguishing bot-authored PRs from human ones) | low-frequency, used deliberately not decoratively |

Rebuild these as a small Go `template.FuncMap`/CSS custom-property set (`--ssdlc-bg`,
`--ssdlc-critical`, `--ssdlc-success`, ...) rather than hardcoding oklch strings per template —
one token file, matching the mockup's actual palette, not a re-guessed one.

**Layout, taken directly from the mockup's structure:**
- Fixed-width left sidebar (256px in the mockup's `Main.dc.html`), dark, holding primary nav +
  the signed-in operator's identity — present on every authenticated screen.
- Content is card-based on a slightly darker page background (`oklch(0.165 ...)` page vs.
  `oklch(0.27 ...)` above it) — same "surface floats above canvas" relationship the mockup uses
  throughout, not a flat single-background page.
- Status is never color-alone: every gate/finding state pairs its color with a text label and,
  where the mockup does this (PR queue rows, finding severity), an icon — carried into HTMX
  partials as a shared `{{template "statusBadge"}}` so the mapping lives in exactly one place.
- Information density matches the mockup's PR report and dashboard: multiple data points per
  row (repo, branch, gate state, age, author, finding count) rather than one fact per card —
  this is explicitly what the user wants preserved, not simplified into a lighter/emptier layout.

## Architecture

One Go binary (module `ssdlc-portal`, new to this repo — confirmed no Go project exists here
today), server-rendered HTML + HTMX + the color/type system above, per D10's mandate: *"one Go
service ... server-rendered HTML/HTMX. One binary, no SPA build chain."* No client-side
framework, no `npm`/build step. `net/http` + `html/template` is enough; reach for a router
library (`chi` or the stdlib 1.22+ `http.ServeMux` pattern matching, matching the Go version
already used by ACS elsewhere on this machine) rather than a full web framework.

**The portal owns no security state**, per D10, literally: no database. Every page is built from
live API calls at request time:
- **Gitea API** (`/api/v1/...`) — repos, PRs, commit statuses, teams (for role checks), the
  `exceptions` repo's contents.
- **Woodpecker API** (`/api/repos/{id}/pipelines/...`) — pipeline runs, step logs, for the PR
  security report's raw scanner output when a developer wants to see past the summary.

No caching layer, no background refresh job — page-load latency is "however fast Gitea/Woodpecker
answer," which is fine at this fleet size and avoids the exact risk D10 warns about (a cache is
owned state with its own staleness/security-boundary problems).

**Auth:** a new, dedicated Gitea OAuth2 application (separate `client_id`/`client_secret` from
Woodpecker's own — different callback URL, different blast radius if one leaks). Session is a
signed, `HttpOnly`, `Secure`, `SameSite=Lax` cookie holding the operator's Gitea access token
(encrypted at rest in the cookie, not just signed — the token is a real credential). Role/team
membership is read from Gitea **on every privileged action**, never cached in the session —
demoting or promoting someone in Gitea takes effect on their very next click, not next login.

## Exceptions — the real mechanism

```
Portal: exception request                Gitea: exceptions repo
────────────────────────                 ──────────────────────
operator opens a Critical finding
  on a PR, clicks "Request exception"
        │
        ▼
form: justification + expiry (≤90 days,
  enforced) + ticket reference
        │
        ▼
status: pending, visible in the queue
        │
   second operator (not the requester,
   checked via Gitea team membership)
   approves
        │
        ▼
portal commits a signed JSON record ───► exceptions/<repo>/<fingerprint>.json
  {repo, finding_fingerprint, severity,   {"repo": "...", "finding_fingerprint": "...",
   expiry, approvers: [a, b], ticket}      "severity": "critical", "expiry": "2026-...",
                                            "approvers": ["alice","bob"], "ticket": "..."}
```

`policy/severity.rego` gains a rule that, given a finding's fingerprint, checks for a matching
unexpired record in a checked-out copy of the `exceptions` repo and downgrades/excludes it from
the blocking set. `policy-eval/evaluate-findings.py` gains the clone-and-pass-through step
(mirroring how it already clones nothing today — this is new, not a rewire of existing logic).

**Signing** — "signed record" per DESIGN.md means committed by an identity Gitea can attribute
(the portal's own machine account, matching how `bot-approver.py` already has a scoped identity),
not a cryptographic signature scheme — matches the existing project's use of the term elsewhere
(e.g. `bot-approver.py`'s "signed record" language for its own audit trail). Call this out
explicitly in the implementation so nobody later assumes GPG/Sigstore involvement that isn't
there.

**Verification plan for this specifically** (the part worth over-verifying, since it's the
feature's whole reason to exist): onboard a fresh demo repo, push a planted Critical finding,
confirm the gate blocks it (real Gitea commit status, `failure`), request and approve an
exception through the portal with two distinct operator accounts, push the same commit again
(or re-trigger), confirm the commit status flips to `success` — screenshotted at each step.

## Screens

**Login** — Gitea OAuth button only. No password field of its own; the portal never sees or
stores a Gitea password.

**Dashboard** — "My PRs" (authored by or requesting review from the signed-in operator, across
every onboarded repo), each row showing gate state (pill, color-coded per the token system
above), repo, branch, age, finding count by severity. An "Attention queue" section above it:
anything red or pending human action, platform-wide, not scoped to "mine" — matching D10's
"attention queue" language.

**PR security report** — one PR's full finding list, grouped by tool (Gitleaks/Semgrep/Trivy),
each finding showing severity, rule ID, file:line, the human-readable explanation, and — this is
the "explain trace" D10 asks for — which `policy/severity.rego` rule made the block/pass decision
for it, and whether an exception record was consulted (and if so, which one, linking to it).
"Request exception" appears inline per-finding for Critical/High findings.

**Onboarding wizard** — a thin UI over `onboard-repo.sh`'s real six steps (repo lookup, baseline
generation, pipeline template commit, gate-bot collaborator, branch protection), run live,
showing each step's actual output as it happens rather than a canned progress bar — if step 3
fails for real, the wizard shows the real error, not a generic "something went wrong."

**Exceptions** — the pending queue (awaiting a second approver), the active list (unexpired
records, with days-remaining), and the request form. Wired to the mechanism above end to end.

**Not built, shown as disabled/placeholder nav items with a one-line "why":** Report export,
Posture (Grafana embed), Admin health.

## Non-goals (explicit, so scope doesn't quietly grow)

- No git browsing, diff viewer, or code hosting of any kind — D10's own rule: "we aggregate; we
  never re-implement the forge." Every such link opens the real Gitea page in a new tab.
- No new persistent datastore. If a future need genuinely requires one, that is a new design
  discussion, not an assumption baked in here.
- No DefectDojo integration in this build — the exceptions mechanism above is real but
  deliberately narrower than D10's eventual DefectDojo-backed version.
- No changes to Woodpecker, Gitea, or the existing pipeline templates beyond what the exceptions
  mechanism requires (`policy/severity.rego`, `policy-eval/evaluate-findings.py`).

## Testing

- New Go code: `go vet ./...` and `go test ./...`, matching this session's own standard for
  every other piece of code touched in this project.
- `policy/severity.rego` changes: extend the existing Rego unit tests
  (`policy/severity_test.rego`, run via `conftest verify`, per `tests/unit/run-unit-tests.sh`'s
  existing (currently skipped locally) conftest check) — new tests for the exception-honouring
  rule specifically, both the "unexpired record downgrades a Critical" and "expired record does
  not" cases.
- `policy-eval/evaluate-findings.py` changes: extend `tests/unit/test_evaluate_findings.py`.
- End-to-end: the plant-block-approve-reverify cycle described above, run against the live
  `minimal` profile stack, screenshotted — not just unit-tested.

## Open questions carried into planning, not resolved here

- Exact Go router/template-organisation choice (a planning-time detail, not an architectural
  one — either choice satisfies "one binary, no SPA build chain").
- Whether the portal runs via `go run` alongside the other bare-metal services
  (`scripts/quickstart.sh`'s documented pattern) or gets its own `compose/minimal` service
  entry — decide once the binary exists and its actual resource footprint is known.
