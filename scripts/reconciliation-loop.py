#!/usr/bin/env python3
"""
scripts/reconciliation-loop.py

DESIGN.md's D4: Gitea does not auto-retry a webhook whose delivery
attempt failed -- it sits in the delivery history awaiting *manual*
redelivery, and ten minutes of platform downtime loses those events
outright. Without something watching for this, a PR whose webhook was
lost sits forever with no verdict, developers notice, and a gate that
gets stuck is a gate that gets disabled. This is that "something."

Scope, deliberately narrower than D4's literal wording ("no terminal
status"), and the reason why is the whole point of this docstring:

Two mechanisms were considered and rejected before writing this one,
both because they can't be trusted to mean what they'd need to mean:

  1. Woodpecker's own `POST /api/repos/{id}/pipelines` (CreatePipeline).
     Confirmed from Woodpecker's own source (server/api/pipeline.go):
     this creates an `EventManual`-type pipeline against a branch head,
     not a `pull_request`-type one. fast.woodpecker.yml's approval-check
     step is scoped `when: event: pull_request` -- a manually-triggered
     pipeline would simply SKIP it, and skip is not the same as pass,
     but a naive reconciliation script trusting "the pipeline ran" as
     "the PR is fine" would treat it that way.
  2. Gitea's `POST /repos/{o}/{r}/hooks/{id}/tests` (TestHook). Confirmed
     from Gitea's own source: its swagger summary is literally "Test a
     push webhook" -- it synthesizes a push-shaped payload for a given
     ref, never a pull_request one. Same problem as (1): approval-check
     would never run, and the fast-gate scanning steps that DO run under
     a push-shaped event would report success on a status context that
     branch protection's glob pattern (ssdlc/security-gate/**) is
     configured to accept -- silently satisfying the required check
     without policy-eval's approval-check ever having evaluated this PR
     at all. That is not an availability fix; it is a new bypass, of
     exactly the class SADR-0001/0003/0009 exist to close.

The only recovery mechanism used here is a REAL, unmodified git action:
an empty commit (`git commit --allow-empty`), pushed for real, through
the same path a developer's own push takes. Gitea fires its own genuine
push + PR-synchronize webhooks for it -- nothing synthesized, nothing
this script has to vouch for the shape of. This changes the PR's head
SHA (D3: discard any result whose SHA is no longer the PR head) rather
than retroactively producing a verdict for the original stuck commit --
that is correct, not a limitation: the original commit was never really
evaluated, and pretending otherwise would be the bug.

Detection is intentionally narrower than "no terminal status" too: a PR
counts as stuck only if NO status context matching
`ssdlc/security-gate/pr/*` exists for its current head SHA at all --
not merely non-terminal. Distinguishing a genuinely stuck `pending` (the
webhook that started it got lost after all) from an actively-running one
needs a staleness threshold this pass deliberately doesn't build, to
avoid nudging a pipeline that's simply still running. Tracked as a
follow-up, not silently assumed solved.

Side effect worth documenting, not hiding: if a repo has
`dismiss_stale_approvals` enabled, nudging a stuck PR with a new commit
will dismiss any approvals already on it, same as a developer's own
push would. That's correct behavior, not a bug -- the code that
actually lands is the new commit, and it deserves the same fresh review
any other new commit would get.

Token: write:repository only, on its own identity -- enough to push a
commit, nothing more. Deliberately not the gate bot's approve/merge
token or policy-eval's read-only one; a third, narrowly-scoped
credential for a third distinct job.

Required environment:
  GITEA_URL                base URL Gitea is reachable at from wherever
                            this script runs (host-reachable or the
                            internal docker-network form, matching how
                            it's actually being invoked)
  GITEA_RECONCILE_TOKEN    write:repository only
  GITEA_RECONCILE_USER     the account name that token belongs to
  REPO_OWNER
  REPO_NAME
  POLL_INTERVAL_SECONDS    default 60
  RUN_ONCE                 "1" for a single pass (testing); default loops
"""
import json
import os
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

STATUS_CONTEXT_PREFIX = "ssdlc/security-gate/pr/"
MAX_NUDGES_PER_PR_PER_RUN = 3


