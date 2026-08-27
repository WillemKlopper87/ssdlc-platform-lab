#!/usr/bin/env python3
"""Unit tests for bot-approver's protected-base gate contract.

No HTTP calls are made: file_at_ref is replaced with a small Contents API
fixture, so these tests specifically prove the fail-closed comparison.
"""
import importlib.util
import os
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "bot-approver.py"
SPEC = importlib.util.spec_from_file_location("bot_approver", SCRIPT)
BOT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BOT)

PR = {"base": {"sha": "base-sha"}, "head": {"sha": "head-sha"}}


def with_files(files):
    def fake_file_at_ref(_owner, _repo, path, ref):
        blob_sha = files.get((path, ref))
        return {"sha": blob_sha} if blob_sha is not None else None

    return fake_file_at_ref


def with_trees(trees):
    def fake_tree_at_ref(_owner, _repo, ref):
        return trees.get(ref)

    return fake_tree_at_ref


MATCHING_TREES = {
    "base-sha": {"tree": [{"path": "policy/vendored-rules/python/a.yml", "type": "blob", "sha": "rule-a"}]},
    "head-sha": {"tree": [{"path": "policy/vendored-rules/python/a.yml", "type": "blob", "sha": "rule-a"}]},
}


def test_equal_files_are_accepted():
    paths = (".woodpecker.yml", "policy/severity.rego")
    files = {(path, ref): f"{path}-{ref}" for path in paths for ref in ("base-sha", "head-sha")}
    for path in paths:
        files[(path, "head-sha")] = files[(path, "base-sha")]
    original_paths, original_file_at_ref, original_tree_at_ref = BOT.gate_managed_paths, BOT.file_at_ref, BOT.tree_at_ref
    BOT.gate_managed_paths = lambda: paths
    BOT.file_at_ref = with_files(files)
    BOT.tree_at_ref = with_trees(MATCHING_TREES)
    try:
        assert BOT.gate_contract_matches_base("owner", "repo", PR)
    finally:
        BOT.gate_managed_paths, BOT.file_at_ref, BOT.tree_at_ref = original_paths, original_file_at_ref, original_tree_at_ref


def test_changed_or_missing_file_is_rejected():
    original_paths, original_file_at_ref, original_tree_at_ref = BOT.gate_managed_paths, BOT.file_at_ref, BOT.tree_at_ref
    BOT.gate_managed_paths = lambda: (".woodpecker.yml",)
    BOT.tree_at_ref = with_trees(MATCHING_TREES)
    try:
        BOT.file_at_ref = with_files({(".woodpecker.yml", "base-sha"): "base", (".woodpecker.yml", "head-sha"): "head"})
        assert not BOT.gate_contract_matches_base("owner", "repo", PR)

        BOT.file_at_ref = with_files({(".woodpecker.yml", "base-sha"): "base"})
        assert not BOT.gate_contract_matches_base("owner", "repo", PR)
    finally:
        BOT.gate_managed_paths, BOT.file_at_ref, BOT.tree_at_ref = original_paths, original_file_at_ref, original_tree_at_ref


def test_changed_or_truncated_vendored_tree_is_rejected():
    original_paths, original_file_at_ref, original_tree_at_ref = BOT.gate_managed_paths, BOT.file_at_ref, BOT.tree_at_ref
    BOT.gate_managed_paths = lambda: ()
    BOT.file_at_ref = with_files({})
    try:
        changed = dict(MATCHING_TREES)
        changed["head-sha"] = {"tree": [{"path": "policy/vendored-rules/python/a.yml", "type": "blob", "sha": "attacker-rule"}]}
        BOT.tree_at_ref = with_trees(changed)
        assert not BOT.gate_contract_matches_base("owner", "repo", PR)

        truncated = dict(MATCHING_TREES)
        truncated["head-sha"] = {"truncated": True, "tree": []}
        BOT.tree_at_ref = with_trees(truncated)
        assert not BOT.gate_contract_matches_base("owner", "repo", PR)
    finally:
        BOT.gate_managed_paths, BOT.file_at_ref, BOT.tree_at_ref = original_paths, original_file_at_ref, original_tree_at_ref


def test_missing_pr_commit_metadata_is_rejected():
    assert not BOT.gate_contract_matches_base("owner", "repo", {"base": {}, "head": {"sha": "head"}})


def test_configured_paths_trim_blank_entries():
    previous = os.environ.get("GATE_MANAGED_PATHS")
    os.environ["GATE_MANAGED_PATHS"] = " policy/severity.rego, , .woodpecker.yml "
    try:
        assert BOT.gate_managed_paths() == ("policy/severity.rego", ".woodpecker.yml")
    finally:
        if previous is None:
            os.environ.pop("GATE_MANAGED_PATHS", None)
        else:
            os.environ["GATE_MANAGED_PATHS"] = previous


TESTS = [
    test_equal_files_are_accepted,
    test_changed_or_missing_file_is_rejected,
    test_changed_or_truncated_vendored_tree_is_rejected,
    test_missing_pr_commit_metadata_is_rejected,
    test_configured_paths_trim_blank_entries,
]

for test in TESTS:
    test()
    print(f"PASS {test.__name__}")

print(f"{len(TESTS)} passed")
