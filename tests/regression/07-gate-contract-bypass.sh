# tests/regression/07-gate-contract-bypass.sh — SADR-0017, automated.
#
# The one class of Sprint 01 bypass this project's own docs/TODO.md flagged
# as unit-tested (tests/unit/test_bot_approver.py's mocked file_at_ref) but
# never proven live against a real Gitea+Woodpecker stack -- matching this
# project's own established discipline that a unit test alone is not
# sufficient evidence for a security-critical claim (every prior SADR pairs
# one with a live regression test; this file closes that gap for SADR-0017
# specifically). Three scenarios:
#
#   A. A PR that alters a vendored Semgrep rule relative to its protected base
#      gets ZERO bot votes, even when the unchanged pipeline reports every step
#      green. This proves the tree-level comparison, not just the older
#      single-file comparison.
#   B. A human approval survives neither Gitea's own dismiss_stale_approvals
#      NOR (independently, per SADR-0003's lesson that Gitea's own state
#      cannot be trusted alone) verify-approvals.py's own commit_id check,
#      once a new commit lands on the PR.
#   C. The exact same freshness requirement applied to the BOT's own vote:
#      an old bot approval for a superseded head does not satisfy
#      verify-approvals.py's bot_login branch, and bot-approver.py does not
#      get stuck refusing to ever vote again -- it re-votes for the new
#      head once that head's own pipeline is green.
#
# Requires GITEA, GITEA_INTERNAL, WOODPECKER, GITEA_ADMIN_TOKEN,
# WOODPECKER_PAT, FORGERY_CONTAINER already set (run-minimal-suite.sh sets
# these). Reuses the Woodpecker polling helpers (_wp_wait_for_pipeline,
# _wp_step_state, _wp_latest_pipeline_for_commit) defined in
# 05-bot-approver.sh -- source that file before this one.

_gate_bypass_working_pipeline() {
  cat <<'YAML'
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
}

