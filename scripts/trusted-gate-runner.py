#!/usr/bin/env python3
"""Run a platform-owned gate bundle and write a signed PR-head attestation.

This is the first operational boundary for SADR-0017. It downloads the exact
Gitea pull-request head using a read-only token, extracts it to a fresh
directory, invokes a platform-configured bundle command, validates the bundle
result, and writes an atomic attestation for bot-approver.py. It never reads a
pipeline definition or executable command from the application checkout.

The bundle command is configured by the platform operator as
GATE_BUNDLE_COMMAND (an absolute executable plus arguments; no shell syntax).
It receives GATE_WORKSPACE, GATE_OUTPUT_DIR,
GATE_HEAD_SHA, GATE_PR_NUMBER, GATE_REPOSITORY_OWNER and
GATE_REPOSITORY_NAME. It must write $GATE_OUTPUT_DIR/result.json:

  {"decision":"pass", "scanners":{"secrets":"success", ...}}

The runner does NOT expose GATE_ATTESTATION_KEY to that command. Deploy the
bundle in an isolated worker/container with no forge write token and no bot
credentials. This script is a one-repository polling worker for the pilot;
RUN_ONCE=1 makes it suitable for a scheduler or smoke test.
"""
import io
import json
import os
import shlex
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))
from gate_contract.attestation import contract_digest, sign


REQUIRED_ENV = (
    "GITEA_URL", "GITEA_RUNNER_TOKEN", "REPO_OWNER", "REPO_NAME",
    "GATE_BUNDLE_COMMAND", "GATE_ATTESTATIONS_DIR", "GATE_ATTESTATION_KEY",
)


def fail(message):
    print(f"trusted-gate-runner: {message}", file=sys.stderr)


