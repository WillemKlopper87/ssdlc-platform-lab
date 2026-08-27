#!/usr/bin/env python3
"""
policy-eval/evaluate-findings.py

The severity-threshold gate DESIGN.md's roadmap calls "the project" for
Milestone 3: reads each scanner's raw report, normalizes it through
normalise/*_adapter.py (docs/adr/0018), and evaluates the combined
finding list against policy/severity.rego via Conftest -- one reviewable
policy file, not blocking logic scattered across each scanner's own
--exit-code flag the way pipelines/fast.woodpecker.yml did before this
existed.

Exit 0: no Critical/High findings (Medium/Low are logged, not blocking,
        per framework §2.4 -- matches DESIGN.md's severity table exactly).
Exit 1: at least one Critical or High finding -- stdout lists every one,
        with its severity, tool, rule, and location, so a developer
        knows exactly what to fix without opening a separate dashboard.
Exit 2: could not evaluate at all (a scanner report is malformed, or
        Conftest itself fails to run) -- fails closed, matching every
        other gate step in this project (D4: no terminal status, no
        "success").

Explicitly NOT baseline/differential gating. Every finding here is
judged on its own merits, whether newly introduced or pre-existing in a
repo being onboarded. That gap is real and tracked in docs/TODO.md, not
hidden -- it depends on the exceptions-repo infrastructure (Milestone 4)
this script has no access to yet.

Usage (each report path is optional -- a scanner that found nothing, or
wasn't run this pipeline, is not an error):
  python3 evaluate-findings.py \
    --gitleaks gitleaks-report.json \
    --semgrep semgrep-report.json \
    --trivy trivy-report.json
"""
import argparse
import json
import os
import subprocess
import sys
import tempfile

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
NORMALISE_DIR = os.path.join(SCRIPT_DIR, "..", "normalise")
POLICY_DIR = os.path.join(SCRIPT_DIR, "..", "policy")
CONFTEST_BIN = os.environ.get("CONFTEST_BIN", "conftest")

sys.path.insert(0, NORMALISE_DIR)
import gitleaks_adapter  # noqa: E402
import semgrep_adapter  # noqa: E402
import trivy_adapter  # noqa: E402


def load_report(path):
    if not path or not os.path.exists(path):
        return None
    with open(path) as fh:
        content = fh.read().strip()
    return json.loads(content) if content else None


def collect_findings(args):
    findings = []

    gitleaks_data = load_report(args.gitleaks)
    if gitleaks_data is not None:
        findings += gitleaks_adapter.normalize(gitleaks_data)

    semgrep_data = load_report(args.semgrep)
    if semgrep_data is not None:
        findings += semgrep_adapter.normalize(semgrep_data.get("results", []))

    trivy_data = load_report(args.trivy)
    if trivy_data is not None:
        findings += trivy_adapter.normalize(trivy_data.get("Results"))

    return findings


def run_conftest(findings):
    """Returns (exit_code_ok, warnings, failures). Conftest needs a real
    file, not stdin, for --output=json to behave predictably -- confirmed
    live rather than assumed, matching every other tool integration in
    this project."""
    if not findings:
        return True, [], []

    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as tmp:
        json.dump(findings, tmp)
        tmp_path = tmp.name

    try:
        try:
            result = subprocess.run(
                [CONFTEST_BIN, "test", "--policy", POLICY_DIR, "--output=json", tmp_path],
                capture_output=True, text=True,
            )
        except OSError as exc:
            print(f"policy-eval: FATAL: could not start conftest: {exc}", file=sys.stderr)
            sys.exit(2)
        # conftest exits 1 when there are failures -- that is success for
        # THIS script's purposes (it means conftest ran correctly and
        # found something); only a genuine crash (no parseable JSON) is
        # a fatal evaluation error.
        if result.returncode not in (0, 1):
            print(
                f"policy-eval: FATAL: conftest exited {result.returncode}:\n{result.stdout}\n{result.stderr}",
                file=sys.stderr,
            )
            sys.exit(2)

        try:
            data = json.loads(result.stdout) if result.stdout.strip() else []
        except json.JSONDecodeError:
            print(f"policy-eval: FATAL: conftest produced unparseable output:\n{result.stdout}\n{result.stderr}", file=sys.stderr)
            sys.exit(2)

        warnings, failures = [], []
        for entry in data:
            warnings += [w["msg"] for w in entry.get("warnings", [])]
            failures += [f["msg"] for f in entry.get("failures", [])]
        return True, warnings, failures
    finally:
        os.unlink(tmp_path)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--gitleaks", default=os.environ.get("GITLEAKS_REPORT", "gitleaks-report.json"))
    parser.add_argument("--semgrep", default=os.environ.get("SEMGREP_REPORT", "semgrep-report.json"))
    parser.add_argument("--trivy", default=os.environ.get("TRIVY_REPORT", "trivy-report.json"))
    args = parser.parse_args()

    try:
        findings = collect_findings(args)
    except (OSError, ValueError, TypeError, KeyError) as exc:
        print(f"policy-eval: FATAL: could not read or normalize scanner report: {exc}", file=sys.stderr)
        return 2

    by_severity = {"critical": 0, "high": 0, "medium": 0, "low": 0}
    for f in findings:
        by_severity[f["severity"]] = by_severity.get(f["severity"], 0) + 1

    print(
        f"policy-eval: {len(findings)} finding(s) normalized -- "
        f"critical={by_severity['critical']} high={by_severity['high']} "
        f"medium={by_severity['medium']} low={by_severity['low']}"
    )

    _, warnings, failures = run_conftest(findings)

    for w in warnings:
        print(f"  WARN  {w}")
    for f in failures:
        print(f"  FAIL  {f}")

    if failures:
        print(f"policy-eval: FAIL -- {len(failures)} Critical/High finding(s) block the merge")
        return 1

    print("policy-eval: PASS -- no Critical/High findings")
    return 0


if __name__ == "__main__":
    sys.exit(main())
