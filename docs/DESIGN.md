# SSDLC-Platform-Lab

A self-hosted, fully open-source secure-SDLC lab: Gitea + Woodpecker CI + gates that block merges until code proves itself.

**Location:** `c:\applications\SSDLC-Platform-Lab` — standalone, no dependency on any other project in the workspace.

**Status:** design, reviewed three times; Milestone 0 complete, Milestone 1 substantially complete, Milestone 2 opened. The third review pass closed two holes the first two missed: **exception materialization** (the gate could not honour an approved exception without breaking its own no-DefectDojo rule — see D5) and **differential baseline gating** (onboarding any repo with existing findings would have blocked every PR from day one — see *Policy model*).

**Milestone 2 has a first real artifact.** [SADR-0008](adr/0008-reviewdog-inline-comments.md): `pipelines/fast.woodpecker.yml` now posts real inline PR comments via reviewdog's native Gitea reporter, sourced from Semgrep's SARIF output — live-tested with two genuine vulnerabilities in a fixture file, producing two correctly-attributed, correctly-linked comments on the real PR, zero manual scanner invocation. Deliberately non-blocking, matching the milestone's "comments only, measure false-positive rate first" scope — `sast`/`dependencies`/`secrets` remain the unchanged, separate gate. Six real bugs found and fixed along the way, one of which (Gitea deriving `clone_url` from the triggering request's `Host` header, not solely `ROOT_URL`) is a standing operational requirement for every future live test against this profile, not specific to this experiment.

All four of Milestone 0's items are **built and empirically tested**, not merely designed, and D1's live spike (the one item still open at the end of Milestone 0) is now **fully closed** too:

- **D2** (identity-bound merge control) — confirmed forgeable, then confirmed the fix works, both directions, live against Gitea 1.27.2. [SADR-0001](adr/0001-commit-status-forgery.md).
- **D7** (server-side secret gate) — built, and its one subtle security property (suppressions read from the default branch, never the incoming ref) proven with a real attack attempt that failed as designed. [SADR-0002](adr/0002-pre-receive-secret-gate.md).
- **D6** (platform self-patching) — version pin corrected from an earlier `1.26.4` floor to the actual current `1.27.2`, cross-checked against 84 real GitHub Security Advisories (not summarised secondhand); two advisories found that bear directly on other decisions, tracked as follow-ups. [PINNED_VERSIONS.md](PINNED_VERSIONS.md).
- **D1** (pipeline-exit-code gate) — mechanism confirmed against Woodpecker's source in Milestone 0; the live spike in Milestone 1 found and fixed three real bugs (a documented upstream URL-split limitation, Gitea's own SSRF protection correctly firing, and an undocumented CSRF-token requirement that was the actual cause of the session's hardest blocker) and finished with a real pipeline's `exit 1` becoming a real Gitea commit status. [SADR-0004](adr/0004-woodpecker-live-spike.md) — also corrects the assumed status-context string; see D1 below.

Milestone 1 has produced real, working, live-tested artifacts, not designs waiting to be built:

- `compose/minimal/` — Gitea + Woodpecker + Postgres, named Docker volumes (not host bind mounts —
  see [SADR-0005](adr/0005-sca-dast-automation.md), a real Windows filesystem-corruption finding),
  a live-verified push → webhook → pipeline → commit-status loop.
- `pipelines/fast.woodpecker.yml` — the paved-road pipeline template (Gitleaks → Semgrep → Trivy →
  summary) every onboarded repo receives automatically.
- `scripts/onboard-repo.sh` — the platform-admin automation that activates a repo, commits the
  pipeline template into it, and sets branch protection, so a developer's entire interaction with
  the platform is: clone, code, push, read the result.

**Live-proven, completely:** a simulated developer with zero platform knowledge cloned an onboarded
repo, added a real vulnerable dependency (`pyyaml==5.3.1`, CVE-2020-14343), and pushed. No scanner
was invoked by hand. Trivy caught it, the pipeline failed, and Gitea's branch protection genuinely
blocked the PR — confirmed via the actual commit-status API response, not inferred. Five real bugs
surfaced and were fixed getting there (Windows bind-mount corruption, a branch-protection model that
initially locked the platform's own automation out, `network_mode: service:gitea` covering the OAuth
path but not pipeline execution, `ROOT_URL` needing to be the container-reachable hostname rather
than `127.0.0.1`, and `trivy --skip-db-update` failing with no prior cache) — full detail in
[SADR-0005](adr/0005-sca-dast-automation.md).

**DAST is now proven, and automated.** [SADR-0006](adr/0006-dast-zap-juice-shop.md): a real
`zap-baseline.py` scan against a live OWASP Juice Shop instance produced real, risk-classified
findings — missing security headers, cache-control misconfigurations, a dangerous-JS-function match
— landing as `WARN`, none as `FAIL`, confirming rather than contradicting this design's placement of
DAST as a warn-mode, pre-prod-staging signal. [SADR-0007](adr/0007-scheduled-dast-automation.md) then
closed the automation gap: a real Woodpecker cron job, triggering `pipelines/dast-scheduled.woodpecker.yml`
against a persistent staging target, produced the same findings with no scanner command run by hand.
Both scanning legs named in "go build and live-test SCA and DAST" are now proven **and** automated,
not merely designed. Two genuine Woodpecker mechanics were hit and worked around but not fully
explained along the way (`from_secret` not populating despite matching Woodpecker's own test
fixtures, and server-side `${...}` template substitution reaching into YAML comments) — tracked as
open questions in SADR-0007, not silently resolved.

**SADR-0003 is now confirmed too — and it's serious.** The PR-retargeting approval bypass
(CVE-2026-58439) is real, live-tested: an approval given against an unprotected branch survives a
retarget to a protected one, unchanged, and satisfies `required_approvals` there without the
approver ever reviewing the code as it lands. Worse, the obvious fix — `dismiss_stale_approvals` —
was tested directly and **does not help**: it only fires on new commits, not on the base branch
changing. This is a second, independent bypass of D2's approval half, alongside SADR-0001's
status-forgery bypass of its status half, and **neither is closed by anything in the current
design**. `policy-eval` (D1) must independently re-verify that counted approvals were given against
the PR's *current* base, not merely trust Gitea's stored `official` flag — now a hard Milestone 3
requirement, not an optional hardening note. [SADR-0003](adr/0003-pr-retargeting-approval-bypass.md).

---

## Context

**Problem.** The SSDLC Framework (`SDLC/SSDLC_Framework(1).docx`, v1.1) defines a complete secure development lifecycle — governance, coding standards, pipeline gates, deployment controls, runtime monitoring, ISO/IEC 27001 evidence. It is a Word document. Nothing enforces it.