def api(path):
    request = urllib.request.Request(
        os.environ["GITEA_URL"].rstrip("/") + "/api/v1" + path,
        headers={"Authorization": f"token {os.environ['GITEA_RUNNER_TOKEN']}"},
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return response.read()
    except urllib.error.HTTPError as error:
        fail(f"Gitea HTTP {error.code} for {path}")
    except OSError as error:
        fail(f"Gitea request failed for {path}: {error}")
    return None


def safe_extract(archive, destination):
    root = Path(destination).resolve()
    for member in archive.getmembers():
        member_path = (root / member.name).resolve()
        if member.issym() or member.islnk() or root not in member_path.parents and member_path != root:
            raise ValueError(f"unsafe archive member: {member.name}")
    archive.extractall(root)


def checkout_head(owner, repo, head_sha, workspace):
    archive_path = f"/repos/{urllib.parse.quote(owner, safe='')}/{urllib.parse.quote(repo, safe='')}/archive/{urllib.parse.quote(head_sha, safe='')}.tar.gz"
    payload = api(archive_path)
    if payload is None:
        return None
    try:
        with tarfile.open(fileobj=io.BytesIO(payload), mode="r:gz") as archive:
            safe_extract(archive, workspace)
        roots = [path for path in Path(workspace).iterdir() if path.is_dir()]
        if len(roots) != 1:
            raise ValueError("archive did not contain exactly one repository root")
        return roots[0]
    except (tarfile.TarError, OSError, ValueError) as error:
        fail(f"could not safely extract {head_sha[:12]}: {error}")
        return None


def load_contract():
    path = Path(os.environ.get("GATE_CONTRACT_FILE", PROJECT_ROOT / "gate-contract" / "contract.json"))
    try:
        contract = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        fail(f"cannot load platform contract {path}: {error}")
        return None, None
    scanners = contract.get("required_scanners")
    if not isinstance(scanners, list) or not all(isinstance(value, str) for value in scanners):
        fail("contract has no valid required_scanners list")
        return None, None
    return contract, tuple(scanners)


def execute_bundle(workspace, output_dir, pr):
    command = os.environ["GATE_BUNDLE_COMMAND"]
    environment = {
        key: value for key, value in os.environ.items()
        if key not in {"GATE_ATTESTATION_KEY", "GITEA_RUNNER_TOKEN", "GITEA_BOT_TOKEN", "WOODPECKER_TOKEN"}
    }
    environment.update({
        "GATE_WORKSPACE": str(workspace),
        "GATE_OUTPUT_DIR": str(output_dir),
        "GATE_HEAD_SHA": pr["head"]["sha"],
        "GATE_PR_NUMBER": str(pr["number"]),
        "GATE_REPOSITORY_OWNER": os.environ["REPO_OWNER"],
        "GATE_REPOSITORY_NAME": os.environ["REPO_NAME"],
    })
    # Parse operator-owned arguments without a shell. This prevents application
    # content from becoming shell syntax through the workspace or environment.
    result = subprocess.run(shlex.split(command), cwd=workspace, env=environment, timeout=int(os.environ.get("GATE_BUNDLE_TIMEOUT_SECONDS", "900")))
    if result.returncode != 0:
        fail(f"bundle failed for PR #{pr['number']} with exit {result.returncode}")
        return None
    try:
        result_file = output_dir / "result.json"
        return json.loads(result_file.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        fail(f"bundle did not produce valid result.json: {error}")
        return None


def issue(owner, repo, pr, contract, required_scanners, result):
    if result.get("decision") not in {"pass", "fail"} or not isinstance(result.get("scanners"), dict):
        fail(f"bundle result for PR #{pr['number']} has invalid decision/scanners")
        return False
    if any(result["scanners"].get(scanner) != "success" for scanner in required_scanners):
        fail(f"bundle result for PR #{pr['number']} is missing a successful required scanner")
        return False
    document = sign({
        "schema_version": 1,
        "repository_owner": owner,
        "repository_name": repo,
        "pull_request": pr["number"],
        "head_sha": pr["head"]["sha"],
        "contract_digest": contract_digest(contract),
        "decision": result["decision"],
        "scanners": result["scanners"],
    }, os.environ["GATE_ATTESTATION_KEY"])
    destination = Path(os.environ["GATE_ATTESTATIONS_DIR"])
    destination.mkdir(parents=True, exist_ok=True)
    target = destination / f"{owner}--{repo}--{pr['head']['sha']}.json"
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=destination, delete=False) as handle:
        json.dump(document, handle, sort_keys=True, indent=2)
        handle.write("\n")
        temporary = Path(handle.name)
    os.replace(temporary, target)
    print(f"trusted-gate-runner: wrote {target.name} ({result['decision']})")
    return True


def run_once():
    missing = [name for name in REQUIRED_ENV if not os.environ.get(name)]
    if missing:
        fail(f"missing required configuration: {', '.join(missing)}")
        return 2
    contract, scanners = load_contract()
    if contract is None:
        return 2
    owner, repo = os.environ["REPO_OWNER"], os.environ["REPO_NAME"]
    data = api(f"/repos/{urllib.parse.quote(owner, safe='')}/{urllib.parse.quote(repo, safe='')}/pulls?state=open")
    if data is None:
        return 1
    try:
        prs = json.loads(data)
    except json.JSONDecodeError:
        fail("Gitea returned invalid pull-request JSON")
        return 1
    for pr in prs:
        head_sha = pr.get("head", {}).get("sha")
        if not head_sha or not pr.get("number"):
            fail("skipping PR with no number or head SHA")
            continue
        with tempfile.TemporaryDirectory(prefix="ssdlc-gate-") as temporary:
            workspace = checkout_head(owner, repo, head_sha, temporary)
            if workspace is None:
                continue
            output = Path(temporary) / "output"
            output.mkdir()
            result = execute_bundle(workspace, output, pr)
            if result is not None:
                issue(owner, repo, pr, contract, scanners, result)
    return 0


def main():
    while True:
        result = run_once()
        if os.environ.get("RUN_ONCE") == "1":
            return result
        time.sleep(int(os.environ.get("POLL_INTERVAL_SECONDS", "30")))


if __name__ == "__main__":
    sys.exit(main())
