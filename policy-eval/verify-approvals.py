#!/usr/bin/env python3
"""
policy-eval/verify-approvals.py

Independently re-derives whether a pull request's approvals are valid for
its CURRENT base branch, instead of trusting Gitea's stored `official`
flag -- which is not recalculated when a PR is retargeted
(GHSA-w5pg-649r-p6gg / CVE-2026-58439). Confirmed exploitable live against
this project's pinned Gitea version in docs/adr/0003: an approval given
while a PR targets an unprotected branch survives a retarget to a
protected one, `official: true` unchanged, with `dismiss_stale_approvals`
proven not to help (it reacts to new commits, not to the base changing).

Exit 0: enough independently-valid approvals exist for the current base.
Exit 1: not enough -- stdout lists exactly which reviews were rejected and
        why, so the PR author knows what to do (retarget earlier, or get a
        fresh review).
Exit 2: could not evaluate at all (API/auth failure) -- fails closed, same
        as any other gate step (DESIGN.md D4: no terminal status, no
        "success").

Scope, deliberately narrow -- matches docs/adr/0003's decision item 2 and
nothing more:
  - Only the APPROVED side of Gitea's review model. REQUEST_CHANGES /
    COMMENT reviews are left entirely to Gitea's own merge-check; this
    script does not reimplement that.
  - Both approvals_whitelist_username AND approvals_whitelist_teams are
    resolved (docs/adr/0013) -- a reviewer counts if they're named
    directly, or are a member of any whitelisted team, at evaluation
    time. Team membership is re-read on every run, same as everything
    else here; nothing is cached across evaluations.
  - A reviewer's vote is their MOST RECENT review, by submitted_at, and it
    only counts if that latest review is APPROVED and not dismissed -- a
    later REQUEST_CHANGES supersedes an earlier APPROVED, matching Gitea's
    own semantics rather than a naive "any APPROVED review ever" count.
  - When GATE_BOT_LOGIN is configured, the required approvals must include a
    current-head approval from that bot and a separate non-author human. This
    makes the automated gate an identity-bound approval, not a raw count.

Required environment:
  GITEA_URL                base URL, e.g. http://gitea:3500
  POLICY_EVAL_GITEA_TOKEN  READ-ONLY token: read:repository, read:issue,
                           and (docs/adr/0013) read:organization -- the
                           last one is only exercised when a repo's
                           branch protection actually sets
                           approvals_whitelist_teams, but confirmed live
                           that GET /teams/{id}/members 403s without it
                           even though GET /repos/{o}/{r}/teams (listing
                           which teams have repo access at all) does not
                           need it, so it must be granted up front, not
                           added reactively. Belonging to an account with
                           admin permission on the repo -- confirmed live
                           that reading branch_protections needs
                           repo-admin permission, not merely write (a
                           plain write collaborator gets 403).
                           Deliberately NOT the gate bot's approve/merge
                           token: DESIGN.md is explicit that token "never
                           enters a build container." This token can only
                           read; it cannot approve, merge, or push
                           anything.
  CI_REPO_OWNER            Woodpecker-native (see pipelines/fast.woodpecker.yml)
  CI_REPO_NAME             Woodpecker-native
  CI_COMMIT_PULL_REQUEST   Woodpecker-native; empty/unset on a plain push
"""
import json
import os
import sys
import urllib.error
import urllib.request
from datetime import datetime


