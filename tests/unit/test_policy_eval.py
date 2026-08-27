#!/usr/bin/env python3
"""
tests/unit/test_policy_eval.py — fast, no-Docker regression coverage for
policy-eval/verify-approvals.py, against a stub Gitea (stub_gitea.py)
instead of a live one.

Complements, does not replace, tests/regression/04-approval-check.sh and
06-team-whitelist.sh -- those prove the mechanism against a REAL Gitea
and are the ones that actually matter; this exists so the same decision
logic can be re-checked on every push, cheaply, with no Docker-in-Docker
question to answer (see docs/adr/0016). Every scenario here mirrors one
already proven live in docs/adr/0009 or docs/adr/0013 -- fixtures are
built from the real response shapes captured during those live sessions,
not invented.

Runs policy-eval as a real subprocess against its real script file, not
an imported/monkeypatched copy -- the thing under test stays exactly
what would actually run in production.
"""
import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(__file__))
from stub_gitea import StubGitea

SCRIPT = os.path.join(os.path.dirname(__file__), "..", "..", "policy-eval", "verify-approvals.py")

PASS_COUNT = 0
FAIL_COUNT = 0


def assert_eq(actual, expected, description):
    global PASS_COUNT, FAIL_COUNT
    if actual == expected:
        print(f"  PASS: {description} (got: {actual!r})")
        PASS_COUNT += 1
    else:
        print(f"  FAIL: {description} -- expected {expected!r}, got {actual!r}", file=sys.stderr)
        FAIL_COUNT += 1


def run_policy_eval(fixtures, owner="o", repo="r", pr_number="1", extra_env=None):
    stub = StubGitea(fixtures)
    stub.start()
    try:
        env = {
            **os.environ,
            "GITEA_URL": stub.url,
            "POLICY_EVAL_GITEA_TOKEN": "unused-by-the-stub",
            "CI_REPO_OWNER": owner,
            "CI_REPO_NAME": repo,
            "CI_COMMIT_PULL_REQUEST": pr_number,
        }
        if extra_env:
            env.update(extra_env)
        result = subprocess.run(
            [sys.executable, SCRIPT], env=env, capture_output=True, text=True, timeout=10,
        )
        return result.returncode, result.stdout, result.stderr
    finally:
        stub.stop()


def pr(base_ref="main", author="alice", head_sha="deadbeef00"):
    return {
        "number": 1,
        "user": {"login": author},
        "base": {"ref": base_ref},
        "head": {"sha": head_sha},
    }


def protection(required=1, whitelist=None, teams=None):
    return {
        "required_approvals": required,
        "enable_approvals_whitelist": bool(whitelist or teams),
        "approvals_whitelist_username": whitelist or [],
        "approvals_whitelist_teams": teams or [],
    }


def review(login, state="APPROVED", submitted_at="2026-08-26T10:00:00Z", dismissed=False,
           commit_id="deadbeef00"):
    return {
        "user": {"login": login},
        "state": state,
        "submitted_at": submitted_at,
        "dismissed": dismissed,
        "commit_id": commit_id,
    }


def retarget_event(created_at="2026-08-26T10:05:00Z"):
    return {"type": "change_target_branch", "created_at": created_at}


def scenario_1_stale_approval_blocked():
    print("=== scenario 1: approval predates a retarget -- blocked (mirrors docs/adr/0009 scenario 1) ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr()),
        "/api/v1/repos/o/r/branch_protections/main": (200, protection(required=1, whitelist=["bob"])),
        "/api/v1/repos/o/r/issues/1/timeline": (200, [retarget_event(created_at="2026-08-26T10:05:00Z")]),
        "/api/v1/repos/o/r/pulls/1/reviews": (200, [review("bob", submitted_at="2026-08-26T10:00:00Z")]),
    }
    code, out, _ = run_policy_eval(fixtures)
    assert_eq(code, 1, "pre-retarget approval is rejected, exit 1")
    assert_eq("predates the current base" in out, True, "rejection reason names the retarget")


def scenario_2_fresh_approval_after_retarget_passes():
    print("=== scenario 2: fresh approval after retarget -- passes (mirrors docs/adr/0009 scenario 2) ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr()),
        "/api/v1/repos/o/r/branch_protections/main": (200, protection(required=1, whitelist=["bob"])),
        "/api/v1/repos/o/r/issues/1/timeline": (200, [retarget_event(created_at="2026-08-26T10:05:00Z")]),
        "/api/v1/repos/o/r/pulls/1/reviews": (200, [review("bob", submitted_at="2026-08-26T10:10:00Z")]),
    }
    code, _, _ = run_policy_eval(fixtures)
    assert_eq(code, 0, "post-retarget approval counts, exit 0")


def scenario_3_whitelist_rejection_no_retarget():
    print("=== scenario 3: non-whitelisted approver, no retarget involved -- blocked (mirrors docs/adr/0009 scenario 3) ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr()),
        "/api/v1/repos/o/r/branch_protections/main": (200, protection(required=1, whitelist=["bob"])),
        "/api/v1/repos/o/r/issues/1/timeline": (200, []),
        "/api/v1/repos/o/r/pulls/1/reviews": (200, [review("carol")]),
    }
    code, out, _ = run_policy_eval(fixtures)
    assert_eq(code, 1, "non-whitelisted approver rejected, exit 1")
    assert_eq("not in base" in out, True, "rejection reason names the whitelist, not the retarget")


