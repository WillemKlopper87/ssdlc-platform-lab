# Self-defence

DESIGN.md's D6: **the platform patches itself first, on a tighter SLA than it imposes on product
teams.** A gate that is more careful about the code it reviews than about the software it is built
from is not credible. This document is the standing operational checklist that decision implies —
most of it is not yet automated, and this file says so directly rather than implying otherwise.

## Why this exists, concretely

Gitea is the platform's arbiter of identity, branch protection, and audit record. A vulnerability in
Gitea itself is not "one more finding" — it can compromise the thing the entire gate depends on for
integrity. Direct research against the GitHub Security Advisories API on 2026-08-23 found **84
advisories for `go-gitea/gitea` in a four-month window**, several bearing directly on decisions this
platform has already made (see `docs/PINNED_VERSIONS.md`'s "Gitea CVE posture" section for the current
detail — that section, not this one, is the source of truth and decays faster than this document).

## The standing checklist

- [x] **Pin every component by exact version, never a floating tag.** Done and enforced by convention
      — `docs/PINNED_VERSIONS.md` is the single source of truth, checked against each project's actual
      releases, not carried over from memory.
- [ ] **Pin container images by digest, not tag.** D6's stated target. **Not currently done** —
      confirmed live: `compose/minimal/docker-compose.yml` pins `gitea/gitea:1.27.2`,
      `woodpeckerci/woodpecker-server:v3.17.0`, `woodpeckerci/woodpecker-agent:v3.17.0`, and
      `postgres:16-alpine` by tag. `gate-bundle/run-container.sh` is the one place in the repo that
      *does* enforce digest-only (`case "${GATE_BUNDLE_IMAGE}" in *@sha256:*) ;; *) exit 2 ;; esac` —
      that pattern should extend to `compose/minimal` before this checklist item is honestly checked
      off.
- [x] **Set `REVERSE_PROXY_TRUSTED_PROXIES` explicitly, do not enable `[markup.*]` external
      renderers.** The two RCE-class advisories found in the 2026-08-23 research
      (`CVE-2026-73539`, argv injection via an external markup renderer; `CVE-2026-59774`,
      unauthenticated file read via `go-org`) both require an optional renderer to be configured at
      all. Mitigation is a configuration choice already implied by the design — never turn these on
      without a specific, vetted need.
- [ ] **Enforce IMDSv2 on the host.** Not applicable to the current local/minimal profile (no cloud
      metadata service present); becomes a real checklist item once a `terraform/<cloud-provider>/`
      profile exists.
- [x] **A self-patch SLA tighter than framework §2.4's 24-48h Critical window for product teams.**
      Stated as policy in D6; not yet backed by an automated alert — see "What's not automated" below.
- [ ] **A RACI row for platform self-maintenance.** DESIGN.md flags the framework has none; not yet
      added anywhere in this repo's own docs.
- [x] **Dogfood: the platform's own repo is onboarded to its own gates.** True in the sense that
      `pipelines/fast.woodpecker.yml`, `policy/severity.rego`, etc. are exercised live against real
      onboarded test repos throughout the SADR history (SADR-0005 onward) — but see the honest caveat
      below on what dogfooding the platform's *own* IaC currently proves.

## The standing query — re-run this, don't trust a cached answer

```sh
gh api repos/go-gitea/gitea/security-advisories --paginate > gitea_advisories.json
```

This is not a one-time Milestone-0 lookup. `docs/PINNED_VERSIONS.md` states it plainly: this file "is
stale the day after it's written." Re-run the same pattern (`gh api repos/<owner>/<repo>/releases/latest`,
or the advisories endpoint for a component with a CVE history) before every version bump, and before
trusting any pin in this repository that's more than a few weeks old.

## The honest gap in dogfooding the platform's own IaC

SADR-0010 found this directly: Checkov has **zero built-in policies for the `docker` Terraform
provider's resource types**. A clean Checkov run against `terraform/local/` proves Checkov has nothing
to say about those resources, not that they're secure. Dogfooding becomes meaningful once there are
Kubernetes manifests, Dockerfiles, or cloud IaC for Checkov to actually evaluate — tracked in
`docs/TODO.md`, not silently claimed as done.

## What's not automated yet

- No scheduled job re-runs the `gh api` advisory query and alerts on a new relevant CVE — it is a
  manual step today, described here so it happens, not enforced by tooling.
- No alerting exists for "a pinned version in `docs/PINNED_VERSIONS.md` is now behind the latest
  release" — re-verification is a human discipline at each milestone boundary, per that file's own
  "Re-verification cadence" section.
- Trivy's own scheduled DAST/dependency scanning (DESIGN.md's *deep*/*scheduled* pipeline tiers)
  covers *onboarded application repos*, not the platform's own container images or its own
  Terraform/Ansible IaC on a recurring schedule.

## Related

`docs/PINNED_VERSIONS.md` for current pins and the live CVE research this document summarises the
policy for; [`THREAT_MODEL.md`](THREAT_MODEL.md) for how a Gitea compromise specifically threatens the
gate's integrity, not just availability; [`OPERATIONS.md`](OPERATIONS.md) for upgrade mechanics.
