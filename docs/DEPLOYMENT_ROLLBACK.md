# Deployment versioning and rollback

Two distinct things share this name here — don't conflate them:

1. **How this repo's own codebase gets deployed and rolled back** — the lab itself, running as
   `compose/minimal` today. This section.
2. **What this platform *produces* for the repos it gates** (SBOM, provenance, signed release
   artifacts) — that's [`ROADMAP.md`](ROADMAP.md) Phase 3, "Trusted release," already scoped and
   not re-decided here. §3 below maps the two together so Phase 3 implementation work has a
   concrete versioning vocabulary to build against.

Follows `C:\applications\docs\templates\DEPLOYMENT_ROLLBACK_PLAYBOOK_TEMPLATE.md`. Like
HR_system, this repo runs Pattern B (single node, no orchestrator) today — `ARCHITECTURE.md`'s
component table shows everything live in `compose/minimal` on one Docker host; Kubernetes is
explicitly Phase 5, "deliberately postponed work." Don't borrow Pattern A (revision/traffic-split)
language for this repo until Phase 5 actually lands.

## 1. Versioning the lab's own build artifacts

`PINNED_VERSIONS.md` already does half of this — it pins every *third-party* tool (Gitea,
Woodpecker, scanners) to an exact version, "never a floating tag," re-verified against each
project's GitHub Releases API. Extend that same discipline to this repo's **own** built artifacts,
which aren't in that table today because they aren't external:

- `sidecar/` (the reporting service), `gate-bundle/` (SADR-0017's trust-boundary bundle, referenced
  in `docs/OPERATIONS.md`'s trusted-runner section as
  `GATE_BUNDLE_IMAGE=registry.example.internal/ssdlc-gate@sha256:<immutable-image-digest>`), and
  any future container this repo builds: tag with the **git SHA** at minimum. `gate-bundle`
  already goes further and correctly — a **content digest** (`@sha256:...`), not even a mutable
  tag — because SADR-0017's trust model specifically needs to prove which exact bundle produced an
  attestation. That's the strictest end of §3's template spectrum and the right target for
  anything else in the trust path (`policy/`, `normalise/*_adapter.py`,
  `pipelines/fast.woodpecker.yml` — the same file set `bot-approver.py`'s `GATE_CONTRACT_ENFORCE`
  already treats as gate-managed).
- Everything *outside* the trust path (the sidecar, dashboards) can use the lighter git-SHA-tag
  scheme the template describes — digest-pinning is warranted specifically where SADR-0017's
  "prove which exact code produced this verdict" property matters, not everywhere uniformly.

## 2. Deploy & rollback for `compose/minimal` (Pattern B)

Today's actual mechanism, stated explicitly since `docs/OPERATIONS.md` documents *starting* the
stack (`scripts/quickstart.sh`, Terraform-then-Compose ordering) but not upgrading or rolling back
an already-running one:

- Deploy: pull/rebuild the new pinned versions from `PINNED_VERSIONS.md` (plus this repo's own
  tagged artifacts per §1), `scripts/quickstart.sh` again — Terraform-owned network/volume state is
  preserved (`OPERATIONS.md`'s existing warning against a bare `docker compose up` applies to
  rollback the same way it applies to first start).
- Rollback: re-pin the previous versions/SHAs and re-run. Same "minutes not seconds, no warm second
  instance" caveat as HR_system's ADR-012 — there's no revision to shift traffic to on a single
  Compose host.
- Keep the last 3 known-good `PINNED_VERSIONS.md` snapshots (the file is already git-tracked, so
  this is really "keep the last 3 commits that touched it easy to find" — a `git log --
  docs/PINNED_VERSIONS.md` note, not new infrastructure).
- Postgres backs both Gitea and Woodpecker (`init-multi-db.sh`) — back it up before any version
  bump that includes a Gitea or Woodpecker **major** version, the same "backup gate before a
  migrating change" rule the template states in its §7, even though this stack's migrations are
  each tool's own internal schema migration rather than something this repo's CI runs directly.

## 3. What Phase 3 ("Trusted release") should reuse from the template, not reinvent

`ROADMAP.md` already scopes Phase 3 as: CycloneDX/SPDX SBOMs (Syft, already pinned), in-toto/SLSA
provenance containing the Gate Contract + source SHA, Cosign signing, "release traces from
protected source to signed artifact and deploy." Mapped onto the generic template's vocabulary:

- Template §2 (versioning scheme) → Phase 3's provenance attestation *is* the versioning scheme,
  just cryptographically strengthened: instead of "tagged with the git SHA," it's "tagged with the
  git SHA **and attested** that the SHA was built by the expected pipeline from the expected
  policy digest" — literally SADR-0017's model, generalized from the gate bundle to every release
  artifact this platform (or a repo it gates) produces.
- Template §8 (approval gates) → Phase 3's "release approval only for a real target" is this
  template's production-promotion gate, expressed as a signed attestation check instead of a
  GitHub Environment reviewer — a stronger mechanism for the same purpose, appropriate to what this
  platform is for.
- Template §6 (automated rollback triggers) is **not** in Phase 3's scope and shouldn't be forced
  into it — this platform gates merges and attests releases; it does not currently run or monitor
  anything at runtime for the repos it gates (Phase 5's Kubernetes/Falco work is where that would
  eventually live). Don't imply a rollback-trigger capability Phase 3 doesn't build.

## 4. This platform as the upstream "detection" stage for other projects

The wider pattern this template describes starts with a bot noticing a new dependency release and
opening a PR (template §1). `ROADMAP.md` Phase 2 already schedules **Renovate with a Gitea
Dependency Dashboard** — once live, a Renovate-opened PR against any repo onboarded here
(`scripts/onboard-repo.sh`) goes through the exact same `pipelines/fast.woodpecker.yml` gate as a
human PR, per the existing architecture diagram in `ARCHITECTURE.md`. That is this platform's
version of the template's "the bot's PR gets zero special treatment" rule — already the design,
not a new decision.

This is **not currently wired to TTLI or HR_system's GitHub repos** — those onboard to GitHub
Actions directly (their own `ci.yml` / `hcm-ci.yml`), and this platform gates repos hosted on its
own Gitea instance via `onboard-repo.sh`. If a repo is ever mirrored/onboarded here in addition to
its GitHub pipeline, this platform's gate would sit *upstream* of that repo's existing deploy
pipeline (`deployment-rollback-strategy.md` for TTLI, `ADR-012` for HR_system) — blocking merge on
security findings before that repo's own versioning/canary/rollback machinery ever sees the commit.
Worth knowing as the shape of the option; not something to build until a repo is actually onboarded.
