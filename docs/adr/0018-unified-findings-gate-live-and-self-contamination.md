# SADR-0018: The unified findings gate, proven live end-to-end — and a self-contamination bug that would have permanently blocked every PR

**Status:** Confirmed by experiment: a real onboarded repo, a real PR carrying real Critical/High
findings, a real block, a real fix, and a real pass on the same PR after remediation
**Date:** 2026-08-26/27
**Milestone:** 3 — closes `docs/TODO.md`'s "unified findings gate ... still needs a live Woodpecker
run against real Gitleaks, Semgrep, and Trivy reports before it replaces the prior per-scanner
blocking behavior with confidence"
**Security & Privacy Impact:** High. This *is* the fast gate's actual block/pass decision for every
onboarded repo (DESIGN.md's architecture diagram: "policy-eval > Rego + baseline + exceptions repo —
THE EXIT CODE IS THE GATE"). The bug this SADR documents would have made that exit code permanently
`1` regardless of the scanned code — the opposite failure mode from a bypass, but just as disqualifying
for a gate meant to be trusted.

## Question

Three per-scanner `--exit-code`/`--error` flags (Gitleaks, Semgrep, Trivy) previously made the
block/pass call independently, with no shared severity model and no single reviewable policy —
exactly the "severity handling scattered across three tools' own flag choices" DESIGN.md's Policy
model section warns against. Built instead: `normalise/*_adapter.py` (one tool-specific adapter per
scanner, each producing the same finding schema), `policy/severity.rego` (one Rego policy enforcing
framework §2.4's SLA table — Critical blocks, High fails, Medium warns, Low logs), and
`policy-eval/evaluate-findings.py` (the evaluator: loads each report, normalizes, runs Conftest,
translates warnings/failures into exit 0/1/2). Unit-tested in isolation (`tests/unit/test_normalise.py`,
`test_evaluate_findings.py`, `policy/severity_test.rego`) before this SADR — 17, 8, and 5 checks
respectively, all passing standalone with no Docker. The open question this SADR answers: does it
actually work end-to-end, wired into `pipelines/fast.woodpecker.yml`, against real scanner output,
through a real PR, on a real onboarded repo — not fixtures?

## Method — live, through `pilot-app`, a real onboarded repo

Onboarded a fresh test repo (`pilot-app`) via `scripts/onboard-repo.sh` and opened a PR carrying two
genuine, deliberately introduced findings: a `pyyaml` CVE (Critical, via Trivy's SCA scan of
`requirements.txt`) and a `subprocess.run(..., shell=True)` call (High, via Semgrep's
`p/security-audit`, confirmed live to report `extra.severity: ERROR` — see
`normalise/semgrep_adapter.py`'s own comment on why Semgrep's severity scale has no Critical tier at
all and impact/likelihood/confidence metadata was checked and rejected as noisier than the tool's own
verdict).

**First live result:** `policy-eval-findings` exited 1, correctly identifying both findings by
severity, tool, rule, and location. Gitea branch protection then correctly rejected an attempted merge
of the still-red PR with HTTP 405 — the gate and the enforcement point both worked, independently
confirmed.

**Bug found live, not in a fixture:** the *next* pipeline run — after nothing in the actual scanned
code had changed — also failed, but for a reason with nothing to do with the pyyaml/subprocess
findings. Semgrep's `p/secrets` ruleset flagged a "detected Facebook OAuth access token" **inside
`semgrep-report.sarif` itself** — the SARIF file the *same* `sast` step had written moments earlier in
the same shared pipeline workspace, on its first (SARIF) invocation, before its second (JSON)
invocation re-scanned `.` and picked that file up as source. Separately, Trivy's filesystem scan
carries its own default secret detector and would have hit the same class of self-match against
`gitleaks-report.json` (written by the earlier `secrets` step into the same workspace) once Trivy's own
`--exit-code` was removed in favor of centralized gating (below) and its finding volume grew past what
`--severity=CRITICAL,HIGH` had been quietly filtering out.

This is not a one-off flaky finding: every onboarded repo's paved-road pipeline writes these same
report filenames into the same shared workspace, every run, in the same order. Once `sast`'s JSON
invocation (or `dependencies`) scans `.` including its own or a sibling step's prior output, the
self-match is deterministic and code-independent — a gate that would have reported `FAIL` on *every*
PR, forever, regardless of what a developer actually changed. Confirmed by reasoning through the
pipeline's own step order (`secrets` → `sast` → `dependencies`, each writing into the one shared
workspace all steps share) rather than assumed from the symptom alone.

**Fix**, in `pipelines/fast.woodpecker.yml`:
- Both `semgrep` invocations gained `--exclude=gitleaks-report.json` (the JSON run also excludes its
  own prior `--sarif` output: `--exclude=semgrep-report.sarif`).
- The `trivy fs` invocation gained `--skip-files=gitleaks-report.json,semgrep-report.sarif,semgrep-report.json`.

Excluding by literal filename is sufficient (not a per-repo ignore file) because these are this
platform's own paved-road report names, identical across every onboarded repo — not developer-authored
paths that could legitimately vary.

**Two other, related decisions folded into the same pipeline change, both already true architecturally
and now confirmed live together:**
1. Each scanner's own `--exit-code`/`--error` flag was removed (`gitleaks --exit-code=0`, semgrep's
   JSON run dropped `--error`, `trivy fs --exit-code=0`). Each step's job is now only to *produce* its
   report file; `policy-eval-findings` is the sole block/pass decision, evaluated against one
   reviewable policy instead of three scattered flags.
