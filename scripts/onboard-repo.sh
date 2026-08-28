#!/bin/sh
# onboard-repo.sh — bring a repo onto the SSDLC platform, once, automatically.
#
# This is the automation boundary: everything in this script is a platform
# admin's one-time setup action. Nothing here is something a developer ever
# runs, configures, or even needs to know exists. After this script
# completes, the developer experience is: clone, code, push, read the
# result Gitea/Woodpecker shows on the commit/PR. That is the whole
# interaction surface — this is the concrete mechanism that guarantees it.
#
# What it does, in order:
#   1. Activates the repo in Woodpecker, authenticated with a durable
#      Bearer token (docs/adr/0011's fix for SADR-0004's own decision
#      item 3 -- GET /web-config.js with a session cookie for a
#      legitimately-issued CSRF token, then POST /api/user/token once;
#      never the hand-rolled session+CSRF dance SADR-0004 explicitly
#      flagged as "not the pattern for production automation" and this
#      script used unmodified until docs/adr/0016)
#   2. Commits pipelines/fast.woodpecker.yml as .woodpecker.yml, plus
#      everything it depends on at runtime -- policy-eval/, normalise/,
#      policy/severity.rego (docs/adr/0016; previously only
#      .woodpecker.yml itself was committed, silently leaving
#      approval-check's own dependency on policy-eval/verify-approvals.py
#      unmet in any repo onboarded for real, masked only because every
#      live test of it manually copied the file in first)
#   3. Adds the gate bot as a write collaborator (docs/adr/0011) -- it
#      needs to be able to cast the "bot" half of the two required
#      approvals set below
#   4. Sets branch protection requiring the fast gate's status context
#      (glob-matched — see DESIGN.md D1's correction: Woodpecker's real
#      context string is "ssdlc/security-gate/<event>/<workflow>", not the
#      bare string originally assumed) and required_approvals: 2 (D2,
#      live-tested end-to-end in docs/adr/0011)
#
# IMPORTANT: required_approvals: 2 means nothing merges until BOTH the bot
# and a human approve. scripts/bot-approver.py must already be running
# (watching this repo) for onboarded repos to be mergeable at all -- see
# docs/adr/0011 before running this script against a repo real developers
# depend on.
#
# Usage:
#   ./onboard-repo.sh <owner> <repo>
#
# Requires environment variables (see .env in the compose profile this is
# run against):
#   GITEA_URL           e.g. http://127.0.0.1:3500
#   WOODPECKER_URL       e.g. http://127.0.0.1:8000
#   GITEA_ADMIN_TOKEN     a Gitea API token with write:repository, write:admin
#   WOODPECKER_TOKEN      a durable Woodpecker personal access token (see
#                          docs/adr/0011's woodpecker_get_pat method, or
#                          any equivalent one-time login) -- this script
#                          does NOT mint it for you; that's a one-time
#                          interactive OAuth step, not something to
#                          re-derive on every onboarding run

set -eu

OWNER="${1:?usage: onboard-repo.sh <owner> <repo>}"
REPO="${2:?usage: onboard-repo.sh <owner> <repo>}"

: "${GITEA_URL:?set GITEA_URL}"
: "${WOODPECKER_URL:?set WOODPECKER_URL}"
: "${GITEA_ADMIN_TOKEN:?set GITEA_ADMIN_TOKEN}"
: "${WOODPECKER_TOKEN:?set WOODPECKER_TOKEN}"

echo "==> Onboarding ${OWNER}/${REPO} onto the SSDLC platform"

