#!/usr/bin/env python3
"""Tests for the platform-owned bundle result contract."""
import importlib.util
import json
import os
import tempfile
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[2] / "gate-bundle" / "run-pilot-bundle.py"
SPEC = importlib.util.spec_from_file_location("pilot_bundle", SCRIPT)
BUNDLE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUNDLE)


class Result:
    def __init__(self, returncode):
        self.returncode = returncode


def with_environment():
    workspace, output = tempfile.TemporaryDirectory(), tempfile.TemporaryDirectory()
    previous = {name: os.environ.get(name) for name in ("GATE_WORKSPACE", "GATE_OUTPUT_DIR")}
    os.environ.update({"GATE_WORKSPACE": workspace.name, "GATE_OUTPUT_DIR": output.name})
    return workspace, output, previous


def restore(previous):
    for name, value in previous.items():
        if value is None:
            os.environ.pop(name, None)
        else:
            os.environ[name] = value


def write_valid_reports(output_dir):
    """docs/adr/0025: validate_report() now requires each report to
    actually exist and match its scanner's real top-level JSON shape
    before that scanner can be labelled "success" -- write genuinely
    valid-shaped (if empty-of-findings) reports so a mocked subprocess.run
    (which never touches the filesystem itself) still produces a result
    this check accepts, the same way a real scanner run would."""
    Path(output_dir, "gitleaks-report.json").write_text("[]", encoding="utf-8")
    Path(output_dir, "semgrep-report.json").write_text('{"results": []}', encoding="utf-8")
    Path(output_dir, "trivy-report.json").write_text('{"Results": []}', encoding="utf-8")


def test_policy_findings_create_a_valid_fail_result():
    workspace, output, previous = with_environment()
    original_run = BUNDLE.subprocess.run
    original_digest = BUNDLE.compute_policy_digest
    try:
        write_valid_reports(output.name)
        calls = []
        BUNDLE.subprocess.run = lambda command, **_kwargs: (calls.append(command) or Result(1 if len(calls) == 4 else 0))
        BUNDLE.compute_policy_digest = lambda: "sha256:" + "ab" * 32
        assert BUNDLE.main() == 0
        result = json.loads(Path(output.name, "result.json").read_text(encoding="utf-8"))
        assert result["decision"] == "fail"
        assert result["scanners"] == {"secrets": "success", "sast": "success", "dependencies": "success"}
        assert set(result["report_digests"]) == {"secrets", "sast", "dependencies"}, "a real hash is recorded per validated report"
        assert result["policy_digest"] == "sha256:" + "ab" * 32
        # docs/adr/0023: GATE_WORKSPACE is a Gitea archive extraction with no
        # .git directory. Without --no-git, gitleaks silently scans "0
        # commits" and reports zero findings regardless of file content --
        # confirmed live with a real secret. Guard the flag stays present.
        gitleaks_call = calls[0]
        assert "gitleaks" in gitleaks_call[0]
        assert "--no-git" in gitleaks_call, "gitleaks must run with --no-git against an archive-extracted workspace"
        # docs/adr/0025 H3: the policy evaluator must be told explicitly
        # where this workspace's baseline lives and what root its Semgrep
        # report's paths need stripped -- both silently defaulted wrong
        # otherwise (see run-pilot-bundle.py's own comment on this call).
        policy_call = calls[3]
        assert "--baseline" in policy_call, "the evaluator must be pointed at the workspace's own baseline, not a cwd-relative default"
        assert str(Path(workspace.name, ".ssdlc", "baseline.json")) in policy_call
        assert "--semgrep-root" in policy_call, "the evaluator must be told the Semgrep scan root to strip"
        assert workspace.name in policy_call
    finally:
        BUNDLE.subprocess.run = original_run
        BUNDLE.compute_policy_digest = original_digest
        restore(previous)
        workspace.cleanup()
        output.cleanup()


