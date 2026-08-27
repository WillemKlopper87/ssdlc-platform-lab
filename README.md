# SSDLC-Platform-Lab

A self-hosted, fully open-source secure-SDLC lab: Gitea + Woodpecker CI + gates that block merges until code proves itself.

Implements the organisation's SSDLC Framework (`../SDLC/SSDLC_Framework(1).docx`, v1.1) and the two workflow diagrams
in that folder — `ssdlc_pipeline_overview.mermaid` and `ssdlc_approval_workflow.mermaid`. Those source documents and
this implementation are versioned together; where they disagree, the framework wins unless this repo records the
divergence as a Security Architecture Decision Record (SADR) under `docs/adr/`.

## Status

The lab now has a runnable minimal Gitea + Woodpecker profile, policy
normalisation/evaluation, onboarding scripts, and a first trusted-pilot
foundation. It is still a pilot: the final independently released and signed
gate bundle has not yet been implemented. Full design lives in
[`docs/DESIGN.md`](docs/DESIGN.md), and the delivery plan is in
[`docs/ROADMAP.md`](docs/ROADMAP.md).

**Milestone 0 (design rework) is in progress.** See [`docs/adr/0001-commit-status-forgery.md`](docs/adr/0001-commit-status-forgery.md)
for the first open question — whether a developer's own Gitea token can forge the gate's commit status — resolved
empirically, not assumed.

## Why this exists

The framework defines a complete secure development lifecycle. It is a Word document; nothing enforces it. This lab
makes it mechanical: every PR is scanned automatically, findings surface inline with file/line context, and merge is
blocked until Critical/High findings are fixed or formally risk-accepted under the framework's own §3 two-party
exception process.

This is not a scanner. Every scanner already exists. The work is the connective tissue — the gate, the exception
workflow, the baseline model that makes rollout survivable on a real repo, and the evidence trail for ISO/IEC 27001 —
that no open-source project currently ships.

## Repository map

| Path | Contents |
|---|---|
| `docs/` | Design doc, ADRs/SADRs, operational runbooks (populated per milestone) |
| `policy/` | Rego policy, unit-tested, versioned like code |
| `normalise/` | Per-tool severity-normalisation adapters + fixtures |
| `sidecar/` | The stateless reporting service (not in the merge-decision trust path) |
| `compose/` | Docker Compose profiles — `forgery-test/` is the Milestone 0 experiment harness |
| `ansible/`, `terraform/`, `kubernetes/` | Infrastructure as code, by deployment profile |
| `pipelines/`, `templates/` | Woodpecker pipeline templates; paved-road scaffolding for onboarded repos |
| `examples/` | Deliberately vulnerable repos used by the end-to-end test |
| `scripts/` | `quickstart.sh`, `onboard-repo.sh`, `e2e.sh`, etc. (added per milestone) |

## Getting started

Copy `compose/minimal/.env.example` to `compose/minimal/.env`, set the required
secrets, and follow [`docs/OPERATIONS.md`](docs/OPERATIONS.md). Run the
environment checks first with `scripts/doctor.sh`.

For developers using the pilot, see [SECURITY.md](SECURITY.md) for what a gate
means, how to remediate a finding, and how to use the optional local checks.
