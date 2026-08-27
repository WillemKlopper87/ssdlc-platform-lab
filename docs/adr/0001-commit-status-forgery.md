# SADR-0001: Commit-status forgery is real — branch protection cannot rest on status alone

**Status:** Confirmed by experiment; mitigation independently verified
**Date:** 2026-08-23
**Milestone:** 0 (design rework) — resolves the open question blocking D2
**Security & Privacy Impact:** Critical. This is the root-of-trust assumption for the entire gate.
**Gitea version tested:** Initial run on `1.26`; **reconfirmed on `1.27.2`** (the actual current
release as of this date — see [PINNED_VERSIONS.md](../PINNED_VERSIONS.md)). Identical result on
both. This is Gitea's permission-model design, not a patched bug — no version pin fixes it.

## Question

D2 in the design suspected that Gitea matches required status checks by *context name*, not by
*reporter identity* — meaning any repo collaborator with write access could post a status under the
gate's context (`ssdlc/security-gate`) and satisfy branch protection without ever running a scan.
This had to be verified against a real instance, not assumed.

## Method

A throwaway Gitea 1.26 instance (`compose/forgery-test/docker-compose.yml`, sqlite backend, torn
down after the test — nothing here persists) was provisioned with:

- `gateadmin` — admin account, used only for setup (repo creation, branch protection, verification)
- `alice` — an ordinary account added as a repo **collaborator with `write` permission** (exactly
  what any developer on a team has) and issued a personal access token scoped to **`write:repository`
  only** — no admin scope, no special role
- Repo `gateadmin/forge-target`, branch protection on `main`:
  `enable_status_check: true`, `status_check_contexts: ["ssdlc/security-gate"]`,
  `required_approvals: 0` (held at zero deliberately, to isolate the status-check variable from the
  approval-count variable)

Steps, each checked against the API, not inferred:

1. Alice pushes `feature/alice-change` and opens PR #1 against `main`.
2. **Baseline:** Alice attempts to merge. Result: `405 Not all required status checks successful`.
   Confirms protection is active and the context is genuinely unsatisfied.
3. Alice — using only her `write:repository` token — issues:
   ```
   POST /api/v1/repos/gateadmin/forge-target/statuses/<PR-head-SHA>
   {"state":"success","context":"ssdlc/security-gate","description":"forged by alice, not the gate"}
   ```
4. Combined status for that SHA flips to `success`, `creator: alice`.
5. Alice attempts to merge again.

(One false start: the first attempt used a SHA pulled from the wrong field in the PR-creation
response — `merge_base`, not `head.sha` — and correctly stayed blocked. That was an extraction bug
in the test script, not a finding. Re-run against the real head SHA below, verified independently
via `GET .../pulls/1` → `head.sha`.)

## Result

```
POST /statuses/f111e246f3a009fbda4d5edcdc8fe2ed93a2c7f7  → 201, creator: alice
GET  /commits/f111e24.../status                          → state: success, total_count: 1
POST /pulls/1/merge  {"Do":"merge"}                       → 200 OK

GET /pulls/1  → "merged": true, "merged_by": {"login": "alice"}
GET /commits?sha=main → main now contains "Merge pull request 'alice: add notes' (#1) ..."
```

**Confirmed.** A collaborator with ordinary write access, holding a token scoped to nothing more than
`write:repository`, forged the gate's exact status context and merged her own unreviewed,
unscanned PR straight past branch protection. No admin privilege, no bypass flag, no misconfiguration
beyond the default Gitea permission model — this is what *every* developer on a real team can do
today, on this Gitea version, against this exact protection config.

## Round 2 — does `required_approvals: 2` actually close this path?

D2 recommended the fix; it had not been tested. Same instance, same repo, `required_approvals`
raised from `0` to `2`, a third account (`bob`, ordinary `write` collaborator) added to serve as
the second human reviewer:

```
PR #2 opened by alice
Alice forges "ssdlc/security-gate" = success on the head SHA (same trick as round 1)   → 201
Alice attempts merge, 0 real approvals + forged status                                  → 405
  "Does not have enough approvals"
Alice attempts to approve her OWN pull                                                  → 422
  "approve your own pull is not allowed"                                    (Gitea-native, no
                                                                               custom code needed)
Bob approves (1 real approval)
Alice attempts merge, 1 real approval + forged status                                   → 405
  "Does not have enough approvals"
Gateadmin approves, playing the gate-bot role (2nd real approval)
Alice attempts merge, 2 real approvals + status                                         → 200 OK
```

