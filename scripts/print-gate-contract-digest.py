#!/usr/bin/env python3
"""Print the canonical SHA-256 digest of a platform gate-contract JSON file."""
import json
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))
from gate_contract.attestation import contract_digest


path = Path(sys.argv[1]) if len(sys.argv) == 2 else PROJECT_ROOT / "gate-contract" / "contract.json"
try:
    document = json.loads(path.read_text(encoding="utf-8"))
except (OSError, json.JSONDecodeError) as error:
    print(f"print-gate-contract-digest: cannot read {path}: {error}", file=sys.stderr)
    sys.exit(2)
print(contract_digest(document))
