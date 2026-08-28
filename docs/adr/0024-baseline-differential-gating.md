# SADR-0024: Baseline/differential gating, live-verified — and two bugs it took to get there

**Status:** Accepted, implemented, live-verified.

**Date:** 2026-08-28
**Milestone:** 3 -- closes `docs/what_next.md`'s Recommended-order item 4 ("the highest-leverage
thing achievable purely in software right now")
**Security & Privacy Impact:** High. This is the mechanism that lets a real, pre-existing repo be
onboarded without every PR being permanently blocked by history no one on that PR caused --
`DESIGN.md`'s own "rollout killer neither earlier review caught." A broken baseline either fails
open (silently drops a real finding into the baseline, no different from ignoring it) or fails
closed (blocks every PR forever on findings no PR introduced) -- both bugs found live below were the
second failure mode.

## Question

`policy-eval/evaluate-findings.py` judged every finding on its own merits, with no concept of "this
repo already had this problem before onboarding." `docs/what_next.md` named this the single highest-
leverage gap achievable without a second host. Build it: a platform-admin tool that snapshots a
repo's pre-existing findings at onboarding, a self-shrinking baseline file, and a gate that blocks
only *new* findings.

## What was built

- `scripts/generate-baseline.py` -- platform-admin tool, run against a real checkout before the
  fast-gate pipeline template is committed. Scans with Semgrep (vendored rules) and Trivy only --
  Gitleaks is never run here, since DESIGN.md's D7 rule ("secrets are never baselined") means a
  secret finding could never legitimately appear in the output regardless. Self-shrinking by
  construction: a refresh's new baseline is the *intersection* of the existing baseline and current
  findings, never anything added that wasn't already there.
- `policy-eval/evaluate-findings.py --baseline` -- splits findings into new (gates as before) and
  baselined (reported as pre-existing debt, never blocks, never silently discarded). Secrets bypass
  the baseline unconditionally, matching D7.
- `scripts/bot-approver.py` -- `.ssdlc/baseline.json` added to `DEFAULT_GATE_MANAGED_PATHS`. A PR
  cannot pad its own repo's baseline with its own new finding and expect the bot to miss the diff --
  the existing base-vs-head comparison protects it exactly like every other managed file.
- `scripts/onboard-repo.sh` -- generates the baseline from the repo's current state *before*
  committing any platform files, seeding a shrink-only refresh from any baseline already present.
- Unit-tested in isolation first (`tests/unit/test_evaluate_findings.py`,
  `tests/unit/test_generate_baseline.py`) -- 5 and 3 new checks respectively, all passing standalone
  with no Docker, before either live bug below was found.

## Bug 1, found live: the vendored-rules tree scans itself

`scripts/onboard-repo.sh` commits `policy/vendored-rules/` -- 1298 files, ~700 of them deliberately
vulnerable rule-test fixtures (`bash/ifs-tampering.bash`, `c/double-free.c`, and so on, one per
sibling rule, by design) -- into every onboarded repo's own `main` branch. `generate-baseline.py`,
the fast gate's `sast`/`dependencies` steps, and `gate-bundle/run-pilot-bundle.py` all then scanned
the checked-out repo wholesale. Once that tree is present, scanning "." or the workspace root scans
those fixtures too, as if they were the onboarded repo's own application code.

This is the same self-contamination class SADR-0018 already found and fixed for report filenames
(`gitleaks-report.json`, `semgrep-report.sarif`) written into the same shared workspace -- just a
target eleven times the size that was never checked against back then, because at the time SADR-0018
was written, no repo had yet been re-onboarded after `policy/vendored-rules/` was committed to it.

**Confirmed live, not inferred:** a direct `generate-baseline.py` run against a workspace that
already had these platform files committed (simulating a real second onboarding pass) produced
**2564 findings**, the overwhelming majority self-matches against `policy/vendored-rules/`'s own
fixtures -- e.g. `rules.bash.ifs-tampering:/src/policy/vendored-rules/bash/ifs-tampering.bash:2`,
`rules.c.double-free:/src/policy/vendored-rules/c/double-free.c:7`. No error, no crash -- silently
wrong output that looks like a real scan result.

**Fix:** exclude the platform's own managed paths (`scripts/bot-approver.py`'s
`DEFAULT_GATE_MANAGED_PATHS`/`DEFAULT_GATE_MANAGED_TREE_PREFIXES`, duplicated locally in each of the
three sites since they run in three different execution contexts that cannot share a Python import --
a committed pipeline YAML, a standalone Docker image that never copies `bot-approver.py` in, and a
standalone script) from every scan: `--exclude` per path for Semgrep, `--skip-files`/`--skip-dirs`
for Trivy. Applied in `pipelines/fast.woodpecker.yml` (both `sast` invocations and the `dependencies`
step), `gate-bundle/run-pilot-bundle.py`, and `scripts/generate-baseline.py`.

