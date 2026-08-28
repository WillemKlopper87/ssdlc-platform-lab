#!/usr/bin/env python3
"""Platform-owned scanner bundle for trusted-gate-runner.py.

The application checkout is scan input only. Scanner commands, policy,
normalisers and Semgrep rules are built into this image. On success it writes
the runner result contract to GATE_OUTPUT_DIR/result.json. A scanner or policy
error exits non-zero without a result, which prevents an attestation.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))
from gate_contract.attestation import policy_digest

# The absolute in-image path gate-bundle/Dockerfile's `COPY policy/ ./policy/`
# produces (WORKDIR /opt/ssdlc) -- a separate module-level function, not
# inlined into main(), so tests/unit/test_gate_bundle.py can monkeypatch it
# the same way it already monkeypatches subprocess.run, without needing a
# real built image on disk.
POLICY_DIR = Path("/opt/ssdlc/policy")

# docs/adr/0024 verification: onboard-repo.sh commits these same
# platform-owned paths into the application repo this bundle scans
# (scripts/bot-approver.py's DEFAULT_GATE_MANAGED_PATHS/_TREE_PREFIXES --
# duplicated here, not imported, since bot-approver.py is never copied into
# this image, see gate-bundle/Dockerfile). Scanning GATE_WORKSPACE wholesale
# means policy/vendored-rules/'s own ~700 deliberately-vulnerable rule-test
# fixtures (bash/ifs-tampering.bash, c/double-free.c, ...) get scanned as if
# they were the application's code -- confirmed live to produce thousands of
# self-matches with no error of any kind. Same self-contamination class
# docs/adr/0018 found for report filenames in the fast gate, never checked
# against this larger target until now, and present here too since this
# bundle scans the same committed checkout.
PLATFORM_MANAGED_FILES = (
    ".woodpecker.yml",
    "policy-eval/verify-approvals.py",
    "policy-eval/evaluate-findings.py",
    "normalise/gitleaks_adapter.py",
    "normalise/semgrep_adapter.py",
    "normalise/trivy_adapter.py",
    "policy/severity.rego",
    ".ssdlc/baseline.json",
)
PLATFORM_MANAGED_DIRS = ("policy/vendored-rules",)


def compute_policy_digest():
    return policy_digest(POLICY_DIR)


def run(command, name):
    result = subprocess.run(command, cwd=os.environ["GATE_WORKSPACE"])
    if result.returncode != 0:
        print(f"gate-bundle: {name} failed with exit {result.returncode}", file=sys.stderr)
        raise SystemExit(2)


def main():
    workspace = Path(os.environ["GATE_WORKSPACE"])
    output = Path(os.environ["GATE_OUTPUT_DIR"])
    if not workspace.is_dir():
        print("gate-bundle: GATE_WORKSPACE is not a directory", file=sys.stderr)
        return 2
    output.mkdir(parents=True, exist_ok=True)
    reports = {
        "gitleaks": output / "gitleaks-report.json",
        "semgrep": output / "semgrep-report.json",
        "trivy": output / "trivy-report.json",
    }
    # Semgrep persists a small settings file even for a local, offline scan.
    # The bundle container is read-only, so keep that incidental state in the
    # runner-provided output mount rather than weakening container isolation.
    os.environ["SEMGREP_SETTINGS_FILE"] = str(output / "semgrep-settings.yml")
    # docs/adr/0023: --no-git is required here, not optional. GATE_WORKSPACE
    # is trusted-gate-runner.py's checkout_head() output -- a Gitea archive
    # tarball extracted fresh, which (like `git archive` itself) contains no
    # .git directory at all. Without --no-git, gitleaks treats the missing
    # .git as "0 commits scanned" and silently reports zero findings
    # regardless of file content -- confirmed live: a real, non-allowlisted
    # AWS-shaped key produced ZERO findings without this flag, and was
    # correctly caught with it. This is the one gitleaks invocation in the
    # whole platform that needs the flag: the fast gate's own secrets step
    # (pipelines/fast.woodpecker.yml) runs against a real `git clone`
    # (Woodpecker's own clone step), where scanning commit history is
    # correct and more thorough -- do not add --no-git there too.
    semgrep_excludes = []
    for path in PLATFORM_MANAGED_FILES + PLATFORM_MANAGED_DIRS:
        semgrep_excludes += ["--exclude", path]
    run(["gitleaks", "detect", "--no-git", "--source", str(workspace), "--report-format=json", "--report-path", str(reports["gitleaks"]), "--exit-code=0", "--no-banner"], "secrets scan")
    run(["semgrep", "--disable-version-check", "--metrics=off", "--config=/opt/ssdlc/policy/vendored-rules", *semgrep_excludes, "--json", "--output", str(reports["semgrep"]), str(workspace)], "SAST scan")
    run(["trivy", "fs", "--cache-dir=/opt/ssdlc/trivy-cache", "--skip-db-update", "--exit-code=0", "--format=json",
         "--skip-files", ",".join(PLATFORM_MANAGED_FILES),
         "--skip-dirs", ",".join(PLATFORM_MANAGED_DIRS),
         "--output", str(reports["trivy"]), str(workspace)], "dependency scan")
    policy = subprocess.run([
        "python3", "/opt/ssdlc/policy-eval/evaluate-findings.py",
        "--gitleaks", str(reports["gitleaks"]),
        "--semgrep", str(reports["semgrep"]),
        "--trivy", str(reports["trivy"]),
    ])
    if policy.returncode not in (0, 1):
        print(f"gate-bundle: policy evaluator failed with exit {policy.returncode}", file=sys.stderr)
        return 2
    (output / "result.json").write_text(json.dumps({
        "decision": "pass" if policy.returncode == 0 else "fail",
        "scanners": {"secrets": "success", "sast": "success", "dependencies": "success"},
        # docs/adr/0022: lets the runner's attestation record which exact
        # scanning policy (vendored rules + severity.rego) produced this
        # decision, so a bundle rebuild with a silently different ruleset
        # cannot pass verification against an operator's expected digest.
        "policy_digest": compute_policy_digest(),
    }, sort_keys=True) + "\n", encoding="utf-8")
    # A policy finding is a valid, signed "fail" result, not a runner crash.
    # The runner records it for audit; the bot rejects its decision.
    return 0


if __name__ == "__main__":
    sys.exit(main())
