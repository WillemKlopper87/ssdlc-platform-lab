#!/usr/bin/env python3
"""Fail-closed tests for the trusted-runner attestation bridge."""
import importlib.util
import json
import os
import sys
import tempfile
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))
from gate_contract.attestation import sign


SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "bot-approver.py"
SPEC = importlib.util.spec_from_file_location("bot_approver_attestation", SCRIPT)
BOT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BOT)

KEY = "test-only-attestation-key"
CONTRACT = "sha256:0123456789abcdef"
POLICY = "sha256:fedcba9876543210"
PR = {"number": 42, "head": {"sha": "a" * 40}}


def document(**changes):
    value = {
        "schema_version": 1,
        "repository_owner": "team",
        "repository_name": "demo",
        "pull_request": 42,
        "head_sha": "a" * 40,
        "contract_digest": CONTRACT,
        "policy_digest": POLICY,
        "decision": "pass",
        "scanners": {"secrets": "success", "sast": "success", "dependencies": "success"},
    }
    value.update(changes)
    return sign(value, KEY)


def configured_store():
    directory = tempfile.TemporaryDirectory()
    names = ("GATE_ATTESTATIONS_DIR", "GATE_ATTESTATION_KEY", "GATE_CONTRACT_DIGEST", "GATE_POLICY_DIGEST")
    previous = {name: os.environ.get(name) for name in names}
    os.environ.update({
        "GATE_ATTESTATIONS_DIR": directory.name,
        "GATE_ATTESTATION_KEY": KEY,
        "GATE_CONTRACT_DIGEST": CONTRACT,
        "GATE_POLICY_DIGEST": POLICY,
    })
    return directory, previous


def restore(previous):
    for name, value in previous.items():
        if value is None:
            os.environ.pop(name, None)
        else:
            os.environ[name] = value


def write(directory, value):
    Path(directory, f"team--demo--{'a' * 40}.json").write_text(json.dumps(value), encoding="utf-8")


def test_valid_attestation_passes():
    directory, previous = configured_store()
    try:
        write(directory.name, document())
        assert BOT.trusted_attestation_matches("team", "demo", PR)
    finally:
        restore(previous)
        directory.cleanup()


def test_modified_or_wrong_head_attestation_fails():
    directory, previous = configured_store()
    try:
        tampered = document()
        tampered["decision"] = "pass-with-tampering"
        write(directory.name, tampered)
        assert not BOT.trusted_attestation_matches("team", "demo", PR)

        write(directory.name, document(head_sha="b" * 40))
        assert not BOT.trusted_attestation_matches("team", "demo", PR)
    finally:
        restore(previous)
        directory.cleanup()


def test_missing_scanner_or_configuration_fails():
    directory, previous = configured_store()
    try:
        write(directory.name, document(scanners={"secrets": "success", "sast": "success"}))
        assert not BOT.trusted_attestation_matches("team", "demo", PR)
        os.environ.pop("GATE_CONTRACT_DIGEST")
        assert not BOT.trusted_attestation_matches("team", "demo", PR)
    finally:
        restore(previous)
        directory.cleanup()


def test_wrong_or_missing_policy_digest_fails():
    directory, previous = configured_store()
    try:
        # A bundle rebuilt with a different (weakened) ruleset would produce
        # this exact shape: everything else about the attestation still
        # checks out, only policy_digest differs.
        write(directory.name, document(policy_digest="sha256:" + "0" * 16))
        assert not BOT.trusted_attestation_matches("team", "demo", PR)

        write(directory.name, document())
        os.environ.pop("GATE_POLICY_DIGEST")
        assert not BOT.trusted_attestation_matches("team", "demo", PR)
    finally:
        restore(previous)
        directory.cleanup()


def test_attestation_mode_does_not_trust_woodpecker_steps():
    directory, previous = configured_store()
    extra_names = ("GATE_ATTESTATION_REQUIRED", "GATE_CONTRACT_ENFORCE")
    extra_previous = {name: os.environ.get(name) for name in extra_names}
    original_gitea, original_woodpecker = BOT.gitea, BOT.woodpecker
    calls = []
    try:
        write(directory.name, document())
        os.environ.update({"GATE_ATTESTATION_REQUIRED": "1", "GATE_CONTRACT_ENFORCE": "0"})

        def fake_gitea(path, method="GET", body=None, allow_404=False):
            calls.append((path, method, body))
            return [] if method == "GET" else {"id": 1}

        BOT.gitea = fake_gitea
        BOT.woodpecker = lambda *_args, **_kwargs: (_ for _ in ()).throw(AssertionError("Woodpecker must not be queried in attestation mode"))
        BOT.process_pr("team", "demo", "unused", "gate-bot", PR)
        assert any(method == "POST" and path.endswith("/reviews") for path, method, _body in calls)
    finally:
        BOT.gitea, BOT.woodpecker = original_gitea, original_woodpecker
        restore(previous)
        restore(extra_previous)
        directory.cleanup()


TESTS = [
    test_valid_attestation_passes,
    test_modified_or_wrong_head_attestation_fails,
    test_missing_scanner_or_configuration_fails,
    test_wrong_or_missing_policy_digest_fails,
    test_attestation_mode_does_not_trust_woodpecker_steps,
]
for test in TESTS:
    test()
    print(f"PASS {test.__name__}")
print(f"{len(TESTS)} passed")