## Bug 2, found live: Trivy's native fingerprint is not what it claims to be

Even after fixing Bug 1, a baseline seeded with one real finding and refreshed against the identical,
unchanged workspace still shrank to zero. `normalise/trivy_adapter.py` had trusted Trivy's own
`Fingerprint` field directly (`v.get("Fingerprint") or ...`), documented as "a genuinely stable
native Fingerprint (a real SHA256 hash) -- no reconstruction needed."

**Confirmed live:** two Trivy scans of a workspace whose `requirements.txt` (and its
`pyyaml==5.3.1`/`CVE-2020-14343` finding) never changed produced two *different* `Fingerprint`
values for that same finding, once unrelated files elsewhere in the tree were added between the two
scans. Two further scans run back-to-back with *no* change at all between them produced the *same*
fingerprint both times -- so the field is stable within one repo state, but not across repo states
that don't touch the vulnerable manifest at all. The recorded fixture
(`normalise/testdata/trivy-sample.json`) explains why: Trivy's own report carries a whole-scan
`ArtifactID` alongside every per-finding `Fingerprint`, consistent with the fingerprint being derived
from something scan-wide rather than purely the finding's own identity.

For a one-off report this is invisible. For baseline/differential gating, where a finding's identity
must survive every *other* commit to the repo, it silently breaks the entire mechanism: a baselined
Trivy finding "disappears" (looks fixed) the moment anything else in the repo changes, then
reappears as "new" on the next scan after that -- blocking a PR for a pre-existing finding nobody
touched. This is precisely the rollout-killer `DESIGN.md` built this feature to prevent, reintroduced
by the fingerprint mechanism meant to serve it.

**Fix:** stop trusting Trivy's native `Fingerprint` field entirely. Construct the fingerprint from
`{VulnerabilityID}:{PkgName}:{target}` only -- CVE id, package name, manifest path, nothing scan-wide.

## Verification

Live, through the actual `generate-baseline.py` code path, not a fixture:

1. Clean workspace, `requirements.txt` with `pyyaml==5.3.1`, no platform files present -- fresh
   baseline: exactly 1 finding (`CVE-2020-14343`/trivy/critical).
2. Same workspace, platform-managed files added and committed (simulating a real second onboarding
   pass) -- **before either fix:** 2564 findings, dominated by vendored-rules self-matches.
3. **After Bug 1's fix, before Bug 2's fix:** noise gone, but the refresh still shrank the seeded
   1-finding baseline to 0 -- Bug 2, isolated and confirmed via the direct Trivy fingerprint
   comparison above.
4. **After both fixes:** fresh baseline against the platform-files-present workspace -- exactly 1
   finding, fingerprint `CVE-2020-14343:pyyaml:requirements.txt`. Refreshed again against the
   identical, unchanged workspace -- `1 findings, was 1`. Stable.
5. Full `tests/unit/` tier re-run clean (18 evaluate-findings checks including the 5 new
   baseline-specific ones, 3 generate-baseline checks, 17 normalise checks including the corrected
   Trivy fingerprint assertion, all other suites unaffected) -- one fixture in
   `tests/unit/test_evaluate_findings.py` needed its hardcoded baseline fingerprint updated to match
   the new constructed form; caught by the same test run, not a separate pass.

## Consequences

- Baseline/differential gating (`docs/what_next.md` item 4) is implemented and live-verified: a
  pre-existing finding does not block, a genuinely new finding still does, secrets are never
  baselined, and the baseline file itself is gate-managed against PR tampering.
- The two-party exception workflow named alongside it in `docs/what_next.md`'s item 4 is **not**
  part of this SADR -- it depends on the exceptions-repo infrastructure (Milestone 4, not built) and
  remains open in `docs/TODO.md`.
- Bug 1's fix closes a real, previously-live gap in the fast gate and the trusted-runner bundle, not
  just the new baseline tool -- both were scanning `policy/vendored-rules/`'s own fixtures on every
  onboarded repo's every run before this SADR, with no visible error. Any repo onboarded before this
  fix has noisy Semgrep/Trivy history worth disregarding.
- A reminder matching SADR-0018's own, now proven twice: whenever this platform's own committed
  files sit inside a workspace a scanner also treats as its target, check for self-contamination
  explicitly -- it does not announce itself as an error.
- A second, independent reminder this SADR adds: **do not trust a third-party tool's own "stable
  identifier" claim for cross-run identity without proving it across runs that vary something other
  than the field being identified.** A single-scan fixture cannot catch fingerprint instability by
  construction, since instability is a property of *pairs* of scans, not one.
