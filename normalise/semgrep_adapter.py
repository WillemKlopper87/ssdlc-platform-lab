#!/usr/bin/env python3
"""
normalise/semgrep_adapter.py

Semgrep -> the unified finding schema (docs/adr/0018). This is the exact
trap DESIGN.md's "severity normalisation contract" warned about, and it
was confirmed live rather than assumed: Semgrep's own JSON output
(extra.severity) only ever holds ERROR / WARNING / INFO -- a three-level
scale, not a CVSS-mapped Critical/High/Medium/Low one. There is no
"Critical" tier to reach for.

Checked, and rejected, an alternative before settling on this mapping:
extra.metadata's impact/likelihood/confidence fields exist, but are
noisy in practice -- confirmed live against a real subprocess-shell-true
(OS command injection) finding, which reported impact="LOW" despite
being a genuinely serious bug. Deriving severity from those fields would
have been a false precision worse than not having it. Semgrep's own
top-level severity verdict is used instead, mapped 1:1, nothing cleverer
layered on top.

Documented limitation, stated plainly rather than hidden: a Semgrep
finding can never be normalized to "critical" through this mapping. If
a specific rule ever needs to be treated as Critical, that requires an
explicit rule_id override list -- not built here, tracked as a
follow-up, not silently assumed solved.

Also confirmed live: extra.fingerprint is not usable -- it is gated
behind Semgrep Registry login and its actual value in an anonymous run
is the literal string "requires login", not a real identifier. This
adapter constructs its own fingerprint instead:
{check_id}:{path}:{start_line}, stable across runs on unchanged code,
which is exactly the property a fingerprint needs for the differential-
gating work this schema exists to eventually support.

Usage: python3 semgrep_adapter.py <semgrep-report.json>
Reads Semgrep's native --json output (NOT --sarif -- SARIF's `level`
field collapses even Semgrep's own three levels further, per DESIGN.md's
own warning; the native JSON is the more informative source and this
adapter's whole job is recovering severity, so it uses the richer input).
"""
import json
import os
import sys

SEVERITY_MAP = {
    "ERROR": "high",
    "WARNING": "medium",
    "INFO": "low",
}


def normalize(semgrep_results):
    out = []
    for r in semgrep_results:
        raw_severity = r["extra"]["severity"]
        check_id = r["check_id"]
        path = r["path"]
        line = r["start"]["line"]
        out.append({
            "tool": "semgrep",
            "rule_id": check_id,
            "severity": SEVERITY_MAP.get(raw_severity, "low"),
            "file": path,
            "line": line,
            "message": r["extra"].get("message", check_id),
            "fingerprint": f"{check_id}:{path}:{line}",
            "raw_severity": raw_severity,
        })
    return out


def main():
    if len(sys.argv) != 2:
        print("usage: semgrep_adapter.py <semgrep-report.json>", file=sys.stderr)
        return 2

    path = sys.argv[1]
    if not os.path.exists(path):
        print(json.dumps([]))
        return 0

    with open(path) as fh:
        data = json.load(fh)

    print(json.dumps(normalize(data.get("results", []))))
    return 0


if __name__ == "__main__":
    sys.exit(main())
