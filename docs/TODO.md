# Outstanding work

## Do next — Sprint 01 trusted pilot foundation

The ordered sprint is in [`SPRINT-01-TRUSTED-PILOT.md`](SPRINT-01-TRUSTED-PILOT.md).
It takes priority over feature expansion because it closes the current trust-
boundary gaps before a real developer team is onboarded.

- [ ] Create and approve the Trusted Gate Contract SADR.
- [ ] Review and approve the additive framework update in
      [`FRAMEWORK-ADDENDUM-2026.md`](FRAMEWORK-ADDENDUM-2026.md); it preserves
      SSDLC Framework v1.1 while versioning current standards and guidance.
- [ ] Explicitly dismiss stale approvals on push; prove it live and in regression coverage.
- [ ] Require a current-head gate-bot verdict plus a distinct non-author human
      approval; do not treat `required_approvals: 2` as proof of those roles.
- [ ] Prevent a PR from supplying the pipeline, policy, or decision code that
      approves itself; make the bot verify a pinned contract/attestation.
- [ ] Add altered-pipeline and altered-policy bypass tests before changing
      enforcement design.
- [ ] Ship the pilot developer guidance and pre-commit setup.

## Existing implementation debt

Pulled from every SADR's own Decision section (docs/adr/0001–0016) and DESIGN.md's Open questions,
not re-derived from scratch. Each item cites where it came from — check that SADR before starting
work, since the reasoning behind *why* often matters as much as the item itself. This file decays
like PINNED_VERSIONS.md does: re-check against the current SADR set at each milestone boundary rather
than trusting it indefinitely.

## Milestone 3 — Enforcement (in progress)

- [x] ~~Wire `terraform/local/`'s network+volumes into `compose/minimal/docker-compose.yml`.~~
      Done — `external: true` for all four resources, `WOODPECKER_BACKEND_DOCKER_NETWORK` updated to
      match. Full stack re-verified: a real pipeline step container proven to reach Gitea over the
      renamed network, and a full container-destroy-and-recreate cycle proven not to lose data in
      the externally-managed volumes. — SADR-0014
- [ ] Live-test `ansible/site.yml` end-to-end on a real POSIX control node (WSL2 with a real distro,
      a Linux CI runner, or macOS — confirmed it cannot run on native Windows). Fix whatever breaks;
      update SADR-0010 with the result before trusting the playbooks un-reviewed. — SADR-0010
- [ ] Package `compose/forgery-test/pre-receive-gitleaks.sh` as the Ansible role that **chains
      after** Gitea's own generated pre-receive hook, not replacing it — install target: every
      onboarded repo, via `onboard-repo.sh`. — SADR-0002
- [ ] Decide and implement: should the pre-receive hook read `.gitleaks.toml` directly (current
      test) or a generated subset compiled from `.ssdlc/suppressions.yaml` (recommended, for
      consistency with the rest of the policy model)? — SADR-0002
- [ ] Budget pre-receive push latency against real repo sizes — the ~100-byte test case was
      dominated by process startup (1.2–3.4s), not representative of a real diff. — SADR-0002
- [x] ~~Build the bot-only approver team + `required_approvals: 2` into `onboard-repo.sh` for
      real.~~ Done and live-tested through an actual merge — `scripts/bot-approver.py` casts the
      bot's vote once scanning steps are green (from outside any build container, per DESIGN.md's
      token rule), `onboard-repo.sh` now sets `required_approvals: 2` and adds `gate-bot` as a
      collaborator. — SADR-0011
- [ ] `scripts/bot-approver.py` is single-repo only (plain env vars) and not yet wired as a standing
      `compose/minimal` service — currently has to be run manually. Needs multi-repo support before
      it can watch every onboarded repo, and a service definition once it does. — SADR-0011
