#!/usr/bin/env python3
"""Regression coverage for policy-eval/evaluate-findings.py fail-closed paths."""
import os
import subprocess
import sys
import tempfile

ROOT_DIR = os.path.join(os.path.dirname(__file__), "..", "..")
SCRIPT = os.path.join(ROOT_DIR, "policy-eval", "evaluate-findings.py")
FIXTURES = os.path.join(ROOT_DIR, "normalise", "testdata")
PASS_COUNT = 0
FAIL_COUNT = 0


def assert_eq(actual, expected, description):
    global PASS_COUNT, FAIL_COUNT
    if actual == expected:
        print(f"  PASS: {description} (got: {actual!r})")
        PASS_COUNT += 1
    else:
        print(f"  FAIL: {description} -- expected {expected!r}, got {actual!r}", file=sys.stderr)
        FAIL_COUNT += 1


def run_evaluator(conftest_body, *report_args):
    """Run the real evaluator with a temporary Conftest executable shim."""
    with tempfile.TemporaryDirectory() as temp_dir:
        conftest = os.path.join(temp_dir, "conftest")
        with open(conftest, "w", newline="\n") as fh:
            fh.write("#!/usr/bin/env python3\n")
            fh.write(conftest_body)
        os.chmod(conftest, 0o755)
        # Windows does not execute an extensionless shebang script via PATH.
        # The CI container uses the extensionless file above; this tiny .cmd
        # launcher lets the same test run on a Windows developer workstation.
        if os.name == "nt":
            with open(os.path.join(temp_dir, "conftest.cmd"), "w", newline="\r\n") as fh:
                fh.write("@echo off\r\n")
                fh.write(f'"{sys.executable}" "%~dp0conftest" %*\r\n')
        env = {
            **os.environ,
            "PATH": temp_dir + os.pathsep + os.environ.get("PATH", ""),
            "CONFTEST_BIN": "conftest.cmd" if os.name == "nt" else "conftest",
        }
        return subprocess.run(
            [sys.executable, SCRIPT, *report_args], env=env, capture_output=True, text=True, timeout=10,
        )


BLOCKING_SHIM = '''import json, sys
findings = json.load(open(sys.argv[-1]))
failures = [{"msg": "%s [%s/%s]" % (f["severity"].upper(), f["tool"], f["rule_id"])}
            for f in findings if f["severity"] in ("critical", "high")]
warnings = [{"msg": "MEDIUM [%s/%s]" % (f["tool"], f["rule_id"])}
            for f in findings if f["severity"] == "medium"]
print(json.dumps([{"failures": failures, "warnings": warnings}]))
sys.exit(1 if failures else 0)
'''


def test_critical_finding_blocks():
    print("=== critical gitleaks finding blocks through the real evaluator ===")
    result = run_evaluator(BLOCKING_SHIM, "--gitleaks", os.path.join(FIXTURES, "gitleaks-sample.json"))
    assert_eq(result.returncode, 1, "critical finding returns the blocking exit code")
    assert_eq("CRITICAL [gitleaks/aws-access-token]" in result.stdout, True, "failure identifies tool and rule")


def test_medium_finding_warns_but_passes():
    print("=== medium-only Semgrep finding warns without blocking ===")
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as fh:
        fh.write('{"results": [{"check_id": "rule", "path": "app.py", "start": {"line": 1}, "extra": {"severity": "WARNING", "message": "warning"}}]}')
        report = fh.name
    try:
        result = run_evaluator(BLOCKING_SHIM, "--semgrep", report)
    finally:
        os.unlink(report)
    assert_eq(result.returncode, 0, "medium-only finding does not block")
    assert_eq("WARN  MEDIUM [semgrep/rule]" in result.stdout, True, "medium finding is surfaced as a warning")


def test_conftest_crash_fails_closed():
    print("=== a Conftest crash fails closed even if it prints JSON ===")
    crash_shim = "import json, sys\nprint(json.dumps([]))\nsys.exit(2)\n"
    result = run_evaluator(crash_shim, "--gitleaks", os.path.join(FIXTURES, "gitleaks-sample.json"))
    assert_eq(result.returncode, 2, "unexpected Conftest exit returns fatal exit code")
    assert_eq("conftest exited 2" in result.stderr, True, "fatal log names the failed evaluator")