def test_missing_report_is_not_labelled_success():
    """docs/adr/0025 (SSDLC_Code_report.md H2): a scanner that exits 0
    without writing its report must never be signed as successful --
    trusted-gate-runner.py's issue() relies on this label being trustworthy
    before it ever produces an attestation."""
    workspace, output, previous = with_environment()
    original_run = BUNDLE.subprocess.run
    original_digest = BUNDLE.compute_policy_digest
    try:
        # Only semgrep and trivy write real reports; gitleaks "succeeds"
        # (exit 0) but never writes gitleaks-report.json -- exactly the
        # failure mode H2 describes.
        Path(output.name, "semgrep-report.json").write_text('{"results": []}', encoding="utf-8")
        Path(output.name, "trivy-report.json").write_text('{"Results": []}', encoding="utf-8")
        BUNDLE.subprocess.run = lambda command, **_kwargs: Result(0)
        BUNDLE.compute_policy_digest = lambda: "sha256:" + "cd" * 32
        assert BUNDLE.main() == 0
        result = json.loads(Path(output.name, "result.json").read_text(encoding="utf-8"))
        assert result["scanners"]["secrets"] != "success", "no report on disk must never read as success"
        assert result["scanners"]["sast"] == "success"
        assert result["scanners"]["dependencies"] == "success"
        assert "secrets" not in result["report_digests"], "no hash is recorded for a report that was never validated"
    finally:
        BUNDLE.subprocess.run = original_run
        BUNDLE.compute_policy_digest = original_digest
        restore(previous)
        workspace.cleanup()
        output.cleanup()


def test_malformed_report_is_not_labelled_success():
    workspace, output, previous = with_environment()
    original_run = BUNDLE.subprocess.run
    original_digest = BUNDLE.compute_policy_digest
    try:
        Path(output.name, "gitleaks-report.json").write_text("[]", encoding="utf-8")
        # Wrong shape: a Semgrep report is always an object with a
        # "results" list, never a bare array.
        Path(output.name, "semgrep-report.json").write_text("[]", encoding="utf-8")
        Path(output.name, "trivy-report.json").write_text('{"Results": []}', encoding="utf-8")
        BUNDLE.subprocess.run = lambda command, **_kwargs: Result(0)
        BUNDLE.compute_policy_digest = lambda: "sha256:" + "ef" * 32
        assert BUNDLE.main() == 0
        result = json.loads(Path(output.name, "result.json").read_text(encoding="utf-8"))
        assert result["scanners"]["sast"] != "success", "the wrong top-level shape must never read as success"
    finally:
        BUNDLE.subprocess.run = original_run
        BUNDLE.compute_policy_digest = original_digest
        restore(previous)
        workspace.cleanup()
        output.cleanup()


def test_compute_policy_digest_is_deterministic_and_content_sensitive():
    with tempfile.TemporaryDirectory() as policy_dir:
        rule_path = Path(policy_dir, "rule.yaml")
        rule_path.write_text("rules: []\n", encoding="utf-8")
        original_policy_dir = BUNDLE.POLICY_DIR
        try:
            BUNDLE.POLICY_DIR = Path(policy_dir)
            first = BUNDLE.compute_policy_digest()
            second = BUNDLE.compute_policy_digest()
            assert first == second, "identical content must produce the identical digest"
            rule_path.write_text("rules: [{}]\n", encoding="utf-8")
            changed = BUNDLE.compute_policy_digest()
            assert changed != first, "a single-byte content change must change the digest"
        finally:
            BUNDLE.POLICY_DIR = original_policy_dir


def test_scanner_crash_prevents_a_result():
    workspace, output, previous = with_environment()
    original_run = BUNDLE.subprocess.run
    try:
        BUNDLE.subprocess.run = lambda *_args, **_kwargs: Result(2)
        try:
            BUNDLE.main()
            raise AssertionError("expected scanner failure")
        except SystemExit as error:
            assert error.code == 2
        assert not Path(output.name, "result.json").exists()
    finally:
        BUNDLE.subprocess.run = original_run
        restore(previous)
        workspace.cleanup()
        output.cleanup()


TESTS = [
    test_policy_findings_create_a_valid_fail_result,
    test_missing_report_is_not_labelled_success,
    test_malformed_report_is_not_labelled_success,
    test_scanner_crash_prevents_a_result,
    test_compute_policy_digest_is_deterministic_and_content_sensitive,
]
for test in TESTS:
    test()
    print(f"PASS {test.__name__}")
print(f"{len(TESTS)} passed")
