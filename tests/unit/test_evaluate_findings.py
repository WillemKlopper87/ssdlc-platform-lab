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


def main():
    test_critical_finding_blocks()
    test_medium_finding_warns_but_passes()
    test_conftest_crash_fails_closed()
    test_malformed_report_fails_closed()
    print(f"\n=== evaluate-findings unit summary: {PASS_COUNT} passed, {FAIL_COUNT} failed ===")
    return 0 if FAIL_COUNT == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