**Goal.** A lab other developers can stand up and point their projects at, which makes the framework mechanical: scanning runs automatically, findings surface in the PR with file/line context, and **merge is blocked until Critical/High findings are fixed or formally risk-accepted**.

**Non-goal.** This is not a scanner. Every scanner already exists and is excellent. The work is *integration and enforcement* — the connective tissue no open-source project currently ships.

### Source documents

| Document | Contributes |
|---|---|
| `SSDLC_Framework(1).docx` | Severity SLAs (§2.4), exception process (§3), AI-assistant policy (§4), stage matrix (§3.2), ISO 27001 evidence map (§7), metrics (§6) |
| `ssdlc_pipeline_overview.mermaid` | DESIGN → DEVELOP → DEPLOY → MANAGE, findings feeding back to design |
| `ssdlc_approval_workflow.mermaid` | pre-commit → fast gates → peer review → merge → GitHub mirror → deep gates → release gate → runtime |

The **framework's five phases** are the build target; the four-phase diagram is a communications artifact. Where this plan and a source document disagree, the source document wins — except where this plan explicitly records a divergence as a SADR (see D8).

---

## Decisions

Each carries risk. Each is a correction to an earlier draft.

### D1 — The gate is a pipeline exit code, not a service

**Policy evaluation is a Woodpecker step** — a `policy-eval` container that reads normalised scanner output and exits non-zero on violation. Woodpecker reports commit status to Gitea natively. No custom service sits in the trust path.