def test_malformed_report_fails_closed():
    print("=== malformed scanner JSON fails closed with a clear error ===")
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as fh:
        fh.write("not-json")
        report = fh.name
    try:
        result = run_evaluator(BLOCKING_SHIM, "--gitleaks", report)
    finally:
        os.unlink(report)
    assert_eq(result.returncode, 2, "malformed report returns fatal exit code")
    assert_eq("could not read or normalize scanner report" in result.stderr, True, "fatal log identifies report handling")


def _write_json(content):
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as fh:
        fh.write(content)
        return fh.name


TRIVY_CRITICAL = '{"Results": [{"Target": "requirements.txt", "Vulnerabilities": [{"VulnerabilityID": "CVE-2099-1", "Severity": "CRITICAL", "PkgName": "pyyaml", "InstalledVersion": "5.3.1", "Title": "test", "Fingerprint": "test-fp-1"}]}]}'


def test_baselined_finding_does_not_block():
    print("=== a finding matching the baseline is pre-existing debt, not a block ===")
    trivy_report = _write_json(TRIVY_CRITICAL)
    # docs/adr/0024 verification: trivy_adapter.py no longer trusts the
    # report's own "Fingerprint" field (found unstable across scans --
    # see normalise/trivy_adapter.py's docstring), so the baseline fixture
    # must match the adapter's constructed "rule_id:pkg:target" form instead.
    baseline = _write_json(
        '{"schema_version": 1, "findings": [{"tool": "trivy", "fingerprint": "CVE-2099-1:pyyaml:requirements.txt", "rule_id": "CVE-2099-1", "severity": "critical"}]}'
    )
    try:
        result = run_evaluator(BLOCKING_SHIM, "--trivy", trivy_report, "--baseline", baseline)
    finally:
        os.unlink(trivy_report)
        os.unlink(baseline)
    assert_eq(result.returncode, 0, "a baselined Critical finding does not block")
    assert_eq("BASELINE  [trivy/CVE-2099-1]" in result.stdout, True, "the baselined finding is still surfaced, as debt")
    assert_eq("FAIL" in result.stdout, False, "no FAIL line is printed for a baselined finding")


def test_new_finding_blocks_despite_unrelated_baseline():
    print("=== a genuinely new finding still blocks even with an unrelated baseline present ===")
    trivy_report = _write_json(TRIVY_CRITICAL)
    baseline = _write_json(
        '{"schema_version": 1, "findings": [{"tool": "trivy", "fingerprint": "some-other-fp", "rule_id": "CVE-0000-0", "severity": "critical"}]}'
    )
    try:
        result = run_evaluator(BLOCKING_SHIM, "--trivy", trivy_report, "--baseline", baseline)
    finally:
        os.unlink(trivy_report)
        os.unlink(baseline)
    assert_eq(result.returncode, 1, "a finding not in the baseline still blocks")
    assert_eq("CRITICAL [trivy/CVE-2099-1]" in result.stdout, True, "the new finding is reported as a failure")


def test_secret_blocks_even_if_present_in_baseline():
    print("=== DESIGN.md D7: a secret blocks regardless of what the baseline file claims ===")
    gitleaks_report = os.path.join(FIXTURES, "gitleaks-sample.json")
    import json as _json
    with open(gitleaks_report, encoding="utf-8") as fh:
        sample_fingerprint = _json.load(fh)[0]["Fingerprint"]
    baseline = _write_json(
        _json.dumps({"schema_version": 1, "findings": [{"tool": "gitleaks", "fingerprint": sample_fingerprint, "rule_id": "x", "severity": "critical"}]})
    )
    try:
        result = run_evaluator(BLOCKING_SHIM, "--gitleaks", gitleaks_report, "--baseline", baseline)
    finally:
        os.unlink(baseline)
    assert_eq(result.returncode, 1, "a secret still blocks even when its fingerprint is listed in the baseline")
    assert_eq("BASELINE" in result.stdout, False, "a secret is never printed as baseline debt")


def test_malformed_baseline_is_not_fatal():
    print("=== a malformed baseline file is treated as empty, not a crash ===")
    trivy_report = _write_json(TRIVY_CRITICAL)
    baseline = _write_json("not-json")
    try:
        result = run_evaluator(BLOCKING_SHIM, "--trivy", trivy_report, "--baseline", baseline)
    finally:
        os.unlink(trivy_report)
        os.unlink(baseline)
    assert_eq(result.returncode, 1, "malformed baseline still evaluates the finding as new (exit 1, not 2)")
    assert_eq("treating as empty" in result.stderr, True, "the malformed baseline is logged, not silently ignored")


