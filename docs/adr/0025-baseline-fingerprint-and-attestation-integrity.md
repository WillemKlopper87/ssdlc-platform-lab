# SADR-0025: Three real bugs an external code report found in SADR-0024's own work

**Status:** Accepted, implemented, live-verified.

**Date:** 2026-08-28
**Milestone:** 3 -- fixes issues in `SSDLC_Code_report.md`'s critical/high-priority sections (H1, H2,
H3), and one the report itself missed
**Security & Privacy Impact:** High. H1 could have silently converted a shrink-only baseline refresh
into a fresh full-acceptance snapshot. The Semgrep bug (not in the report) meant SADR-0024's
baseline/differential gating never actually worked for Semgrep findings at all -- only Trivy's
survived by coincidence. H2/H3 mean the durable trusted-runner path (not yet deployed) would have
signed inaccurate attestations the moment it was turned on.

## Question

An external review (`SSDLC_Code_report.md`, dated the same day as SADR-0024) re-examined the
baseline/differential-gating feature this platform had just shipped and live-verified. It found two
concrete integrity gaps in code SADR-0024 itself introduced. Independently, verifying the report's
claims against source surfaced a third, more severe bug the report's own live testing had not
happened to trigger (its baseline test apparently only exercised a Trivy finding, the same blind spot
SADR-0024's own first verification pass had). Are all three real, and what's the fix?

## Bug 1 (found independently, not in the report): Semgrep fingerprints never matched across scan sites

`normalise/semgrep_adapter.py` built its fingerprint from Semgrep's own `path` field directly. That
field is not a property of the file -- it's whatever string Semgrep was told to scan. Confirmed live:
scanning an absolute container path (`docker run ... IMAGE semgrep ... /src`, what
`scripts/generate-baseline.py` and `gate-bundle/run-pilot-bundle.py` both do) returns
`path: "/src/app.py"`; scanning `.` (what `pipelines/fast.woodpecker.yml`'s own `sast` step does)
returns `path: "app.py"`. Same finding, same file, two different fingerprints depending only on which
of the three scan sites produced it.

**Consequence:** every Semgrep finding baselined at onboarding still blocked in the fast gate,
unconditionally, on every single PR -- the exact "rollout killer" the whole feature exists to
prevent, reintroduced for one of the two scanners the feature covers. SADR-0024's own live
verification used a `requirements.txt`/Trivy finding exclusively; Trivy's `Target` field happens to
already be scan-root-relative regardless of invocation form, so this never showed up. A finding this
severe surviving the first SADR's own "live proof" discipline is itself the lesson: **one passing
live test against one tool is not proof the mechanism works for every tool it claims to cover.**

**Fix:** `semgrep_adapter.normalize(results, root=None)` now strips an absolute `path` down to
`root`-relative before it ever reaches the fingerprint, using `posixpath` explicitly (not `os.path` --
found live that `os.path.isabs("/src/app.py")` is `False` under Windows' `ntpath`, since Semgrep's
path is always POSIX-style regardless of what host OS happens to be running this adapter; the bug
this guard is meant to catch would have silently never fired in Windows-side testing otherwise).
`evaluate-findings.py` gained `--semgrep-root`; the two absolute-target call sites
(`generate-baseline.py`, `run-pilot-bundle.py`) now pass their own scan root explicitly. The fast
gate's own invocation needs no flag -- its `.` target already returns relative paths, matching the
new default.

## Bug 2 (report's H1): baseline refresh could silently discard the shrink-only guarantee

`scripts/generate-baseline.py::load_existing()` returned `None` for a missing file **and** for one
that existed but couldn't be parsed. `main()` treated `None` as "no baseline yet, create a fresh
full-acceptance snapshot" -- meaning a corrupt existing baseline (bad base64 in `onboard-repo.sh`, a
transient API hiccup, truncated content) silently became "accept every current finding as debt,"
asymmetric with `evaluate-findings.py`'s own handling of the same file at evaluation time (there,
unreadable correctly means "trust nothing," the safe direction for a step that blocks on mismatch --
not one about to overwrite the file).

**Fix:** `load_existing()` now raises `BaselineUnreadable` for anything that exists but fails to
parse or validate; `main()` fails closed (exit 2, file left untouched) rather than falling back to a
fresh snapshot. First-time creation requires an explicit `--create` flag -- `mktemp`'s own behavior
(a bare `mktemp` always creates an empty file) meant `onboard-repo.sh`'s prior version made every
first onboarding look identical to "an unreadable existing file," which is exactly the ambiguity this
fix needs to not silently paper over.

