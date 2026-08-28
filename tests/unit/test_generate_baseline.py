#!/usr/bin/env python3
"""Tests for scripts/generate-baseline.py's shrink-only baseline logic."""
import importlib.util
import json
import os
import tempfile
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "generate-baseline.py"
SPEC = importlib.util.spec_from_file_location("generate_baseline", SCRIPT)
GEN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GEN)


def finding(tool, fingerprint, rule_id="r1", severity="high"):
    return {"tool": tool, "fingerprint": fingerprint, "rule_id": rule_id, "severity": severity, "file": "x", "line": 1, "message": "x"}


def test_fresh_baseline_accepts_every_current_finding():
    with tempfile.TemporaryDirectory() as workspace, tempfile.TemporaryDirectory() as out_dir:
        output = os.path.join(out_dir, "baseline.json")
        original_scan = GEN.scan_workspace
        try:
            GEN.scan_workspace = lambda _ws: [finding("trivy", "a"), finding("semgrep", "b"), finding("gitleaks", "c")]
            import sys
            sys.argv = ["generate-baseline.py", workspace, output]
            assert GEN.main() == 0
            document = json.loads(Path(output).read_text(encoding="utf-8"))
            keys = {(e["tool"], e["fingerprint"]) for e in document["findings"]}
            assert keys == {("trivy", "a"), ("semgrep", "b")}, "gitleaks never enters the baseline, even on first creation"
        finally:
            GEN.scan_workspace = original_scan
    print("PASS test_fresh_baseline_accepts_every_current_finding")


def test_refresh_only_shrinks_never_grows():
    with tempfile.TemporaryDirectory() as workspace, tempfile.TemporaryDirectory() as out_dir:
        output = os.path.join(out_dir, "baseline.json")
        # Existing baseline has two findings; only one is still present in the
        # "current" scan, and a brand-new one has appeared too.
        Path(output).write_text(json.dumps({
            "schema_version": 1,
            "findings": [
                {"tool": "trivy", "fingerprint": "still-here", "rule_id": "r1", "severity": "high"},
                {"tool": "trivy", "fingerprint": "now-fixed", "rule_id": "r2", "severity": "high"},
            ],
        }), encoding="utf-8")
        original_scan = GEN.scan_workspace
        try:
            GEN.scan_workspace = lambda _ws: [
                finding("trivy", "still-here"),
                finding("trivy", "brand-new"),  # must NOT be silently added
            ]
            import sys
            sys.argv = ["generate-baseline.py", workspace, output]
            assert GEN.main() == 0
            document = json.loads(Path(output).read_text(encoding="utf-8"))
            keys = {(e["tool"], e["fingerprint"]) for e in document["findings"]}
            assert keys == {("trivy", "still-here")}, "fixed findings drop out, new findings are never auto-added"
        finally:
            GEN.scan_workspace = original_scan
    print("PASS test_refresh_only_shrinks_never_grows")


def test_unparseable_existing_baseline_falls_back_to_fresh_creation():
    with tempfile.TemporaryDirectory() as out_dir:
        output = os.path.join(out_dir, "baseline.json")
        Path(output).write_text("not-json", encoding="utf-8")
        assert GEN.load_existing(output) is None
    print("PASS test_unparseable_existing_baseline_falls_back_to_fresh_creation")


TESTS = [
    test_fresh_baseline_accepts_every_current_finding,
    test_refresh_only_shrinks_never_grows,
    test_unparseable_existing_baseline_falls_back_to_fresh_creation,
]
for test in TESTS:
    test()
print(f"{len(TESTS)} passed")
