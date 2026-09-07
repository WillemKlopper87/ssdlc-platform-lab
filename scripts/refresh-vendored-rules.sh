#!/bin/sh
# refresh-vendored-rules.sh — re-vendor Semgrep-syntax rules from
# opengrep/opengrep-rules and report (or propose) an update.
#
# Semgrep's own Registry rules cannot legally be vendored into this repo
# (policy/vendored-rules/README.md: the Semgrep Rules License forbids
# redistributing them to a project that serves multiple teams, which this
# platform's whole purpose falls under). The vendored set is sourced from
# opengrep/opengrep-rules instead, pinned to one commit, updated only by
# reviewed PR -- matching docs/DESIGN.md's Rule-set-updates guidance:
# "Pulling live from the Registry at scan time means an upstream rule
# change can block every PR in the estate with no review and no rollback
# ... Rule changes go through the same gate as code."
#
# This script is the "re-clone, diff, open a normal PR" process
# policy/vendored-rules/README.md's own "Updating" section already
# describes in prose, made mechanical. It never pushes directly to main
# and never auto-merges anything -- the diff it produces is a PROPOSAL,
# reviewed exactly like any other pull request.
#
# Scope replicated exactly from policy/vendored-rules/README.md's own
# "Scope" section -- secrets/ from generic/secrets/, one directory per
# language from that language's own lang/security/ (or dockerfile/security/)
# directory. Deliberately NOT the full opengrep-rules tree.
#
# Usage:
#   scripts/refresh-vendored-rules.sh [--source-ref <git-ref>]
#
# Exit codes (a caller -- a human, or the Woodpecker pipeline wrapping
# this -- branches on these; none of them indicate a bug in this script):
#   0  already up to date, nothing proposed
#   1  usage or environment error
#   2  an update is available; written to $REFRESH_OUTPUT_DIR for review,
#      but GH_TOKEN/GH_REPO were not set so no PR was opened
#   3  an update is available and a PR was opened (see stdout for its URL)
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)
VENDORED_DIR="${ROOT_DIR}/policy/vendored-rules"
SOURCE_REPO="https://github.com/opengrep/opengrep-rules.git"
SOURCE_REF="${1:-}"
if [ "${SOURCE_REF}" = "--source-ref" ]; then
  SOURCE_REF="${2:?--source-ref requires a value}"
fi

# The exact language set policy/vendored-rules/README.md documents as
# vendored today. Adding a language here is itself a reviewable decision
# (more surface area, more license/scope to re-confirm) -- this script
# refreshes the existing set, it does not silently grow it.
LANGUAGES="bash c csharp dockerfile go java javascript php python ruby typescript"

WORK_DIR=$(mktemp -d)
cleanup() { rm -rf "${WORK_DIR}"; }
trap cleanup EXIT

echo "==> cloning ${SOURCE_REPO}${SOURCE_REF:+ at ${SOURCE_REF}}"
git clone -q --depth 50 "${SOURCE_REPO}" "${WORK_DIR}/opengrep-rules"
if [ -n "${SOURCE_REF}" ]; then
  git -C "${WORK_DIR}/opengrep-rules" checkout -q "${SOURCE_REF}"
fi
NEW_COMMIT=$(git -C "${WORK_DIR}/opengrep-rules" rev-parse HEAD)
echo "    resolved to ${NEW_COMMIT}"