def scenario_4_team_member_counts():
    print("=== scenario 4: team-whitelisted approver counts (mirrors docs/adr/0013) ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr()),
        "/api/v1/repos/o/r/branch_protections/main": (200, protection(required=1, teams=["reviewers"])),
        "/api/v1/repos/o/r/teams": (200, [{"id": 7, "name": "reviewers"}]),
        "/api/v1/teams/7/members": (200, [{"login": "bob"}]),
        "/api/v1/repos/o/r/issues/1/timeline": (200, []),
        "/api/v1/repos/o/r/pulls/1/reviews": (200, [review("bob")]),
    }
    code, out, _ = run_policy_eval(fixtures)
    assert_eq(code, 0, "team member's approval counts, exit 0")
    assert_eq("counted   bob" in out, True, "bob is explicitly logged as counted")


def scenario_5_team_nonmember_rejected():
    print("=== scenario 5: repo collaborator who is NOT a team member -- rejected (mirrors docs/adr/0013) ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr()),
        "/api/v1/repos/o/r/branch_protections/main": (200, protection(required=1, teams=["reviewers"])),
        "/api/v1/repos/o/r/teams": (200, [{"id": 7, "name": "reviewers"}]),
        "/api/v1/teams/7/members": (200, [{"login": "bob"}]),
        "/api/v1/repos/o/r/issues/1/timeline": (200, []),
        "/api/v1/repos/o/r/pulls/1/reviews": (200, [review("carol")]),
    }
    code, _, _ = run_policy_eval(fixtures)
    assert_eq(code, 1, "non-team-member's approval does not count, exit 1")


def scenario_6_zero_required_approvals_trivial_pass():
    print("=== scenario 6: required_approvals=0 -- trivial pass, no reviews needed ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr()),
        "/api/v1/repos/o/r/branch_protections/main": (200, protection(required=0)),
    }
    code, out, _ = run_policy_eval(fixtures)
    assert_eq(code, 0, "0 required approvals passes without ever reading reviews")
    assert_eq("requires 0 approvals" in out, True, "log line explains why it passed")


def scenario_7_no_protection_rule_trivial_pass():
    print("=== scenario 7: no branch_protections rule at all (404) -- trivial pass ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr()),
        # deliberately no fixture for branch_protections/main -- the stub
        # returns a real 404, exercising verify-approvals.py's own
        # allow_404 path exactly as it would hit a genuinely unprotected base
    }
    code, _, _ = run_policy_eval(fixtures)
    assert_eq(code, 0, "an unprotected base passes with nothing to check")


def scenario_8_self_approval_rejected():
    print("=== scenario 8: the PR author's own review never counts, even if otherwise whitelisted ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr(author="alice")),
        "/api/v1/repos/o/r/branch_protections/main": (200, protection(required=1, whitelist=["alice"])),
        "/api/v1/repos/o/r/issues/1/timeline": (200, []),
        "/api/v1/repos/o/r/pulls/1/reviews": (200, [review("alice")]),
    }
    code, out, _ = run_policy_eval(fixtures)
    assert_eq(code, 1, "self-approval is rejected even when whitelisted, exit 1")
    assert_eq("reviewer is the PR author" in out, True, "rejection reason names self-approval specifically")


def scenario_9_not_a_pr_build():
    print("=== scenario 9: a plain push (no PR number) -- nothing to verify, pass ===")
    code, out, _ = run_policy_eval({}, pr_number="")
    assert_eq(code, 0, "a non-PR build passes without making any API call")
    assert_eq("not a pull request build" in out, True, "log line explains why it passed")


def scenario_10_stale_commit_approval_rejected():
    print("=== scenario 10: approval for an older head commit -- rejected ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr(head_sha="current-sha")),
        "/api/v1/repos/o/r/branch_protections/main": (200, protection(required=1, whitelist=["bob"])),
        "/api/v1/repos/o/r/issues/1/timeline": (200, []),
        "/api/v1/repos/o/r/pulls/1/reviews": (200, [review("bob", commit_id="old-sha")]),
    }
    code, out, _ = run_policy_eval(fixtures)
    assert_eq(code, 1, "approval for a prior head is rejected")
    assert_eq("not current head current-sha" in out, True, "rejection names the current-head requirement")


def scenario_11_bot_and_human_roles_required():
    print("=== scenario 11: current-head bot and separate human approval are both required ===")
    fixtures = {
        "/api/v1/repos/o/r/pulls/1": (200, pr()),
        "/api/v1/repos/o/r/branch_protections/main": (200, protection(required=2)),
        "/api/v1/repos/o/r/issues/1/timeline": (200, []),
        "/api/v1/repos/o/r/pulls/1/reviews": (200, [review("gate-bot"), review("bob")]),
    }
    code, _, _ = run_policy_eval(fixtures, extra_env={"GATE_BOT_LOGIN": "gate-bot"})
    assert_eq(code, 0, "current-head bot plus human approval passes")

    fixtures["/api/v1/repos/o/r/pulls/1/reviews"] = (200, [review("bob"), review("carol")])
    code, out, _ = run_policy_eval(fixtures, extra_env={"GATE_BOT_LOGIN": "gate-bot"})
    assert_eq(code, 1, "two humans cannot substitute for the gate bot")
    assert_eq("required gate bot 'gate-bot'" in out, True, "failure names the missing bot role")


def main():
    scenario_1_stale_approval_blocked()
    scenario_2_fresh_approval_after_retarget_passes()
    scenario_3_whitelist_rejection_no_retarget()
    scenario_4_team_member_counts()
    scenario_5_team_nonmember_rejected()
    scenario_6_zero_required_approvals_trivial_pass()
    scenario_7_no_protection_rule_trivial_pass()
    scenario_8_self_approval_rejected()
    scenario_9_not_a_pr_build()
    scenario_10_stale_commit_approval_rejected()
    scenario_11_bot_and_human_roles_required()

    print(f"\n=== unit summary: {PASS_COUNT} passed, {FAIL_COUNT} failed ===")
    return 0 if FAIL_COUNT == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
