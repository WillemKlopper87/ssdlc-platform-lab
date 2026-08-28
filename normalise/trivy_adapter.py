#!/usr/bin/env python3
"""
normalise/trivy_adapter.py

Trivy -> the unified finding schema (docs/adr/0018). Trivy already
reports a clean CRITICAL/HIGH/MEDIUM/LOW/UNKNOWN string directly on each
vulnerability, no mapping ambiguity there.

The native "Fingerprint" field is deliberately NOT used, unlike
Semgrep's own registry-gated one this adapter also reconstructs. Found
live (docs/adr/0024 verification): it is stable across repeated scans of
IDENTICAL content, but changes when ANY unrelated file elsewhere in the
scanned tree changes -- even though the vulnerable manifest itself, and
the vulnerability it names, are untouched. The recorded fixture below
shows why: Trivy's own report carries a whole-scan "ArtifactID" alongside
each finding, and the per-vulnerability Fingerprint is derived from
something scan-wide like it, not purely the finding's own identity. For
a one-off report that is invisible; for this platform's baseline/
differential gating (docs/adr/0024), where a finding's identity must
survive an unrelated commit elsewhere in the repo, it silently breaks
the entire mechanism -- a baselined finding "disappears" (looks fixed)
the moment anything else in the repo changes, and reappears as "new" on
the next scan after that, blocking a PR for a pre-existing finding no
one touched. Confirmed live: identical requirements.txt content produced
two different Fingerprint values across two generate-baseline.py runs
that differed only in unrelated files added elsewhere in the tree.
Constructing the fingerprint from CVE id + package + manifest path only
is the one thing here guaranteed independent of everything else in the
repo.

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
                "fingerprint": f"{v['VulnerabilityID']}:{v['PkgName']}:{target}",
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