# python3, not sed: found live that BusyBox sed (the alpine:3 pipeline
# image Woodpecker actually runs this in -- confirmed by re-running this
# exact sed invocation inside that same image) does not handle this
# pattern's backreference the way GNU sed does and silently matches
# nothing, no error, no warning. python3 is already a hard dependency
# below (the README rewrite); using it here too removes an entire class
# of "which coreutils flavor is installed" bugs rather than chasing them
# one at a time.
CURRENT_COMMIT=$(python3 -c "
import re, sys
text = open(sys.argv[1], encoding='utf-8').read()
m = re.search(r'^- \*\*Commit pinned:\*\* \`([0-9a-f]+)\`', text, re.MULTILINE)
print(m.group(1) if m else '')
" "${VENDORED_DIR}/README.md")
echo "    currently vendored: ${CURRENT_COMMIT:-none recorded}"

if [ "${NEW_COMMIT}" = "${CURRENT_COMMIT}" ]; then
  echo "==> policy/vendored-rules already at ${NEW_COMMIT}, nothing to do"
  exit 0
fi

# Reproduce the exact selective copy policy/vendored-rules/README.md
# documents: secrets/ from generic/secrets/, each language from its own
# lang/security/ (dockerfile/security/ for dockerfile), dropping every
# framework-specific subdirectory that would otherwise come along.
STAGE_DIR="${WORK_DIR}/staged"
mkdir -p "${STAGE_DIR}/secrets"
cp -r "${WORK_DIR}/opengrep-rules/generic/secrets/." "${STAGE_DIR}/secrets/"
for lang in ${LANGUAGES}; do
  src="${WORK_DIR}/opengrep-rules/${lang}/security"
  if [ ! -d "${src}" ]; then
    echo "    WARNING: ${lang}/security no longer exists upstream -- skipping (was this renamed?)" >&2
    continue
  fi
  mkdir -p "${STAGE_DIR}/${lang}"
  cp -r "${src}/." "${STAGE_DIR}/${lang}/"
done
cp "${VENDORED_DIR}/LICENSE" "${STAGE_DIR}/LICENSE" 2>/dev/null || true

# Diff against what's actually committed today, ignoring README.md itself
# (its provenance section is expected to change every refresh) and LICENSE
# (copied forward unconditionally above, not sourced fresh per rule file).
#
# Copying both sides into scratch trees with those two files removed,
# then a plain `diff -rq` with no flags beyond that, rather than
# `diff --exclude=...`: found live that the Woodpecker pipeline's
# alpine:3 image has BusyBox's diff, not GNU diff, and BusyBox's
# `diff -rq` has no --exclude option at all ("unrecognized option") --
# this portable form works identically under either implementation.
COMPARE_OLD="${WORK_DIR}/compare-old"
COMPARE_NEW="${WORK_DIR}/compare-new"
cp -r "${VENDORED_DIR}" "${COMPARE_OLD}"
cp -r "${STAGE_DIR}" "${COMPARE_NEW}"
rm -f "${COMPARE_OLD}/README.md" "${COMPARE_OLD}/LICENSE" "${COMPARE_NEW}/README.md" "${COMPARE_NEW}/LICENSE"
if diff -rq "${COMPARE_OLD}" "${COMPARE_NEW}" >"${WORK_DIR}/diff-summary.txt" 2>&1; then
  echo "==> content identical to what's vendored even though the upstream commit moved (${CURRENT_COMMIT:-none} -> ${NEW_COMMIT})"
  echo "    updating only the recorded commit SHA would be a no-op rule change -- treating as up to date"
  exit 0
fi

CHANGED_COUNT=$(wc -l <"${WORK_DIR}/diff-summary.txt" | tr -d ' ')
echo "==> ${CHANGED_COUNT} file(s) differ:"
cat "${WORK_DIR}/diff-summary.txt"

OUTPUT_DIR="${REFRESH_OUTPUT_DIR:-${WORK_DIR}/output}"
rm -rf "${OUTPUT_DIR}"
cp -r "${STAGE_DIR}" "${OUTPUT_DIR}"
echo "==> proposed policy/vendored-rules content written to ${OUTPUT_DIR}"

if [ -z "${GH_TOKEN:-}" ] || [ -z "${GH_REPO:-}" ]; then
  echo "==> GH_TOKEN/GH_REPO not set -- reporting the diff only, opening no PR"
  echo "    review ${OUTPUT_DIR}, then update policy/vendored-rules/README.md's"
  echo "    Commit pinned / Vendored fields to ${NEW_COMMIT} / today's date"
  exit 2
fi

BRANCH="chore/refresh-vendored-rules-$(date +%Y%m%d)"
echo "==> GH_TOKEN present -- opening a PR against ${GH_REPO} on branch ${BRANCH}"
# Clone GH_REPO fresh via GH_TOKEN rather than assuming this script's own
# working directory (ROOT_DIR) already IS a checkout of it with push
# access configured. Found live: deployed standalone (this script and a
# copy of policy/vendored-rules committed into a separate ops repo for a
# demo, rather than living inside ssdlc-platform-lab's own checkout), the
# original version of this step ran `git push origin` against THAT repo's
# own Gitea remote while asking `gh` to open a PR against a completely
# different GitHub repo -- pushed the branch nowhere `gh` could see it,
# then failed with an unrelated Gitea auth error. Cloning the actual
# named target explicitly makes this correct regardless of where the
# script happens to run from.
GH_WORK_DIR="${WORK_DIR}/gh-repo"
git clone -q "https://x-access-token:${GH_TOKEN}@github.com/${GH_REPO}.git" "${GH_WORK_DIR}"
git -C "${GH_WORK_DIR}" checkout -q -b "${BRANCH}"
rsync -a --delete --exclude=README.md --exclude=LICENSE "${STAGE_DIR}/" "${GH_WORK_DIR}/policy/vendored-rules/"
python3 - "${GH_WORK_DIR}/policy/vendored-rules/README.md" "${CURRENT_COMMIT}" "${NEW_COMMIT}" <<'PYEOF'
import sys, datetime
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path, encoding="utf-8") as fh:
    text = fh.read()
today = datetime.date.today().isoformat()
text = text.replace(f"**Commit pinned:** `{old}`", f"**Commit pinned:** `{new}`")
text = text.replace("- **Vendored:**", f"- **Vendored:** {today} (refreshed from `{old}`)\n- **Previously vendored:**", 1)
with open(path, "w", encoding="utf-8") as fh:
    fh.write(text)
PYEOF
git -C "${GH_WORK_DIR}" add policy/vendored-rules
git -C "${GH_WORK_DIR}" -c user.email="platform@ssdlc.local" -c user.name="ssdlc-platform-rule-refresh" \
  commit -q -m "chore: refresh vendored Semgrep rules to opengrep-rules@${NEW_COMMIT}

${CHANGED_COUNT} file(s) changed. This is a PROPOSAL -- it changes what
the security gate flags across every onboarded repo and must be reviewed
like any other policy change (docs/DESIGN.md, Rule-set updates)."
git -C "${GH_WORK_DIR}" push -q origin "${BRANCH}"
PR_URL=$(GH_TOKEN="${GH_TOKEN}" gh pr create --repo "${GH_REPO}" --head "${BRANCH}" \
  --title "chore: refresh vendored Semgrep rules to opengrep-rules@${NEW_COMMIT:0:12}" \
  --body "Automated rule-set refresh (scripts/refresh-vendored-rules.sh). ${CHANGED_COUNT} file(s) changed from \`${CURRENT_COMMIT:-none}\` to \`${NEW_COMMIT}\`. Never auto-merged -- this changes what the gate flags across every onboarded repo. Review the diff like any other policy change before merging.")
echo "==> opened ${PR_URL}"
exit 3
