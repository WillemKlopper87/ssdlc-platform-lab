#!/usr/bin/env python3
"""Print the deterministic digest of the bundled scanning policy.

Covers exactly what gate-bundle/Dockerfile's `COPY policy/ ./policy/` puts
into the trusted-runner image: policy/severity.rego and
policy/vendored-rules/ (docs/adr/0020's 594 vendored Semgrep rules). Compare
this against the operator-configured GATE_POLICY_DIGEST on the bot sidecar
(docs/OPERATIONS.md's trusted-runner section) after any bundle rebuild --
a rule change is a policy change and must go through the same review as any
other file in this repo, per DESIGN.md's rule-set-update guidance.
"""
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))
from gate_contract.attestation import policy_digest

path = Path(sys.argv[1]) if len(sys.argv) == 2 else PROJECT_ROOT / "policy"
if not path.is_dir():
    print(f"print-policy-digest: {path} is not a directory", file=sys.stderr)
    sys.exit(2)
print(policy_digest(path))