**Confirmed both directions.** Forging the status is no longer sufficient by itself — Gitea tracks
approval count independently of status checks, and self-approval is rejected natively. The merge
only succeeds once two *distinct* accounts genuinely approve. This is exactly the shape D2's
mitigation needs; the only remaining implementation gap is making sure one of those two approvals
is bound to the gate's own automated verdict (the bot account) rather than being satisfiable by two
colluding humans — which is what "bot-only approver team, token held solely by the sidecar" (below)
closes.

## Decision

D2's identity-bound mitigations are **not optional hardening — they are mandatory from Milestone 3,
day one**, and item 2 below is now empirically verified to work, not merely proposed:

1. **Bot-only approver team.** A protected reviewer team whose sole member is the gate's own bot
   account; the token lives only in the sidecar. Gitea blocks self-approval, which status posting
   does not (verified above).
2. **`required_approvals: 2`** — the bot *and* at least one human. **Verified above**: this closes
   the exact path round 1 exploited. Round 1 ran at `0` deliberately, to isolate the status-check
   variable; round 2 proves `2` is sufficient and that Gitea enforces it independently of any status.
3. **Dismiss stale approvals on push**, or the bot-approval mitigation degrades to a push-after-approve
   race. *Not yet independently tested — flagged for a Milestone 3 regression test alongside the
   PR-retargeting bypass below.*
4. **Forks disabled org-wide** (already decided independently for other reasons — see DESIGN.md D2).
5. **gitea-mq merge queue**, or the ~100-line merge-when-green fallback in the sidecar, so the bot
   is the only identity that can ever write to `main` — verified by this same experiment methodology
   before it is trusted. `gitea-mq` has zero tagged releases as of this date (pushed 2026-08-20,
   actively developed, 27 stars, MIT) — pin by commit SHA if used, not by tag; re-assess before
   Milestone 3 commits to it as primary rather than the sidecar fallback.

Status-as-gate, alone, is not a control on Gitea's default permission model. It is now provably
theatre unless paired with an identity-bound approval requirement — and that requirement is now
provably effective, not just plausible. Milestone 3 does not ship without item 2 at minimum, and
both experiments above become permanent regression tests — see DESIGN.md's Verification section.

## A second, independent bypass — now confirmed live, and the inference below turned out wrong

While researching current Gitea advisories for [PINNED_VERSIONS.md](../PINNED_VERSIONS.md), a
**different** approval-bypass surfaced: **GHSA-w5pg-649r-p6gg / CVE-2026-58439**, "Branch Protection
Bypass via PR Retargeting Preserves Stale `official` Approval Flag" (published 2026-07-13,
`vulnerable_range: <= 1.26.4`, high severity). The mechanism: a PR approved while targeting a branch
with weak or no protection can be retargeted to a protected branch, and the approval's `official`
flag is not recalculated against the new target's rules — so a stale, never-actually-validated
approval can count toward `required_approvals` on `main`.

This is architecturally distinct from the status-forgery finding above (it defeats the *approval*
half of the control, not the *status* half). At the time this was written, it had not been tested,
and — reasoning from the disclosure-timing pattern seen across this advisory batch — it seemed
**likely fixed by `1.27.0` or `1.27.1`**, though no explicit "fixed in" statement was found in the
advisory body, so that was flagged explicitly as an inference, not a confirmation.

**The inference was wrong.** [SADR-0003](0003-pr-retargeting-approval-bypass.md) tested this
directly against Gitea `1.27.2` — the version this project pins — and confirmed the bypass is fully
exploitable: a PR retargeted from an unprotected branch merged into a protected one using only a
stale, never-revalidated approval. The obvious mitigation, `dismiss_stale_approvals`, was also tested
directly and does not help — it reacts to new commits, not to the base branch changing. This stands
alongside the status-forgery bypass above as a second, independent, live-confirmed hole in the
approval half of D2's control; see SADR-0003 for the full method, evidence, and required Milestone 3
mitigations (`policy-eval` must independently re-verify approvals against the PR's current base
rather than trusting Gitea's stored `official` flag).

## Reproduce it yourself

```
cd compose/forgery-test
docker compose up -d
# wait for http://localhost:3500/api/healthz to return "pass"
# then follow the steps above, or re-run the (uncommitted, throwaway) shell
# transcript this SADR was written from
docker compose down -v
```

Nothing from a run is meant to persist — `data/` is gitignored and the compose file is a **test
harness**, not a deployment profile. Do not reuse this container for anything but re-running this
experiment.