def test_missing_baseline_file_matches_prior_behaviour():
    print("=== no baseline file at all -- identical to this script's pre-baseline behaviour ===")
    result = run_evaluator(BLOCKING_SHIM, "--gitleaks", os.path.join(FIXTURES, "gitleaks-sample.json"), "--baseline", "/no/such/file.json")
    assert_eq(result.returncode, 1, "a missing baseline file blocks exactly as if no baseline feature existed")


import json as _json_for_exceptions


def _write_exceptions_dir(records):
    """records: list of dicts, each written as its own <n>.json file in a
    fresh temp directory. Returns the directory path (caller cleans it up)."""
    import shutil
    exceptions_dir = tempfile.mkdtemp()
    for i, record in enumerate(records):
        with open(os.path.join(exceptions_dir, f"record-{i}.json"), "w", encoding="utf-8") as fh:
            _json_for_exceptions.dump(record, fh)
    return exceptions_dir


def test_approved_unexpired_exception_suppresses_matching_finding():
    print("=== an approved, unexpired exception for this repo suppresses its matching finding ===")
    trivy_report = _write_json(TRIVY_CRITICAL)
    exceptions_dir = _write_exceptions_dir([{
        "repo": "gateadmin/gate-demo",
        "finding_fingerprint": "CVE-2099-1:pyyaml:requirements.txt",
        "severity": "critical",
        "expiry": "2099-01-01T00:00:00Z",
        "requester": "alice",
        "approvers": ["bob"],
        "ticket": "TICKET-1",
        "approved": True,
    }])
    try:
        result = run_evaluator(
            BLOCKING_SHIM, "--trivy", trivy_report,
            "--exceptions-dir", exceptions_dir, "--repo", "gateadmin/gate-demo",
        )
    finally:
        os.unlink(trivy_report)
        import shutil as _shutil
        _shutil.rmtree(exceptions_dir)
    assert_eq(result.returncode, 0, "an excepted Critical finding does not block")
    assert_eq("EXCEPTION  [trivy/CVE-2099-1]" in result.stdout, True, "the excepted finding is still surfaced, as an exception")
    assert_eq("FAIL" in result.stdout, False, "no FAIL line is printed for an excepted finding")


def test_exception_for_different_repo_does_not_suppress():
    print("=== an exception filed for a different repo does not suppress this repo's finding ===")
    trivy_report = _write_json(TRIVY_CRITICAL)
    exceptions_dir = _write_exceptions_dir([{
        "repo": "gateadmin/some-other-repo",
        "finding_fingerprint": "CVE-2099-1:pyyaml:requirements.txt",
        "severity": "critical",
        "expiry": "2099-01-01T00:00:00Z",
        "requester": "alice",
        "approvers": ["bob"],
        "ticket": "TICKET-1",
        "approved": True,
    }])
    try:
        result = run_evaluator(
            BLOCKING_SHIM, "--trivy", trivy_report,
            "--exceptions-dir", exceptions_dir, "--repo", "gateadmin/gate-demo",
        )
    finally:
        os.unlink(trivy_report)
        import shutil as _shutil
        _shutil.rmtree(exceptions_dir)
    assert_eq(result.returncode, 1, "a same-fingerprint exception for a DIFFERENT repo does not suppress this finding")
    assert_eq("CRITICAL [trivy/CVE-2099-1]" in result.stdout, True, "the finding is still reported as a failure")


def test_expired_exception_does_not_suppress():
    print("=== an expired exception no longer suppresses its finding ===")
    trivy_report = _write_json(TRIVY_CRITICAL)
    exceptions_dir = _write_exceptions_dir([{
        "repo": "gateadmin/gate-demo",
        "finding_fingerprint": "CVE-2099-1:pyyaml:requirements.txt",
        "severity": "critical",
        "expiry": "2020-01-01T00:00:00Z",
        "requester": "alice",
        "approvers": ["bob"],
        "ticket": "TICKET-1",
        "approved": True,
    }])
    try:
        result = run_evaluator(
            BLOCKING_SHIM, "--trivy", trivy_report,
            "--exceptions-dir", exceptions_dir, "--repo", "gateadmin/gate-demo",
        )
    finally:
        os.unlink(trivy_report)
        import shutil as _shutil
        _shutil.rmtree(exceptions_dir)
    assert_eq(result.returncode, 1, "an expired exception blocks exactly as if it didn't exist")