test_gate_contract_bypass() {
  test_start "SADR-0017: Gate Contract bypass attempts"

  # ---------------------------------------------------------------------
  # Scenario A: an altered vendored rule is rejected by GATE_CONTRACT_ENFORCE,
  # regardless of how green the PR's pipeline reports.
  # ---------------------------------------------------------------------
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" gate-bot-07a GateBot123Pw! gate-bot-07a@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" alice-07a AlicePw123! alice-07a@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" bob-07a BobPw123456! bob-07a@example.com
  local bot_token_a alice_token_a bob_token_a
  bot_token_a=$(gitea_mint_token "$GITEA" gate-bot-07a 'GateBot123Pw!' bot-token-07a '["write:repository"]')
  alice_token_a=$(gitea_mint_token "$GITEA" alice-07a 'AlicePw123!' alice-token-07a '["write:repository"]')
  bob_token_a=$(gitea_mint_token "$GITEA" bob-07a 'BobPw123456!' bob-token-07a '["write:repository"]')

  curl -s -X POST "${GITEA}/api/v1/user/repos" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" -d '{"name":"gate-bypass-07a","auto_init":true,"default_branch":"main"}' >/dev/null
  for u in alice-07a bob-07a gate-bot-07a; do
    curl -s -X PUT "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07a/collaborators/${u}" \
      -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null
  done

  # Establish the PROTECTED BASE's gate file first -- a real onboarded repo
  # via onboard-repo.sh, admin-committed to main directly, matching what
  # bot-approver.py's protected-base comparison actually compares against.
  local base_content_b64
  base_content_b64=$(_gate_bypass_working_pipeline | base64 -w0 2>/dev/null || _gate_bypass_working_pipeline | base64 | tr -d '\n')
  curl -sf -X POST "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07a/contents/.woodpecker.yml" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d "{\"content\":\"${base_content_b64}\",\"message\":\"platform: baseline gate file\",\"branch\":\"main\"}" >/dev/null
  local base_rule_b64
  base_rule_b64=$(printf '%s\n' 'rules: []' | base64 -w0 2>/dev/null || printf '%s\n' 'rules: []' | base64 | tr -d '\n')
  curl -sf -X POST "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07a/contents/policy/vendored-rules/pilot.yml" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d "{\"content\":\"${base_rule_b64}\",\"message\":\"platform: baseline vendored rule\",\"branch\":\"main\"}" >/dev/null

  # Two clear steps, not nested -- a nested command substitution here
  # silently swallowed a failure the first time this test was run live
  # (05-bot-approver.sh's own repo_id/wp_repo pattern below is the proven
  # one; matching it deliberately).
  local repo_id_a
  repo_id_a=$(curl -s "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07a" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
  local wp_repo_a
  wp_repo_a=$(curl -s -X POST "${WOODPECKER}/api/repos?forge_remote_id=${repo_id_a}" -H "Authorization: Bearer ${WOODPECKER_PAT}")
  local wp_repo_id_a
  wp_repo_id_a=$(echo "$wp_repo_a" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

  curl -sf -X POST "${WOODPECKER}/api/repos/${wp_repo_id_a}/secrets" -H "Authorization: Bearer ${WOODPECKER_PAT}" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"policy_eval_gitea_token\",\"value\":\"$(gitea_mint_token "$GITEA" gateadmin 'Gateadmin123!' policy-eval-readonly-07a '["read:repository","read:issue"]')\",\"events\":[\"push\",\"pull_request\"]}" >/dev/null

  # The attack changes only a platform-owned vendored rule. The pipeline stays
  # byte-identical to the protected base, so a green run cannot be mistaken for
  # proof that the approved ruleset was used.
  local work_dir_a
  work_dir_a=$(mktemp -d)
  git clone -q "${GITEA}/gateadmin/gate-bypass-07a.git" "$work_dir_a"
  (
    cd "$work_dir_a"
    git config user.email alice-07a@example.com
    git config user.name alice-07a
    git checkout -q -b feature/altered-gate
    printf '%s\n' '# attacker disables the pilot rule' 'rules: []' > policy/vendored-rules/pilot.yml
    echo "alice's change" > notes.txt
    git add .
    git commit -q -m "alice: change + ALTERED vendored Semgrep rule"
  )
  git_push_origin "$work_dir_a" "$alice_token_a" feature/altered-gate
  rm -rf "$work_dir_a"

  local pra_json pra_num pra_head_sha
  pra_json=$(curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07a/pulls" \
    -H "Authorization: token ${alice_token_a}" -H "Content-Type: application/json" \
    -d '{"title":"alice altered vendored rule","head":"feature/altered-gate","base":"main"}')
  pra_num=$(echo "$pra_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")
  pra_head_sha=$(echo "$pra_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['head']['sha'])")

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07a/pulls/${pra_num}/reviews" \
    -H "Authorization: token ${bob_token_a}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  local pipeline_a
  pipeline_a=$(_wp_wait_for_pipeline "$wp_repo_id_a" pull_request "$pra_head_sha")
  assert_eq "$([ -n "$pipeline_a" ] && echo present || echo missing)" "present" "scenario A: the altered PR's own pipeline ran"
  assert_eq "$(_wp_step_state "$wp_repo_id_a" "$pipeline_a" code-scan)" "success" \
    "scenario A: the altered PR's own pipeline reports green (this is exactly why file-comparison, not pipeline trust, is required)"

  export GITEA_URL="$GITEA"
  export GITEA_BOT_TOKEN="$bot_token_a"
  export GITEA_BOT_LOGIN="gate-bot-07a"
  export REPO_OWNER=gateadmin
  export REPO_NAME=gate-bypass-07a
  export WOODPECKER_URL="$WOODPECKER"
  export WOODPECKER_TOKEN="$WOODPECKER_PAT"
  export WOODPECKER_REPO_ID="$wp_repo_id_a"
  export RUN_ONCE=1
  # The minimal live fixture seeds only this file plus the vendor tree. Keep
  # the file contract narrow, while DEFAULT_GATE_MANAGED_TREE_PREFIXES still
  # protects policy/vendored-rules without any test-only override.
  export GATE_MANAGED_PATHS=".woodpecker.yml"
  unset GATE_CONTRACT_ENFORCE   # explicitly rely on the real default (enforced), not an override
  python3 "${SCRIPT_DIR}/../../scripts/bot-approver.py" >/tmp/gate-bypass-a-output.txt 2>&1

  local bot_votes_a
  bot_votes_a=$(curl -s "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07a/pulls/${pra_num}/reviews" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    | python3 -c "
import json, sys
reviews = json.load(sys.stdin)
print(sum(1 for r in reviews if r['user']['login'] == 'gate-bot-07a'))
")
  assert_eq "$bot_votes_a" "0" "scenario A: bot casts NO vote on a PR that altered a vendored rule, despite a green pipeline"
  grep_rc_a=0; grep -q "managed tree differs from protected base" /tmp/gate-bypass-a-output.txt || grep_rc_a=$?
  assert_eq "$grep_rc_a" "0" "scenario A: bot's own log identifies the protected vendored-rules tree"
  rm -f /tmp/gate-bypass-a-output.txt

  # ---------------------------------------------------------------------
  # Scenarios B & C: stale human approval and stale bot approval, both
  # after a new commit, in one PR's lifecycle.
  # ---------------------------------------------------------------------
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" gate-bot-07b GateBot123Pw! gate-bot-07b@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" alice-07b AlicePw123! alice-07b@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" bob-07b BobPw123456! bob-07b@example.com
  local bot_token_b alice_token_b bob_token_b readonly_token_b
  bot_token_b=$(gitea_mint_token "$GITEA" gate-bot-07b 'GateBot123Pw!' bot-token-07b '["write:repository"]')
  alice_token_b=$(gitea_mint_token "$GITEA" alice-07b 'AlicePw123!' alice-token-07b '["write:repository"]')
  bob_token_b=$(gitea_mint_token "$GITEA" bob-07b 'BobPw123456!' bob-token-07b '["write:repository"]')
  readonly_token_b=$(gitea_mint_token "$GITEA" gateadmin 'Gateadmin123!' policy-eval-readonly-07b '["read:repository","read:issue"]')

  curl -s -X POST "${GITEA}/api/v1/user/repos" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" -d '{"name":"gate-bypass-07b","auto_init":true,"default_branch":"main"}' >/dev/null
  for u in alice-07b bob-07b gate-bot-07b; do
    curl -s -X PUT "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07b/collaborators/${u}" \
      -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null
  done
  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07b/branch_protections" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"branch_name":"main","required_approvals":2,"dismiss_stale_approvals":true}' >/dev/null

  local repo_id_b
  repo_id_b=$(curl -s "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07b" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
  local wp_repo_b
  wp_repo_b=$(curl -s -X POST "${WOODPECKER}/api/repos?forge_remote_id=${repo_id_b}" -H "Authorization: Bearer ${WOODPECKER_PAT}")
  local wp_repo_id_b
  wp_repo_id_b=$(echo "$wp_repo_b" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
  curl -sf -X POST "${WOODPECKER}/api/repos/${wp_repo_id_b}/secrets" -H "Authorization: Bearer ${WOODPECKER_PAT}" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"policy_eval_gitea_token\",\"value\":\"${readonly_token_b}\",\"events\":[\"push\",\"pull_request\"]}" >/dev/null

  local work_dir_b
  work_dir_b=$(mktemp -d)
  git clone -q "${GITEA}/gateadmin/gate-bypass-07b.git" "$work_dir_b"
  (
    cd "$work_dir_b"
    git config user.email alice-07b@example.com
    git config user.name alice-07b
    git checkout -q -b feature/freshness
    _gate_bypass_working_pipeline > .woodpecker.yml
    echo "v1" > notes.txt
    git add .
    git commit -q -m "alice: v1"
  )
  git_push_origin "$work_dir_b" "$alice_token_b" feature/freshness

  local prb_json prb_num prb_head_sha1
  prb_json=$(curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07b/pulls" \
    -H "Authorization: token ${alice_token_b}" -H "Content-Type: application/json" \
    -d '{"title":"alice freshness","head":"feature/freshness","base":"main"}')
  prb_num=$(echo "$prb_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")
  prb_head_sha1=$(echo "$prb_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['head']['sha'])")

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07b/pulls/${prb_num}/reviews" \
    -H "Authorization: token ${bob_token_b}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  # set +e/-e around exactly this command -- a bare `x=$(cmd)` or plain
  # `cmd; rc=$?` aborts the WHOLE SUITE under this file's inherited `set -e`
  # the instant the command exits nonzero, same bug lib.sh's run_policy_eval
  # comment documents hitting twice already. This script deliberately does
  # NOT reuse run_policy_eval here because GATE_BOT_LOGIN must be set, which
  # that helper does not accept as a parameter.
  set +e
  GATE_BOT_LOGIN=gate-bot-07b GITEA_URL="$GITEA" POLICY_EVAL_GITEA_TOKEN="$readonly_token_b" \
    CI_REPO_OWNER=gateadmin CI_REPO_NAME=gate-bypass-07b CI_COMMIT_PULL_REQUEST="$prb_num" \
    python3 "${SCRIPT_DIR}/../../policy-eval/verify-approvals.py" >/tmp/gate-bypass-b-output.txt 2>&1
  pre_bot_exit=$?
  set -e
  assert_eq "$pre_bot_exit" "1" "before the bot has voted at all: human-only approval is not sufficient"

  local pipeline_b1
  pipeline_b1=$(_wp_wait_for_pipeline "$wp_repo_id_b" pull_request "$prb_head_sha1")
  export GITEA_URL="$GITEA"; export GITEA_BOT_TOKEN="$bot_token_b"; export GITEA_BOT_LOGIN="gate-bot-07b"
  export REPO_OWNER=gateadmin; export REPO_NAME=gate-bypass-07b
  export WOODPECKER_URL="$WOODPECKER"; export WOODPECKER_TOKEN="$WOODPECKER_PAT"; export WOODPECKER_REPO_ID="$wp_repo_id_b"
  export RUN_ONCE=1; export GATE_CONTRACT_ENFORCE=0   # this scenario tests approval freshness, not file-tampering
  python3 "${SCRIPT_DIR}/../../scripts/bot-approver.py" >/tmp/gate-bypass-b-bot1.txt 2>&1

  local bot_votes_sha1
  bot_votes_sha1=$(curl -s "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07b/pulls/${prb_num}/reviews" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    | python3 -c "
import json, sys
reviews = json.load(sys.stdin)
print(sum(1 for r in reviews if r['user']['login'] == 'gate-bot-07b' and r['state'] == 'APPROVED'))
")
  assert_eq "$bot_votes_sha1" "1" "scenario B/C setup: bot approved the first head as expected"

  # --- Now push a NEW commit. Both bob's and the bot's approvals are for
  #     the superseded head. ---
  (
    cd "$work_dir_b"
    echo "v2" > notes.txt
    git add .
    git commit -q -m "alice: v2 -- supersedes the approved head"
  )
  git_push_origin "$work_dir_b" "$alice_token_b" feature/freshness
  rm -rf "$work_dir_b"

  local prb_head_sha2
  prb_head_sha2=$(curl -s "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07b/pulls/${prb_num}" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" | python3 -c "import json,sys; print(json.load(sys.stdin)['head']['sha'])")
  assert_eq "$([ "$prb_head_sha2" != "$prb_head_sha1" ] && echo changed || echo unchanged)" "changed" \
    "scenario B/C: the PR head actually moved to a new commit"

  set +e
  GATE_BOT_LOGIN=gate-bot-07b GITEA_URL="$GITEA" POLICY_EVAL_GITEA_TOKEN="$readonly_token_b" \
    CI_REPO_OWNER=gateadmin CI_REPO_NAME=gate-bypass-07b CI_COMMIT_PULL_REQUEST="$prb_num" \
    python3 "${SCRIPT_DIR}/../../policy-eval/verify-approvals.py" >/tmp/gate-bypass-b-output2.txt 2>&1
  stale_exit=$?
  set -e
  assert_eq "$stale_exit" "1" "scenario B/C: BOTH the stale human approval and the stale bot approval fail to satisfy the new head"
  grep_rc_bot=0; grep -q "required gate bot .* has not approved the current head" /tmp/gate-bypass-b-output2.txt || grep_rc_bot=$?
  assert_eq "$grep_rc_bot" "0" "scenario C: the bot-specific branch names the old bot vote as insufficient, not a generic count failure"

  # --- Legitimate remediation: fresh approvals for the new head ---
  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07b/pulls/${prb_num}/reviews" \
    -H "Authorization: token ${bob_token_b}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  local pipeline_b2
  pipeline_b2=$(_wp_wait_for_pipeline "$wp_repo_id_b" pull_request "$prb_head_sha2")
  assert_eq "$([ -n "$pipeline_b2" ] && echo present || echo missing)" "present" \
    "scenario C: a normal push to an open PR's branch produces a fresh pull_request-event pipeline on its own (no reconciliation needed here)"

  export RUN_ONCE=1
  python3 "${SCRIPT_DIR}/../../scripts/bot-approver.py" >/tmp/gate-bypass-b-bot2.txt 2>&1

  local bot_votes_sha2
  bot_votes_sha2=$(curl -s "${GITEA}/api/v1/repos/gateadmin/gate-bypass-07b/pulls/${prb_num}/reviews" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    | python3 -c "
import json, sys
reviews = json.load(sys.stdin)
matches = [r for r in reviews if r['user']['login'] == 'gate-bot-07b' and r['state'] == 'APPROVED' and r.get('commit_id') == '$prb_head_sha2']
print(len(matches))
")
  assert_eq "$bot_votes_sha2" "1" "scenario C: the bot does NOT get stuck refusing to ever vote again -- it re-votes for the new head once it's green"

  set +e
  GATE_BOT_LOGIN=gate-bot-07b GITEA_URL="$GITEA" POLICY_EVAL_GITEA_TOKEN="$readonly_token_b" \
    CI_REPO_OWNER=gateadmin CI_REPO_NAME=gate-bypass-07b CI_COMMIT_PULL_REQUEST="$prb_num" \
    python3 "${SCRIPT_DIR}/../../policy-eval/verify-approvals.py" >/tmp/gate-bypass-b-output3.txt 2>&1
  final_exit=$?
  set -e
  assert_eq "$final_exit" "0" "scenario B/C: fresh bot + fresh human approval on the current head -- PASSES"

  rm -f /tmp/gate-bypass-b-output.txt /tmp/gate-bypass-b-output2.txt /tmp/gate-bypass-b-output3.txt \
        /tmp/gate-bypass-b-bot1.txt /tmp/gate-bypass-b-bot2.txt
}
