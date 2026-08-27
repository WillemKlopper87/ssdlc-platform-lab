# tests/regression/05-bot-approver.sh — SADR-0011, automated.
#
# Positive path: bob approves (1/2), a pipeline run with a green scanning
# step but a failing approval-check (not enough approvals yet) exists,
# bot-approver.py casts the bot's vote and restarts the pipeline,
# approval-check now passes (2/2), and the PR actually merges. Negative
# path: a PR whose scanning step fails gets zero bot reviews.
#
# Requires GITEA, GITEA_INTERNAL, WOODPECKER, GITEA_ADMIN_TOKEN,
# WOODPECKER_PAT, FORGERY_CONTAINER already set by run-minimal-suite.sh.

_wp_pipelines() {
  curl -s "${WOODPECKER}/api/repos/${1}/pipelines" -H "Authorization: Bearer ${WOODPECKER_PAT}"
}

_wp_pipeline() {
  curl -s "${WOODPECKER}/api/repos/${1}/pipelines/${2}" -H "Authorization: Bearer ${WOODPECKER_PAT}"
}

# _wp_wait_for_pipeline <repo_id> <event> <commit_sha> -- prints the
# matching pipeline's number once it exists and is no longer pending/
# running (does not itself assert pass/fail, just settled).
_wp_wait_for_pipeline() {
  # set +e/-e around every command substitution in this loop -- a bare
  # `var=$(cmd)` assignment aborts the WHOLE SUITE under set -e the
  # instant cmd's exit code is nonzero (found live, twice already this
  # session: docs/adr's own regression suite hit this both in
  # _run_policy_eval and in wait_for_woodpecker_healthy). Early in a
  # poll loop -- before Woodpecker has scheduled anything for this commit
  # yet -- that is the expected, normal case, not an error.
  wp_repo_id="$1"; wp_event="$2"; wp_commit="$3"
  i=0
  while [ "$i" -lt 40 ]; do
    set +e
    number=$(_wp_pipelines "$wp_repo_id" | python3 -c "
import json, sys
data = json.load(sys.stdin)
for p in data:
    if p.get('event') == '$wp_event' and p.get('commit') == '$wp_commit':
        print(p['number'])
        break
")
    set -e
    if [ -n "$number" ]; then
      set +e
      status=$(_wp_pipeline "$wp_repo_id" "$number" | python3 -c "import json,sys; print(json.load(sys.stdin)['status'])")
      set -e
      if [ "$status" != "pending" ] && [ "$status" != "running" ]; then
        echo "$number"
        return 0
      fi
    fi
    i=$((i + 1))
    sleep 3
  done
  echo ""
  return 1
}

_wp_latest_pipeline_for_commit() {
  # Unlike _wp_wait_for_pipeline, doesn't wait -- used after a restart,
  # where the new pipeline number is unknown but always the highest id
  # among matches (docs/adr/0011: restart creates a NEW pipeline number,
  # it does not reuse the old one).
  wp_repo_id="$1"; wp_commit="$2"
  _wp_pipelines "$wp_repo_id" | python3 -c "
import json, sys
data = json.load(sys.stdin)
matches = [p for p in data if p.get('commit') == '$wp_commit' and p.get('event') == 'pull_request']
if matches:
    print(max(matches, key=lambda p: p['id'])['number'])
"
}

_wp_step_state() {
  # $1 = repo_id, $2 = pipeline number, $3 = step name
  _wp_pipeline "$1" "$2" | python3 -c "
import json, sys
d = json.load(sys.stdin)
for wf in d.get('workflows', []):
    for s in wf.get('children', []):
        if s['name'] == '$3':
            print(s['state'])
"
}

test_bot_approver() {
  test_start "SADR-0011: bot-approver"

  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" gate-bot-05 GateBot123Pw! gate-bot-05@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" alice-05 AlicePw123! alice-05@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" bob-05 BobPw123456! bob-05@example.com
  local bot_token alice_token bob_token readonly_token
  bot_token=$(gitea_mint_token "$GITEA" gate-bot-05 'GateBot123Pw!' bot-token '["write:repository"]')
  alice_token=$(gitea_mint_token "$GITEA" alice-05 'AlicePw123!' alice-token '["write:repository"]')
  bob_token=$(gitea_mint_token "$GITEA" bob-05 'BobPw123456!' bob-token '["write:repository"]')
  readonly_token=$(gitea_mint_token "$GITEA" gateadmin 'Gateadmin123!' policy-eval-readonly '["read:repository","read:issue"]')

  curl -s -X POST "${GITEA}/api/v1/user/repos" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" -d '{"name":"bot-approver-05","auto_init":true,"default_branch":"main"}' >/dev/null
  for u in alice-05 bob-05 gate-bot-05; do
    curl -s -X PUT "${GITEA}/api/v1/repos/gateadmin/bot-approver-05/collaborators/${u}" \
      -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null
  done
  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/bot-approver-05/branch_protections" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"branch_name":"main","required_approvals":2,"enable_status_check":true,"status_check_contexts":["ssdlc/security-gate/**"]}' >/dev/null

  local repo_id
  repo_id=$(curl -s "${GITEA}/api/v1/repos/gateadmin/bot-approver-05" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

  local wp_repo
  wp_repo=$(curl -s -X POST "${WOODPECKER}/api/repos?forge_remote_id=${repo_id}" -H "Authorization: Bearer ${WOODPECKER_PAT}")
  local wp_repo_id
  wp_repo_id=$(echo "$wp_repo" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

  curl -s -X POST "${WOODPECKER}/api/repos/${wp_repo_id}/secrets" -H "Authorization: Bearer ${WOODPECKER_PAT}" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"policy_eval_gitea_token\",\"value\":\"${readonly_token}\",\"events\":[\"push\",\"pull_request\"]}" >/dev/null

  # --- Positive path: push a working pipeline, get 1 of 2 approvals ---
  local work_dir
  work_dir=$(mktemp -d)
  git clone -q "${GITEA}/gateadmin/bot-approver-05.git" "$work_dir"
  (
    cd "$work_dir"
    git config user.email alice-05@example.com
    git config user.name alice-05
    git checkout -q -b feature/change
    mkdir -p policy-eval
    cp "${SCRIPT_DIR}/../../policy-eval/verify-approvals.py" policy-eval/verify-approvals.py
    cat > .woodpecker.yml <<'YAML'
steps:
  code-scan:
    image: alpine:3
    commands:
      - 'echo "simulating secrets+sast+dependencies all passing"'
    when:
      - event: [push, pull_request]

  approval-check:
    image: python:3.12-alpine
    environment:
      GITEA_URL: "http://gitea:3500"
      POLICY_EVAL_GITEA_TOKEN:
        from_secret: policy_eval_gitea_token
    commands:
      - python3 policy-eval/verify-approvals.py
    when:
      - event: pull_request
YAML
    echo "alice's change" > notes.txt
    git add .
    git commit -q -m "alice: add change + pipeline"
  )
  git_push_origin "$work_dir" "$alice_token" feature/change
  rm -rf "$work_dir"

  local pr1_json pr1_num pr1_head_sha
  # No manual Host header needed here -- lib.sh's curl() shadow adds it
  # automatically to every call whose URL targets :3500 (which $GITEA
  # does), including this one. PR *creation* needs it just as much as the
  # push does (docs/adr/0011); that's exactly why the fix lives in the
  # shadow now instead of at call sites like this one.
  pr1_json=$(curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/bot-approver-05/pulls" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"title":"alice change","head":"feature/change","base":"main"}')
  pr1_num=$(echo "$pr1_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")
  pr1_head_sha=$(echo "$pr1_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['head']['sha'])")

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/bot-approver-05/pulls/${pr1_num}/reviews" \
    -H "Authorization: token ${bob_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  local pipeline1
  pipeline1=$(_wp_wait_for_pipeline "$wp_repo_id" pull_request "$pr1_head_sha")
  assert_eq "$([ -n "$pipeline1" ] && echo present || echo missing)" "present" "positive path: a pipeline ran for the PR's head commit"

  local code_scan_state approval_check_state
  code_scan_state=$(_wp_step_state "$wp_repo_id" "$pipeline1" code-scan)
  approval_check_state=$(_wp_step_state "$wp_repo_id" "$pipeline1" approval-check)
  assert_eq "$code_scan_state" "success" "positive path: code-scan step is green"
  assert_eq "$approval_check_state" "failure" "positive path: approval-check fails with only 1/2 approvals"

  # $GITEA (host-reachable, 127.0.0.1:3500), not $GITEA_INTERNAL --
  # bot-approver.py runs as a plain host process in this test, not as a
  # container on minimal's own docker network, so "gitea" would not
  # resolve (confirmed live: socket.gaierror / getaddrinfo failed on the
  # first attempt). In real deployment it WOULD run as its own container
  # on that network, where the internal hostname is correct -- this is a
  # test-harness distinction, not something to change in the script.
  export GITEA_URL="$GITEA"
  export GITEA_BOT_TOKEN="$bot_token"
  export GITEA_BOT_LOGIN="gate-bot-05"
  export REPO_OWNER=gateadmin
  export REPO_NAME=bot-approver-05
  export WOODPECKER_URL="$WOODPECKER"
  export WOODPECKER_TOKEN="$WOODPECKER_PAT"
  export WOODPECKER_REPO_ID="$wp_repo_id"
  export RUN_ONCE=1
  # This historical fixture introduces the gate files in the feature PR.
  # Production/onboarded repositories keep them on protected main, where
  # the default GATE_CONTRACT_ENFORCE=1 protects them. The focused unit test
  # covers that fail-closed contract; keep this test focused on bot voting.
  export GATE_CONTRACT_ENFORCE=0
  python3 "${SCRIPT_DIR}/../../scripts/bot-approver.py" >/tmp/bot-approver-output.txt 2>&1

  local bot_reviews_count
  bot_reviews_count=$(curl -s "${GITEA}/api/v1/repos/gateadmin/bot-approver-05/pulls/${pr1_num}/reviews" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    | python3 -c "
import json, sys
reviews = json.load(sys.stdin)
print(sum(1 for r in reviews if r['user']['login'] == 'gate-bot-05' and r['state'] == 'APPROVED'))
")
  assert_eq "$bot_reviews_count" "1" "positive path: bot-approver cast exactly one approval"

  local pipeline1b approval_check_state2
  # The restart creates a NEW pipeline number (docs/adr/0011 bug 3) --
  # do NOT reuse _wp_wait_for_pipeline here, it would happily match the
  # OLD already-failed pipeline (same event+commit, already settled) and
  # return immediately with the wrong one. Wait explicitly for a pipeline
  # number different from $pipeline1 to appear and settle.
  i=0
  pipeline1b=""
  while [ "$i" -lt 40 ]; do
    set +e
    candidate=$(_wp_latest_pipeline_for_commit "$wp_repo_id" "$pr1_head_sha")
    set -e
    if [ -n "$candidate" ] && [ "$candidate" != "$pipeline1" ]; then
      set +e
      status=$(_wp_pipeline "$wp_repo_id" "$candidate" | python3 -c "import json,sys; print(json.load(sys.stdin)['status'])")
      set -e
      if [ "$status" != "pending" ] && [ "$status" != "running" ]; then
        pipeline1b="$candidate"
        break
      fi
    fi
    i=$((i + 1))
    sleep 3
  done
  assert_eq "$([ -n "$pipeline1b" ] && echo present || echo missing)" "present" "positive path: the restarted pipeline (a new number) settled"
  approval_check_state2=$(_wp_step_state "$wp_repo_id" "$pipeline1b" approval-check)
  assert_eq "$approval_check_state2" "success" "positive path: approval-check passes after the bot's restart (2/2 votes)"

  local merge_http
  merge_http=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${GITEA}/api/v1/repos/gateadmin/bot-approver-05/pulls/${pr1_num}/merge" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"Do":"merge"}')
  assert_eq "$merge_http" "200" "positive path: the PR actually merges"

  local merged
  merged=$(curl -s "${GITEA}/api/v1/repos/gateadmin/bot-approver-05/pulls/${pr1_num}" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['merged'])")
  assert_eq "$merged" "True" "positive path: merged flag confirms it independently"

  # --- Negative path: a PR whose scanning genuinely fails ---
  work_dir=$(mktemp -d)
  git clone -q "${GITEA}/gateadmin/bot-approver-05.git" "$work_dir"
  (
    cd "$work_dir"
    git config user.email alice-05@example.com
    git config user.name alice-05
    git checkout -q -b feature/broken
    cat > .woodpecker.yml <<'YAML'
steps:
  code-scan:
    image: alpine:3
    commands:
      - 'echo "simulating a real finding"'
      - 'exit 1'
    when:
      - event: [push, pull_request]

  approval-check:
    image: python:3.12-alpine
    environment:
      GITEA_URL: "http://gitea:3500"
      POLICY_EVAL_GITEA_TOKEN:
        from_secret: policy_eval_gitea_token
    commands:
      - python3 policy-eval/verify-approvals.py
    when:
      - event: pull_request
      - status: [success, failure]
YAML
    echo "broken change" > broken.txt
    git add .
    git commit -q -m "alice: change that fails scanning"
  )
  git_push_origin "$work_dir" "$alice_token" feature/broken
  rm -rf "$work_dir"

  local pr2_json pr2_num pr2_head_sha
  pr2_json=$(curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/bot-approver-05/pulls" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"title":"alice broken","head":"feature/broken","base":"main"}')
  pr2_num=$(echo "$pr2_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")
  pr2_head_sha=$(echo "$pr2_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['head']['sha'])")

  local pipeline2
  pipeline2=$(_wp_wait_for_pipeline "$wp_repo_id" pull_request "$pr2_head_sha")

  export RUN_ONCE=1
  python3 "${SCRIPT_DIR}/../../scripts/bot-approver.py" >/tmp/bot-approver-output2.txt 2>&1

  local bot_reviews_count2
  bot_reviews_count2=$(curl -s "${GITEA}/api/v1/repos/gateadmin/bot-approver-05/pulls/${pr2_num}/reviews" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    | python3 -c "
import json, sys
reviews = json.load(sys.stdin)
print(sum(1 for r in reviews if r['user']['login'] == 'gate-bot-05'))
")
  assert_eq "$bot_reviews_count2" "0" "negative path: bot-approver casts no vote when scanning did not verifiably pass"

  rm -f /tmp/bot-approver-output.txt /tmp/bot-approver-output2.txt
}