def test_unapproved_exception_does_not_suppress():
    print("=== a pending (not yet approved) exception does not suppress its finding ===")
    trivy_report = _write_json(TRIVY_CRITICAL)
    exceptions_dir = _write_exceptions_dir([{
        "repo": "gateadmin/gate-demo",
        "finding_fingerprint": "CVE-2099-1:pyyaml:requirements.txt",
        "severity": "critical",
        "expiry": "2099-01-01T00:00:00Z",
        "requester": "alice",
        "approvers": [],
        "ticket": "TICKET-1",
        "approved": False,
    }])
    try:
        result = run_evaluator(
            BLOCKING_SHIM, "--trivy", trivy_report,
            "--exceptions-dir", exceptions_dir, "--repo", "gateadmin/gate-demo",
        )
    finally:
        os.unlink(trivy_report)
        import shutil as _shutil
        _shutil.rmtree(exceptions_dir)
    assert_eq(result.returncode, 1, "a pending exception blocks exactly as if it didn't exist")


def test_secret_blocks_despite_matching_exception():
    print("=== DESIGN.md D7: a secret blocks regardless of an approved exception record ===")
    gitleaks_report = os.path.join(FIXTURES, "gitleaks-sample.json")
    with open(gitleaks_report, encoding="utf-8") as fh:
        sample_fingerprint = _json_for_exceptions.load(fh)[0]["Fingerprint"]
    exceptions_dir = _write_exceptions_dir([{
        "repo": "gateadmin/gate-demo",
        "finding_fingerprint": sample_fingerprint,
        "severity": "critical",
        "expiry": "2099-01-01T00:00:00Z",
        "requester": "alice",
        "approvers": ["bob"],
        "ticket": "TICKET-1",
        "approved": True,
    }])
    try:
        result = run_evaluator(
            BLOCKING_SHIM, "--gitleaks", gitleaks_report,
            "--exceptions-dir", exceptions_dir, "--repo", "gateadmin/gate-demo",
        )
    finally:
        import shutil as _shutil
        _shutil.rmtree(exceptions_dir)
    assert_eq(result.returncode, 1, "a secret still blocks even with a matching approved exception record")
    assert_eq("EXCEPTION" in result.stdout, False, "a secret is never printed as an exception")


def test_missing_exceptions_dir_is_not_fatal():
    print("=== no --exceptions-dir at all -- identical to this script's pre-exceptions behaviour ===")
    result = run_evaluator(BLOCKING_SHIM, "--gitleaks", os.path.join(FIXTURES, "gitleaks-sample.json"))
    assert_eq(result.returncode, 1, "no exceptions-dir configured blocks exactly as if the feature didn't exist")


def test_malformed_exception_record_is_not_fatal():
    print("=== one malformed exception record is skipped, not fatal, and the rest still apply ===")
    trivy_report = _write_json(TRIVY_CRITICAL)
    exceptions_dir = _write_exceptions_dir([{"not": "a valid record"}])
    with open(os.path.join(exceptions_dir, "corrupt.json"), "w", encoding="utf-8") as fh:
        fh.write("not-json-at-all")
    try:
        result = run_evaluator(
            BLOCKING_SHIM, "--trivy", trivy_report,
            "--exceptions-dir", exceptions_dir, "--repo", "gateadmin/gate-demo",
        )
    finally:
        os.unlink(trivy_report)
        import shutil as _shutil
        _shutil.rmtree(exceptions_dir)
    assert_eq(result.returncode, 1, "malformed exception records are skipped -- finding still evaluates as blocking")


def main():
    test_critical_finding_blocks()
    test_medium_finding_warns_but_passes()
    test_conftest_crash_fails_closed()
    test_malformed_report_fails_closed()
    test_baselined_finding_does_not_block()
    test_new_finding_blocks_despite_unrelated_baseline()
    test_secret_blocks_even_if_present_in_baseline()
    test_malformed_baseline_is_not_fatal()
    test_missing_baseline_file_matches_prior_behaviour()
    test_approved_unexpired_exception_suppresses_matching_finding()
    test_exception_for_different_repo_does_not_suppress()
    test_expired_exception_does_not_suppress()
    test_unapproved_exception_does_not_suppress()
    test_secret_blocks_despite_matching_exception()
    test_missing_exceptions_dir_is_not_fatal()
    test_malformed_exception_record_is_not_fatal()
    print(f"\n=== evaluate-findings unit summary: {PASS_COUNT} passed, {FAIL_COUNT} failed ===")
    return 0 if FAIL_COUNT == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
