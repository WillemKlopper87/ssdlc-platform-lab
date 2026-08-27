# SADR-0003: PR-retargeting approval bypass — confirmed live, and the flagged mitigation doesn't close it

**Status:** Confirmed by experiment, both the vulnerability and the ineffectiveness of the one
mitigation available in Gitea's UI
**Date:** 2026-08-24
**Milestone:** 1 — closes the follow-up tracked since [SADR-0001](0001-commit-status-forgery.md)
**Security & Privacy Impact:** Critical. This is a second, independent way to defeat the approval
half of D2's mitigation — distinct from, and not fixed by, anything SADR-0001 verified.

## Question

While researching Gitea's current CVE posture for [PINNED_VERSIONS.md](../PINNED_VERSIONS.md), a
different advisory surfaced from the one SADR-0001 tested: **GHSA-w5pg-649r-p6gg / CVE-2026-58439**,
"Branch Protection Bypass via PR Retargeting Preserves Stale `official` Approval Flag." The
mechanism as described: an approval given while a PR targets a branch with weak or no protection can
survive a retarget to a protected branch, still counting as "official" there — even though no
reviewer ever evaluated the code against the protected branch's actual rules. SADR-0001 flagged this
as untested and required before Milestone 3 trusts `required_approvals`. This experiment tests it
directly, live, against this project's own Gitea version.

## Method

Reused the `compose/forgery-test` harness (Gitea `1.27.2`, same pattern as SADR-0001). Three
accounts: `gateadmin` (admin/setup), `alice` (PR author, ordinary `write` collaborator), `bob`
(approver, ordinary `write` collaborator). One repo, two branches:

- `main` — **protected**: `required_approvals: 1`, `enable_approvals_whitelist: true`,
  `approvals_whitelist_username: ["bob"]` — only bob's approval counts as official.
- `staging` — **no protection rule at all** — the "weak target" the attack needs.

```
1. Alice opens a PR: feature/alice-work → staging (the unprotected branch)
2. Bob approves it there
   → GET .../reviews: official=True   (Gitea marks it official by default; no whitelist
                                        constrains staging, so nothing withholds the flag)
3. Alice retargets the SAME PR: PATCH .../pulls/1  {"base": "main"}
   → 201 — the retarget itself is not blocked or challenged in any way
4. Re-check the review
   → GET .../reviews: official=True, stale=False   (unchanged by the retarget)
5. Alice attempts to merge into main, relying on ONLY this approval
```

## Result

```
PATCH /pulls/1  {"base": "main"}                     → 201, base.ref now "main"
GET   /pulls/1/reviews                                → official: True, stale: False  (unchanged)
POST  /pulls/1/merge  {"Do": "merge"}                 → 200 OK
GET   /pulls/1                                        → "merged": true, "merged_by": {"login": "alice"}
```

**Confirmed, exactly as the advisory describes.** Bob never reviewed the code as it would land in
`main`. He approved a diff against `staging`, a branch with no protection rules and no approver
restriction — his approval would have been marked official there regardless of who he was. That
approval, unmodified, satisfied `main`'s `required_approvals: 1` with its `approvals_whitelist`
naming bob specifically. Alice, the PR author, retargeted her own PR and merged it, with the only
"review" in the chain having been given against different code, different rules, and arguably by
coincidence rather than by bob ever being asked to evaluate `main`'s protection at all.

## Does `dismiss_stale_approvals` close this? Tested directly — no.

The obvious candidate mitigation is Gitea's `dismiss_stale_approvals` branch-protection setting.
Enabled it (`PATCH .../branch_protections/main {"dismiss_stale_approvals": true}`), confirmed active
via the API, then repeated the identical attack sequence on a fresh PR:

```
Bob approves against staging                          → official: True
Retarget to main (dismiss_stale_approvals now ON)      → 201
Re-check the review
  → official: True, stale: False, dismissed: False     (STILL unchanged)
```

**The setting does not fire.** This makes sense once you know what it actually watches for:
`dismiss_stale_approvals` dismisses approvals when **new commits are pushed to the PR's head** — it
has nothing to do with the PR's *base* changing. A pure retarget introduces no new commits, so there
is nothing for this setting to react to. The one mitigation available through Gitea's branch
protection UI **does not address this attack surface at all**. (The merge attempt on this second PR
hit an unrelated `mergeable: false` — a genuine content conflict from both test PRs touching the same
file — not a policy block; the review-status evidence above already fully answers the question this
experiment was testing, independent of that unrelated conflict.)

## Decision

This is now the platform's **second confirmed, independent bypass** of the approval half of D2's
control, alongside SADR-0001's status-forgery bypass of the status half. Neither is closed by
anything currently in the design. Concretely, for Milestone 3:

1. **Disable retargeting entirely for PRs against protected branches**, if Gitea exposes a setting
   for this — not yet found in this version's branch-protection options; needs a docs check before
   assuming it doesn't exist.
2. **If retargeting cannot be disabled**, the gate's own `policy-eval` step (D1) must independently
   verify, at evaluation time, that every review counted toward `required_approvals` was submitted
   **against the PR's current base**, not merely that a review with `official: true` exists — i.e.,
   re-derive official status server-side rather than trusting Gitea's stored flag after a retarget.
   This is now a hard requirement for the gate design, not an optional hardening note.
3. **A cheaper interim control**: since this attack requires an *unprotected or weakly-protected*
   branch to stage the approval on, protecting **every** branch pattern in onboarded repos (not just
   `main`) closes the specific staging ground this experiment used — worth adding to
   `onboard-repo.sh`'s branch-protection step as a `*` or wildcard rule, pending confirmation Gitea's
   API supports branch-pattern protection rules (not just literal branch names) in this version.
4. **Framework-level**: this is exactly the kind of finding framework §3's exception process exists
   for — until item 2 is built, any repo relying solely on `required_approvals` without a
   retargeting-aware gate should be treated as **not actually hardened**, regardless of what its
   Gitea configuration claims.
5. This experiment (or its automated form) becomes a permanent regression test alongside SADR-0001's
   forged-status test — see DESIGN.md's Verification section, which needs a new line: "PR retargeted
   from an unprotected branch with a prior approval ⇒ merge still blocked without a fresh, base-aware
   review."

## Reproduce it yourself

```
cd compose/forgery-test
docker compose up -d
# wait for http://localhost:3500/api/healthz to return "pass"
# create gateadmin (admin), alice, bob; mint tokens for each
# create a repo, add alice+bob as write collaborators
# create an unprotected "staging" branch off main
# protect main: required_approvals=1, approvals_whitelist=["bob"]
# alice: open a PR targeting staging; bob: approve it
# PATCH the PR's base to "main"; check GET .../reviews — official is still true
# attempt merge — it succeeds
docker compose down -v
```

Nothing from a run is meant to persist — `data/` is gitignored, and this is a test harness, not a
deployment profile.
