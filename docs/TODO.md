# Outstanding work

## Do next — Sprint 01 trusted pilot foundation

The ordered sprint is in [`SPRINT-01-TRUSTED-PILOT.md`](SPRINT-01-TRUSTED-PILOT.md), which tracks
finer-grained checkboxes than this section did — this list had drifted out of sync with it and is now
reconciled. Sprint 01 takes priority over feature expansion because it closes the current
trust-boundary gaps before a real developer team is onboarded. Genuinely outstanding, in priority
order:

## Review update — 2026-08-27

The working tree was clean at review time (HEAD `259292f`); 58 local unit
checks, Python compilation, `git diff --check`, and minimal Compose
configuration validation passed. These are the review findings that must take
priority over new features:

- [x] ~~Live-prove the vendored-Semgrep-rules protection before onboarding a
      pilot.~~ Done — `tests/regression/07-gate-contract-bypass.sh`'s scenario A
      now alters a vendored rule specifically (not `.woodpecker.yml`) and reran
      clean against a real Gitea+Woodpecker stack: 12/12 assertions, including
      confirming the PR's own pipeline genuinely reported `success` (so the
      rejection is provably the tree comparison firing, not an unrelated
      failure) and zero bot votes with the log naming the protected tree.
      — SADR-0017, SADR-0020
- [x] ~~Live-test onboarding for private repositories.~~ Done — a genuinely
      private repo (`private: true`, confirmed via the Gitea API response)
      onboarded cleanly, with the vendored-rules content verified actually
      present afterward (not just that the script exited 0). Re-ran onboarding
      against the same repo immediately after: `git diff --cached --quiet`
      correctly detected `policy/vendored-rules already up to date, nothing
      to commit`, Woodpecker activation and branch protection both handled
      the already-exists case cleanly. — `scripts/onboard-repo.sh`
- [x] ~~Align the trusted gate bundle with the approved fast-gate policy
      before enabling it.~~ Done — `gate-bundle/Dockerfile` and
      `run-pilot-bundle.py` use `policy/vendored-rules` (the same ruleset the
      fast gate uses), and the attestation path now verifies it specifically:
      `run-pilot-bundle.py` computes a content-based digest
      (`gate_contract.attestation.policy_digest`) over its own bundled
      `/opt/ssdlc/policy/` at scan time, carries it into the signed
      attestation via `trusted-gate-runner.py`, and `bot-approver.py` requires
      it to match an operator-configured `GATE_POLICY_DIGEST` — a bundle
      rebuild with a silently different ruleset now fails closed instead of
      going undetected. `scripts/print-policy-digest.py` computes the
      expected value. Live-verified against a real rebuilt image (found and
      fixed a real bug along the way: the Dockerfile never copied
      `gate_contract/` in at all, so the import would have crashed in
      production — no mocked unit test could have caught that). — SADR-0022
- [x] ~~Confirm Gitleaks' `detect` behaves correctly inside the gate bundle
      against a real PR checkout, not just a throwaway directory.~~ **Was
      real, not a test artifact — Critical severity.** Confirmed directly:
      `gitleaks detect` without `--no-git` scans **git commit history**, not
      working-tree files, and `trusted-gate-runner.py`'s `checkout_head()`
      (a Gitea archive-tarball extraction) produces a workspace with **no
      `.git` directory at all** — the same shape `git archive` itself
      produces. Without `.git`, gitleaks silently reported "0 commits
      scanned" and zero findings for a real, non-allowlisted AWS-shaped key,
      regardless of file content. The trusted-runner bundle's secret
      detection was completely, silently inert against its own real input
      shape — every attestation would have reported `"secrets": "success"`
      no matter what secrets were actually present. Fixed: `--no-git` added
      to `gate-bundle/run-pilot-bundle.py`'s gitleaks invocation only (the
      fast gate's own `secrets` step runs against a real `git clone` and is
      unaffected — adding the flag there would be a regression, not a fix).
      Live-verified against a real rebuilt image: the same key now correctly
      produces a blocking `CRITICAL [gitleaks/aws-access-token]` finding.
      Guarded by a new unit assertion. — SADR-0023
- [x] ~~Implement baseline/differential gating~~ — `docs/what_next.md`'s highest-leverage
      software-only item. `scripts/generate-baseline.py` (self-shrinking baseline, Semgrep +
      Trivy only, secrets never baselined per D7), `policy-eval/evaluate-findings.py --baseline`,
      `.ssdlc/baseline.json` added to `bot-approver.py`'s managed paths, wired into
      `onboard-repo.sh`. Found and fixed two real bugs live before calling this done: (1) all three
      scan sites (`generate-baseline.py`, the fast gate, `run-pilot-bundle.py`) were scanning the
      platform's own committed `policy/vendored-rules/` tree as if it were application code — same
      self-contamination class SADR-0018 found for report filenames, an eleven-times-larger target
      never checked against until now, confirmed live at 2564 spurious self-matches; (2) Trivy's
      native `Fingerprint` field, trusted directly by `normalise/trivy_adapter.py`, turned out to
      shift whenever an unrelated file elsewhere in the repo changed — the actual root cause of a
      baselined finding silently vanishing, now fixed by constructing the fingerprint from CVE id +
      package + manifest path only. Live-verified end to end: a seeded 1-finding baseline survives
      an unchanged-content refresh (`1 findings, was 1`) against a workspace shaped like a real
      second onboarding pass. The two-party exception workflow named alongside this in
      `docs/what_next.md`'s item 4 is separate, still open, and depends on Milestone 4's
      exceptions-repo infrastructure. — SADR-0024