def api(base_url, token, path, allow_404=False):
    req = urllib.request.Request(
        f"{base_url}/api/v1{path}",
        headers={"Authorization": f"token {token}"},
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        if allow_404 and e.code == 404:
            return None
        body = e.read().decode("utf-8", "replace")
        print(f"policy-eval: FATAL: GET {path} -> HTTP {e.code}: {body}", file=sys.stderr)
        sys.exit(2)
    except urllib.error.URLError as e:
        print(f"policy-eval: FATAL: GET {path} -> {e}", file=sys.stderr)
        sys.exit(2)


def parse_ts(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


def resolve_team_members(gitea_url, token, owner, repo, team_names):
    """Team names from branch protection -> the set of logins that are
    members of any of them. Resolves via the repo's own team list
    (name -> id, and implicitly confirms the team actually has access to
    THIS repo) rather than an org-wide team search -- a team named in
    approvals_whitelist_teams that isn't actually associated with the
    repo shouldn't silently count, and this makes that the natural
    result rather than a special case.
    """
    if not team_names:
        return set()

    repo_teams = api(gitea_url, token, f"/repos/{owner}/{repo}/teams")
    by_name = {t["name"]: t["id"] for t in repo_teams}

    members = set()
    for name in team_names:
        team_id = by_name.get(name)
        if team_id is None:
            print(
                f"policy-eval: WARNING: approvals_whitelist_teams names '{name}', "
                "which is not a team with access to this repo -- ignoring it, "
                "not treating it as a match for anyone",
                file=sys.stderr,
            )
            continue
        for m in api(gitea_url, token, f"/teams/{team_id}/members"):
            members.add(m["login"])
    return members


def main():
    gitea_url = os.environ["GITEA_URL"].rstrip("/")
    token = os.environ["POLICY_EVAL_GITEA_TOKEN"]
    owner = os.environ["CI_REPO_OWNER"]
    repo = os.environ["CI_REPO_NAME"]
    pr_number = os.environ.get("CI_COMMIT_PULL_REQUEST", "").strip()

    if not pr_number:
        print("policy-eval: not a pull request build, nothing to verify -- pass")
        return 0

    pr = api(gitea_url, token, f"/repos/{owner}/{repo}/pulls/{pr_number}")
    base_ref = pr["base"]["ref"]
    author = pr["user"]["login"]
    head_sha = pr["head"]["sha"]
    bot_login = os.environ.get("GATE_BOT_LOGIN", "").strip()

    protection = api(
        gitea_url, token, f"/repos/{owner}/{repo}/branch_protections/{base_ref}",
        allow_404=True,
    )
    required = protection.get("required_approvals", 0) if protection else 0
    whitelist_on = bool(protection.get("enable_approvals_whitelist")) if protection else False
    whitelist = set(protection.get("approvals_whitelist_username") or []) if protection else set()
    team_names = protection.get("approvals_whitelist_teams") or [] if protection else []

    if bot_login and required < 2:
        print(
            f"policy-eval: FATAL: bot/human approval mode requires at least 2 approvals; branch protection requires {required}",
            file=sys.stderr,
        )
        return 2

    if required <= 0:
        print(f"policy-eval: base '{base_ref}' requires 0 approvals -- pass")
        return 0

    if whitelist_on and team_names:
        team_members = resolve_team_members(gitea_url, token, owner, repo, team_names)
        whitelist |= team_members

    timeline = api(gitea_url, token, f"/repos/{owner}/{repo}/issues/{pr_number}/timeline")
    retarget_events = [e for e in timeline if e.get("type") == "change_target_branch"]
    last_retarget_at = None
    if retarget_events:
        last_retarget_at = max(parse_ts(e["created_at"]) for e in retarget_events)

    reviews = api(gitea_url, token, f"/repos/{owner}/{repo}/pulls/{pr_number}/reviews")

    # A reviewer's vote is their most recent review -- a later
    # REQUEST_CHANGES or re-approval supersedes an earlier one.
    latest = {}
    for r in reviews:
        login = r["user"]["login"]
        ts = parse_ts(r["submitted_at"])
        if login not in latest or ts > latest[login][0]:
            latest[login] = (ts, r)

    print(
        f"policy-eval: PR #{pr_number} -> base '{base_ref}', "
        f"required_approvals={required}"
        + (f", approvals_whitelist={sorted(whitelist)}" if whitelist_on else "")
        + (f" (includes members of team(s) {team_names})" if whitelist_on and team_names else "")
    )
    if last_retarget_at:
        print(
            f"policy-eval: base last changed at {last_retarget_at.isoformat()} "
            "-- approvals submitted at or before this do not count"
        )

    valid_approvers = []
    for login, (ts, r) in sorted(latest.items()):
        if r["state"] != "APPROVED" or r.get("dismissed"):
            continue

        reasons = []
        if login == author:
            reasons.append("reviewer is the PR author")
        if last_retarget_at and ts <= last_retarget_at:
            reasons.append(
                f"submitted {ts.isoformat()}, at or before the last retarget "
                f"({last_retarget_at.isoformat()}) -- predates the current base"
            )
        if r.get("commit_id") != head_sha:
            reasons.append(
                f"review is for commit {r.get('commit_id') or 'unknown'}, not current head {head_sha}"
            )
        if whitelist_on and login not in whitelist:
            reasons.append(f"not in base '{base_ref}''s approvals whitelist")

        if reasons:
            print(f"  REJECTED  {login}  approved {ts.isoformat()}: {'; '.join(reasons)}")
        else:
            print(f"  counted   {login}  approved {ts.isoformat()}")
            valid_approvers.append(login)

    print(f"policy-eval: valid approvals = {len(valid_approvers)} / required = {required}")

    if bot_login:
        humans = [login for login in valid_approvers if login != bot_login]
        if bot_login not in valid_approvers:
            print(f"policy-eval: FAIL -- required gate bot '{bot_login}' has not approved the current head")
            return 1
        if not humans:
            print("policy-eval: FAIL -- a distinct non-author human approval is required alongside the gate bot")
            return 1

    if len(valid_approvers) >= required:
        print("policy-eval: PASS")
        return 0

    print("policy-eval: FAIL -- get a fresh review against the current base")
    return 1


if __name__ == "__main__":
    sys.exit(main())