- [x] ~~`policy-eval/verify-approvals.py` does not resolve `approvals_whitelist_teams`.~~ Done —
      resolves team membership via Gitea's org/team API, unions it with the username whitelist, and
      fails loud (not a silent miscount) if the token lacks the `read:organization` scope this now
      needs. New regression coverage: `tests/regression/06-team-whitelist.sh`, 22/22 across the full
      forgery suite. — SADR-0013
- [x] ~~The general reconciliation loop (any lost webhook, any stuck PR, not just bot-approver's own
      restart path) doesn't exist.~~ Done — `scripts/reconciliation-loop.py`. Two "obvious" recovery
      mechanisms (Woodpecker's `CreatePipeline`, Gitea's `TestHook`) were checked against their own
      source and rejected: both synthesize a push- or manual-shaped event rather than a genuine
      `pull_request` one, which would silently skip `approval-check` or (worse) satisfy branch
      protection's status glob on a push-shaped status alone — reintroducing exactly the bypass
      class SADR-0001/0003/0009 closed. The safe mechanism is a real, empty `git commit`, pushed for
      real, so Gitea fires its own genuine webhook. Live-tested against a deterministically stuck PR
      (opened before Woodpecker activation, so no webhook existed at all): detected, recovered with
      a real `pull_request`-event pipeline confirmed via a fresh status check, idempotent on re-run,
      and a runaway-safety limit (max 3 nudges) proven against a persistently-broken repo rather than
      just written and trusted. — SADR-0015
      — SADR-0009, SADR-0011, DESIGN.md D4/D5
- [x] ~~Turn each proven experiment into a permanent, automated regression test.~~ Done —
      `tests/regression/` (SADR-0012): 27 assertions across SADR-0001/0002/0003/0009/0011, two
      runners (`run-forgery-suite.sh`, `run-minimal-suite.sh`), both re-runnable from a cold
      harness. Found and fixed 8 more real bugs building it (`set -e` semantics twice, a POSIX vs
      bash substitution bug, a missing repo path in a hand-built push URL, missing function
      arguments, the `Host: gitea:3500` requirement recurring a third time on PR creation).
- [x] ~~`tests/regression/` is runnable, not yet run automatically.~~ Partially done — split into
      two tiers rather than naively wiring the whole (Docker-dependent) suite into the fast gate,
      which would need Docker-in-Docker on a step and compound this project's own documented
      Docker-socket privilege concern. Tier 1 (`tests/unit/` — syntax checks, the SADR-0007 `${...}`
      lint, `policy-eval` against a stub Gitea, no Docker at all) is built, live-tested standalone
      (20/20), and wired into `pipelines/self-verify.woodpecker.yml`. — SADR-0016
