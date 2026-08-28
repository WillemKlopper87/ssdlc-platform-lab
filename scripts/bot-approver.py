#!/usr/bin/env python3
"""
scripts/bot-approver.py

The minimal, load-bearing piece of D2's "bot-only approver team" mechanism
that nothing in this project has built yet: something has to actually cast
the bot's vote. Without it, `required_approvals: 2` (D2's proven mitigation
for SADR-0001's status-forgery bypass) is unsatisfiable -- no account ever
gives the bot's half of the two required approvals, and every PR on a repo
configured that way is permanently unmergeable. This script closes that gap
for real, live-tested against compose/minimal's actual Gitea+Woodpecker
stack, not a design sketch.

Deliberately NOT a Woodpecker pipeline step. DESIGN.md is explicit that the
gate bot's token "lives only in the sidecar... and never enters a build
container" -- a Woodpecker step's container runs the SAME untrusted PR
content this whole platform exists to gate, and a compromised scanner image
or a sneaky pipeline definition could exfiltrate a token that lived there.
This script is meant to run as its own standing process (a
`docker compose`/systemd-managed service, poll loop), holding the bot's
token in isolation from every build agent.

What it does, once per poll cycle, per configured repo:
  1. List open PRs; for each, get the CURRENT head SHA (D3: "discard any
     result whose SHA is no longer the PR head" -- always re-read this
     fresh, never cache it across cycles).
  2. Find the latest Woodpecker pipeline run for that exact SHA.
  3. Check every step EXCEPT `approval-check` succeeded. `approval-check`
     is excluded deliberately: it is the one step whose own pass/fail
     depends circularly on the bot's vote already existing (it counts
     valid approvals against `required_approvals`, and the bot's approval
     is one of them) -- waiting on it here would deadlock, since the bot
     would never approve until approval-check passes, and approval-check
     can never pass until the bot approves.
  4. Skip if the bot's own most recent review already targets this exact
     SHA and is APPROVED -- makes the whole cycle idempotent; re-running
     it does nothing once the bot has already voted for the current head.
  5. Otherwise: POST the bot's approval, then restart the pipeline so
     `approval-check` gets a fresh run that counts the new vote. If a
     human reviewer already approved earlier, this one restart is enough
     for the PR to become mergeable; if not, `approval-check` still fails
     with a clear reason (not enough approvals) and nothing here loops or
     retries destructively -- the next poll cycle just tries again.

Token scope: `write:repository` only, on the bot's own dedicated account --
enough to POST a review, nothing more. Still sensitive (D2's "crown jewel"
language applies), so it must live only wherever this script runs, set via
GITEA_BOT_TOKEN, never committed, never reused for anything else.

Gate contract: `GATE_CONTRACT_ENFORCE=1` is the secure default. Set it to
`0` only for a deliberately isolated legacy test fixture while migrating a
repository that does not yet contain the managed gate files on its protected
base branch. `GATE_MANAGED_PATHS` may narrow or extend the comma-separated
path list only when the platform operator has reviewed that change.
"""
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

# Scripts are executed by absolute path in the sidecar, so add the project
# root explicitly instead of relying on the caller's current directory.
PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if PROJECT_ROOT not in sys.path:
    sys.path.insert(0, PROJECT_ROOT)
from gate_contract.attestation import signature_is_valid

EXCLUDE_STEPS = {"approval-check"}
# These files are platform-owned gate logic. A PR may contain application code
# to scan, but it cannot change the pipeline, approval evaluator, finding
# normalisers, or severity policy and then use that changed logic to earn the
# bot approval. The durable successor is SADR-0017's signed, external gate
# bundle; this protected-base comparison is the safe Sprint 01 transition.
DEFAULT_GATE_MANAGED_PATHS = (
    ".woodpecker.yml",
    "policy-eval/verify-approvals.py",
    "policy-eval/evaluate-findings.py",
    "normalise/gitleaks_adapter.py",
    "normalise/semgrep_adapter.py",
    "normalise/trivy_adapter.py",
    "policy/severity.rego",
    # docs/adr/0024: per-repo content (unlike every other entry above, which
    # is byte-identical across every onboarded repo), but the same
    # base-vs-head comparison protects it correctly regardless -- a PR
    # cannot add its own new finding to ITS OWN repo's baseline and expect
    # the bot to miss the change, since this compares against that same
    # repo's protected base, never a canonical platform-wide copy.
    ".ssdlc/baseline.json",
)
# Large platform-owned trees are compared through Gitea's recursive Git Trees
# API, once per base/head, rather than one Contents API call for every file.
# This makes the vendored Semgrep rules part of the protected gate contract.
DEFAULT_GATE_MANAGED_TREE_PREFIXES = ("policy/vendored-rules/",)


