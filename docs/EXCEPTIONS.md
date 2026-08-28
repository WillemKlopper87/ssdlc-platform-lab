# Exceptions

**Status: not built.** This document records the design (DESIGN.md's D5 and *Exception flow*
section, framework §3) and, honestly, the gap that leaves in the platform today. Do not read this as
a description of running functionality — check `docs/TODO.md` and [`ARCHITECTURE.md`](ARCHITECTURE.md)
for current status before relying on anything below.

## Why this exists

Framework §3 requires that a real finding a team cannot immediately fix be **formally risk-accepted**,
not silently ignored: joint sign-off from the Security Officer *and* the accountable Engineering Lead,
a hard expiry of ≤ 90 days, no silent auto-renewal, and quarterly reporting. Without this, a gate that
only blocks has exactly one failure mode under real pressure: someone with admin access weakens the
policy to unblock a release, and the weakening is never reviewed or reverted.

## Two distinct mechanisms — do not conflate them

DESIGN.md draws this line deliberately and it matters for what gets built:

- **False positive** — the scanner is wrong, not the code. Fix: a rule-level suppression in
  `.ssdlc/suppressions.yaml`, with rule ID, justification, owner, and expiry, committed as an ordinary
  PR to the policy repo. Fast path, no risk acceptance, no two-party sign-off — it's a policy
  correction, not a risk decision.
- **Real finding, accepted** — the finding is real and the team is choosing to ship anyway, with a
  documented reason and a deadline. This is the framework §3 flow below. It requires DefectDojo's
  **Risk Acceptance** object (hard expiry, reactivates on expiry) as the register, and a signed record
  in a dedicated `exceptions` repo as the enforcement artifact.

**Making a developer file a risk acceptance for a false positive poisons the risk register and every
metric computed from it** — this is DESIGN.md's own warning, worth repeating here because it's the
easiest way to get this wrong once exceptions exist at all.

## The designed flow (not yet built)

```
Gate blocks PR (Critical)
   │
   ├─ fix -> push -> rescan -> green -> merge          (the only path that exists today)
   │
   └─ /exception <finding-id> <justification>           (does not exist yet)
          │  sidecar opens DefectDojo Risk Acceptance (pending)
          │  notifies security-officers AND the repo's eng-lead
          ▼
      BOTH must run /approve-exception <id> <days>
          ├─ verify each approver is in their Gitea team (server-side API check)
          ├─ verify approver != PR author (no self-approval)
          ├─ verify the two approvers are distinct humans
          │     (in a small org one person may sit in both teams)
          ├─ verify days <= the policy max for that severity
          ▼
      - Risk Acceptance activated in DefectDojo         (the register)
      - signed record committed to the exceptions repo   (the enforcement
        artifact -- policy-eval would clone this and honour unexpired
        records, per D5)
      - appended to an append-only audit log
      - pipeline re-triggered; policy-eval reads the record, gate passes,
        merge unblocked
      - sticky comment: "Exception active until YYYY-MM-DD"
```

**Why the enforcement artifact is git, not a DefectDojo API call at evaluation time**: D5's whole
argument is that the merge decision must never depend on a heavyweight service being up. DefectDojo's
import is async and its dedup is slow; putting it in the merge path means DefectDojo downtime stops
the org from shipping. The signed exceptions-repo record is small, fast to clone, and versioned like
everything else the gate trusts.

**Expiry needs no enforcement hook.** An expired record simply stops matching on the next evaluation,
and the gate blocks again automatically. A scheduled job (also not built) would handle the
*bookkeeping* — reactivating the DefectDojo finding, marking the repo release-ineligible, notifying
owners, feeding the quarterly §3 report — but the *gate's* re-arming does not depend on that job
running.

**Approval is synchronously blocking a developer**, which DESIGN.md flags plainly: a CISO on leave is
a merge freeze. The design calls for a documented approval SLA (1 business day), a named fallback
approver, and a tracked "developer hours lost waiting on exceptions" metric — because that number,
not the security metrics, determines whether the gate survives contact with a real org.

## Onboarding legacy repos: the baseline problem

The same exceptions-repo infrastructure is meant to carry each project's **baseline** — the fingerprints
of every finding already on the default branch at onboarding time. This is not itself an exception
mechanism, but it depends on the same store, so it's tracked here rather than invented as a separate
system:

- A full scan at onboarding time records what already exists. Those findings become tracked debt with
  owners and SLA clocks — not PR blockers.
- `policy-eval` would then block only on findings **not** in the baseline.
- Secrets are never baselined — a secret blocks wherever and whenever it appears, no exception.
- The baseline only shrinks: a fixed finding leaves it and cannot silently return.

## Current reality

**None of this exists.** [`POLICY.md`](POLICY.md) and `policy/severity.rego`'s own header comment say
so directly: every finding is judged on its own merits, whether pre-existing or newly introduced.
There is no `.ssdlc/suppressions.yaml` convention implemented, no exceptions repo, no `/exception`
command, no DefectDojo. `fingerprint` is computed by every `normalise/` adapter and carried through
the pipeline specifically so this is a wiring problem when the time comes, not a schema problem — but
nothing reads it today.

**Update (SADR-0024/0025): baseline/differential gating now exists.** `scripts/generate-baseline.py`
snapshots a repo's pre-existing findings at onboarding into `.ssdlc/baseline.json`, and
`evaluate-findings.py --baseline` blocks only what's genuinely new — a repo with inherited debt no
longer blocks every PR from day one for problems the PR did not cause. This page's own subject, the
two-party *exception* workflow (accepting a genuinely NEW finding, not baselining pre-existing ones),
is still not built and is a separate gap from the one baseline gating closes — do not conflate the
two. Sprint 01's original restriction to pilot repos with no pre-existing findings predates this fix
and is a candidate for re-verification, not something to keep citing this paragraph's old reasoning for.

**The only accepted-risk path that exists today is out-of-band**: editing `policy/severity.rego`
itself (a reviewed PR to the platform repo, same as any policy change) or not merging. There is no
per-finding, time-boxed, two-party exception yet — anyone proposing to ship past a real Critical/High
finding today is making a platform-level policy change, not using an exceptions workflow, and it
should go through review as one.

## Sequencing

Milestone 4 in `DESIGN.md`'s roadmap and Phase 2 ("measured enforcement") / Phase 5 (deferred
capabilities) in `docs/ROADMAP.md`. Explicitly out of Sprint 01's scope per
`docs/SPRINT-01-TRUSTED-PILOT.md`. Do not build partial exception handling ahead of the exceptions
repo and DefectDojo integration — a gate that can be talked out of blocking without the two-party,
expiring, audited record is worse than no exception mechanism at all.
