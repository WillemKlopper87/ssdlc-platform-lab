# tests/regression/06-team-whitelist.sh — SADR-0013, automated.
#
# policy-eval/verify-approvals.py originally resolved only
# approvals_whitelist_username, leaving approvals_whitelist_teams
# entirely unhandled -- a real gap, since team-based whitelists are at
# least as common as username lists in real orgs. Three scenarios: a
# team member's approval counts, a non-member repo collaborator's does
# not, and a token missing the read:organization scope this needs fails
# LOUD (exit 2) rather than silently under-counting -- getting this
# backwards would fail safe (never a false PASS) but would be a
# confusing, hard-to-diagnose false FAIL in real operation.
#
# Requires GITEA, GITEA_ADMIN_TOKEN, FORGERY_CONTAINER, SCRIPT_DIR
# already set. Needs an ORG, not a personal-account repo -- Gitea teams
# are an org concept -- the first test in this suite to use one.

test_team_whitelist() {
  test_start "SADR-0013: team-based approval whitelist"

  curl -s -X POST "${GITEA}/api/v1/orgs" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" -d '{"username":"ssdlc-org-06"}' >/dev/null

  local team_json team_id
  team_json=$(curl -s -X POST "${GITEA}/api/v1/orgs/ssdlc-org-06/teams" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{"name":"reviewers","permission":"write","units":["repo.code","repo.pulls"]}')
  team_id=$(echo "$team_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" alice-06 AlicePw123! alice-06@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" bob-06 BobPw123456! bob-06@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" carol-06 CarolPw123! carol-06@example.com
  local alice_token bob_token carol_token
  alice_token=$(gitea_mint_token "$GITEA" alice-06 'AlicePw123!' alice-06-token '["write:repository"]')
  bob_token=$(gitea_mint_token "$GITEA" bob-06 'BobPw123456!' bob-06-token '["write:repository"]')
  carol_token=$(gitea_mint_token "$GITEA" carol-06 'CarolPw123!' carol-06-token '["write:repository"]')

  # bob is on the "reviewers" team; carol is a plain repo collaborator,
  # not a team member -- that distinction is exactly what this test proves
  curl -s -X PUT "${GITEA}/api/v1/teams/${team_id}/members/bob-06" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" >/dev/null

  curl -s -X POST "${GITEA}/api/v1/orgs/ssdlc-org-06/repos" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" -d '{"name":"team-whitelist-06","auto_init":true,"default_branch":"main"}' >/dev/null
  for u in alice-06 carol-06; do
    curl -s -X PUT "${GITEA}/api/v1/repos/ssdlc-org-06/team-whitelist-06/collaborators/${u}" \
      -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null
  done
  curl -s -X PUT "${GITEA}/api/v1/teams/${team_id}/repos/ssdlc-org-06/team-whitelist-06" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" >/dev/null

  curl -s -X POST "${GITEA}/api/v1/repos/ssdlc-org-06/team-whitelist-06/branch_protections" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"branch_name":"main","required_approvals":1,"enable_approvals_whitelist":true,"approvals_whitelist_teams":["reviewers"]}' >/dev/null

  git_push_feature_branch "$GITEA" alice-06 "$alice_token" team-whitelist-06 feature/change notes.txt "hello" ssdlc-org-06
  local pr_json pr_num
  pr_json=$(curl -s -X POST "${GITEA}/api/v1/repos/ssdlc-org-06/team-whitelist-06/pulls" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"title":"alice change","head":"feature/change","base":"main"}')
  pr_num=$(echo "$pr_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")

  curl -s -X POST "${GITEA}/api/v1/repos/ssdlc-org-06/team-whitelist-06/pulls/${pr_num}/reviews" \
    -H "Authorization: token ${bob_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null
  curl -s -X POST "${GITEA}/api/v1/repos/ssdlc-org-06/team-whitelist-06/pulls/${pr_num}/reviews" \
    -H "Authorization: token ${carol_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  # --- Scenario 1: a token WITHOUT read:organization fails loud, not silently ---
  local scope_token
  scope_token=$(gitea_mint_token "$GITEA" gateadmin 'Gateadmin123!' policy-eval-noscope-06 '["read:repository","read:issue"]')
  run_policy_eval ssdlc-org-06 team-whitelist-06 "$pr_num" "$scope_token"
  assert_eq "$POLICY_EVAL_EXIT" "2" "scenario 1: missing read:organization scope fails loud (exit 2), not a silent miscount"

  # --- Scenario 2: with the right scope, bob (team member) counts, carol (collaborator, not a member) does not ---
  local readonly_token
  readonly_token=$(gitea_mint_token "$GITEA" gateadmin 'Gateadmin123!' policy-eval-readonly-06 '["read:repository","read:issue","read:organization"]')
  run_policy_eval ssdlc-org-06 team-whitelist-06 "$pr_num" "$readonly_token"
  assert_eq "$POLICY_EVAL_EXIT" "0" "scenario 2: team member's approval satisfies required_approvals:1 -> PASSES"

  grep_rc=0; grep -q "counted   bob-06" /tmp/policy-eval-output.txt || grep_rc=$?
  assert_eq "$grep_rc" "0" "scenario 2: bob (team member) is explicitly counted"
  grep_rc=0; grep -q "REJECTED  carol-06" /tmp/policy-eval-output.txt || grep_rc=$?
  assert_eq "$grep_rc" "0" "scenario 2: carol (collaborator, not a team member) is explicitly rejected"

  rm -f /tmp/policy-eval-output.txt
}
