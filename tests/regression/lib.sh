# tests/regression/lib.sh — shared helpers for the regression suite.
#
# Every test in this directory reproduces one SADR's live experiment as an
# assertable script instead of a hand-followed "Reproduce it yourself"
# section. These helpers exist because every one of those SADRs hit the
# same handful of real bugs independently (see the comments at each call
# site below) — centralising them here means a future test gets the fix
# for free instead of rediscovering it a fourth or fifth time.
#
# Not a general-purpose library: every function here is scoped to exactly
# what this project's own experiments needed, confirmed live, not a
# speculative wrapper around the whole Gitea/Woodpecker API surface.

set -u

# --- Structural fix for the Host: gitea:3500 bug -----------------------
# Cost real time three separate times (SADR-0008, SADR-0011, SADR-0012)
# despite being documented as a comment at every call site each time --
# SADR-0012 named this explicitly as needing an actual fix, not a fourth
# reminder. Gitea derives clone_url (and the webhook payload built from
# it) from the TRIGGERING REQUEST'S Host header, not solely ROOT_URL, and
# every call that could create or move a ref needs it -- which turned out
# to include PR creation, not just pushes, found the hard way in
# docs/adr/0011.
#
# Rather than trust every call site to remember it (proven three times
# not to work), shadow the `curl` command itself with a shell function.
# Every existing `curl ...` call in this suite -- and every future one --
# now carries the header automatically whenever the URL targets Gitea's
# port, with zero call-site changes required. Matching on ":3500"
# specifically (not just Gitea's presence) is safe across every compose
# profile in this project: Woodpecker is always :8000, Gitea is always
# :3500, and nothing else in these test harnesses uses either port.
# Falls through to the real curl untouched for every other call
# (Woodpecker's API, anything else) -- `command curl` bypasses this
# shadow deliberately, so it can't recurse into itself.
curl() {
  for arg in "$@"; do
    case "$arg" in
      *:3500*)
        command curl -H "Host: ${GITEA_HOST_HEADER:-gitea:3500}" "$@"
        return $?
        ;;
    esac
  done
  command curl "$@"
}
# -------------------------------------------------------------------------

PASS_COUNT=0
FAIL_COUNT=0
CURRENT_TEST=""

test_start() {
  CURRENT_TEST="$1"
  echo ""
  echo "=== $1 ==="
}

pass() {
  echo "  PASS: $1"
  PASS_COUNT=$((PASS_COUNT + 1))
}

fail() {
  echo "  FAIL: $1" >&2
  FAIL_COUNT=$((FAIL_COUNT + 1))
}

# assert_eq <actual> <expected> <description>
assert_eq() {
  if [ "$1" = "$2" ]; then
    pass "$3 (got: $1)"
  else
    fail "$3 -- expected '$2', got '$1'"
  fi
}

# assert_http <actual_code> <expected_code> <description>
assert_http() {
  assert_eq "$1" "$2" "$3"
}

regression_summary() {
  echo ""
  echo "=== regression summary: ${PASS_COUNT} passed, ${FAIL_COUNT} failed ==="
  [ "$FAIL_COUNT" -eq 0 ]
}

# wait_for_woodpecker_healthy <base_url>
# Confirmed live from Woodpecker's own source this session (docs/adr/0011):
# GET /healthz pings the DB and returns 204 on success, not a JSON body
# the way Gitea's does -- do not reuse wait_for_gitea_healthy's grep-based
# check here, the response shapes are genuinely different.
wait_for_woodpecker_healthy() {
  # Same class of bug as _run_policy_eval (docs/adr's own regression
  # test found it twice): a bare `code=$(curl ...)` assignment aborts the
  # whole script under `set -e` the instant curl itself fails -- and on
  # an early retry, before Woodpecker's socket is even listening, it
  # reliably does (connection refused, or an empty reply mid-startup).
  # set +e/-e around the one command that's expected to fail sometimes.
  base_url="$1"
  i=0
  while [ "$i" -lt 60 ]; do
    set +e
    code=$(curl -s -o /dev/null -w "%{http_code}" -m 3 "${base_url}/healthz" 2>/dev/null)
    set -e
    [ "$code" = "204" ] && return 0
    i=$((i + 1))
    sleep 2
  done
  echo "woodpecker did not become healthy in time" >&2
  return 1
}