`onboard-repo.sh` itself needed a matching fix, not just the Python side: it checks the Gitea Contents
API's **HTTP status** explicitly now -- `404` means genuinely absent (`--create`), `200` decodes the
real content (no `--create`, no `|| true` swallowing a decode failure), anything else aborts the
onboarding run outright rather than guessing. The base64-decode step passes its response file as a
Python `sys.argv` element, not interpolated into the `-c` script text -- found live that the
interpolated form breaks specifically when this script is run from Windows Git Bash (a real
dev/test path for this project, even though production always runs it on Linux), since Git Bash's
own POSIX-to-Windows path translation only applies to arguments it passes to an invoked program, not
to text embedded inside another argument's string content.

## Bug 3 (report's H2 + H3): the trusted bundle could sign an inaccurate attestation

**H2.** `gate-bundle/run-pilot-bundle.py` wrote `"scanners": {"secrets": "success", ...}` the moment
each scanner's own process exited 0 -- never checking the report file it was supposed to produce
actually existed, was non-empty, or matched that scanner's real top-level JSON shape. A scanner that
exited 0 without writing a report (a regression, an unexpected filesystem interaction) would be
signed as successful regardless.

**H3.** The same script's call into `evaluate-findings.py` passed no `--baseline`, so the default
relative `.ssdlc/baseline.json` resolved against whatever the bundle's own working directory happened
to be -- not the workspace it just scanned. Combined with Bug 1's `--semgrep-root` gap (this bundle's
Semgrep invocation also scans an absolute `str(workspace)` target), a real bundle run would have
evaluated every inherited finding as new, in both scanners.

**Fix:** `validate_report(path, kind)` requires the file to exist, be non-empty, parse as JSON, and
match the specific top-level shape each scanner always produces (`gitleaks`: a list; `semgrep`: an
object with a `results` list; `trivy`: an object whose `Results` is a list or absent). The `scanners`
dict in `result.json` now reflects this per-scanner, not a hardcoded literal; `trusted-gate-runner.py`'s
existing `issue()` check (`result["scanners"].get(scanner) != "success"` blocks attestation) already
does the right thing once given an honest input -- no change needed there. `result.json` also now
carries a `report_digests` object (SHA-256 per validated report), giving the signed attestation
something to bind against beyond "a process exited 0." The `evaluate-findings.py` call gained explicit
`--baseline` (pointed at the workspace's own file) and `--semgrep-root` (the workspace path).

## Verification

Live, not just unit-tested:

- **Bug 1**: ran Semgrep against identical content two ways (absolute `/src` target vs. `.` from
  inside the mounted directory) and confirmed the normalized fingerprint is now byte-identical
  (`rules.python.audit.subprocess-shell-true:app.py:2` both times). Ran the real
  `generate-baseline.py` end to end against a workspace with a genuine Semgrep-detectable finding
  (`subprocess.call(..., shell=True)`) -- baseline correctly records `app.py:2`, not `/src/app.py:2`.
- **Bug 2**: isolated the new HTTP-status branching logic from `onboard-repo.sh` and ran all four
  cases directly -- 404 (create), 200-valid (decodes correctly), 200-malformed (aborts, exit 1),
  500 (aborts with a named error, exit 1) -- all behaved as designed. `tests/unit/test_generate_baseline.py`
  gained three new checks: a corrupt existing baseline raises rather than returning `None`, `main()`
  refuses to overwrite a corrupt file, and `main()` refuses first creation without `--create`.
- **Bug 3**: `tests/unit/test_gate_bundle.py` gained two new checks -- a scanner that "succeeds" with
  no report on disk is never labelled `success`, and a report with the wrong top-level shape is
  rejected the same way. The existing pass test now also asserts `--baseline`/`--semgrep-root` are
  present in the evaluator subprocess call with the correct workspace-derived values.
- Full `tests/unit/` tier re-run clean: 88 checks across 10 files plus the Rego suite, all passing.

## Consequences

- Baseline/differential gating (SADR-0024) now actually covers Semgrep, not only Trivy -- this SADR
  is the point at which that claim becomes true, not SADR-0024 itself.
- First-time baseline creation is now an explicit, deliberate action (`--create`) at every call site,
  not an ambiguous fallback shared with "corrupt file."
- The durable trusted-runner path (still not deployed -- SADR-0017's own gap) would have produced
  attestations that lied about scanner success and ignored the repository's own baseline, on day one
  of being turned on. Both are closed now, before that deployment happens, not discovered after.
- A repeat of this project's own established lesson (SADR-0018's self-contamination, SADR-0023's
  inert Gitleaks): a mechanism proven with **one** live example does not prove the mechanism for
  every case it claims to cover. `docs/adr/0024`'s live verification used Trivy only; this SADR exists
  because an external review re-checked that claim rather than trusting it. Future features spanning
  multiple tools should live-verify each tool independently, not just one representative of the group.
