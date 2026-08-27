# tests/regression/03-pr-retargeting.sh — SADR-0003, automated.
#
# An approval given while a PR targets an unprotected branch survives a
# retarget to a protected one, unchanged -- Gitea does not recalculate
# `official` on retarget. Also confirms dismiss_stale_approvals does NOT
# close this: it reacts to new commits on the PR head, not to the base
# changing.
#
# This test documents raw Gitea behaviour, not the platform's fix --
# test_approval_check (SADR-0009) is the test that proves policy-eval
# actually catches this. Keeping them separate mirrors the two SADRs:
# 0003 found the bug, 0009 closed it.
#
# Requires GITEA, GITEA_ADMIN_TOKEN, FORGERY_CONTAINER already set.

test_pr_retargeting() {
  test_start "SADR-0003: PR-retargeting approval bypass"

  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" alice-03 AlicePw123! alice-03@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" bob-03 BobPw123456! bob-03@example.com
  local alice_token bob_token
  alice_token=$(gitea_mint_token "$GITEA" alice-03 'AlicePw123!' alice-03-token '["write:repository"]')
  bob_token=$(gitea_mint_token "$GITEA" bob-03 'BobPw123456!' bob-03-token '["write:repository"]')

  curl -s -X POST "${GITEA}/api/v1/user/repos" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" -d '{"name":"retarget-03","auto_init":true,"default_branch":"main"}' >/dev/null
  curl -s -X PUT "${GITEA}/api/v1/repos/gateadmin/retarget-03/collaborators/alice-03" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null
  curl -s -X PUT "${GITEA}/api/v1/repos/gateadmin/retarget-03/collaborators/bob-03" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null
  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/retarget-03/branches" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"new_branch_name":"staging","old_branch_name":"main"}' >/dev/null
  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/retarget-03/branch_protections" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"branch_name":"main","required_approvals":1,"enable_approvals_whitelist":true,"approvals_whitelist_username":["bob-03"]}' >/dev/null
  curl -s -X PATCH "${GITEA}/api/v1/repos/gateadmin/retarget-03/branch_protections/main" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"dismiss_stale_approvals":true}' >/dev/null

  git_push_feature_branch "$GITEA" alice-03 "$alice_token" retarget-03 feature/retarget notes.txt "hello"

  local pr_json pr_num
  pr_json=$(curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/retarget-03/pulls" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"title":"alice change","head":"feature/retarget","base":"staging"}')
  pr_num=$(echo "$pr_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/retarget-03/pulls/${pr_num}/reviews" \
    -H "Authorization: token ${bob_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  local official_before
  official_before=$(curl -s "${GITEA}/api/v1/repos/gateadmin/retarget-03/pulls/${pr_num}/reviews" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['official'])")
  assert_eq "$official_before" "True" "bob's approval against staging is official there (no whitelist restricts staging)"

  curl -s -X PATCH "${GITEA}/api/v1/repos/gateadmin/retarget-03/pulls/${pr_num}" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"base":"main"}' >/dev/null

  local official_after
  official_after=$(curl -s "${GITEA}/api/v1/repos/gateadmin/retarget-03/pulls/${pr_num}/reviews" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['official'])")
  assert_eq "$official_after" "True" "official flag is UNCHANGED after retarget, even with dismiss_stale_approvals on -- the bug"

  local retarget_merge_http
  retarget_merge_http=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${GITEA}/api/v1/repos/gateadmin/retarget-03/pulls/${pr_num}/merge" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"Do":"merge"}')
  assert_eq "$retarget_merge_http" "200" "Gitea's own merge check accepts the stale, never-revalidated approval -- the bypass"
}