echo "--> [1/6] Looking up the repo's forge-internal ID"
REPO_ID=$(curl -sf "${GITEA_URL}/api/v1/repos/${OWNER}/${REPO}" \
  -H "Authorization: token ${GITEA_ADMIN_TOKEN}" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
echo "    repo id: ${REPO_ID}"

echo "--> [2/6] Activating in Woodpecker"
ACTIVATE_HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer ${WOODPECKER_TOKEN}" \
  -X POST "${WOODPECKER_URL}/api/repos?forge_remote_id=${REPO_ID}")
if [ "$ACTIVATE_HTTP" = "200" ]; then
  echo "    activated"
elif [ "$ACTIVATE_HTTP" = "409" ]; then
  echo "    already active, continuing"
else
  echo "    ERROR: activation returned HTTP ${ACTIVATE_HTTP}" >&2
  exit 1
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

echo "--> [3/6] Generating the differential-gating baseline (docs/adr/0024)"
# Scans the repo's CURRENT state -- deliberately before any platform file is
# committed below, so the baseline reflects the repo's own pre-existing
# findings, not the platform's own paved-road files (normalise/, policy/,
# etc.) that are about to land in the same tree. Re-running onboarding
# against an already-onboarded repo refreshes (shrinks) the existing
# baseline rather than starting over -- generate-baseline.py's own
# intersect-with-existing logic handles that.
BASELINE_WORK_DIR=$(mktemp -d)
git -c http.extraHeader="Authorization: token ${GITEA_ADMIN_TOKEN}" clone -q "${GITEA_URL}/${OWNER}/${REPO}.git" "${BASELINE_WORK_DIR}"
# docs/adr/0025 (SSDLC_Code_report.md H1): `mktemp` always creates an
# EMPTY file, so a bare `BASELINE_TMP_FILE=$(mktemp)` made
# generate-baseline.py's own "does this path exist" check always true --
# even on a repo's genuine first onboarding, with nothing committed yet.
# Not created here; only created below once an existing baseline's
# content is actually confirmed and decoded.
BASELINE_TMP_FILE=$(mktemp -u)
# HTTP status distinguishes "genuinely no baseline yet" (404 -- first
# onboarding, generate-baseline.py gets --create) from "something is
# already there" (200 -- decode it, no --create, so a shrink-only refresh
# is the only path) from anything else (network/auth/server error --
# FAIL, not silently guess either way). The previous version of this
# script folded a failed API call into the same "empty string" signal as
# a genuine 404, and swallowed a base64 decode failure with `|| true` --
# both could turn a shrink-only refresh into a fresh full-acceptance
# snapshot on nothing more than a transient hiccup (SSDLC_Code_report.md's
# own wording for exactly this risk).
BASELINE_HTTP_RESPONSE=$(mktemp)
BASELINE_HTTP_STATUS=$(curl -s -o "${BASELINE_HTTP_RESPONSE}" -w "%{http_code}" \
  "${GITEA_URL}/api/v1/repos/${OWNER}/${REPO}/contents/.ssdlc/baseline.json" \
  -H "Authorization: token ${GITEA_ADMIN_TOKEN}")
GENERATE_BASELINE_CREATE_FLAG=""
if [ "$BASELINE_HTTP_STATUS" = "404" ]; then
  GENERATE_BASELINE_CREATE_FLAG="--create"
elif [ "$BASELINE_HTTP_STATUS" = "200" ]; then
  # The response path is passed as argv, not interpolated into the -c
  # string: found live that a path embedded in the script TEXT (as the
  # first version of this fix did) never gets Git Bash's own argv-level
  # POSIX-to-Windows path translation, since that translation only
  # applies to arguments bash itself passes to the invoked program --
  # breaking this specific decode step whenever onboard-repo.sh is run
  # from Windows Git Bash (a real, previously-used dev/test path for this
  # project, even though production always runs it from a real Linux
  # host). Passing it as sys.argv[1] instead lets bash translate it
  # exactly the same way it already does for generate-baseline.py's own
  # workspace argument below.
  python3 -c "
import base64, json, sys
with open(sys.argv[1], encoding='utf-8') as fh:
    document = json.load(fh)
sys.stdout.buffer.write(base64.b64decode(document['content']))
" "${BASELINE_HTTP_RESPONSE}" > "${BASELINE_TMP_FILE}"
  # set -eu (top of this script) aborts here on either python3's own
  # non-zero exit (malformed API response, missing 'content' key) or a
  # failed redirect -- deliberately no `|| true` guard, unlike the
  # version this replaces.
else
  echo "    ERROR: could not check for an existing baseline (HTTP ${BASELINE_HTTP_STATUS}) -- refusing to guess whether one exists" >&2
  rm -f "${BASELINE_HTTP_RESPONSE}"
  rm -rf "${BASELINE_WORK_DIR}"
  exit 1
fi
rm -f "${BASELINE_HTTP_RESPONSE}"
HEAD_SHA=$(git -C "${BASELINE_WORK_DIR}" rev-parse HEAD)
python3 "${SCRIPT_DIR}/generate-baseline.py" "${BASELINE_WORK_DIR}" "${BASELINE_TMP_FILE}" --repo "${OWNER}/${REPO}" --commit "${HEAD_SHA}" ${GENERATE_BASELINE_CREATE_FLAG}
rm -rf "${BASELINE_WORK_DIR}"

echo "--> [4/6] Committing the fast-gate pipeline template and its dependencies"

# commit_file <local_path> <remote_path>
# Generalized from what used to be .woodpecker.yml-only logic. Found
# live, the hard way, building docs/adr/0016: `approval-check` (SADR-
# 0009) already ran `python3 policy-eval/verify-approvals.py` and had
# done so since it was added -- but onboard-repo.sh had ONLY ever
# committed .woodpecker.yml itself, never policy-eval/. Every prior live
# test of approval-check worked around this by manually copying the
# file into the test repo, silently masking a real gap in the actual
# onboarding automation until policy-eval-findings needed normalise/ and
# policy/ too and made it impossible to keep ignoring.
commit_file() {
  local_path="$1"
  remote_path="$2"
  content_b64=$(base64 -w0 "${local_path}" 2>/dev/null || base64 "${local_path}" | tr -d '\n')

  existing_sha=$(curl -s "${GITEA_URL}/api/v1/repos/${OWNER}/${REPO}/contents/${remote_path}" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" | python3 -c "import json,sys
try:
    print(json.load(sys.stdin)['sha'])
except Exception:
    print('')" 2>/dev/null || echo "")

  if [ -n "$existing_sha" ]; then
    payload=$(python3 -c "import json; print(json.dumps({
      'content': '''${content_b64}''',
      'message': 'ssdlc: update ${remote_path} (onboard-repo.sh)',
      'sha': '${existing_sha}',
      'branch': 'main'
    }))")
    curl -sf -X PUT "${GITEA_URL}/api/v1/repos/${OWNER}/${REPO}/contents/${remote_path}" \
      -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
      -d "${payload}" -o /dev/null
    echo "    updated ${remote_path}"
  else
    payload=$(python3 -c "import json; print(json.dumps({
      'content': '''${content_b64}''',
      'message': 'ssdlc: add ${remote_path} (onboard-repo.sh)',
      'branch': 'main'
    }))")
    curl -sf -X POST "${GITEA_URL}/api/v1/repos/${OWNER}/${REPO}/contents/${remote_path}" \
      -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
      -d "${payload}" -o /dev/null
    echo "    created ${remote_path}"
  fi
}

