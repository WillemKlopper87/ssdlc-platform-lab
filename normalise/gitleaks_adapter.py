#!/usr/bin/env python3
"""
normalise/gitleaks_adapter.py

Gitleaks -> the unified finding schema (docs/adr/0018). Confirmed live
against a real Gitleaks run (v8.30.1) rather than assumed from docs:
Gitleaks findings carry NO severity field at all -- there is nothing to
map. This is not a gap in the adapter; it is the correct reflection of
framework policy (DESIGN.md's severity table): a "Verified secret" is
always Critical, always blocking, never exempted. Every finding this
adapter emits is unconditionally "critical".

Gitleaks already provides a genuinely stable, ready-made fingerprint
(commit:file:rule:line) -- used directly, not reconstructed.

Usage: python3 gitleaks_adapter.py <gitleaks-report.json>
Prints the normalized finding list as JSON to stdout. An empty/absent
report (no leaks) is not an error -- prints an empty list.
"""
import json
import os
import sys


def normalize(gitleaks_findings):
    out = []
    for f in gitleaks_findings:
        out.append({
            "tool": "gitleaks",
            "rule_id": f["RuleID"],
            "severity": "critical",
            "file": f["File"],
            "line": f.get("StartLine"),
            "message": f.get("Description", f["RuleID"]),
            "fingerprint": f["Fingerprint"],
            "raw_severity": None,
        })
    return out


def main():
    if len(sys.argv) != 2:
        print("usage: gitleaks_adapter.py <gitleaks-report.json>", file=sys.stderr)
        return 2

    path = sys.argv[1]
    if not os.path.exists(path):
        # Gitleaks with --exit-code=0 and no leaks may not write a report
        # file at all -- absence of the file means "nothing found", not
        # an error this adapter should fail on.
        print(json.dumps([]))
        return 0

    with open(path) as fh:
        content = fh.read().strip()
    findings = json.loads(content) if content else []

    print(json.dumps(normalize(findings)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
