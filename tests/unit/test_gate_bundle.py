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
    try:
        calls = []
        BUNDLE.subprocess.run = lambda command, **_kwargs: (calls.append(command) or Result(1 if len(calls) == 4 else 0))
        assert BUNDLE.main() == 0
        result = json.loads(Path(output.name, "result.json").read_text(encoding="utf-8"))
        assert result["decision"] == "fail"
        assert result["scanners"] == {"secrets": "success", "sast": "success", "dependencies": "success"}
    finally:
        BUNDLE.subprocess.run = original_run
        restore(previous)
        workspace.cleanup()
        output.cleanup()


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


TESTS = [test_policy_findings_create_a_valid_fail_result, test_scanner_crash_prevents_a_result]
for test in TESTS:
    test()
    print(f"PASS {test.__name__}")
print(f"{len(TESTS)} passed")