commit_file "${SCRIPT_DIR}/../pipelines/fast.woodpecker.yml" ".woodpecker.yml"
commit_file "${SCRIPT_DIR}/../policy-eval/verify-approvals.py" "policy-eval/verify-approvals.py"
commit_file "${SCRIPT_DIR}/../policy-eval/evaluate-findings.py" "policy-eval/evaluate-findings.py"
commit_file "${SCRIPT_DIR}/../normalise/gitleaks_adapter.py" "normalise/gitleaks_adapter.py"
commit_file "${SCRIPT_DIR}/../normalise/semgrep_adapter.py" "normalise/semgrep_adapter.py"
commit_file "${SCRIPT_DIR}/../normalise/trivy_adapter.py" "normalise/trivy_adapter.py"
commit_file "${SCRIPT_DIR}/../policy/severity.rego" "policy/severity.rego"
commit_file "${BASELINE_TMP_FILE}" ".ssdlc/baseline.json"
rm -f "${BASELINE_TMP_FILE}"

# commit_directory <local_dir> <remote_dir>
# docs/adr/0020: policy/vendored-rules/ is 594 files -- commit_file's
# one-API-call-per-file Contents API approach does not scale to that (594
# sequential HTTP round-trips per onboarding run). A real git clone/push
# is faster and simpler for a whole tree. Idempotent: a re-run with no
# actual content change produces an empty `git commit`, which this
# function treats as success, not an error, matching commit_file's own
# "already up to date" tolerance.
commit_directory() {
  local_dir="$1"
  remote_dir="$2"
  work_dir=$(mktemp -d)
  # The platform onboards private repositories too. Supplying the token as an
  # HTTP header avoids embedding it in a clone URL (and therefore in .git/
  # config or process output), matching the authenticated push below.
  git -c http.extraHeader="Authorization: token ${GITEA_ADMIN_TOKEN}" clone -q "${GITEA_URL}/${OWNER}/${REPO}.git" "${work_dir}" 2>/dev/null || {
    echo "    ERROR: could not clone ${OWNER}/${REPO} to commit ${remote_dir}" >&2
    rm -rf "${work_dir}"
    exit 1
  }
  mkdir -p "${work_dir}/${remote_dir}"
  cp -r "${local_dir}/." "${work_dir}/${remote_dir}/"
  (
    cd "${work_dir}"
    git config user.email "platform@ssdlc.local"
    git config user.name "ssdlc-platform-onboarding"
    git add "${remote_dir}"
    if git diff --cached --quiet; then
      echo "    ${remote_dir} already up to date, nothing to commit"
    else
      git commit -q -m "ssdlc: sync ${remote_dir} (onboard-repo.sh)"
      git -c http.extraHeader="Authorization: token ${GITEA_ADMIN_TOKEN}" push -q origin HEAD:main
      echo "    committed ${remote_dir} ($(find "${local_dir}" -type f | wc -l | tr -d ' ') files)"
    fi
  )
  rm -rf "${work_dir}"
}