def api(base_url, token, method, path, body=None, allow_404=False):
    data = None
    headers = {"Authorization": f"token {token}"}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(f"{base_url}{path}", data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        if allow_404 and e.code == 404:
            return None
        body_text = e.read().decode("utf-8", "replace")
        print(f"reconciliation-loop: HTTP {e.code} on {method} {path}: {body_text}", file=sys.stderr)
        return None


def gitea(path, method="GET", body=None, allow_404=False):
    return api(
        os.environ["GITEA_URL"].rstrip("/"), os.environ["GITEA_RECONCILE_TOKEN"],
        method, f"/api/v1{path}", body, allow_404,
    )


def has_pr_status(owner, repo, sha):
    status = gitea(f"/repos/{owner}/{repo}/commits/{sha}/status")
    if not status:
        return False
    # Confirmed live: Gitea returns "statuses": null (a present key with
    # a null value) when none exist yet, not an absent key -- dict.get's
    # default only fires for a MISSING key, so `.get("statuses", [])`
    # still returns None here and the loop below blows up on it.
    return any(s["context"].startswith(STATUS_CONTEXT_PREFIX) for s in (status.get("statuses") or []))


def nudge(owner, repo, branch, token, user):
    """Real git push of a real empty commit -- see module docstring for
    why this, and not any form of synthesized webhook replay."""
    base_url = os.environ["GITEA_URL"].rstrip("/")
    with tempfile.TemporaryDirectory() as work_dir:
        clone_url = f"{base_url}/{owner}/{repo}.git"
        subprocess.run(["git", "clone", "-q", "-b", branch, clone_url, work_dir], check=True)
        subprocess.run(["git", "-C", work_dir, "config", "user.email", f"{user}@ssdlc.local"], check=True)
        subprocess.run(["git", "-C", work_dir, "config", "user.name", user], check=True)
        subprocess.run(
            ["git", "-C", work_dir, "commit", "--allow-empty", "-m",
             "reconciliation: no gate verdict was ever recorded for the prior commit "
             "(a lost webhook, not a real failure) -- this empty commit re-triggers evaluation"],
            check=True,
        )
        push_url = f"{base_url.replace('://', f'://{user}:{token}@')}"
        # git's own HTTP client, not the shell curl -- push to origin
        # (already correctly pathed from the clone), auth via header,
        # matching tests/regression/lib.sh's git_push_origin pattern
        # rather than embedding credentials in a rebuilt URL.
        subprocess.run(
            ["git", "-C", work_dir, "-c", f"http.extraHeader=Authorization: token {token}",
             "push", "-q", "origin", branch],
            check=True,
        )


def run_once(owner, repo, token, user, nudge_counts):
    prs = gitea(f"/repos/{owner}/{repo}/pulls?state=open") or []
    for pr in prs:
        number = pr["number"]
        sha = pr["head"]["sha"]
        branch = pr["head"]["ref"]

        if has_pr_status(owner, repo, sha):
            continue

        count = nudge_counts.get(number, 0)
        if count >= MAX_NUDGES_PER_PR_PER_RUN:
            print(
                f"PR #{number}: still no gate status after {count} nudges -- "
                "stopping, this needs a human, not another automated push"
            )
            continue

        print(f"PR #{number}: no '{STATUS_CONTEXT_PREFIX}*' status for head {sha[:12]} -- nudging")
        nudge(owner, repo, branch, token, user)
        nudge_counts[number] = count + 1


def main():
    owner = os.environ["REPO_OWNER"]
    repo = os.environ["REPO_NAME"]
    token = os.environ["GITEA_RECONCILE_TOKEN"]
    user = os.environ["GITEA_RECONCILE_USER"]
    interval = int(os.environ.get("POLL_INTERVAL_SECONDS", "60"))
    once = os.environ.get("RUN_ONCE") == "1"

    nudge_counts = {}
    while True:
        run_once(owner, repo, token, user, nudge_counts)
        if once:
            return 0
        time.sleep(interval)


if __name__ == "__main__":
    sys.exit(main())