- [x] ~~`self-verify.woodpecker.yml` has not been proven as an actual running Woodpecker pipeline
      yet.~~ Done, once Docker came back — 20/20 checks passed inside a real pipeline run, triggered
      by a real webhook. Found and fixed two more real bugs getting there: remapping only
      `WOODPECKER_HOST` without also moving `WOODPECKER_SERVER_ADDR` breaks webhook delivery
      silently (Gitea's delivery is container-to-container, never touches the host port mapping);
      and the pipeline's own header comment violated the SADR-0007 `${...}` rule it was describing
      — this project's own lint would have caught it, but was never re-run after that file was
      added. — SADR-0016 addendum
- [ ] Tier 2 (the live-integration half of `tests/regression/`, real nested Gitea+Woodpecker) needs
      a genuinely isolated second Woodpecker agent/host to run safely — designed (mirrors D2's
      fork-isolation pattern, inverted) but deliberately not built: this environment has only one
      machine, and "isolation" tested against a single shared host wouldn't actually prove isolation.
      Same honest gap as the Ansible item below, same reason. — SADR-0016
- [ ] `compose/minimal`'s Woodpecker host port (8000) is hardcoded, not configurable — a real, if
      minor, gap this session's own port-8000 collision exposed. Worth a variable if this profile
      ever runs on a machine with something else already bound to that port. — SADR-0016
- [x] ~~The `Host: gitea:3500` requirement has now cost real time three separate times.~~ Fixed —
      `lib.sh` shadows the `curl` shell command itself, adding the header to every call targeting
      Gitea's port automatically; no call site needs to remember it anymore. `git_push_origin` sets
      it unconditionally too (git's own HTTP calls don't go through the shadowed `curl`). Both test
      suites re-verified clean after the change (27/27). — SADR-0012 addendum
- [ ] The `curl` shadow in `tests/regression/lib.sh` is scoped to that test suite only — the same
      class of bug could still recur in any *production* script that talks to Gitea from outside its
      own docker network (`onboard-repo.sh`, a future `scripts/bootstrap-platform.sh`, etc.). Not
      fixed there: those scripts run against real deployments where forcing a `gitea:3500`-shaped
      Host header would be wrong, not just unnecessary. Worth a real answer once such a script needs
      to run against this project's own local dev profile specifically, not before.

## Fast-gate / pipeline debt

- [x] The unified findings gate (`policy-eval/evaluate-findings.py`, Rego
      severity policy, and scanner normalisation adapters) — live-tested
      against real Gitleaks, Semgrep, and Trivy reports through a real
      onboarded repo: blocked a PR carrying real Critical/High findings,
      then confirmed `success` on the same PR once remediated. Found and
      fixed a live self-contamination bug along the way (Semgrep and Trivy
      flagging the platform's own prior-step report files). — SADR-0018
- [ ] Persist a shared Trivy DB volume + scheduled out-of-band refresh job, then re-add
      `--skip-db-update` to the `dependencies` step — every run currently re-downloads ~109MB, which
      will not stay under the fast gate's 3-minute budget at real scale. — SADR-0005
- [ ] Vendor Semgrep rulesets into the policy repo instead of pulling live from the registry at scan
      time — an upstream rule change can currently block every PR in the estate with no review and
      no rollback. — DESIGN.md, Policy model / Rule-set updates
- [ ] `onboard-repo.sh`'s push-whitelist gives whoever holds `GITEA_ADMIN_TOKEN` a standing PR-review
      bypass on every onboarded repo. Needs a narrower-scoped automation identity — likely resolved
      once Milestone 3's sidecar exists and this doesn't have to be a full admin token. — SADR-0005
- [ ] Extend the `inline-comments` (reviewdog) step to also consume Trivy's and Gitleaks's output,
      not just Semgrep's. — SADR-0008
- [ ] Milestone 2's actual measurement goal hasn't started: the false-positive rate on reviewdog's
      findings against real, non-fixture code. This gates whether inline comments should ever become
      blocking. — SADR-0008
- [ ] Two unresolved Woodpecker mechanics, root cause not found (tracked honestly, not swept under
      anything): (a) `from_secret` didn't populate a value into a step's environment despite matching
      Woodpecker's own e2e test fixture syntax exactly; (b) the precise scope of Woodpecker's
      server-side `${...}` template substitution — it consumes dollar-brace patterns file-wide,
      **including comments**, before real YAML parsing. Neither blocks current needs; both matter for
      whoever next needs real secret-backed parameterization in a Woodpecker pipeline. — SADR-0007
- [ ] Find a real mechanism for ZAP's structured JSON/HTML report output from inside a Woodpecker
      step — `-J`/`-r` need a genuine host bind mount at `/zap/wrk`, which no Woodpecker-orchestrated
      step provides. Console-text parsing works today but throws away the structured artifact.
      — SADR-0007
- [ ] A reverse proxy in front of Gitea+Woodpecker for any profile beyond local — would permanently
      remove the `network_mode: service:gitea` / explicit `ROOT_URL` / `WOODPECKER_BACKEND_DOCKER_NETWORK`
      workarounds this profile currently depends on. — SADR-0004, SADR-0005

## Documentation debt

DESIGN.md's own repository structure names these; none exist yet:

- [ ] `docs/ARCHITECTURE.md`, `GATE_CONTRACT.md`, `POLICY.md`, `EXCEPTIONS.md`, `ONBOARDING.md`,
      `DEVELOPER_SETUP.md`, `AI_ASSISTANT_POLICY.md`, `OPERATIONS.md`, `SELF_DEFENCE.md`,
      `COMPLIANCE.md`, `THREAT_MODEL.md`
- [ ] `OPERATIONS.md` specifically needs, verbatim, three rules already paid for in debugging time:
      "never write a literal `${...}`-shaped string anywhere in a `.woodpecker.yml`, including
      comments, unless it's a value Woodpecker is meant to substitute" (SADR-0007); "every
      host-side call that can trigger a Gitea webhook — pushes **and** PR creation, not pushes
      alone — needs an explicit `Host: gitea:3500` header" (SADR-0008, re-confirmed and extended in
      SADR-0011 after forgetting it and hitting the same bug again); and "to script Woodpecker's
      API, use `GET /web-config.js` with a session cookie for a legitimately-issued CSRF token, then
      `POST /api/user/token` once for a durable PAT — never read the CSRF secret from the database"
      (SADR-0004 decision item 3, closed for real in SADR-0011).

## Operator experience / persistence (flagged critical in the build-ledger review)

- [ ] `scripts/quickstart.sh` does not exist — this is Milestone 1's own named acceptance test.
- [ ] `scripts/doctor.sh`, `e2e.sh`, `backup.sh`, `restore-drill.sh`, `evidence-export.sh` do not
      exist.
- [ ] Nothing persists evidence, history, or metrics yet. Prometheus/Grafana are named in DESIGN.md's
      `minimal` profile but not present in `compose/minimal/docker-compose.yml` —
      `ansible/playbooks/50-runtime.yml` says so explicitly rather than pretending to bring them up.
- [ ] The platform's own secrets (`.env`, `.gitea_admin_token`) are plaintext on disk. DESIGN.md's
      target for the minimal profile is sops + age-encrypted files, decrypted at deploy time by
      Ansible — not wired up anywhere in this repo yet (no age keys, no sops config exist). —
      SADR-0010, DESIGN.md Deployment profiles
- [ ] The platform has never scanned itself end-to-end (D6 dogfooding). SADR-0010 found this is
      currently a hollow claim even where attempted: Checkov has zero built-in policies for the
      `docker` Terraform provider's resource types, so a clean run there proves nothing. Dogfooding
      becomes meaningful once there's Kubernetes manifests, Dockerfiles, or cloud IaC for Checkov to
      actually evaluate.

## Open design questions (DESIGN.md)

- [ ] GitHub mirror: public, private, or drop entirely?
- [ ] Ollama model + host for framework §4's internal-first LLM review (own box vs. shared, model
      size vs. available RAM).
- [ ] Identity source: Gitea local accounts vs LDAP/OIDC — determines how "Security Officer" and
      "Engineering Lead" get established for two-party exception approval (Milestone 4).
- [ ] First real target repo to onboard once Milestone 3's enforcement is trustworthy enough for a
      pilot.

## Correctly not started yet (sequencing, not neglect)

- [ ] `kubernetes/` (Kyverno, Falco, Trivy Operator) — Milestone 6, after a real K8s target exists.
      Building it now would repeat the exact "untested speculative IaC" mistake SADR-0010 worked to
      avoid for cloud Terraform.
- [ ] `terraform/<cloud-provider>/` for real host provisioning (Hetzner/AWS/DO) — needs real
      credentials before writing anything; do not write untested cloud HCL. — SADR-0010
- [ ] DefectDojo / Dependency-Track / exceptions-repo materialization — Milestone 4.
- [ ] The sidecar (Go, stateless) — sticky PR comments, `/rescan` and `/exception` commands —
      Milestone 3/4. Its reconciliation-loop responsibility now has a working standalone
      implementation (`scripts/reconciliation-loop.py`, SADR-0015) to fold in rather than build from
      scratch; `bot-approver.py` (SADR-0011) is the other natural candidate to consolidate into it.