def api(base_url, headers, method, path, body=None, allow_404=False):
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers = {**headers, "Content-Type": "application/json"}
    req = urllib.request.Request(
        f"{base_url}{path}", data=data, method=method, headers=headers
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        if allow_404 and e.code == 404:
            return None
        body_text = e.read().decode("utf-8", "replace")
        print(f"bot-approver: HTTP {e.code} on {method} {path}: {body_text}", file=sys.stderr)
        return None


def gitea(path, method="GET", body=None, allow_404=False):
    return api(
        os.environ["GITEA_URL"].rstrip("/"),
        {"Authorization": f"token {os.environ['GITEA_BOT_TOKEN']}"},
        method,
        f"/api/v1{path}",
        body,
        allow_404,
    )


def woodpecker(path, method="GET", body=None):
    return api(
        os.environ["WOODPECKER_URL"].rstrip("/"),
        {"Authorization": f"Bearer {os.environ['WOODPECKER_TOKEN']}"},
        method,
        f"/api{path}",
        body,
    )


def gate_managed_paths():
    configured = os.environ.get("GATE_MANAGED_PATHS", "")
    if not configured.strip():
        return DEFAULT_GATE_MANAGED_PATHS
    return tuple(path.strip() for path in configured.split(",") if path.strip())


def file_at_ref(owner, repo, path, ref):
    encoded_path = urllib.parse.quote(path, safe="/")
    encoded_ref = urllib.parse.quote(ref, safe="")
    return gitea(
        f"/repos/{owner}/{repo}/contents/{encoded_path}?ref={encoded_ref}",
        allow_404=True,
    )


def tree_at_ref(owner, repo, ref):
    return gitea(
        f"/repos/{owner}/{repo}/git/trees/{urllib.parse.quote(ref, safe='')}?recursive=true",
        allow_404=True,
    )


def managed_tree_at_ref(owner, repo, ref, prefix):
    """Return a deterministic path-to-blob map for one protected tree.

    Gitea marks an oversized recursive response as truncated. Treat that as a
    failure, rather than comparing an incomplete list and granting approval.
    """
    response = tree_at_ref(owner, repo, ref)
    if not isinstance(response, dict) or response.get("truncated"):
        return None
    entries = response.get("tree")
    if not isinstance(entries, list):
        return None
    result = {}
    for entry in entries:
        if not isinstance(entry, dict):
            return None
        path, blob_sha = entry.get("path"), entry.get("sha")
        if isinstance(path, str) and path.startswith(prefix):
            if entry.get("type") != "blob" or not isinstance(blob_sha, str):
                return None
            result[path] = blob_sha
    # The platform-owned tree must exist at both revisions. An empty/missing
    # rules directory is not equivalent to the approved security policy.
    return result or None


def gate_contract_matches_base(owner, repo, pr):
    """Return true only when the PR kept all platform-owned gate files
    byte-identical to its target-base revision.

    The Contents API returns Gitea's blob SHA, which avoids downloading or
    executing a PR-controlled file just to compare it. Missing files are a
    mismatch too: deleting an approval script is not a harmless change.
    """
    base_sha = pr.get("base", {}).get("sha")
    head_sha = pr.get("head", {}).get("sha")
    if not base_sha or not head_sha:
        print("gate-contract: PR response lacks base/head SHA -- fail closed", file=sys.stderr)
        return False

    for path in gate_managed_paths():
        base_file = file_at_ref(owner, repo, path, base_sha)
        head_file = file_at_ref(owner, repo, path, head_sha)
        if not base_file or not head_file or base_file.get("sha") != head_file.get("sha"):
            print(
                f"gate-contract: managed file differs from protected base: {path} -- bot will not approve",
                file=sys.stderr,
            )
            return False
    for prefix in DEFAULT_GATE_MANAGED_TREE_PREFIXES:
        base_tree = managed_tree_at_ref(owner, repo, base_sha, prefix)
        head_tree = managed_tree_at_ref(owner, repo, head_sha, prefix)
        if base_tree is None or head_tree is None or base_tree != head_tree:
            print(
                f"gate-contract: managed tree differs from protected base: {prefix} -- bot will not approve",
                file=sys.stderr,
            )
            return False
    return True


def _safe_attestation_component(value):
    """Prevent repository fields from escaping the platform-controlled store."""
    return value and all(character.isalnum() or character in "._-" for character in value)


def trusted_attestation_matches(owner, repo, pr):
    """Validate a trusted-runner result, bound to this exact PR and head SHA."""
    # docs/adr/0022: GATE_POLICY_DIGEST is the operator-configured expected
    # value of scripts/print-policy-digest.py -- verifying it here means a
    # bundle rebuild with a silently different ruleset (a rule added, removed,
    # or edited) cannot pass attestation just because the scan itself ran.
    required = ("GATE_ATTESTATIONS_DIR", "GATE_ATTESTATION_KEY", "GATE_CONTRACT_DIGEST", "GATE_POLICY_DIGEST")
    missing = [name for name in required if not os.environ.get(name)]
    if missing:
        print(f"gate-attestation: missing required configuration {', '.join(missing)} -- fail closed", file=sys.stderr)
        return False
    head_sha = pr.get("head", {}).get("sha")
    pr_number = pr.get("number")
    if not head_sha or not pr_number or not _safe_attestation_component(owner) or not _safe_attestation_component(repo):
        print("gate-attestation: invalid PR identity -- fail closed", file=sys.stderr)
        return False
    path = os.path.join(os.environ["GATE_ATTESTATIONS_DIR"], f"{owner}--{repo}--{head_sha}.json")
    try:
        with open(path, encoding="utf-8") as handle:
            attestation = json.load(handle)
    except (OSError, json.JSONDecodeError) as error:
        print(f"gate-attestation: unavailable or invalid {path}: {error} -- fail closed", file=sys.stderr)
        return False
    if not signature_is_valid(attestation, os.environ["GATE_ATTESTATION_KEY"]):
        print("gate-attestation: signature invalid -- fail closed", file=sys.stderr)
        return False
    expected_scanners = tuple(
        scanner.strip() for scanner in os.environ.get("GATE_REQUIRED_SCANNERS", "secrets,sast,dependencies").split(",") if scanner.strip()
    )
    expected = {
        "schema_version": 1,
        "repository_owner": owner,
        "repository_name": repo,
        "pull_request": pr_number,
        "head_sha": head_sha,
        "contract_digest": os.environ["GATE_CONTRACT_DIGEST"],
        "policy_digest": os.environ["GATE_POLICY_DIGEST"],
        "decision": "pass",
    }
    if any(attestation.get(field) != value for field, value in expected.items()):
        print("gate-attestation: identity, contract, or decision mismatch -- fail closed", file=sys.stderr)
        return False
    scanners = attestation.get("scanners")
    if not isinstance(scanners, dict) or any(scanners.get(name) != "success" for name in expected_scanners):
        print("gate-attestation: required scanner result missing or unsuccessful -- fail closed", file=sys.stderr)
        return False
    return True


def process_pr(owner, repo, woodpecker_repo_id, bot_login, pr):
    pr_number = pr["number"]
    head_sha = pr["head"]["sha"]

    if os.environ.get("GATE_CONTRACT_ENFORCE", "1") != "0":
        if not gate_contract_matches_base(owner, repo, pr):
            return
    attestation_required = os.environ.get("GATE_ATTESTATION_REQUIRED", "0") == "1"
    if attestation_required:
        if not trusted_attestation_matches(owner, repo, pr):
            return
    else:
        pipelines = woodpecker(f"/repos/{woodpecker_repo_id}/pipelines") or []
        candidates = [
            p for p in pipelines if p.get("event") == "pull_request" and p.get("commit") == head_sha
        ]
        if not candidates:
            print(f"PR #{pr_number}: no pipeline run yet for head {head_sha[:12]} -- skip")
            return
        pipeline = max(candidates, key=lambda p: p["id"])

        detail = woodpecker(f"/repos/{woodpecker_repo_id}/pipelines/{pipeline['number']}")
        if not detail:
            return
        steps = [s for wf in detail.get("workflows", []) for s in wf.get("children", [])]
        blocking = [s for s in steps if s["name"] not in EXCLUDE_STEPS and s["name"] != "clone"]
        if not blocking or any(s["state"] != "success" for s in blocking):
            states = {s["name"]: s["state"] for s in blocking}
            print(f"PR #{pr_number}: scanning steps not all green yet ({states}) -- skip")
            return

    reviews = gitea(f"/repos/{owner}/{repo}/pulls/{pr_number}/reviews") or []
    bot_reviews = [r for r in reviews if r["user"]["login"] == bot_login]
    if bot_reviews:
        latest = max(bot_reviews, key=lambda r: r["submitted_at"])
        if latest["state"] == "APPROVED" and latest["commit_id"] == head_sha:
            print(f"PR #{pr_number}: bot already approved head {head_sha[:12]} -- skip")
            return

    evidence = "trusted attestation" if attestation_required else "scanning steps green"
    print(f"PR #{pr_number}: {evidence}, bot has not voted for {head_sha[:12]} -- approving")
    result = gitea(
        f"/repos/{owner}/{repo}/pulls/{pr_number}/reviews",
        method="POST",
        body={"event": "APPROVED", "body": "Automated: fast-gate scanning steps passed for this commit."},
    )
    if result is None:
        print(f"PR #{pr_number}: approval POST failed -- not restarting the pipeline")
        return

    if not attestation_required:
        print(f"PR #{pr_number}: approved, restarting pipeline #{pipeline['number']} for a fresh approval-check")
        woodpecker(f"/repos/{woodpecker_repo_id}/pipelines/{pipeline['number']}", method="POST")


def run_once(owner, repo, woodpecker_repo_id, bot_login):
    prs = gitea(f"/repos/{owner}/{repo}/pulls?state=open") or []
    for pr in prs:
        process_pr(owner, repo, woodpecker_repo_id, bot_login, pr)


def main():
    owner = os.environ["REPO_OWNER"]
    repo = os.environ["REPO_NAME"]
    woodpecker_repo_id = os.environ["WOODPECKER_REPO_ID"]
    bot_login = os.environ["GITEA_BOT_LOGIN"]
    interval = int(os.environ.get("POLL_INTERVAL_SECONDS", "30"))
    once = os.environ.get("RUN_ONCE") == "1"

    while True:
        run_once(owner, repo, woodpecker_repo_id, bot_login)
        if once:
            return 0
        time.sleep(interval)


if __name__ == "__main__":
    sys.exit(main())
