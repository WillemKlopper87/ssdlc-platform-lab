#!/bin/sh
# tests/unit/run-unit-tests.sh
#
# The fast, no-privilege verification tier (docs/adr/0016): syntax
# checks, the docs/adr/0007 ${...} lint, and policy-eval's decision
# logic against a stub Gitea -- no Docker, no network beyond localhost,
# nothing that needs Docker-in-Docker. Safe to run on every push to this
# platform's own repo, on the ordinary agent.
#
# The full live-integration tier (tests/regression/, a real Gitea and
# Woodpecker) is what actually proves these mechanisms work end to end
# -- this tier exists to catch the cheap mistakes fast, not to replace
# that proof.

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
FAILED=0

echo "=== lint-syntax ==="
sh "${SCRIPT_DIR}/lint-syntax.sh" || FAILED=1

echo ""
echo "=== lint-woodpecker-yaml ==="
sh "${SCRIPT_DIR}/lint-woodpecker-yaml.sh" || FAILED=1

echo ""
echo "=== test_policy_eval ==="
python3 "${SCRIPT_DIR}/test_policy_eval.py" || FAILED=1

echo ""
echo "=== test_normalise ==="
python3 "${SCRIPT_DIR}/test_normalise.py" || FAILED=1

echo ""
echo "=== test_evaluate_findings ==="
python3 "${SCRIPT_DIR}/test_evaluate_findings.py" || FAILED=1

echo ""
echo "=== test_bot_approver ==="
python3 "${SCRIPT_DIR}/test_bot_approver.py" || FAILED=1

echo ""
echo "=== test_gate_attestation ==="
python3 "${SCRIPT_DIR}/test_gate_attestation.py" || FAILED=1

echo ""
echo "=== test_trusted_gate_runner ==="
python3 "${SCRIPT_DIR}/test_trusted_gate_runner.py" || FAILED=1

echo ""
echo "=== test_gate_bundle ==="
python3 "${SCRIPT_DIR}/test_gate_bundle.py" || FAILED=1

echo ""
echo "=== test_onboarding_source ==="
python3 "${SCRIPT_DIR}/test_onboarding_source.py" || FAILED=1

echo ""
echo "=== test_generate_baseline ==="
python3 "${SCRIPT_DIR}/test_generate_baseline.py" || FAILED=1

echo ""
echo "=== conftest verify (policy/severity_test.rego) ==="
if command -v conftest >/dev/null 2>&1; then
  conftest verify --policy "${SCRIPT_DIR}/../../policy" || FAILED=1
else
  echo "SKIP: conftest not on PATH -- install it to run this check (docs/PINNED_VERSIONS.md)" >&2
fi

echo ""
if [ "$FAILED" -eq 0 ]; then
  echo "=== unit tier: all checks passed ==="
else
  echo "=== unit tier: at least one check failed ===" >&2
fi
exit "$FAILED"