- [ ] **Deploy and live-test the isolated trusted runner**, then enable
      `GATE_ATTESTATION_REQUIRED=1` only for the selected pilot repository.
      Prove normal merge, altered pipeline/policy rejection, missing
      attestation rejection, and a signed failing decision retained as audit
      evidence. The existing HMAC is a pilot bridge; replace it with an
      independently managed signing identity before production. — SADR-0017
- [ ] **Register and evidence the Trivy DB refresh cron.** The shared cache
      and refresh pipeline are implemented, but the Woodpecker cron job still
      requires deliberate registration and an owner/review cadence. — SADR-0021
- [ ] **Obtain licensing review for the OpenGrep-derived vendored rules**
      before broad rollout. Their internal-use position is documented but not
      legally approved. — SADR-0020

- [ ] **Replace PR-controlled gate files with a platform-controlled gate bundle/image** (the durable
      SADR-0017 design), or at minimum make `GATE_ATTESTATION_REQUIRED=1` the default rather than the
      transitional `GATE_CONTRACT_ENFORCE` byte-comparison. `scripts/trusted-gate-runner.py` and
      `gate-bundle/` are built and unit-tested; nothing is deployed because this environment has only
      one machine and the design specifically needs an isolated runner host. See
      [`GATE_CONTRACT.md`](GATE_CONTRACT.md).
- [x] ~~Consolidated regression cases for the Gate Contract specifically: altered pipeline, altered
      policy, stale approval, and an old bot approval~~ Done —
      `tests/regression/07-gate-contract-bypass.sh`, wired into `run-minimal-suite.sh`. Three
      scenarios, 12 assertions, all live against a real Gitea+Woodpecker stack: (A) a PR that alters
      `.woodpecker.yml` relative to its protected base gets zero bot votes even though its own
      self-controlled pipeline reports fully green — proving file-comparison, not pipeline trust, is
      what's actually doing the work; (B) a human approval stops satisfying the gate the instant a
      new commit supersedes its head, independent of Gitea's own `dismiss_stale_approvals`; (C) the
      same freshness requirement applied to the bot's own vote specifically — an old bot approval for
      a superseded head fails `verify-approvals.py`'s bot-specific check (not just a generic
      approval-count failure), and the bot does not get stuck refusing to ever vote again once the
      new head is green. Found and fixed two real bugs building it: `tests/regression/lib.sh`'s
      `woodpecker_get_pat` only handled a first-time OAuth consent, silently returning "User not
      authorized" against an already-authorized account (skip-consent redirect case, now handled);
      and a nested command-substitution pattern for repo activation silently swallowed a failure mode
      that a step-by-step version (matching `05-bot-approver.sh`'s own proven pattern) does not.
      "Missing report" and "forged status" were judged already adequately covered — the former by
      existing unit tests (`test_evaluate_findings.py`, `test_gate_attestation.py`), the latter live
      in SADR-0001 — and not duplicated here.
- [ ] An independently managed signing identity to replace `gate_contract/attestation.py`'s HMAC
      pilot bridge, once the runner above is deployed.
- [ ] Review and approve `FRAMEWORK-ADDENDUM-2026.md` as a formal enhancement to SSDLC Framework v1.1
      — written, not yet through any approval step.

Already done, despite this section previously listing them as outstanding:
- [x] ~~Create and approve the Trusted Gate Contract SADR~~ — [SADR-0017](adr/0017-trusted-gate-contract.md).
- [x] ~~Explicitly dismiss stale approvals on push~~ — `onboard-repo.sh` sets `dismiss_stale_approvals: true`.
- [x] ~~Require a current-head gate-bot verdict plus a distinct non-author human approval~~ —
      `verify-approvals.py` requires both when `GATE_BOT_LOGIN` is set; unit-tested.
- [x] ~~Prevent a PR from supplying the pipeline/policy/decision code that approves itself~~ —
      transitionally, via `bot-approver.py`'s protected-base byte-comparison
      (`GATE_CONTRACT_ENFORCE=1`, default on). The durable attestation path above is the remaining,
      not-yet-deployed half of this item — do not check off the harder item above by pointing at this one.
- [x] ~~Ship the pilot developer guidance and pre-commit setup~~ — [SECURITY.md](../SECURITY.md),
      `.pre-commit-config.yaml`, and now the full doc set under *Documentation debt* below.

## Existing implementation debt

Pulled from every SADR's own Decision section (docs/adr/0001–0018) and DESIGN.md's Open questions,
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
- [x] ~~Persist a shared Trivy DB volume + scheduled out-of-band refresh job, then re-add
      `--skip-db-update` to the `dependencies` step~~ Done -- a Terraform-managed volume
      (`ssdlc-<profile>-trivy-db-cache`) mounted at Trivy's own default cache path
      (`/root/.cache/trivy`) into **every** step container on the agent via
      `WOODPECKER_BACKEND_DOCKER_VOLUMES` (confirmed against Woodpecker's own source that this list
      is agent-level and applied unconditionally, not something a PR can influence). The
      `dependencies` step self-heals: `--skip-db-update` only when a cached DB is already present, so
      a fresh/empty volume still primes itself on its own first real use rather than requiring the
      refresh job to have already run. `pipelines/trivy-db-refresh.woodpecker.yml` added as the
      scheduled (`event: cron`) refresh job DESIGN.md calls for, matching `dast-scheduled.woodpecker.yml`'s
      precedent -- written and its own command live-tested standalone, but not yet registered as an
      actual Woodpecker cron job (that's a manual registration step, same as self-verify's own cron
      job needed in SADR-0016). Live-tested end to end through two real pipeline runs on an onboarded
      repo: confirmed via step logs that a primed cache produces zero DB-download network activity
      on both a push and a PR event. -- SADR-0021
- [x] ~~Vendor Semgrep rulesets into the policy repo instead of pulling live from the registry at
      scan time~~ Done, with a real detour — `policy/vendored-rules/` (594 files, secrets + core
      per-language security rules), live-tested through a real onboarded repo and PR (a real AWS key
      correctly blocked the merge). **Found live that Semgrep's own Registry rules cannot legally be
      vendored**: the Semgrep Rules License explicitly prohibits distributing them or making them
      available to others as a service, which this platform's whole purpose (serving every onboarded
      team) falls under. Sourced from `opengrep/opengrep-rules` instead (LGPL-2.1 + a Commons Clause
      restriction on reselling, not reviewed by counsel for this exact internal-use case — see
      `policy/vendored-rules/README.md`). `onboard-repo.sh` gained a `commit_directory` helper (a real
      git clone/push, not 594 Contents-API calls) to actually get this into onboarded repos. — SADR-0020
