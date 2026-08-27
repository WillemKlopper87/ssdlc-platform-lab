#!/usr/bin/env python3
"""
normalise/trivy_adapter.py

Trivy -> the unified finding schema (docs/adr/0018). The easiest of the
three adapters, confirmed live rather than assumed: Trivy already
reports a clean CRITICAL/HIGH/MEDIUM/LOW/UNKNOWN string directly on each
vulnerability, and a genuinely stable native Fingerprint (a real SHA256
hash) -- no reconstruction needed, unlike Semgrep's gated one.

"UNKNOWN" (Trivy's own catch-all when a source doesn't classify
severity) maps to "low" here, deliberately conservative in the opposite
direction from Semgrep's ceiling: an unclassified dependency finding
should not silently disappear from view, but it also shouldn't be
treated as equivalent to a confirmed Critical/High CVE with no basis for
that judgment.

File/line: dependency vulnerabilities don't have a meaningful line
number the way code findings do -- line is always null here. The file
is the manifest Trivy attributed the finding to (Result.Target,
e.g. "requirements.txt"), not a field on the vulnerability itself.

Usage: python3 trivy_adapter.py <trivy-report.json>
"""
import json
import os
import sys

SEVERITY_MAP = {
    "CRITICAL": "critical",
    "HIGH": "high",
    "MEDIUM": "medium",
    "LOW": "low",
    "UNKNOWN": "low",
}


def normalize(results):
    out = []
    for res in results or []:
        target = res.get("Target", "")
        for v in res.get("Vulnerabilities") or []:
            raw_severity = v.get("Severity", "UNKNOWN")
            out.append({
                "tool": "trivy",
                "rule_id": v["VulnerabilityID"],
                "severity": SEVERITY_MAP.get(raw_severity, "low"),
                "file": target,
                "line": None,
                "message": f"{v['PkgName']} {v.get('InstalledVersion', '')}: {v.get('Title', v['VulnerabilityID'])}",
                "fingerprint": v.get("Fingerprint") or f"{v['VulnerabilityID']}:{v['PkgName']}:{target}",
                "raw_severity": raw_severity,
            })
    return out


def main():
    if len(sys.argv) != 2:
        print("usage: trivy_adapter.py <trivy-report.json>", file=sys.stderr)
        return 2

    path = sys.argv[1]
    if not os.path.exists(path):
        print(json.dumps([]))
        return 0

    with open(path) as fh:
        data = json.load(fh)

    print(json.dumps(normalize(data.get("Results"))))
    return 0


if __name__ == "__main__":
    sys.exit(main())