# woodpecker_get_pat <gitea_url> <woodpecker_url> <username> <password> <oauth_client_id>
# The full OAuth2 login dance (docs/adr/0004's method) followed by the
# CLEAN way to mint a durable API token (docs/adr/0011's fix for
# SADR-0004's own decision item 3): GET /web-config.js with a session
# cookie for a legitimately-issued CSRF token, then POST /api/user/token
# once -- never reading the CSRF secret out of the database, which this
# project's own harness permission classifier blocked outright when
# tried live.
#
# gitea_url must be the same "gitea:3500"-style host compose/minimal's
# ROOT_URL actually uses; this function rewrites it to the host-callable
# form itself (see gitea_host_callable below) wherever it needs to
# actually issue a request from outside the compose network.
woodpecker_get_pat() {
  gitea_internal_url="$1"; woodpecker_url="$2"; username="$3"; password="$4"; oauth_client_id="$5"
  gitea_host_url="$6"   # the host-reachable form, e.g. http://127.0.0.1:3500
  cookies=$(mktemp)

  curl -s -c "$cookies" -b "$cookies" -X POST "${gitea_host_url}/user/login" \
    --data-urlencode "user_name=${username}" --data-urlencode "password=${password}" \
    --data-urlencode "remember=on" >/dev/null

  authz_headers=$(mktemp)
  curl -s -c "$cookies" -b "$cookies" "${woodpecker_url}/authorize" -D "$authz_headers" -o /dev/null

  location=$(grep -i '^Location:' "$authz_headers" | sed 's/[Ll]ocation: //' | tr -d '\r')
  qs="${location#*\?}"
  rm -f "$authz_headers"

  grant_page=$(mktemp)
  curl -s -c "$cookies" -b "$cookies" "${gitea_host_url}/login/oauth/authorize?${qs}" -o "$grant_page"

  # state and redirect_uri come straight back out of the consent page's
  # own hidden fields rather than being re-derived -- Gitea validates
  # them against exactly what it issued.
  state=$(grep -o 'name="state" value="[^"]*"' "$grant_page" | sed 's/.*value="//;s/"$//')
  redirect_uri=$(grep -o 'name="redirect_uri" value="[^"]*"' "$grant_page" | sed 's/.*value="//;s/"$//')
  rm -f "$grant_page"

  grant_headers=$(mktemp)
  curl -s -c "$cookies" -b "$cookies" -X POST "${gitea_host_url}/login/oauth/grant" \
    --data-urlencode "client_id=${oauth_client_id}" \
    --data-urlencode "state=${state}" \
    --data-urlencode "scope=" --data-urlencode "nonce=" \
    --data-urlencode "redirect_uri=${redirect_uri}" \
    --data-urlencode "granted=true" \
    -D "$grant_headers" -o /dev/null

  grant_location=$(grep -i '^Location:' "$grant_headers" | sed 's/[Ll]ocation: //' | tr -d '\r')
  code=$(echo "$grant_location" | sed -n 's/.*code=\([^&]*\).*/\1/p')
  rm -f "$grant_headers"

  curl -s -c "$cookies" -b "$cookies" "${woodpecker_url}/authorize?code=${code}&state=${state}" -o /dev/null

  webconfig=$(mktemp)
  curl -s -c "$cookies" -b "$cookies" "${woodpecker_url}/web-config.js" -o "$webconfig"
  csrf=$(grep -o '"eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]*\.[A-Za-z0-9_-]*"' "$webconfig" | head -1 | tr -d '"')
  rm -f "$webconfig"

  pat=$(curl -s -b "$cookies" -X POST "${woodpecker_url}/api/user/token" -H "X-CSRF-TOKEN: ${csrf}")
  rm -f "$cookies"
  printf '%s' "$pat"
}

# wait_for_gitea_healthy <base_url>
# Gitea's health endpoint is JSON. Do not match a literal serialized form:
# v1.27.2 pretty-prints it while earlier harness runs returned compact JSON.
# Parsing the field tests the actual contract, not whitespace formatting.
wait_for_gitea_healthy() {
  base_url="$1"
  i=0
  while [ "$i" -lt 60 ]; do
    if curl -s -m 3 "${base_url}/api/healthz" 2>/dev/null \
      | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin).get("status") == "pass" else 1)' 2>/dev/null; then
      return 0
    fi
    i=$((i + 1))
    sleep 2
  done
  echo "gitea did not become healthy in time" >&2
  return 1
}

# gitea_create_user <base_url> <container> <username> <password> <email> [--admin]
# Uses `gitea admin user create` via docker exec -- the same idempotent-ish
# bootstrap method proven live in docs/adr/0009 and docs/adr/0011. Callers
# are expected to use a fresh harness per run, so no existence check here.
gitea_create_user() {
  base_url="$1"; container="$2"; username="$3"; password="$4"; email="$5"; admin_flag="${6:-}"
  docker exec -u git "$container" gitea admin user create \
    --username "$username" --password "$password" --email "$email" \
    ${admin_flag:+--admin} --must-change-password=false >/dev/null 2>&1
}