2. Trivy's `--severity=CRITICAL,HIGH` filter was removed too — `policy-eval-findings` needs Medium/Low
   findings to warn and track them per framework §2.4's table, not have them silently discarded before
   the evaluator ever sees them.

**Re-verified after the fix, same PR, no scanned-code changes beyond the pipeline template itself:**
pushed a fresh empty commit (the `git commit --allow-empty` recovery pattern SADR-0015 established,
needed here because a Docker Desktop engine restart mid-session orphaned the in-flight pipeline run —
`docker compose ps` showed every container `Exited (255)` at the same timestamp, and the Woodpecker
server's own logs recorded `queue: task not found` / `stream: not found` for the orphaned pipeline IDs
on restart, confirming it does not auto-resume a run an engine restart interrupted). The retriggered
run:

```
GET .../commits/<head>/status
  state: success
    ssdlc/security-gate/push/woodpecker -> success  (pipeline #30)

GET .../pulls/1
  mergeable: true
  merged: false
```

The same PR, same underlying Critical/High findings never removed from the branch, now passes —
because the fix targeted the self-contamination false positive specifically, not the severity
threshold that correctly caught the real findings in the first run.

## Decision

1. **The unified findings gate is confirmed live**, not just unit-tested: `docs/TODO.md`'s item is
   closed. Real Gitleaks/Semgrep/Trivy output, through the real adapters, through the real Rego policy,
   through a real PR that was correctly blocked and — once actually fixed — correctly passed.
2. **Excluding a pipeline's own report-file names from every scanner invocation that scans `.` is now
   a required pattern for this project**, not just this one instance. Any future scanner step added to
   `pipelines/*.woodpecker.yml` that writes a report into the shared workspace before a later step
   scans the same tree needs the same treatment, or it reintroduces this exact bug under a new
   filename.
3. **Numbering correction, recorded here rather than silently fixed**: this work's own code comments
   (written before this SADR existed) cite `docs/adr/0016` throughout — `normalise/*_adapter.py`,
   `policy/severity.rego`, `policy-eval/evaluate-findings.py`, `pipelines/fast.woodpecker.yml`,
   `tests/unit/test_normalise.py`. A concurrent session claimed 0016 for a different topic
   (self-verify CI wiring) and committed it first. All six files' comments were corrected to
   `docs/adr/0018` to match this document; `pipelines/self-verify.woodpecker.yml` and
   `tests/unit/test_policy_eval.py`'s own `docs/adr/0016` references were left alone — checked
   individually and confirmed they genuinely are about SADR-0016's topic, not a second instance of the
   same mislabeling.
4. **Baseline/differential gating remains explicitly unbuilt**, same honest gap `policy/severity.rego`
   and `evaluate-findings.py`'s own docstrings already state: every finding is judged on its own merits
   regardless of whether it's newly introduced or pre-existing in an onboarded legacy repo, pending the
   exceptions-repo infrastructure (Milestone 4).
5. **Fail-closed on evaluation failure (exit 2) is unit-tested but was not separately live-triggered**
   in this pass — a genuinely malformed scanner report or a Conftest crash during a real pipeline run
   is a natural follow-up live check, not assumed proven by the unit tests alone.

## Reproduce it yourself

```
cd compose/minimal && docker compose up -d
# (terraform apply first if the network/volumes don't exist yet — docs/adr/0014)
GITEA_ADMIN_TOKEN=... WOODPECKER_TOKEN=... scripts/onboard-repo.sh <owner>/pilot-app
# push a branch with a real pyyaml CVE in requirements.txt and a
# subprocess.run(..., shell=True) call; open a PR
# -> policy-eval-findings exits 1, branch protection rejects merge (405)
# fix the underlying findings for real, or (to reproduce the bug this SADR
# documents) push ANY unrelated change and watch sast/dependencies self-match
# on gitleaks-report.json / semgrep-report.sarif written by earlier steps
GET .../repos/<owner>/pilot-app/commits/<head>/status
GET .../repos/<owner>/pilot-app/pulls/<n>   # mergeable: true once genuinely fixed
```
