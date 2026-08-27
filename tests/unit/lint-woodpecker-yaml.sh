#!/bin/sh
# tests/unit/lint-woodpecker-yaml.sh
#
# Enforces docs/adr/0007's decision item 4, as code instead of a
# reminder someone has to remember: never write a literal ${...}-shaped
# string anywhere in a .woodpecker.yml file, including comments, unless
# it's a value Woodpecker is meant to substitute.
#
# Why this is worth a lint rather than another documented rule: found
# live in docs/adr/0007 that Woodpecker's compiler text-substitutes
# dollar-brace patterns across the ENTIRE file, comments included,
# before real YAML parsing -- writing the literal characters "${VAR}" in
# an explanatory comment about the bug produced a blocking parse error
# on the next trigger ("unable to parse variable name"). No legitimate
# use of ${...} exists anywhere in this project's pipeline templates
# today (grep confirms it); this file's job is to make sure that stays
# true, on every push, with no Docker needed.

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT="${SCRIPT_DIR}/../.."

violations=0

for f in $(find "$REPO_ROOT/pipelines" -name '*.yml' 2>/dev/null); do
  if grep -n '\${' "$f" >/dev/null 2>&1; then
    echo "FAIL: $f contains a literal \${...} pattern -- forbidden by docs/adr/0007 decision item 4:"
    grep -n '\${' "$f" | sed 's/^/    /'
    violations=$((violations + 1))
  fi
done

if [ "$violations" -eq 0 ]; then
  echo "PASS: no literal \${...} patterns in any pipelines/*.yml file"
  exit 0
else
  echo ""
  echo "$violations file(s) violate docs/adr/0007's rule. Map Woodpecker's own CI_*"
  echo "variables via plain shell 'export VAR=\"\$OTHER\"' instead, matching the"
  echo "pattern already used in pipelines/fast.woodpecker.yml's inline-comments step."
  exit 1
fi
