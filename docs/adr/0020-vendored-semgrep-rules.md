# SADR-0020: Vendored SAST rules, and a real licensing detour

**Status:** Accepted, implemented.

**Date:** 2026-08-27
**Milestone:** 3 — fast-gate/pipeline debt
**Security & Privacy Impact:** Medium (integrity of the SAST ruleset; also a genuine licensing
question, not purely technical)

## Question

`pipelines/fast.woodpecker.yml`'s `sast` step pulled `p/security-audit` and `p/secrets` live from
the Semgrep Registry on every scan. `DESIGN.md`'s own *Rule-set updates* guidance names the risk
plainly: "an upstream rule change can currently block every PR in the estate with no review and no
rollback." `docs/TODO.md` tracked vendoring the ruleset as outstanding fast-gate debt. How?

## What went wrong with the obvious answer

The obvious approach — download the resolved `p/security-audit` + `p/secrets` rule YAML (272 rules
combined, confirmed via a dry-run scan) and commit it — turned out not to be straightforwardly
possible, and then not legally available at all:

- Semgrep's CLI does not expose a clean way to export the resolved registry configuration as source
  YAML. `semgrep show dump-config` dumps the engine's internal OCaml AST representation, not
  something `--config` can consume. No local cache of the downloaded rule YAML survives a scan
  (checked directly: nothing under `~/.semgrep` or `~/.cache` after a real `--config=p/...` run).
  Fetching `https://semgrep.dev/p/security-audit` directly returns the marketing site's HTML, not
  raw YAML — registry resolution happens through Semgrep's own internal API, not a static file.
- More fundamentally: **Semgrep's Registry rules carry their own license**
  (`semgrep.dev/legal/rules-license`), fetched and read directly rather than assumed. It grants use
  "for your own internal business purposes" but states plainly: *"This license does not allow you
  to distribute the rules, or to make them available to others as a service."* This platform's
  entire purpose is scanning every onboarded team's own repos — copying the rule files into this
  repo falls squarely under that restriction. `DESIGN.md`'s own component inventory had already
  flagged this exact risk before this SADR existed: *"Opengrep — Continuity plan, given the Semgrep
  Registry's licensing history. Watch, don't adopt yet."* This SADR is that watch item resolving
  sooner than planned, forced by trying to actually do the vendoring DESIGN.md's roadmap called for.

## Decision

