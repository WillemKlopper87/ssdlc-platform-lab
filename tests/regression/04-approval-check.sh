# tests/regression/04-approval-check.sh — SADR-0009, automated.
#
# Four scenarios for policy-eval/verify-approvals.py, the actual fix for
# SADR-0003's bypass: the exact retarget attack (blocked), the legitimate
# remediation (a fresh approval after retarget, passes), a whitelist
# rejection with no retarget involved at all (rules out a fix that only
# works via the retarget-timestamp path), and a mixed valid/invalid
# reviewer case on the same PR.
#
# Requires GITEA, GITEA_ADMIN_TOKEN, FORGERY_CONTAINER, SCRIPT_DIR already
# set. Uses run_policy_eval from lib.sh (owner defaults to "gateadmin",
# the owner every repo in this file happens to use).

test_approval_check() {
  test_start "SADR-0009: policy-eval approval-check"

  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" alice-04 AlicePw123! alice-04@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" bob-04 BobPw123456! bob-04@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" carol-04 CarolPw123! carol-04@example.com
  local alice_token bob_token carol_token readonly_token
  alice_token=$(gitea_mint_token "$GITEA" alice-04 'AlicePw123!' alice-04-token '["write:repository"]')
  bob_token=$(gitea_mint_token "$GITEA" bob-04 'BobPw123456!' bob-04-token '["write:repository"]')
  carol_token=$(gitea_mint_token "$GITEA" carol-04 'CarolPw123!' carol-04-token '["write:repository"]')
  readonly_token=$(gitea_mint_token "$GITEA" gateadmin 'Gateadmin123!' policy-eval-readonly-04 '["read:repository","read:issue"]')

  curl -s -X POST "${GITEA}/api/v1/user/repos" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" -d '{"name":"approval-check-04","auto_init":true,"default_branch":"main"}' >/dev/null
  for u in alice-04 bob-04 carol-04; do
    curl -s -X PUT "${GITEA}/api/v1/repos/gateadmin/approval-check-04/collaborators/${u}" \
      -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null
  done
  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/approval-check-04/branches" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"new_branch_name":"staging","old_branch_name":"main"}' >/dev/null
  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/approval-check-04/branch_protections" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"branch_name":"main","required_approvals":1,"enable_approvals_whitelist":true,"approvals_whitelist_username":["bob-04"]}' >/dev/null

  # --- Scenarios 1 & 2: retarget attack, then legitimate remediation ---
  git_push_feature_branch "$GITEA" alice-04 "$alice_token" approval-check-04 feature/retarget notes.txt "hello"
  local pr1_json pr1_num
  pr1_json=$(curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/approval-check-04/pulls" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"title":"retarget test","head":"feature/retarget","base":"staging"}')
  pr1_num=$(echo "$pr1_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/approval-check-04/pulls/${pr1_num}/reviews" \
    -H "Authorization: token ${bob_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null
  curl -s -X PATCH "${GITEA}/api/v1/repos/gateadmin/approval-check-04/pulls/${pr1_num}" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"base":"main"}' >/dev/null

  run_policy_eval gateadmin approval-check-04 "$pr1_num" "$readonly_token"
  assert_eq "$POLICY_EVAL_EXIT" "1" "scenario 1: stale pre-retarget approval -> policy-eval FAILS (the bypass, blocked)"
  grep_rc=0; grep -q "predates the current base" /tmp/policy-eval-output.txt || grep_rc=$?
  assert_eq "$grep_rc" "0" "scenario 1: rejection reason correctly names the retarget"

  # bob approves AGAIN, now that the PR actually targets main
  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/approval-check-04/pulls/${pr1_num}/reviews" \
    -H "Authorization: token ${bob_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  run_policy_eval gateadmin approval-check-04 "$pr1_num" "$readonly_token"
  assert_eq "$POLICY_EVAL_EXIT" "0" "scenario 2: fresh approval after retarget -> policy-eval PASSES"

  # --- Scenarios 3 & 4: whitelist rejection, independent of any retarget ---
  git_push_feature_branch "$GITEA" alice-04 "$alice_token" approval-check-04 feature/no-retarget notes2.txt "hello2"
  local pr2_json pr2_num
  pr2_json=$(curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/approval-check-04/pulls" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"title":"no retarget test","head":"feature/no-retarget","base":"main"}')
  pr2_num=$(echo "$pr2_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/approval-check-04/pulls/${pr2_num}/reviews" \
    -H "Authorization: token ${carol_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  run_policy_eval gateadmin approval-check-04 "$pr2_num" "$readonly_token"
  assert_eq "$POLICY_EVAL_EXIT" "1" "scenario 3: non-whitelisted approver, no retarget involved -> FAILS"

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/approval-check-04/pulls/${pr2_num}/reviews" \
    -H "Authorization: token ${bob_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  run_policy_eval gateadmin approval-check-04 "$pr2_num" "$readonly_token"
  assert_eq "$POLICY_EVAL_EXIT" "0" "scenario 4: valid (bob) + rejected (carol) reviewer together -> PASSES"

  rm -f /tmp/policy-eval-output.txt
}