commit_directory "${SCRIPT_DIR}/../policy/vendored-rules" "policy/vendored-rules"
echo "    all pipeline dependencies committed"

echo "--> [5/6] Adding the gate bot as a collaborator"
# Needed so it can hold the "bot" half of the two required approvals below
# -- write permission only, matching every other human collaborator; the
# bot's approve-only token (scripts/bot-approver.py, docs/adr/0011) is a
# separate, narrower credential from this collaborator grant itself.
GITEA_BOT_USER="${GITEA_BOT_USER:-gate-bot}"
curl -sf -X PUT "${GITEA_URL}/api/v1/repos/${OWNER}/${REPO}/collaborators/${GITEA_BOT_USER}" \
  -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
  -d '{"permission":"write"}' -o /dev/null
echo "    ${GITEA_BOT_USER} added as a write collaborator"

echo "--> [6/6] Setting branch protection on main"
# Glob-matched context, per DESIGN.md D1's correction (SADR-0004): the real
# Woodpecker context string includes /<event>/<workflow>, not just the
# bare "ssdlc/security-gate" this design originally assumed.
#
# push_whitelist for the platform account itself: without this, branch
# protection blocks the platform's OWN future re-runs of this script (e.g.
# to push an updated pipeline template) exactly as hard as it blocks a
# developer's direct push -- found live, the hard way, during Milestone 1
# testing (the second onboarding run 403'd on its own commit).
#
# Gitea's actual model, also found the hard way (the first fix attempt set
# enable_push_whitelist:true with enable_push left false, which the API
# silently accepted and then ignored): enable_push=false means NO ONE can
# push, full stop, whitelist or not. To get "only these accounts can push,
# everyone else must go through review" you need enable_push=true AND
# enable_push_whitelist=true together -- the whitelist only means anything
# once direct push is allowed in the first place, restricted to specific
# accounts.
#
# Security tradeoff, stated plainly: this gives whoever holds
# GITEA_ADMIN_TOKEN a standing bypass of PR review on every onboarded repo.
# That token must be scoped and protected accordingly -- this is the same
# concern D2 raises about auditing broad-scope PATs, applied to the
# platform's own automation identity. Acceptable for Milestone 1's scope
# (this script IS the platform admin's tool); revisit before any real
# developer team is onboarded.
AUTOMATION_USER=$(curl -sf "${GITEA_URL}/api/v1/user" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['login'])")

# required_approvals: 2, no whitelist -- D2's mitigation for SADR-0001's
# status-forgery bypass, live-tested end-to-end in docs/adr/0011 (bot
# approves once scanning steps are green, a human approves independently,
# Gitea blocks self-approval natively, merge needs both). Was 0 through
# Milestone 1/2 -- deliberately, since nothing gave the bot's vote until
# scripts/bot-approver.py existed. Raising it here without that service
# actually running against this repo makes every PR unmergeable, not
# safer: bot-approver.py must be running (watching this repo) before or
# immediately after onboarding, not left as a later step.
PROTECT_HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${GITEA_URL}/api/v1/repos/${OWNER}/${REPO}/branch_protections" \
  -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
  -d "{
    \"branch_name\": \"main\",
    \"enable_status_check\": true,
    \"status_check_contexts\": [\"ssdlc/security-gate/**\"],
    \"required_approvals\": 2,
    \"dismiss_stale_approvals\": true,
    \"enable_push\": true,
    \"enable_push_whitelist\": true,
    \"push_whitelist_usernames\": [\"${AUTOMATION_USER}\"]
  }")
if [ "$PROTECT_HTTP" = "201" ]; then
  echo "    protection created (push whitelist: ${AUTOMATION_USER})"
else
  curl -sf -X PATCH "${GITEA_URL}/api/v1/repos/${OWNER}/${REPO}/branch_protections/main" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d "{
      \"enable_status_check\": true,
      \"status_check_contexts\": [\"ssdlc/security-gate/**\"],
      \"required_approvals\": 2,
      \"dismiss_stale_approvals\": true,
      \"enable_push\": true,
      \"enable_push_whitelist\": true,
      \"push_whitelist_usernames\": [\"${AUTOMATION_USER}\"]
    }" -o /dev/null
  echo "    protection updated (push whitelist: ${AUTOMATION_USER})"
fi

echo ""
echo "==> ${OWNER}/${REPO} onboarded."
echo "    Developers: clone it, code, push. That's the whole interaction."
echo "    Every push runs Gitleaks + Semgrep + Trivy automatically and reports"
echo "    pass/fail as the commit status, with details one click away in Woodpecker."
