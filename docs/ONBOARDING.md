# Onboarding a repository

This is a **platform-admin action**, not something a developer runs. After it completes, a
developer's entire interaction with the platform is: clone, code, push, read the result Gitea shows
on the commit or PR. `scripts/onboard-repo.sh`'s own header comment states this as the concrete
guarantee the script exists to provide — read it before changing the script, since every step in it
is there because skipping it breaks that guarantee for real (see the bug histories below).

## Before you onboard anything

**Baseline/differential gating now exists (SADR-0024/0025)** — step [3/6] below scans the repo's
current state and writes `.ssdlc/baseline.json` *before* any platform file is committed, so
pre-existing findings become tracked debt rather than PR blockers. Sprint 01's original restriction to
pilot repos with no pre-existing findings predated this and can be revisited — but the two-party
*exception* workflow for genuinely NEW findings is still absent (see [`EXCEPTIONS.md`](EXCEPTIONS.md)),
so a real Critical/High finding introduced by a PR still has no accepted-risk path other than fixing it
or a reviewed policy change.

**`scripts/bot-approver.py` must already be running, watching this repo, before or immediately after
onboarding.** Onboarding sets `required_approvals: 2` on branch protection. Nothing gives the bot's
half of those two approvals until `bot-approver.py` is running against the repo — raising
`required_approvals` without it makes every PR permanently unmergeable, not safer. See
[`GATE_CONTRACT.md`](GATE_CONTRACT.md) for what the bot actually checks before it votes.

## Prerequisites

Environment variables `onboard-repo.sh` requires (see `compose/minimal/.env` for the profile these
come from):

| Variable | What it is |
|---|---|
| `GITEA_URL` | e.g. `http://127.0.0.1:3500` |
| `WOODPECKER_URL` | e.g. `http://127.0.0.1:8000` (or your configured `WOODPECKER_PORT`) |
| `GITEA_ADMIN_TOKEN` | A Gitea API token with `write:repository`, `write:admin` — this is the platform-admin credential; it holds a standing push-whitelist bypass of PR review on every onboarded repo (see the security tradeoff noted in the script itself), so scope and protect it accordingly |
| `WOODPECKER_TOKEN` | A durable Woodpecker personal access token. **Not minted by this script** — it's a one-time interactive OAuth step (`GET /web-config.js` with a session cookie for a legitimately-issued CSRF token, then `POST /api/user/token` once). Do not reconstruct CSRF secrets from the database; see `docs/OPERATIONS.md`'s rules-learned section |

## Run it

```sh
./scripts/onboard-repo.sh <owner> <repo>
```

## What it actually does, in order

1. **Looks up the repo's Gitea-internal ID**, then activates it in Woodpecker (`POST
   /api/repos?forge_remote_id=...`). A `409` (already active) is treated as success, not an error —
   re-running onboarding against an already-onboarded repo is safe.
2. **Commits the fast-gate pipeline template and everything it depends on at runtime** —
   `.woodpecker.yml`, `policy-eval/verify-approvals.py`, `policy-eval/evaluate-findings.py`, all three
   `normalise/*_adapter.py` files, and `policy/severity.rego`. **This list matters**: an earlier
   version of this script committed only `.woodpecker.yml` itself, which silently left
   `approval-check`'s own dependency on `verify-approvals.py` unmet in every repo onboarded for real —
   masked for a long time because every live test manually copied the file in first. If you add a new
   scanner or policy dependency (see [`POLICY.md`](POLICY.md)'s "Adding a new scanner"), add its
   `commit_file` call here too, or it will work in this repo's own tests and silently fail on the next
   real onboarding.
3. **Adds the gate bot (`gate-bot` by default, `GITEA_BOT_USER` to override) as a write collaborator**
   — needed so it can hold the "bot" half of the two required approvals. This is a separate,
   narrower credential from the bot's own approve-only token (`GITEA_BOT_TOKEN`, held only by
   `bot-approver.py`'s process).
4. **Sets branch protection on `main`**:
   - `status_check_contexts: ["ssdlc/security-gate/**"]` — a **glob**, not an exact string.
     Woodpecker's real context string is `ssdlc/security-gate/<event>/<workflow>` (a push produces a
     different string than a PR); see DESIGN.md D1's correction.
   - `required_approvals: 2`, `dismiss_stale_approvals: true`.
   - `enable_push: true` + `enable_push_whitelist: true` with the platform automation account
     whitelisted. **Both flags together, not just the whitelist** — Gitea's actual model (found the
     hard way, live) is that `enable_push: false` blocks *everyone* regardless of whitelist; you need
     push allowed *and* restricted-to-whitelist together to get "only these accounts can push directly,
     everyone else goes through review." Without the whitelist, branch protection blocks the
     platform's own future re-runs of this script exactly as hard as it blocks a developer's direct
     push.

## After onboarding

The commit status a developer sees is `ssdlc/security-gate/<event>/<workflow>` — `push` or `pr`, not
`pull_request`, confirmed live and corrected in SADR-0004/SADR-0005. Every push runs Gitleaks, Semgrep,
and Trivy automatically; results appear as the commit status, with detail one click away in
Woodpecker.

**Verify the bot is actually voting.** Open a real PR against the onboarded repo and confirm, in
order: the pipeline runs and reports status; `bot-approver.py`'s logs show it evaluating the PR (not
skipping on a mismatched `GATE_CONTRACT_ENFORCE` comparison — see [`GATE_CONTRACT.md`](GATE_CONTRACT.md)
for what that check compares); the bot's approval lands once scanning steps are green; a human
approval plus the bot's approval together satisfy `required_approvals: 2` and the PR becomes
mergeable.

## Current limitations, stated honestly

- **Single-repo, manual process.** `bot-approver.py` takes one repo's identity via plain environment
  variables and has to be started by hand — it is not yet a `compose/minimal` service, and does not
  yet watch multiple onboarded repos from one process. See `docs/TODO.md`.
- **No pre-receive secret hook installed by this script.** SADR-0002 built and live-tested the
  mechanism (`compose/forgery-test/pre-receive-gitleaks.sh`), but it is not yet packaged as the
  Ansible role that chains it onto every onboarded repo's own pre-receive hook.
- **`GITEA_ADMIN_TOKEN` is a full admin credential** used for every onboarding action, including the
  push-whitelist grant. This is acceptable for Sprint 01's scope (this script *is* the platform
  admin's own tool) but is explicitly flagged in `docs/TODO.md` as needing a narrower-scoped
  automation identity before broad rollout.
- ~~No baseline write.~~ **Fixed (SADR-0024/0025).** Step [3/6] scans the repo's existing default-branch
  state before any platform file is committed and writes `.ssdlc/baseline.json`. Re-running onboarding
  refreshes (shrinks) it rather than starting over.

## Related

[`GATE_CONTRACT.md`](GATE_CONTRACT.md) for what the bot actually verifies before approving,
[`ARCHITECTURE.md`](ARCHITECTURE.md) for the full push-to-merge data flow, `docs/OPERATIONS.md` for
bringing up the profile this all runs against.
