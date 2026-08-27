#!/bin/sh
# tests/unit/lint-syntax.sh
#
# Syntax-checks every shell and Python script in this repo -- the
# cheapest possible check, deliberately: no Docker, no network, just
# parsing. Exists to catch the class of mistake this project has hit
# live and repeatedly (a missing argument, a stray character) at the
# moment it's introduced, on every push, rather than at the next live
# regression run minutes or days later.

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT="${SCRIPT_DIR}/../.."

violations=0

for f in $(find "$REPO_ROOT" -name '*.sh' -not -path '*/node_modules/*' -not -path '*/.terraform/*' 2>/dev/null); do
  if ! sh -n "$f" 2>/tmp/lint-syntax-err.$$; then
    echo "FAIL: $f"
    sed 's/^/    /' /tmp/lint-syntax-err.$$
    violations=$((violations + 1))
  fi
  rm -f /tmp/lint-syntax-err.$$
done

for f in $(find "$REPO_ROOT" -name '*.py' -not -path '*/node_modules/*' -not -path '*/.terraform/*' 2>/dev/null); do
  if ! python3 -m py_compile "$f" 2>/tmp/lint-syntax-err.$$; then
    echo "FAIL: $f"
    sed 's/^/    /' /tmp/lint-syntax-err.$$
    violations=$((violations + 1))
  fi
  rm -f /tmp/lint-syntax-err.$$
done

if [ "$violations" -eq 0 ]; then
  echo "PASS: every .sh and .py file in the repo parses cleanly"
  exit 0
else
  echo ""
  echo "$violations file(s) failed to parse."
  exit 1
fi
