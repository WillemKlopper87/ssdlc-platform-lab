#!/usr/bin/env python3
"""Unit tests for the runner's signed, atomic attestation output."""
import importlib.util
import json
import os
import sys
import tempfile
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))
from gate_contract.attestation import contract_digest, signature_is_valid

SPEC = importlib.util.spec_from_file_location("trusted_gate_runner", PROJECT_ROOT / "scripts" / "trusted-gate-runner.py")
RUNNER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNNER)

CONTRACT = {"required_scanners": ["secrets", "sast", "dependencies"]}
PR = {"number": 7, "head": {"sha": "c" * 40}}
RESULT = {"decision": "pass", "scanners": {"secrets": "success", "sast": "success", "dependencies": "success"}}


def configured_store():
    directory = tempfile.TemporaryDirectory()
    names = ("GATE_ATTESTATIONS_DIR", "GATE_ATTESTATION_KEY")
    previous = {name: os.environ.get(name) for name in names}
    os.environ.update({"GATE_ATTESTATIONS_DIR": directory.name, "GATE_ATTESTATION_KEY": "runner-test-key"})
    return directory, previous


def restore(previous):
    for name, value in previous.items():
        if value is None:
            os.environ.pop(name, None)
        else:
            os.environ[name] = value


def test_pass_result_creates_a_valid_head_bound_attestation():
    directory, previous = configured_store()
    try:
        assert RUNNER.issue("team", "demo", PR, CONTRACT, tuple(CONTRACT["required_scanners"]), RESULT)
        path = Path(directory.name, f"team--demo--{'c' * 40}.json")
        value = json.loads(path.read_text(encoding="utf-8"))
        assert value["contract_digest"] == contract_digest(CONTRACT)
        assert value["head_sha"] == PR["head"]["sha"]
        assert signature_is_valid(value, "runner-test-key")
    finally:
        restore(previous)
        directory.cleanup()


def test_incomplete_result_is_not_attested():
    directory, previous = configured_store()
    try:
        incomplete = {"decision": "pass", "scanners": {"secrets": "success"}}
        assert not RUNNER.issue("team", "demo", PR, CONTRACT, tuple(CONTRACT["required_scanners"]), incomplete)
        assert not list(Path(directory.name).iterdir())
    finally:
        restore(previous)
        directory.cleanup()


def test_policy_failure_is_recorded_but_not_marked_pass():
    directory, previous = configured_store()
    try:
        failure = {"decision": "fail", "scanners": RESULT["scanners"]}
        assert RUNNER.issue("team", "demo", PR, CONTRACT, tuple(CONTRACT["required_scanners"]), failure)
        value = json.loads(Path(directory.name, f"team--demo--{'c' * 40}.json").read_text(encoding="utf-8"))
        assert value["decision"] == "fail"
    finally:
        restore(previous)
        directory.cleanup()


TESTS = [
    test_pass_result_creates_a_valid_head_bound_attestation,
    test_incomplete_result_is_not_attested,
    test_policy_failure_is_recorded_but_not_marked_pass,
]
for test in TESTS:
    test()
    print(f"PASS {test.__name__}")
print(f"{len(TESTS)} passed")