**Source:** `github.com/opengrep/opengrep-rules`, the official Opengrep project's own fork of
`semgrep-rules`, kept in Semgrep-compatible rule YAML syntax. Its engine repo (`opengrep/opengrep`)
is clean LGPL-2.1, actively maintained (pushed within the last 24 hours as of this research, ~3000
stars). The rules repo carries **LGPL-2.1 plus a "Commons Clause" restriction** — text confirmed
directly from the repo's own `LICENSE` file, not assumed from the engine's license. Commons Clause
blocks *selling* the software (providing it to third parties for a fee whose value derives
substantially from the software's own functionality) — it does not clearly restrict this platform's
actual use (scanning one organization's own onboarded repos, not offered externally). **This reading
has not been reviewed by counsel** and is recorded as such in `policy/vendored-rules/README.md`, not
presented as settled.

A cleanly MIT-licensed alternative exists (`AikidoSec/opengrep-rules`, zero licensing ambiguity) but
was passed over for this vendoring in favor of the official, more actively maintained, more
comprehensive set — a real tradeoff, decided by the platform owner, not assumed.

**Scope, deliberately bounded — not the full repository:**
- `secrets/` — from `generic/secrets/` (225 files), the closest equivalent to `p/secrets`.
- One directory per language (`python/`, `javascript/`, `typescript/`, `go/`, `java/`, `php/`,
  `ruby/`, `csharp/`, `bash/`, `c/`, `dockerfile/`) — each sourced from that language's own core
  `lang/security/` directory, **not** the hundreds of framework-specific directories
  (`python/django/security`, `javascript/angular/security`, and so on) `opengrep-rules` also carries.
  594 files total, ~4.8 MB, pinned to commit `f1d2b562b414783763fd02a6ed2736eaed622efa`.

**The engine did not change.** `pipelines/fast.woodpecker.yml`'s `sast` step still runs
`semgrep/semgrep:1.174.0` — only `--config=p/security-audit --config=p/secrets` became
`--config=policy/vendored-rules`. Semgrep OSS reads Opengrep-sourced rule YAML without issue (same
syntax, confirmed live). A full engine swap to Opengrep itself is a separate, larger decision, not
folded into this one.

## Getting it into onboarded repos: `onboard-repo.sh`'s `commit_directory`

`commit_file`'s one-Contents-API-call-per-file pattern (already used for `.woodpecker.yml`,
`policy-eval/`, `normalise/`, `policy/severity.rego`) does not scale to 594 files — that many
sequential HTTP round-trips per onboarding run. Added `commit_directory`: a real `git clone`, copy,
`git add`/`git commit`/`git push` over HTTP with the admin token, idempotent (an unchanged tree
produces an empty commit, treated as success). Live-tested through a real onboarded throwaway repo:
1298 files committed in one push (594 rule files plus each rule's own co-located test fixtures).

## A gap this creates, named rather than hidden

`bot-approver.py`'s `GATE_CONTRACT_ENFORCE` protected-base comparison (SADR-0017) is **not** extended
to `policy/vendored-rules/` — its mechanism is one Contents-API blob-SHA request per configured path,
and doing that 594 times per PR evaluation, every poll cycle, does not scale. A PR could currently
weaken the vendored ruleset (delete a rule, add an always-pass rule) without the bot's file-tamper
check catching it. Flagged directly in `bot-approver.py`'s own `DEFAULT_GATE_MANAGED_PATHS` comment
and in `docs/TODO.md`, not silently left uncovered. The real fix is a tree-level comparison (one Git
Trees API call for the whole subtree's SHA, not 594 individual blob lookups) — not built here, since
it's a distinct piece of work from vendoring the rules themselves.

## Also found and fixed: lint scope

`tests/unit/lint-syntax.sh`'s Python-syntax check started recursing into
`policy/vendored-rules/`'s own co-located test fixtures (each rule ships small example snippets
demonstrating the vulnerable/safe pattern it targets) and failed on two of them
(`python/audit/sqli/aiopg-sqli.py`, `asyncpg-sqli.py`) — both use a bare `await` outside an async
function, deliberately, as isolated pattern-matching fixtures rather than complete runnable
programs. Excluded `policy/vendored-rules/` from the syntax lint, same treatment `node_modules/` and
`.terraform/` already get: not this project's code, not meant to independently parse.

## Verification

- Isolated: vendored ruleset loaded via `semgrep --config=policy/vendored-rules` inside a real
  `semgrep/semgrep:1.174.0` container — 317 rules ran cleanly (a few harmless future-syntax
  deprecation warnings, no errors); correctly detected a real AWS access-token pattern
  (`rules.secrets.gitleaks.aws-access-token`, marked blocking).
- Live, end to end: a throwaway repo onboarded with the updated `onboard-repo.sh`
  (`commit_directory` confirmed working, 1298 files in one push); a real PR carrying the same AWS
  key produced a `sast` step that completed successfully (config loaded and ran without error) and a
  `policy-eval-findings` step that correctly failed closed.
- Full `tests/unit/` tier and both lint scripts re-run clean after the `lint-syntax.sh` fix.

## Consequences

- `docs/TODO.md`'s vendoring item is closed, but with a licensing caveat that needs real legal review
  before this repository (or a product built on it) is used more broadly than an internal pilot, or
  made public — recorded in `policy/vendored-rules/README.md`, not silently assumed resolved.
- Coverage is narrower than the live `p/security-audit`+`p/secrets` pull was (594 curated files vs.
  272 resolved registry rules, different selection criteria — not a strict superset or subset).
  Acceptable for a pilot; framework-specific coverage (Django, Flask, Angular, etc.) can be added
  deliberately, per framework, once a real onboarded repo needs it.
- The new `bot-approver.py` gate-managed-paths gap (tree-level comparison not built) is real and
  tracked, not a regression introduced silently.