- [x] ~~Protect `policy/vendored-rules/` in the bot's protected-base comparison.~~
      `bot-approver.py` now compares one recursive Git Trees API result for
      each base/head revision, filtered to the vendor directory, and fails
      closed on a missing/truncated tree or any path/blob-SHA difference. Unit
      tests cover changed and truncated trees. The required live bypass proof
      remains at the top of this TODO. — SADR-0020, follow-up to SADR-0017
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

DESIGN.md's own repository structure names these. All eleven now exist:

- [x] ~~`docs/ARCHITECTURE.md`, `GATE_CONTRACT.md`, `POLICY.md`, `EXCEPTIONS.md`, `ONBOARDING.md`,
      `DEVELOPER_SETUP.md`, `AI_ASSISTANT_POLICY.md`, `OPERATIONS.md`, `SELF_DEFENCE.md`,
      `COMPLIANCE.md`, `THREAT_MODEL.md`~~ Written, each grounded directly in the current code and
      SADRs rather than restating DESIGN.md's target design — `ARCHITECTURE.md`, `GATE_CONTRACT.md`,
      and `THREAT_MODEL.md` distinguish live/tested mechanisms from built-not-deployed and
      not-yet-built ones explicitly (e.g. the trusted-runner attestation path, baseline gating,
      Prometheus metrics); `EXCEPTIONS.md` and `COMPLIANCE.md` are honest that most of their subject
      matter is designed, not built. Re-check each against `ARCHITECTURE.md`'s current-state table at
      the next milestone boundary — this list decays like PINNED_VERSIONS.md does.
- [x] ~~`OPERATIONS.md` specifically needs, verbatim, three rules already paid for in debugging
      time~~ Done — its "Rules learned from live incidents" section now states all three in full,
      each tied to its originating SADR(s).

## Operator experience / persistence (flagged critical in the build-ledger review)

- [x] ~~`scripts/quickstart.sh` does not exist~~ — it exists: preflight via `doctor.sh`, then
      Terraform apply, then `docker compose up -d`. This item was stale.
- [x] ~~`scripts/doctor.sh` does not exist~~ — it exists: read-only preflight (tool availability,
      Docker daemon reachability, `.env` placeholder check, `docker compose config` validation). Also
      stale.
- [ ] `e2e.sh`, `backup.sh`, `restore-drill.sh`, `evidence-export.sh` do not exist.
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
