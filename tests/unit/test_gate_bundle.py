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


def test_policy_findings_create_a_valid_fail_result():
    workspace, output, previous = with_environment()
    original_run = BUNDLE.subprocess.run
    original_digest = BUNDLE.compute_policy_digest
    try:
        calls = []
        BUNDLE.subprocess.run = lambda command, **_kwargs: (calls.append(command) or Result(1 if len(calls) == 4 else 0))
        BUNDLE.compute_policy_digest = lambda: "sha256:" + "ab" * 32
        assert BUNDLE.main() == 0
        result = json.loads(Path(output.name, "result.json").read_text(encoding="utf-8"))
        assert result["decision"] == "fail"
        assert result["scanners"] == {"secrets": "success", "sast": "success", "dependencies": "success"}
        assert result["policy_digest"] == "sha256:" + "ab" * 32
        # docs/adr/0023: GATE_WORKSPACE is a Gitea archive extraction with no
        # .git directory. Without --no-git, gitleaks silently scans "0
        # commits" and reports zero findings regardless of file content --
        # confirmed live with a real secret. Guard the flag stays present.
        gitleaks_call = calls[0]
        assert "gitleaks" in gitleaks_call[0]
        assert "--no-git" in gitleaks_call, "gitleaks must run with --no-git against an archive-extracted workspace"
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
    test_scanner_crash_prevents_a_result,
    test_compute_policy_digest_is_deterministic_and_content_sensitive,
]
for test in TESTS:
    test()
    print(f"PASS {test.__name__}")
print(f"{len(TESTS)} passed")
