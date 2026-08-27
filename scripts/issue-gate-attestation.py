#!/usr/bin/env python3
"""Sign one trusted-runner gate result for bot-approver consumption.

Run this only inside the platform-owned runner after it has checked out the
application revision and executed the pinned gate bundle. It intentionally
does not run in Woodpecker or an application repository: possession of
GATE_ATTESTATION_KEY is authority to release a bot approval.

Input is a JSON document on stdin. Required fields are repository_owner,
repository_name, pull_request, head_sha, contract_digest, decision, and
scanners (a map of scanner name to "success"). The signed document is written
to --output. The caller must set GATE_ATTESTATION_KEY in its private runtime.
"""
import argparse
import json
import os
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))
from gate_contract.attestation import sign


REQUIRED = {
    "repository_owner", "repository_name", "pull_request", "head_sha",
    "contract_digest", "decision", "scanners",
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    key = os.environ.get("GATE_ATTESTATION_KEY")
    if not key:
        print("issue-gate-attestation: GATE_ATTESTATION_KEY is required", file=sys.stderr)
        return 2
    try:
        document = json.load(sys.stdin)
    except json.JSONDecodeError as error:
        print(f"issue-gate-attestation: invalid input JSON: {error}", file=sys.stderr)
        return 2
    missing = sorted(REQUIRED - document.keys())
    if missing or document.get("decision") not in {"pass", "fail"} or not isinstance(document.get("scanners"), dict):
        print(f"issue-gate-attestation: invalid input; missing={missing}", file=sys.stderr)
        return 2
    document["schema_version"] = 1
    signed = sign(document, key)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(signed, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main())
