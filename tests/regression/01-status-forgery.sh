# tests/regression/01-status-forgery.sh — SADR-0001, automated.
#
# Round 1: at required_approvals=0, a collaborator with only a
# write:repository token forges the gate's own status context and merges
# unreviewed, unscanned code. Round 2: the same forgery, at
# required_approvals=2, does NOT merge -- forging status is insufficient;
# distinct real approvals are still required.
#
# Requires GITEA (base url) and GITEA_ADMIN_TOKEN already set by the caller.

test_status_forgery() {
  test_start "SADR-0001: commit-status forgery"

  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" alice-01 AlicePw123! alice-01@example.com
  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" bob-01 BobPw123456! bob-01@example.com
  local alice_token bob_token
  alice_token=$(gitea_mint_token "$GITEA" alice-01 'AlicePw123!' alice-01-token '["write:repository"]')
  bob_token=$(gitea_mint_token "$GITEA" bob-01 'BobPw123456!' bob-01-token '["write:repository"]')

  curl -s -X POST "${GITEA}/api/v1/user/repos" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" -d '{"name":"forge-target-01","auto_init":true,"default_branch":"main"}' >/dev/null
  curl -s -X PUT "${GITEA}/api/v1/repos/gateadmin/forge-target-01/collaborators/alice-01" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null
  curl -s -X PUT "${GITEA}/api/v1/repos/gateadmin/forge-target-01/collaborators/bob-01" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null

  # --- Round 1: required_approvals = 0, isolates the status-check variable ---
  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/branch_protections" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"branch_name":"main","enable_status_check":true,"status_check_contexts":["ssdlc/security-gate"],"required_approvals":0}' >/dev/null

  # Real git push, not the Contents API's new_branch_name -- found live
  # (this test's own first run) that it fails unreliably ("user cannot
  # commit to repo") once the source branch has any restriction at all,
  # the same failure mode docs/adr/0009's live testing hit and worked
  # around the same way.
  git_push_feature_branch "$GITEA" alice-01 "$alice_token" forge-target-01 feature/round1 notes.txt "hello"

  local pr1_json pr1_num head_sha
  pr1_json=$(curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"title":"round1","head":"feature/round1","base":"main"}')
  pr1_num=$(echo "$pr1_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")
  head_sha=$(echo "$pr1_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['head']['sha'])")

  local baseline_http
  baseline_http=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls/${pr1_num}/merge" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"Do":"merge"}')
  assert_eq "$baseline_http" "405" "baseline: merge blocked before any status exists"

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/statuses/${head_sha}" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"state":"success","context":"ssdlc/security-gate","description":"forged"}' >/dev/null

  local forge_merge_http
  forge_merge_http=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls/${pr1_num}/merge" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"Do":"merge"}')
  assert_eq "$forge_merge_http" "200" "round 1: forged status alone merges at required_approvals=0 (the vulnerability, as documented)"

  # --- Round 2: required_approvals = 2, no whitelist -- D2's proven mitigation ---
  curl -s -X PATCH "${GITEA}/api/v1/repos/gateadmin/forge-target-01/branch_protections/main" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" \
    -d '{"required_approvals":2}' >/dev/null

  git_push_feature_branch "$GITEA" alice-01 "$alice_token" forge-target-01 feature/round2 notes2.txt "hello2"

  local pr2_json pr2_num head_sha2
  pr2_json=$(curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"title":"round2","head":"feature/round2","base":"main"}')
  pr2_num=$(echo "$pr2_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['number'])")
  head_sha2=$(echo "$pr2_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['head']['sha'])")

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/statuses/${head_sha2}" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" \
    -d '{"state":"success","context":"ssdlc/security-gate","description":"forged"}' >/dev/null

  local self_approve_http
  self_approve_http=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls/${pr2_num}/reviews" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}')
  assert_eq "$self_approve_http" "422" "self-approval rejected natively by Gitea"

  local forged_only_merge_http
  forged_only_merge_http=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls/${pr2_num}/merge" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"Do":"merge"}')
  assert_eq "$forged_only_merge_http" "405" "round 2: forged status alone does NOT merge at required_approvals=2 -- the mitigation"

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls/${pr2_num}/reviews" \
    -H "Authorization: token ${bob_token}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  local one_approval_http
  one_approval_http=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls/${pr2_num}/merge" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"Do":"merge"}')
  assert_eq "$one_approval_http" "405" "still blocked with only 1 of 2 required approvals"

  curl -s -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls/${pr2_num}/reviews" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"event":"APPROVED"}' >/dev/null

  local two_approvals_http
  two_approvals_http=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${GITEA}/api/v1/repos/gateadmin/forge-target-01/pulls/${pr2_num}/merge" \
    -H "Authorization: token ${alice_token}" -H "Content-Type: application/json" -d '{"Do":"merge"}')
  assert_eq "$two_approvals_http" "200" "merges once 2 distinct real approvals exist"
}