# gitea_mint_token <base_url> <username> <password> <token_name> <scopes_json_array>
# e.g. gitea_mint_token "$GITEA" alice AlicePw123! alice-token '["write:repository"]'
gitea_mint_token() {
  base_url="$1"; username="$2"; password="$3"; token_name="$4"; scopes="$5"
  curl -s -u "${username}:${password}" -X POST "${base_url}/api/v1/users/${username}/tokens" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"${token_name}\",\"scopes\":${scopes}}" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['sha1'])"
}

# run_policy_eval <owner> <repo> <pr_number> <readonly_token>
# Runs policy-eval/verify-approvals.py against a live PR. Always returns
# 0 itself; the real exit code lands in $POLICY_EVAL_EXIT, and stdout+
# stderr land in /tmp/policy-eval-output.txt for callers that want to
# grep the reasoning, not just the exit code. Deliberate, and trickier
# than it first looked: this whole suite runs under `set -e`, which
# fires the INSTANT any bare simple command fails -- not "when the
# function it's inside eventually returns nonzero". Found live, twice:
# the first fix attempt just appended a `POLICY_EVAL_EXIT=$?` line after
# the python3 call, which does nothing -- set -e aborts AT the failing
# python3 command itself, so that next line is never reached, silently.
# The only thing that actually works is disabling -e for the exact
# command that's allowed to fail, then restoring it immediately after.
run_policy_eval() {
  owner="$1"; repo="$2"; pr_number="$3"; readonly_token="$4"
  set +e
  GITEA_URL="$GITEA" POLICY_EVAL_GITEA_TOKEN="$readonly_token" \
    CI_REPO_OWNER="$owner" CI_REPO_NAME="$repo" CI_COMMIT_PULL_REQUEST="$pr_number" \
    python3 "${SCRIPT_DIR}/../../policy-eval/verify-approvals.py" > /tmp/policy-eval-output.txt 2>&1
  POLICY_EVAL_EXIT=$?
  set -e
}

# git_push_origin <repo_dir> <token> <branch_name>
# Pushes "origin" (already has the correct /owner/repo.git path from
# whatever cloned repo_dir) with the token as an Authorization header --
# never rebuild a push URL by hand, see the two failed attempts recorded
# in git history for this file (git blame this line if curious). Always
# carries the Host: gitea:3500 header unconditionally now -- the `curl`
# shadow function above closes this for every curl call automatically,
# but `git push` makes its own HTTP requests via libcurl, not the shell
# `curl` command, so it needs the same fix applied explicitly here
# instead of inheriting it for free.
git_push_origin() {
  repo_dir="$1"; token="$2"; branch_name="$3"
  git -C "$repo_dir" -c http.extraHeader="Authorization: token ${token}" \
      -c http.extraHeader="Host: ${GITEA_HOST_HEADER:-gitea:3500}" push -q origin "$branch_name" 2>&1
}

# git_push_feature_branch <base_url> <user> <token> <repo> <branch_name> <filename> <content> [owner]
# Real git clone -> branch -> commit -> push, off whatever the repo's
# current default branch is. Deliberately not the Contents API's
# new_branch_name shortcut -- found unreliable live (this suite's own
# first run) once the source branch carries any protection rule at all.
# owner defaults to "gateadmin" (every repo in this suite until team
# whitelists needed an org-owned one) -- found live: hardcoding it here
# unconditionally meant an org-owned repo's clone silently targeted the
# wrong (nonexistent) path, "gateadmin/<repo>" instead of "<org>/<repo>".
git_push_feature_branch() {
  base_url="$1"; user="$2"; token="$3"; repo="$4"; branch_name="$5"; filename="$6"; content="$7"
  owner="${8:-gateadmin}"
  work_dir=$(mktemp -d)
  git clone -q "${base_url}/${owner}/${repo}.git" "$work_dir" 2>/dev/null
  (
    cd "$work_dir"
    git config user.email "${user}@example.com"
    git config user.name "$user"
    git checkout -q -b "$branch_name"
    printf '%s\n' "$content" > "$filename"
    git add "$filename"
    git commit -q -m "${user}: ${filename}"
  )
  git_push_origin "$work_dir" "$token" "$branch_name"
  rm -rf "$work_dir"
}
