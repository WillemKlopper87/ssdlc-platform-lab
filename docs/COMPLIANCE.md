# Compliance

**This document is a mapping and a status report, not a certification claim.**
`FRAMEWORK-ADDENDUM-2026.md` states this directly: implementing this platform's automation does not
by itself constitute ISO/IEC 27001 certification. ISO 27001 is a risk-managed ISMS — people, process,
evidence, and review, of which this platform's technical controls are one input. Read every "evidence
source" column below as *"where the artifact would come from if the wider ISMS around this platform
exists,"* not as *"this control is certified."*

## Framework §7 control map

| Control area | Intended evidence artifact | Current source | Status |
|---|---|---|---|
| A.8.28 Secure coding | Passing gate status per PR (Gitleaks/Semgrep/Trivy, [`POLICY.md`](POLICY.md)) | Woodpecker commit status + `policy-eval-findings` stdout | **Live**, per onboarded repo — not yet exported anywhere durable |
| A.8.24 Use of cryptography (secrets handling) | Verified-secret detection + rotation record | Gitleaks (server-side, [`SELF_DEFENCE.md`](SELF_DEFENCE.md)'s D7 reference) | **Detection live**; automatic incident-opening and rotation runbook (D7) not built |
| A.5.24-A.5.27 Incident management | Auto-opened incident on a verified secret | — | **Not built.** Currently manual: [SECURITY.md](../SECURITY.md)'s "if a credential may have been committed" guidance |
| A.8.29 Security testing | SAST/SCA findings, per-PR | `normalise/*_adapter.py` output, `policy-eval-findings` | **Live**, not yet persisted past the pipeline run's own logs |
| A.8.30 Outsourced development | Same gate applied regardless of code origin | [`AI_ASSISTANT_POLICY.md`](AI_ASSISTANT_POLICY.md) | **Live** — the gate does not distinguish AI-assisted from hand-written code |
| A.5.36 Compliance with policies | Two-party risk acceptance for accepted findings | Exceptions repo, DefectDojo Risk Acceptance | **Not built** — see [`EXCEPTIONS.md`](EXCEPTIONS.md) |
| A.5.23 Cloud services security | Deployment-profile scoping (`minimal`/`full`/`kubernetes`) | `DESIGN.md`'s *Deployment profiles* | Design only; only `minimal` is live |
| A.8.9 Configuration management | Pinned versions, reviewed policy changes | `docs/PINNED_VERSIONS.md`, `policy/` under normal PR review | **Live** for version pins; Semgrep rulesets are still pulled live from the registry, not vendored (tracked in `docs/TODO.md`) |
| A.5.31 Legal/regulatory requirements | Licensing review for every dependency | `DESIGN.md`'s *External integrations* licensing caution | Manual, not automated |
| A.8.13 Information backup | Restore-drill evidence | — | **Not built.** No `backup.sh`/`restore-drill.sh` exist yet (`docs/TODO.md`) |
| A.5.28 Collection of evidence | Append-only audit log of gate decisions and approvals | — | **Not built.** Gitea's own PR/review history is the only durable record today, and it is not exported anywhere outside Gitea itself |

## What "evidence" means here today

The only durable, inspectable record that exists right now is **Gitea's own state**: PR history,
review records, commit statuses, and branch-protection configuration. Nothing exports this to an
append-only store outside the tools, which DESIGN.md's *Evidence and durability* section names as a
real requirement — without it, a compromised credential with write access to Gitea could in principle
alter the very history meant to prove the gate worked. There is currently no mitigation for that beyond
Gitea's own access controls.

## Metrics named in framework §6, and their current state

| Framework metric | Intended series (`DESIGN.md`) | Current state |
|---|---|---|
| MTTR Critical/High | `ssdlc_finding_remediation_seconds` | Not instrumented — no Prometheus in `compose/minimal` |
| % PRs passing first run | `ssdlc_gate_first_run_pass_total` / `ssdlc_gate_evaluations_total` | Not instrumented |
| Mean time to revoke (D7) | `ssdlc_secret_revocation_seconds` | Not instrumented — no automatic revocation flow exists yet |
| Exception wait time | `ssdlc_exception_decision_seconds` | Not instrumented — no exceptions flow exists yet |
| Gate health | `ssdlc_gate_errors_total`, `ssdlc_reconcile_queue_depth` | `scripts/reconciliation-loop.py` exists and is live-tested (SADR-0015) but does not currently emit a metric anywhere; its behaviour is only visible in its own process logs |

## OWASP references adopted (framework addendum §1)

Per `FRAMEWORK-ADDENDUM-2026.md`: OWASP Top 10:2025 for developer awareness, OWASP ASVS 5.0.0 as the
versioned application-verification reference, OWASP LLMSVS for any product that itself embeds an LLM
(see [`AI_ASSISTANT_POLICY.md`](AI_ASSISTANT_POLICY.md)), and OWASP SAMM as an annual maturity
discussion tool — not a target to complete all at once. **A scanner result is evidence for a control,
never proof that a product is secure by design** — the addendum's own framing, repeated here because
it is the right caution for how to read every "Live" row in the table above.

## What to do before claiming any of this in an audit

1. Re-read the specific SADR backing the row you're citing — this document summarises; the SADR is the
   evidence.
2. Confirm the underlying mechanism is still live by checking `docs/TODO.md` and
   [`ARCHITECTURE.md`](ARCHITECTURE.md)'s current-state table, not this document alone — both decay
   and should be re-checked at each milestone boundary, same discipline as `docs/PINNED_VERSIONS.md`.
3. Do not present an unenforced or manual control as automated. Several rows above are explicitly
   "design only" or "manual" — say so if asked, rather than letting the existence of this document
   imply otherwise.

## Sequencing

Full evidence export, retention policy, and KPI dashboards are Milestone 7 (`DESIGN.md`) / Phase 4
(`docs/ROADMAP.md`) — after the trusted pilot gate, baseline gating, and exceptions flow are proven,
not before.