**Mechanism confirmed against Woodpecker's actual source** (`server/forge/gitea/gitea.go`, `server/forge/common/status.go`, main branch, 2026-08-23 — not documentation, not assumed): Woodpecker's Gitea driver calls `client.CreateStatus(owner, repo, sha, ...)` — the identical Gitea API endpoint exercised directly in [SADR-0001](adr/0001-commit-status-forgery.md). Status mapping is explicit in `getStatus()`: `model.StatusFailure` (a step's non-zero exit) → `gitea.StatusFailure`; `model.StatusSuccess` → `gitea.StatusSuccess`. The context string comes from `GetPipelineStatusContext()`, built from server-level config (`server.Config.Server.StatusContext` / `StatusContextFormat`) — meaning `ssdlc/security-gate` is a deployment-time configuration choice, not something to hope Woodpecker happens to produce. This confirms D1's mechanism is real and controllable, not merely plausible.

**Live spike complete — fully confirmed, end to end.** [SADR-0004](adr/0004-woodpecker-live-spike.md). The interactive OAuth2 consent step turned out to be fully scriptable via curl and cookie jars — no browser needed. Getting from login to a real pipeline result took three real bugs found and fixed across two sessions: an internal-vs-external Gitea URL split (a documented, still-open Woodpecker limitation, [issue #1560](https://github.com/woodpecker-ci/woodpecker/issues/1560)), Gitea's own outbound-webhook SSRF protection correctly refusing a loopback target, and — the actual cause of the original 401s, found only by reading raw Woodpecker source directly — an undocumented `X-CSRF-TOKEN` requirement on session-cookie-authenticated writes. With all three fixed: pushed a real `.woodpecker.yml` with a step that `exit 1`s, and confirmed the resulting Gitea commit status: `state: failure`, `context: ssdlc/security-gate/push/woodpecker`, `creator: gateadmin` (the integration, not a human).

**One correction this produced:** the status context is not the bare `ssdlc/security-gate` this design assumed throughout — Woodpecker's template appends `/{event}/{workflow}`, so a push produces `ssdlc/security-gate/push/woodpecker` and a PR produces a *different* string. (Confirmed twice now, and corrected once already: it's `ssdlc/security-gate/pr/woodpecker` — Woodpecker's actual event name is `pr`, not `pull_request` as first assumed here; see [SADR-0005](adr/0005-sca-dast-automation.md).) Milestone 3's branch protection must use a **glob pattern** (`ssdlc/security-gate/**`), not an exact match — already implemented this way in `scripts/onboard-repo.sh`, and verified live to match both event types correctly. Every other reference to the bare context string in this design should be read with that correction in mind.

The custom service is demoted to a **reporting sidecar**: pretty PR comment, DefectDojo push, Prometheus metrics, exception commands. If it dies, developers lose a nice comment. They do not lose the gate, and no PR gets stuck.

This removes bespoke queueing, retry, idempotency and status-reconciliation code that Woodpecker already has, and shrinks the blast radius of a sidecar compromise from "mark any commit green estate-wide" to "post a misleading comment."

### D2 — Commit status alone cannot be the control

Gitea matches required checks by **context name**. `POST /repos/{owner}/{repo}/statuses/{sha}` is a repo-write operation, and every developer has repo write — that is how they push. Nothing binds a context name to an authorised reporter.

**Confirmed empirically, not assumed** — [SADR-0001](adr/0001-commit-status-forgery.md), 2026-08-23, Gitea 1.26. A throwaway instance was provisioned with a plain collaborator (`write` permission, a token scoped to nothing but `write:repository` — exactly what every real developer holds) and branch protection requiring `ssdlc/security-gate` to succeed. Baseline: merge correctly blocked (`405 Not all required status checks successful`) with no status present. Then:

```
POST /api/v1/repos/gateadmin/forge-target/statuses/<PR-head-SHA>
{"state":"success","context":"ssdlc/security-gate"}
→ 201, creator: alice

POST /pulls/1/merge {"Do":"merge"}
→ 200 OK
GET  /pulls/1 → "merged": true, "merged_by": {"login": "alice"}
```

The PR merged. An ordinary collaborator, with no admin privilege and no misconfiguration beyond Gitea's default permission model, forged the gate's exact status and merged an unreviewed, unscanned PR straight past branch protection. This is not a theoretical risk — it is what every developer on a real team can do today, against this exact protection config.

Therefore, from Milestone 3 day one, an identity-bound control is **mandatory, not optional hardening**:

- A **protected reviewer team whose only member is a bot account**, with the token held solely by the platform. Gitea blocks self-approval and supports dismissing stale approvals — that is identity-bound in a way status is not.
- **Required approvals = 2: the bot plus at least one human.** Misconfigure this as 1 with the bot whitelisted, and a green pipeline merges with no human review at all — peer review silently evaporates.
- Enable **dismiss stale approvals on new commits**, or the bot-approval mitigation becomes a push-after-approve race.
- Ideally remove direct merge from developers entirely and serialise through a merge queue ([gitea-mq](https://github.com/Mic92/gitea-mq)), so the bot is the only identity that can write to `main`. gitea-mq is a small third-party project — verify its maintenance state before depending on it. Contingency: a ~100-line merge-when-green loop in the sidecar, acceptable only because Gitea re-enforces branch protection at merge time, so the sidecar still cannot merge a red PR.
- Audit PATs for broad `write:repository` scope.
- **Disable repo forks org-wide** — internal, branch-based PRs only. This removes the entire fork-secrets exposure class (including GHSA-rjvx-x5h2-6px5) and the fork-pipeline secret-withholding problem in one setting.

**`required_approvals` alone is not sufficient — confirmed by a second, independent bypass.** [SADR-0003](adr/0003-pr-retargeting-approval-bypass.md): an approval given against an unprotected branch survives a retarget to a protected one unchanged, and satisfies `required_approvals` there without the approver ever evaluating the code against the target's actual rules. `dismiss_stale_approvals` — the obvious fix — was tested directly and does **not** help; it only reacts to new commits, not to the base branch changing. This means the bot-approver-team mitigation above is necessary but not sufficient on its own: `policy-eval` (D1) must independently re-verify at evaluation time that every counted approval was given against the PR's *current* base, rather than trusting Gitea's stored `official` flag. Hard Milestone 3 requirement, not optional hardening.

### D3 — Everything keys to the scanned commit SHA

Statuses, comments, findings, cache entries. Discard any result whose SHA is no longer the PR head. Cancel in-flight pipelines for superseded SHAs.

No shared temp-JSON directory on local disk — it collides under concurrency, dies with the container, and makes the sidecar stateful for no benefit. Artifacts stay in the pipeline or go to object storage keyed by SHA.

### D4 — Fail closed, and reconcile

No terminal status ⇒ not mergeable. Never write `success` on an error path. Every scanner has a hard timeout; timeout ⇒ blocked, never ⇒ pass.

But failing closed produces **stuck PRs with no explanation**, and stuck PRs are how gates get switched off. Gitea does **not** auto-retry a webhook whose delivery attempt failed — it sits in the delivery history awaiting *manual* redelivery. Ten minutes of downtime loses those events outright.

So: a **reconciliation loop** in the sidecar. Every 60s, poll Gitea for open PRs whose head SHA has no terminal status, and **re-trigger the pipeline through Woodpecker's API**. ~50 lines. It makes webhook delivery best-effort rather than load-bearing. Alert on queue depth — a rising queue is the early warning.

Be precise about what this means for the sidecar: it sits outside the **integrity** path (it cannot make a red PR green — only the pipeline can produce a passing verdict) but inside the **availability** path (it is what un-sticks PRs whose events were lost). That recovery duty is exactly why it must stay stateless and boring.

### D5 — The gate decision never depends on a heavyweight service

Block/allow is computable from **scanner output + policy + local state** alone. DefectDojo and Dependency-Track receive results *after* the decision, asynchronously. DefectDojo's import is async and its dedup is slow; putting it in the merge path means DefectDojo downtime stops the org from shipping.

**Exception materialization — the hole this rule opened.** Approved risk acceptances live in DefectDojo, but the gate may not read DefectDojo. As previously drafted, the gate literally could not honour an approved exception. The fix: on two-party sign-off, the sidecar commits a **signed record** — `{repo, finding fingerprint, severity, expiry, both approvers, ticket}` — to a dedicated `exceptions` repo on the same Gitea instance. `policy-eval` clones that repo (tiny, fast) and honours unexpired records. The DefectDojo Risk Acceptance remains the **register** (reporting, quarterly §3 review, ISO evidence); the git record is the **enforcement artifact**. Gitea being down stops all merging anyway, so this adds no new availability dependency. And expiry needs no enforcement hook at all: an expired record simply stops matching, and the next evaluation blocks again. The pre-deploy gate reads the same store.

The same repo carries each project's **baseline** (see *Policy model*) — the gate's entire local state is one small, signed, versioned git repo.

### D6 — The platform patches itself first

The design makes Gitea the arbiter of identity, branch protection, and audit record. Gitea's [June 2026 security release](https://hivesecurity.gitlab.io/blog/gitea-forgejo-nine-cves-1263-security-release-2026/) (fixed in 1.26.4) carried nine CVEs, several of which hit this design directly — `REVERSE_PROXY_TRUSTED_PROXIES` wildcard admin bypass, a branch-protection cache bug, webhook SSRF against cloud metadata, and a fork-endpoint authz gap.

**Superseded by direct research, 2026-08-23** — full detail in [PINNED_VERSIONS.md](PINNED_VERSIONS.md#gitea-cve-posture-as-of-2026-08-23), pulled from the GitHub Security Advisories API (84 advisories, not a summary). The June batch above was real, but it is one round in an ongoing pattern: **latest stable is now `1.27.2`**, and a further seven advisories published the day after it shipped. Two of them are not just "patch and move on" — they land on decisions already made:

- **CVE-2026-58439** — a *second, independent* approval-bypass (stale `official` flag survives PR retargeting), distinct from the status-forgery bypass in [SADR-0001](adr/0001-commit-status-forgery.md). Not yet tested; tracked as SADR-0003, required before Milestone 3 trusts `required_approvals`.
- **CVE-2026-73278 / CVE-2026-73535** — WebAuthn/TOTP bypassed on the OAuth2/OIDC sign-in path. Directly relevant to D10 and the Entra ID SSO integration-menu recommendation — verify 2FA enforcement on the SSO path specifically before trusting it for Security-Officer/Eng-Lead identity.

Two RCEs (markup-renderer argv injection; unauthenticated file read via `go-org`) both require an optional external markup renderer to be configured — mitigated entirely by **never enabling `[markup.*]` external renderers**, which this design has no reason to turn on.

Therefore:
- Pin Gitea **exactly `1.27.2`** (not a floating tag, not the June-era `1.26.4` floor — see PINNED_VERSIONS.md for why that pin is now stale). Set `REVERSE_PROXY_TRUSTED_PROXIES` explicitly. Enforce IMDSv2 on the host. Leave `[markup.*]` external renderers disabled.
- A **self-patch SLA tighter than the one imposed on product teams** — framework §2.4's 24–48h Critical applies to the platform first. 84 advisories in four months is the argument for why this SLA is not decorative.
- **Dogfood.** The platform's own Compose/IaC passes Checkov. Its images are pinned **by digest**, not tag, and Trivy-scanned on a schedule. Its own repo is onboarded to its own gates. If it cannot pass its own gates, nothing else should be asked to.
- Add a **RACI row for platform self-maintenance** — the framework has none.
- The `gh api repos/<owner>/<repo>/security-advisories` pattern used to produce this section is a **standing operational query**, not a one-time Milestone-0 lookup — re-run it before every version bump.

### D7 — Detecting a secret is not remediating it

By the time Gitleaks fires in CI, the credential is in Gitea's object store, the webhook payload, Woodpecker's build log, the DefectDojo finding body, and every clone taken since. Blocking the PR changes none of that. Framework §2.5 mandates rotation "immediately on suspected compromise"; nothing in the earlier draft triggered it.

- First-line detection moves to a **server-side pre-receive hook** so the objects never land. The hook honours suppressions **only as they exist on the default branch — never from the incoming ref**, which the pusher controls; otherwise a real secret ships alongside its own allowlist entry. False-positive path: land the suppression PR first (it contains no secret, so it passes), then push. Two steps, deliberately. Scope the hook to the incoming objects only, and budget its push latency.
- A true positive **automatically opens an incident** and pages the credential owner with a rotation runbook (framework §5.3).
- Build logs and finding bodies are treated as secret-bearing: access-controlled, retention-capped, redacted on ingest.
- Track **mean time to revoke** alongside framework §6's mean time to remediate.

### D8 — Record the divergences as SADRs

Framework §3.4 names **GitHub Actions** as the reference CI configuration. Choosing Woodpecker silently contradicts the governing document and will surface as an audit finding. Write it up as a SADR under `/docs/adr/` per framework §1.2 and update §3.4. Same for any other deliberate divergence.

### D10 — The SSDLC Portal: one front door, no security state

Developers get a custom web portal (login via **Gitea OAuth SSO**, roles read live from Gitea teams): dashboard of my PRs + gate states + attention queue, PR security report with explain trace, **report export** (per-PR / per-release / ISO evidence pack, signed HTML/PDF), exception request + two-party approval queue, repo onboarding wizard, posture (embedded Grafana), admin health (doctor as endpoint).

Rules that keep it buildable and honest:
- **The portal owns no security state.** Thin aggregation over Gitea, Woodpecker, DefectDojo, exceptions-repo and Prometheus APIs. Gitea stays the system of record; the platform is fully functional from Milestone 3 without the portal.
- Its only writes (rescan, exception, onboard) go through the **same audited sidecar paths** as the comment commands, authorised by the caller's own Gitea teams.
- **No git browsing, no diff viewer, no code hosting** — those links open Gitea. We aggregate; we never re-implement the forge.
- Stack: one Go service (reuses the sidecar's API clients) + server-rendered HTML/HTMX. One binary, no SPA build chain.
- Milestone 8, ~4 weeks, can run in parallel after M4. Full UI mockups live in the review pack artifact.

### D9 — Reuse before building

Six mature projects cover work the earlier draft assigned to custom code. See *Component inventory*.

---

## Architecture

```
  Developer machine
  └─ pre-commit  (Gitleaks, Semgrep, ruff/eslint/gosec)
         │  convenience only — `--no-verify` defeats it, see m-notes
         ▼
  ┌──────────────────────────────────────────────────────────────┐
  │ Gitea  (≥1.26.4, pinned by digest)                           │
  │  · pre-receive hook: secrets never land                      │
  │  · branch protection: bot-only approver team + status check  │
  │  · dismiss stale approvals on push                           │
  │  · push mirror ──────────────────────────────► GitHub        │
  └───────┬──────────────────────────────────────────────────────┘
          │ native OAuth2 webhook (Woodpecker registers its own)
          ▼
  ┌──────────────────────────────────────────────────────────────┐
  │ Woodpecker CI          — reports commit status NATIVELY      │
  │                                                              │
  │  FAST  (PR open/sync — target < 3 min)                       │
  │   1  Gitleaks       secrets              ~5s                 │
  │   2  Semgrep        SAST, vendored rules ~30s                │
  │   3  Trivy          SCA, differential    ~20s                │
  │   4  Checkov        IaC, if manifests    ~15s                │
  │   5  normalise ──►  one schema, per-tool adapters            │
  │   6  policy-eval ►  Rego + baseline + exceptions repo        │
  │                     — THE EXIT CODE IS THE GATE              │
  │   7  reviewdog   ►  inline PR comments, deduped              │
  │                                                              │
  │  DEEP  (on merge to main — latency irrelevant)               │
  │   Semgrep full ruleset · Syft SBOM · Trivy image scan        │
  │                                                              │
  │  PRE-DEPLOY  (release gate — framework §3.2)                 │
  │   final Trivy · Cosign sign+attest · active-exception check  │
  │   ⇢ blocks if Critical with no active exception              │
  │                                                              │
  │  SCHEDULED  ZAP baseline vs staging (warn) · Renovate        │
  └───────┬──────────────────────────────────────────────────────┘
          │ async POST — never in the merge path
          ▼
  ┌──────────────────────────────────────────────────────────────┐
  │ Reporting sidecar  (custom, NOT in the trust path)           │
  │   · sticky PR comment  · DefectDojo push  · metrics          │
  │   · /rescan and /exception commands                          │
  │   · reconciliation loop (D4)                                 │
  └───────┬──────────────────────────────────────────────────────┘
    ┌─────┴──────────────┬─────────────────┬──────────────────┐
    ▼                    ▼                 ▼                  ▼
 DefectDojo        Dependency-Track   Prometheus          SIEM
 findings +        continuous CVE     gate metrics        append-only
 risk register     watch on shipped   + SLAs              evidence
 (§3)              SBOMs (§5.2)                           (§5.1)
```

**MANAGE layer:** Falco → Falcosidekick, Trivy Operator, Nginx + OWASP CRS WAF, all shipping to Prometheus/Loki/SIEM.

### Closing the MANAGE → DESIGN loop

Both diagrams draw this loop. Nothing in the earlier draft implemented it: Falco fires on a container — *which repo? which team? which PR introduced it?* Unanswered, so the loop stays a dotted line and a manual triage step that never happens.

The mechanism is provenance, which the design already half-has:

```
image digest → Cosign attestation → build → repo → team
                                              ↑
                                    service ownership catalog
                                    (a YAML file in git — do
                                     not buy Backstage for this)
```

Falco → Falcosidekick → alert enriched with repo/team → **auto-created Gitea issue in the owning repo**. Trivy Operator finds a new CVE on a running image → that service's next release blocks at the pre-deploy gate.

---

## Component inventory

### Reuse — replaces planned custom code

| Project | License | Replaces | Why |
|---|---|---|---|
| **[reviewdog](https://github.com/reviewdog/reviewdog)** | MIT | Hand-written PR comment posting | Native **Gitea reporter**. Inline line-level comments, multiline ranges, and **skips already-posted comments**. Ingests SARIF and arbitrary errorformat. Removes the largest, fiddliest chunk of custom code. No code-suggestion support on Gitea (GitHub-only). |
| **[Renovate](https://github.com/renovatebot/renovate)** | AGPL-3.0 | Dependabot | **Dependabot cannot work with Gitea** — it is a GitHub-hosted service. Renovate self-hosts as a container/cron job, 90+ package managers, first-class Gitea driver. Framework §2.4 requires automated dependency-update PRs; this is the only OSS way to get them here. Renovate PRs pass the same gates and **still require a human review** — automerge, even for patch bumps, only via a recorded SADR. Its PAT is scoped and is its own identity, not the gate bot's. |
| **[OWASP Dependency-Track](https://github.com/DependencyTrack/dependency-track)** | Apache-2.0 | *absent from earlier draft* | Named in the pipeline diagram. Ingests CycloneDX and **continuously re-analyses the whole portfolio** as new CVEs land — catches vulnerabilities in already-shipped code, which build-time scanning never sees. Framework §5.2. Complementary to DefectDojo, not redundant: Dependency-Track owns *"which running things are affected by today's CVE"*, DefectDojo owns *"what did the scanners say about this PR"*. |
| **[Conftest / OPA](https://github.com/open-policy-agent/conftest)** | Apache-2.0 | Bespoke YAML policy interpreter | Rego is a real policy language with real tooling and unit tests. Writing a YAML mini-language and its evaluator is a classic avoidable mistake. |
| **[DefectDojo](https://github.com/DefectDojo/django-DefectDojo)** | BSD-3 | Hand-rolled severity normalisation *and* risk register | Native parsers for 200+ tools with normalised severity and a dedup engine. Its **Risk Acceptance** object has a first-class expiration date and reactivates findings on expiry — very nearly framework §3 as specified. |
| **[gitea-mq](https://github.com/Mic92/gitea-mq)** | — | Trusting developers not to forge status | Merge queue: the bot becomes the only identity that can write `main` (D2). |

### Reuse — core stack

Gitea (MIT) · Woodpecker CI (Apache-2.0) · [Semgrep OSS](https://github.com/semgrep/semgrep) (LGPL-2.1) · [Gitleaks](https://github.com/gitleaks/gitleaks) (MIT) · [Trivy](https://github.com/aquasecurity/trivy) (Apache-2.0) · [Checkov](https://github.com/bridgecrewio/checkov) (Apache-2.0) · [Syft](https://github.com/anchore/syft) (Apache-2.0) · [OWASP ZAP](https://github.com/zaproxy/zaproxy) (Apache-2.0) · [Cosign](https://github.com/sigstore/cosign) (Apache-2.0) · [Kyverno](https://github.com/kyverno/kyverno) (Apache-2.0) · [Falco](https://github.com/falcosecurity/falco) + Falcosidekick (Apache-2.0) · [pre-commit](https://github.com/pre-commit/pre-commit) (MIT) · Prometheus · Grafana · Loki

### Reuse — worth adding

| Project | Why |
|---|---|
| **[OpenSSF Scorecard](https://github.com/ossf/scorecard)** | Scores each repo's posture (branch protection, pinned deps, signed releases). Cheap, high-signal DESIGN-phase KPI. |
| **[Trivy Operator](https://github.com/aquasecurity/trivy-operator)** | In-cluster continuous rescanning — framework §5.2. |
| **[Continue](https://github.com/continuedev/continue) + [Ollama](https://github.com/ollama/ollama)** | The DEVELOP diagram says "Continue + Ollama" and framework §4 **mandates internal-first LLM review** before code reaches any external AI assistant. Omitted from the earlier draft; now first-class. |
| **[OWASP CRS](https://github.com/coreruleset/coreruleset)** | WAF ruleset for MANAGE. |
| **[Opengrep](https://github.com/opengrep/opengrep)** | Adopted sooner than planned, for its **rules** specifically — SADR-0020 found live that Semgrep's own Registry rules cannot legally be vendored into this repo (Semgrep Rules License prohibits redistribution/service use). `policy/vendored-rules/` sources from `opengrep/opengrep-rules` (LGPL-2.1 + Commons Clause; internal-use reading not yet reviewed by counsel — see that directory's README). The Semgrep OSS **engine** is still what runs the fast gate; only the rule *source* changed. |
| **[Nuclei](https://github.com/projectdiscovery/nuclei)** | Fast, template-based CVE/misconfiguration scanning — genuinely complementary to ZAP's deeper crawl-based active/passive scanning, not a duplicate of it. External research (2026-08-24) independently converged on the same DAST pairing. Add alongside ZAP in the scheduled DAST leg, not the fast gate. |

### External integrations (menu — see review pack §11)

The gate is tool-agnostic: any SARIF/JSON emitter = one adapter; anything DefectDojo parses joins reporting free. Recommended order:

1. **Entra ID / AD → Gitea OIDC** (free) — answers the identity question; approver teams become AD groups
2. **MS Teams webhooks** (free) — exception/incident notifications; makes the 1-day SLA real
3. **Harbor** (free OSS) — registry proxy-cache + quarantine-until-scanned; closes a real gap
4. **Semgrep Pro *or* SonarQube Developer** (licensed) — the cross-file-taint gap; pick one. Sonar has no Gitea PR decoration — run it in the deep gate, surface via our portal report + DefectDojo parser
5. **GHAS on the GitHub mirror** (licensed) — the legal path to CodeQL + GitHub secret scanning as a second net
6. **Burp Enterprise / Snyk** (licensed) — only on *measured* ZAP/Trivy pain

Licensing caution: copies must be licensed — unlicensed software inside the ISO-evidence platform is a self-defeating audit finding.

### Dropped

- **SonarQube.** Community Build cannot do branch analysis or PR decoration (Developer Edition features) — it can never contribute to PR feedback. It is simultaneously the heaviest component (~4 GB, JVM + embedded Elasticsearch, needs `vm.max_map_count` tuning) and substantially duplicated by Semgrep + DefectDojo. Framework §3.2 lists it for deep SAST; Semgrep with a fuller ruleset on the deep tier covers that for a first cut. Revisit later if the gap proves real.
- **CodeQL.** Licence permits open-source code or GitHub Advanced Security. A self-hosted private-repo pipeline is neither. Off the table on licensing, not technical grounds.
- **OSV-Scanner as a second SCA.** Trivy is the single SCA source of truth for the gate. Dual-scanning at PR time doubles latency and yields two differently-severitied findings for the same CVE.
- **Snyk.** Commercial; duplicates Trivy. Revisit only if reachability analysis proves necessary to cut false positives.

### Build — the irreducible custom part

1. **`policy-eval` step** — small container: read normalised findings, evaluate Rego, exit non-zero. In the trust path, so it stays tiny and heavily tested.
2. **Per-tool normalise adapters** — see below.
3. **Reporting sidecar** (Go, stateless, ~1,200 lines) — sticky comment, DefectDojo push, metrics, exception commands, reconciliation loop. Not in the trust path.

---

## The severity normalisation contract

The fragile part is **not** SARIF parsing — Semgrep, Trivy, Gitleaks and Checkov all emit SARIF. The trap is that **SARIF `level` has exactly four values**: `error`, `warning`, `note`, `none`. No Critical. No High. No CVSS.

Policy says "block on Critical/High." Recovering that means reading tool-specific extensions — `rule.properties["security-severity"]` as a numeric string, or vendor `properties.severity` — and every tool populates them differently or not at all. **A tool upgrade that changes its severity mapping silently changes what the gate blocks.**

So:

- One tiny internal schema: `{tool, rule_id, severity, file, line, cvss?, fingerprint}`.
- One **per-tool adapter**, each independently tested against recorded fixtures.
- Scanner versions **pinned**. The severity mapping is a **versioned artifact**; changing it is a policy change and goes through review.
- Import to DefectDojo **per-tool with its native parser**, never as a merged SARIF blob — DefectDojo validates the tool name inside the SARIF against the Test Type and [will not combine different tools into one Test](https://docs.defectdojo.com/supported_tools/parsers/file/sarif/). Design one Test per tool per engagement from the start; retrofitting is painful.

---

## Policy model

Rego under `policy/`, evaluated by Conftest, versioned in Git, unit-tested. Thresholds encode framework §2.4 verbatim:

| Severity | Action | Window | Exception |
|---|---|---|---|
| Critical (9.0–10.0) | Block immediately | 24–48 h | ≤ 90 days, two-party |
| High (7.0–8.9) | Fail build | 7–14 days | Time-boxed, two-party |
| Medium (4.0–6.9) | Warn, track | 30 days | n/a |
| Low (0.1–3.9) | Log only | Routine | n/a |
| **Verified secret** | **Block + auto-incident + rotate** | Immediate | **Never** |

### Two distinct mechanisms — do not conflate

- **False positive** → rule-level suppression in `.ssdlc/suppressions.yaml`, with rule ID, justification, owner, expiry. Committed as a PR to the policy repo. **Fast path, no risk acceptance.**
- **Real finding, accepted** → DefectDojo Risk Acceptance with hard expiry and both approvers.

Making developers file a risk acceptance for a false positive poisons the risk register and every metric computed from it.

### Onboarding legacy repos: gate on *introduced* findings

The rollout killer neither earlier review caught. Onboard a real repo carrying 50 pre-existing Highs and every PR is blocked from day one — for problems the PR did not cause. Developers will not distinguish *"the gate found my bug"* from *"the gate is punishing me for history."* They will route around it, and the platform dies in its first week of contact with a real team.

So the PR gate is **differential**:

- **Onboarding writes a baseline**: a full scan records the fingerprints of every finding already on the default branch. Baseline findings go to DefectDojo with owners and §2.4 SLA clocks — they are **debt to burn down, not PR blockers**.
- `policy-eval` blocks only on findings **not in the baseline**. reviewdog's `filter-mode=added` gives the same behaviour for inline comments; dependency findings compare against a base-branch scan.
- **Secrets are never baselined.** A secret blocks wherever and whenever it appears.
- The baseline lives in the exceptions repo (D5), versioned and signed, and it **only shrinks** — a fixed finding leaves the baseline and cannot silently return.
- Baseline burn-down is a per-repo Grafana panel. *That* — visible debt with SLA clocks, reviewed quarterly — is the pressure on legacy findings. The PR gate's job is only to stop the hole getting deeper.

### Rule-set updates

Semgrep rules are **vendored into the policy repo**, versioned, updated by PR. Pulling live from the Registry at scan time means an upstream rule change can block every PR in the estate with no review and no rollback. Trivy's DB is cached on a volume with a scheduled refresh; CI runs `--skip-db-update` so a bad upstream DB cannot break every build at once. **Rule changes go through the same gate as code.**

---

## Exception flow — two-party, per framework §3

Framework §3 requires **joint sign-off from the Security Officer *and* the accountable Engineering Lead**, hard expiry ≤ 90 days, expired exceptions blocking the next release with no silent auto-renewal, and quarterly reporting. An earlier draft dropped the second signature.

```
Gate blocks PR (Critical)
   │
   ├─ fix ──► push ──► rescan ──► green ──► merge
   │
   └─ /exception <finding-id> <justification>
          │  sidecar opens DefectDojo Risk Acceptance (pending)
          │  notifies security-officers AND the repo's eng-lead
          ▼
      BOTH must run  /approve-exception <id> <days>
          ├─ verify each approver ∈ their Gitea team   (API, server-side)
          ├─ verify approver ≠ PR author               (no self-approval)
          ├─ verify the approvers are two DISTINCT humans
          │     (in a small org one person may sit in both teams)
          ├─ verify days ≤ policy max for that severity
          ▼
      · Risk Acceptance activated in DefectDojo      (the register)
      · signed record committed to exceptions repo   (the enforcement
        artifact — D5)
      · appended to append-only audit log
      · sidecar re-triggers the pipeline ⇒ policy-eval reads the
        record ⇒ gate passes ⇒ merge unblocked
      · sticky comment: "Exception active until YYYY-MM-DD"
```

**Approval is synchronously blocking a developer.** A CISO on leave is a merge freeze. So: a documented **approval SLA** (1 business day), a named fallback approver, and a tracked metric for *developer hours lost waiting on exceptions* — that number, not the security metrics, determines whether the gate survives contact with the org.

**Expiry:** enforcement is automatic — an expired record in the exceptions repo simply stops matching, and the next evaluation blocks again (D5). The scheduled job handles the bookkeeping: reactivate the DefectDojo finding, mark the repo release-ineligible, notify owners, feed the quarterly §3 report. Without the job the *register* rots; the *gate* re-arms regardless.

Peer review remains independently required — an exception unblocks the *security* gate, never the *review* gate.

---

## Deployment profiles

The earlier "$50/mo VPS" claim was wrong. Honest numbers, SonarQube dropped:

| Profile | Contents | Resources | Cost |
|---|---|---|---|
| **minimal** | Gitea · Woodpecker (server + agent) · Postgres · policy-eval · sidecar · Prometheus · Grafana | **~4 GB / 2 vCPU** | genuinely cheap; runs on a laptop |
| **full** | + DefectDojo (django + nginx + celery worker + beat + postgres + redis, ~4 GB) · Dependency-Track · Renovate · Loki · Ollama | **~12–16 GB / 4–8 vCPU, 300 GB** | €30–50/mo Hetzner-class; $100–160/mo DO/Linode/AWS |
| **kubernetes** | + Kyverno · Falco · Trivy Operator · ingress WAF | cluster | — |

Budget separately: Trivy DB + image-layer cache (20–50 GB disk), Woodpecker build containers (2–4 GB peak), ZAP (2 GB while running — schedule it on an ephemeral runner rather than resident capacity).

`minimal` is the default and must work flawlessly: full gating, inline comments, metrics.

**Scaling.** 100 PRs/day ≈ 1 concurrent build — comfortable on one 16 GB box with 2 agents. 1,000/day ≈ 10–20 concurrent at peak: you become **disk-I/O bound on Trivy DB updates and image pulls** long before CPU, so add a shared layer cache and a persistent Trivy DB volume first. At that volume the bottleneck is DefectDojo import and dedup — another reason it must not be synchronous (D5). Move to Kubernetes when you need HA or a second agent host, **not before**; migrating while the policy model is still changing means debugging two things at once.

**CI runner privilege.** The agent mounts the Docker socket — a container escape from any build step compromises the CI host and every secret on it. Run rootless or on a separate agent VM. Forks are disabled org-wide (D2), which removes the fork-pipeline secret-exposure class entirely; if that ever changes, fork-triggered pipelines get their own unprivileged agent and no secrets.

**The platform's own secrets.** An earlier draft quietly dropped Vault, leaving bot tokens, OAuth secrets and API keys in Compose `.env` files — while the platform enforces §2.5 on everyone else. `minimal`: [sops](https://github.com/getsops/sops) + age-encrypted files, decrypted at deploy time by Ansible. `full`: add Vault, or keep sops on a single box. Token scoping matters more than the vault: reviewdog's token can only comment; Renovate's is its own identity; the **gate bot's token is the crown jewel** — it approves and merges, it lives only in the sidecar, and it never enters a build container.

**Ollama sizing.** A useful code-review model (7B-class, quantised) wants 6–8 GB by itself. On a 16 GB `full` box that collides with DefectDojo's 4 GB. Either give Ollama its own host (any workstation with spare RAM works — Continue points at a URL) or accept a small model and say so in `AI_ASSISTANT_POLICY.md`.

**Semgrep OSS** has no cross-file/interfile taint analysis (Pro feature). The SAST is sophisticated pattern matching, not dataflow. Set expectations accordingly.

---

## Repository structure

```
SSDLC-Platform-Lab/
├─ README.md
├─ docs/
│   ├─ ARCHITECTURE.md          components, data flow, trust boundaries
│   ├─ GATE_CONTRACT.md         invariants + how each is tested
│   ├─ POLICY.md                writing and testing Rego
│   ├─ EXCEPTIONS.md            §3 two-party workflow, SLA, expiry
│   ├─ ONBOARDING.md            adding a repo to the lab
│   ├─ DEVELOPER_SETUP.md       pre-commit, IDE, Continue/Ollama
│   ├─ AI_ASSISTANT_POLICY.md   framework §4 internal-first review
│   ├─ OPERATIONS.md            upgrades, restore drills, rule updates
│   ├─ SELF_DEFENCE.md          D6 — the platform's own patch SLA
│   ├─ COMPLIANCE.md            ISO 27001 evidence map + export
│   ├─ THREAT_MODEL.md          threat model of the lab itself
│   └─ adr/                     SADRs, incl. Woodpecker-vs-§3.4 (D8)
│
├─ policy/                      *.rego + *_test.rego  (versioned, reviewed)
├─ policy-eval/                 the in-trust-path step — small, hard-tested
├─ normalise/                   per-tool adapters + testdata/ fixtures
├─ sidecar/                     Go: comments, DefectDojo, metrics,
│                               exceptions, reconciliation loop
│
├─ compose/                     compose.{minimal,full}.yaml + services/
├─ ansible/                     playbooks 00-base … 50-runtime, roles/
├─ terraform/                   hosts, networks, volumes, K8s
├─ kubernetes/                  Kyverno, Falco, Trivy Operator
│
│  (the `exceptions` repo — signed exception records + per-repo
│   baselines — is a SEPARATE repo on the Gitea instance itself,
│   created by onboard-repo; it is state, not source)
│
├─ pipelines/                   fast · deep · pre-deploy templates
├─ templates/                   paved-road scaffolding (§1.3):
│                               adr/, threat-model.md, .pre-commit-config
├─ ownership/                   service → repo → team catalog (M9 loop)
├─ examples/                    deliberately vulnerable repos per language
└─ scripts/                     quickstart · onboard-repo · e2e ·
                                backup · restore-drill · evidence-export
```

---

## Roadmap

**Two weeks of design rework first** — settle D2 (verify status forgery on the real Gitea), D1 (pipeline-step gate), D6 (self-defence baseline), D7 (revocation path). These change what gets built in week one; retrofitting any of them is expensive.

| Milestone | Weeks | Outcome |
|---|---|---|
| **0 · Design rework** | 2 | D1/D2/D6/D7 settled. Forgery test run. SADRs written. |
| **1 · Forge + CI** | 2 | `minimal` up, Gitea ≥1.26.4 hardened, OAuth wired, test repo building. `quickstart.sh` is the acceptance test. |
| **2 · Fast gate, comments only** | 2 | Gitleaks + Semgrep + Trivy + normalise adapters. reviewdog inline comments. **No blocking** — measure the false-positive rate first. Enforcing before you know that number is how teams learn to hate the tool. |
| **3 · Enforcement** | 3 | policy-eval step, Rego + tests, **baseline/differential gating**, native Woodpecker status, bot-approver team, reconciliation loop, sticky comment. Branch protection on — **example/pilot repos only** until M4: blocking real teams with no §3 escape hatch violates the framework and burns the goodwill the rollout depends on. **This milestone is the project.** |
| **4 · Exceptions + register** | 2 | DefectDojo, two-party Risk Acceptance, **materialization to the exceptions repo (D5)**, team-membership auth, expiry job, append-only audit log. Enforcement may now widen beyond pilots. |
| **5 · Deep + pre-deploy + supply chain** | 2 | Deep pipeline, Syft SBOM, Dependency-Track, Cosign, pre-deploy gate, GitHub mirror, Renovate. |
| **6 · Deploy + runtime** | 2 | Checkov/Trivy IaC gates, Kyverno, Falco + Falcosidekick, ownership catalog, WAF, ZAP baseline. |
| **7 · Evidence + polish** | 2 | Grafana §6 dashboards, ISO evidence export, example repos, docs, **restore drill**. |
| **8 · Portal v1 (D10)** | 4 | SSO, dashboard, PR security report, report export, exception queue, onboarding wizard, admin health. Parallelisable after M4. |

**~15 weeks part-time**, of which Milestones 0–3 (~9 weeks) is a genuinely useful MVP: every PR scanned, findings inline, merges blocked on Critical, no forgeable gate.

This sequences **inside** the framework §8 programme roadmap (Months 1–3 foundation, 4–6 automation, 7+ operational excellence). The framework's roadmap governs the programme; this one governs the tooling.

**Ongoing cost: 0.25–0.5 FTE standing.** Rule curation and false-positive tuning is by far the largest line item, then CVE-database freshness, quarterly platform upgrades, and the framework's own quarterly reviews (§3 exceptions, §4 approved-AI-tool list, §2.5 credential audit). *A gate nobody tunes becomes a gate everybody bypasses.*

---

## Metrics

Framework §6 mapped to concrete series:

| Framework metric | Series |
|---|---|
| MTTR Critical/High | `ssdlc_finding_remediation_seconds` (histogram by severity) |
| % PRs passing first run | `ssdlc_gate_first_run_pass_total` / `ssdlc_gate_evaluations_total` |
| SBOM coverage | `ssdlc_sbom_coverage_ratio` (Dependency-Track project count) |
| Open exceptions by age | `ssdlc_risk_acceptance_open` (gauge, 30/60/90 d buckets) |
| DAST staging coverage | `ssdlc_dast_endpoints_scanned_ratio` |
| **Mean time to revoke** (D7) | `ssdlc_secret_revocation_seconds` |
| **Exception wait time** | `ssdlc_exception_decision_seconds` — the gate's survival metric |
| Gate health | `ssdlc_gate_errors_total`, `ssdlc_reconcile_queue_depth` |

OpenSSF Scorecard feeds the same dashboard.

---

## Evidence and durability

Framework §7 maps seven ISO 27001 controls to artifacts. None of it is automated in the earlier draft.

- **Scheduled evidence export job** producing the §7 artifact set.
- Export to an **append-only store outside the tools** — signed commits in a git evidence repo, or object storage with object-lock. Evidence must outlive any single component, and this also closes the gap where a compromised sidecar holding a DefectDojo API key could rewrite the audit history it is meant to be creating.
- Ship gate decisions to the **SIEM** framework §5.1 already requires.
- **Backups are not the problem — restores are.** Gitea's DB and its on-disk repo store must be snapshotted as a matched pair; Gitea schema migrations are one-way. DefectDojo Postgres loss costs the risk register, expiry dates and dedup history — §7 evidence for A.8.29 and A.8.30, gone. Run a **quarterly restore drill**, not just a backup job.

---

## Verification

**Invariant tests** — table-driven against recorded scanner fixtures in `normalise/testdata/`:

- scanner timeout ⇒ blocked, never `success`
- duplicate webhook delivery ⇒ one pipeline verdict per SHA, exactly one sticky comment
- push during scan ⇒ old SHA's verdict never lands on the new SHA
- **forged status with the gate context ⇒ merge still blocked** (D2 regression test)
- baseline finding ⇒ does not block; the same rule firing on a **new** line ⇒ blocks
- secret in a baselined repo ⇒ still blocks (secrets are never baselined)
- `/approve-exception` by PR author ⇒ rejected
- `/approve-exception` by one party only ⇒ still pending
- both approvals from the same human via two team memberships ⇒ rejected
- approved exception ⇒ materialized record unblocks the next run **with DefectDojo stopped** (D5 proof)
- expired record ⇒ next evaluation blocks again; pre-deploy gate blocks release
- sidecar killed mid-run ⇒ reconciliation loop recovers the PR within 60s of restart

**End-to-end** (`scripts/e2e.sh`, in CI):
1. bring up `minimal`, seed `examples/vulnerable-python`, onboard it — the onboarding scan writes its baseline
2. open PR with a **new** SQL injection + a planted secret (the repo's pre-existing findings must not block)
3. assert: pre-receive rejects the secret push; inline comments appear; sticky comment lists findings; status `failure`; merge disabled
4. push a fix; assert status flips; sticky comment **updated in place**, not duplicated
5. merge; assert deep pipeline runs, SBOM lands in Dependency-Track
6. assert a forged success status does not enable merge
7. tear down

**Self-hosting test.** The lab's own repo is onboarded to the lab. If it cannot pass its own gates, it is not ready.

---

## Open questions

1. ~~**D2 verification**~~ — **closed.** Confirmed by experiment: [SADR-0001](adr/0001-commit-status-forgery.md). Status forgery works; the identity-bound mitigations in D2 are mandatory from Milestone 3.
2. **GitHub mirror — narrowed, not fully closed.** External research (2026-08-24) recommends a **GitHub App over a PAT or SSH deploy key** for the mirror credential — short-lived installation tokens, fine-grained permissions, a real audit trail, none of a PAT's user-tied ambiguity or a deploy key's coarser scope. Goes further: rather than pushing straight to GitHub `main`, push to `from-gitea/release-YYYY.MM.DD` and open a GitHub PR, so GitHub's own branch protection gets a second, independent say before anything lands — two layers of governance instead of one. Adopt this pattern for Milestone 5. Still open: public or private mirror.
3. **Ollama model + host** — framework §4's internal LLM review needs a model choice and RAM/GPU. Same box or separate?
4. **Identity** — Gitea local accounts, or LDAP/OIDC? Determines how "Security Officer" and "Engineering Lead" are established for two-party approval.
5. **First target repo** — which project onboards once Milestone 3 lands?

---

## Appendix — framework document errata

Found while cross-checking. All in `SSDLC_Framework(1).docx`, all cheap to fix:

- **Section numbering collides.** Numbered sections run 1–4, then unnumbered *Phase 1–5*, then restart at 5–9. Renumber, or prefix the phases.
- **§3 bullet 4** cites "the metrics review (Section 8)" — metrics is **§6**; §8 is the roadmap.
- **§2.3** cites "Section 6.1" for scan stratification — it is **§3.1**.
- **Final Recommendation** cites "the metrics in Section 7" — metrics is **§6**; §7 is ISO evidence.
- **§1 claims scope "from design through decommissioning"** but no phase covers decommissioning — credential revocation, SBOM retirement, data-destruction evidence. Worth a paragraph.
- **§2.4 and §5.2 name Dependabot and Snyk**, neither of which is usable here (Dependabot is GitHub-only; Snyk is commercial). Substitute Renovate and Trivy.
- **§3.2 gives pre-commit a Fail Policy of "Block local commit."** `git commit --no-verify` defeats it and it only runs if installed. It is a developer-experience feature, **not a control** — do not present it as one in ISO evidence. The enforceable equivalent is the pre-receive hook (D7).
- **`c:\applications\SDLC` is untracked in git.** The framework, the diagrams, and this plan should version together — framework §1.2 requires exactly that for architecture decisions.
