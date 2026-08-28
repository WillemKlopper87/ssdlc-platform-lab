#!/usr/bin/env python3
"""docs/adr/0024. Generate or refresh a repo's differential-gating baseline.

Platform-admin tool, run against a real checkout of a repo's default branch
(onboard-repo.sh runs this as part of onboarding; an operator can also run it
by hand to refresh an existing baseline). Never run by a PR -- the output
file (.ssdlc/baseline.json) is gate-managed (scripts/bot-approver.py's
DEFAULT_GATE_MANAGED_PATHS), so a PR cannot alter it and still receive the
bot's approval.

Only Semgrep (against the vendored ruleset, matching the fast gate exactly)
and Trivy findings are ever baselined. Gitleaks is deliberately never run
here: DESIGN.md D7's rule ("secrets are never baselined") means a secret
finding could never appear in the baseline file in the first place, so
scanning for them here would be wasted work with no possible output.

Self-shrinking by construction, not by a separate mode flag: if an existing
baseline file is present, the new baseline is the INTERSECTION of the old
baseline and the findings still actually present -- a fixed finding drops
out automatically, and nothing can be added that wasn't already there,
without deleting the file first (which is itself the deliberate "start a
fresh baseline" action, not something this script does implicitly). This is
DESIGN.md's "the baseline only shrinks" rule, enforced structurally rather
than by convention.

Usage:
  python3 generate-baseline.py <workspace-dir> <output-path> [--repo <owner/repo>] [--commit <sha>]
"""
import argparse
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.dirname(SCRIPT_DIR)
NORMALISE_DIR = os.path.join(PROJECT_ROOT, "normalise")
VENDORED_RULES = os.path.join(PROJECT_ROOT, "policy", "vendored-rules")

sys.path.insert(0, NORMALISE_DIR)
import semgrep_adapter  # noqa: E402
import trivy_adapter  # noqa: E402

SEMGREP_IMAGE = "semgrep/semgrep:1.174.0"
TRIVY_IMAGE = "aquasec/trivy:0.74.0"

# Found live (docs/adr/0024 verification): onboard-repo.sh commits these same
# platform-owned paths into every onboarded repo (scripts/bot-approver.py's
# DEFAULT_GATE_MANAGED_PATHS/_TREE_PREFIXES -- duplicated here rather than
# imported, since this script also runs standalone against an arbitrary
# workspace with no guarantee scripts/bot-approver.py is even present).
# policy/vendored-rules/ alone is 1298 files, ~700 of them deliberately
# vulnerable rule-test fixtures (bash/ifs-tampering.bash, c/double-free.c,
# etc.) -- exactly the pattern each sibling rule is written to match. Once
# committed, scanning "." picks its own fixtures up as if they were the
# onboarded repo's application code: a live run against a workspace that
# already had these files committed produced 2564 findings, almost all
# self-matches against this tree, silently correct-looking output with no
# error of any kind. Same self-contamination class docs/adr/0018 already
# found for report filenames, just for a much larger target never checked
# against back then.
PLATFORM_MANAGED_FILES = (
    ".woodpecker.yml",
    "policy-eval/verify-approvals.py",
    "policy-eval/evaluate-findings.py",
    "normalise/gitleaks_adapter.py",
    "normalise/semgrep_adapter.py",
    "normalise/trivy_adapter.py",
    "policy/severity.rego",
    ".ssdlc/baseline.json",
)
PLATFORM_MANAGED_DIRS = ("policy/vendored-rules",)


def run_scanner(docker_args, name):
    # encoding/errors explicit: found live on Windows -- subprocess.run's
    # default text-mode decoding uses the system ANSI codepage (cp1252 here),
    # which crashes on Docker CLI's own UTF-8 progress-bar output. Does not
    # fail the scan itself (the crash is confined to a background reader
    # thread), but is noisy and worth not having at all.
    result = subprocess.run(
        ["docker", "run", "--rm", *docker_args],
        capture_output=True, text=True, encoding="utf-8", errors="replace",
    )
    if result.returncode not in (0, 1):
        print(f"generate-baseline: {name} failed (exit {result.returncode}):\n{result.stderr}", file=sys.stderr)
        sys.exit(2)
    return result


def scan_workspace(workspace):
    workspace = os.path.abspath(workspace)
    with tempfile.TemporaryDirectory() as reports_dir:
        semgrep_report = os.path.join(reports_dir, "semgrep-report.json")
        trivy_report = os.path.join(reports_dir, "trivy-report.json")

        # semgrep/semgrep:1.174.0 has no ENTRYPOINT set (confirmed live via
        # `docker inspect`: Entrypoint=[], Cmd=["semgrep","--help"]) -- any
        # args supplied to `docker run` replace its CMD wholesale rather than
        # extending an entrypoint, so "semgrep" must be the first token
        # ourselves or Docker tries to exec "--disable-version-check" as the
        # binary. pipelines/fast.woodpecker.yml never hits this because
        # Woodpecker's own step execution runs commands through a shell
        # inside the container, not a raw `docker run <image> <args>` call.
        semgrep_excludes = []
        for path in PLATFORM_MANAGED_FILES + PLATFORM_MANAGED_DIRS:
            semgrep_excludes += ["--exclude", path]
        run_scanner([
            "-v", f"{workspace}:/src:ro", "-v", f"{VENDORED_RULES}:/rules:ro",
            "-v", f"{reports_dir}:/out", SEMGREP_IMAGE,
            "semgrep", "--disable-version-check", "--metrics=off", "--config=/rules",
            *semgrep_excludes,
            "--json", "--output", "/out/semgrep-report.json", "/src",
        ], "semgrep")
        run_scanner([
            "-v", f"{workspace}:/src:ro", "-v", f"{reports_dir}:/out", TRIVY_IMAGE,
            "fs", "--exit-code=0", "--format=json", "--output=/out/trivy-report.json",
            "--skip-files", ",".join(PLATFORM_MANAGED_FILES),
            "--skip-dirs", ",".join(PLATFORM_MANAGED_DIRS),
            "/src",
        ], "trivy")

        findings = []
        if os.path.exists(semgrep_report):
            with open(semgrep_report, encoding="utf-8") as fh:
                data = json.load(fh)
            # docs/adr/0025: this scan's target was "/src" (the container
            # mount point above), not workspace's own host path -- Semgrep
            # reports whatever target it was given, so the root to strip
            # is that literal container path, matching what the fast gate
            # (which scans ".") already returns unprefixed. See
            # normalise/semgrep_adapter.py's docstring for why this matters:
            # without it, every Semgrep finding this script baselines has a
            # different fingerprint than the fast gate computes for the
            # identical file, and it blocks on every PR regardless.
            findings += semgrep_adapter.normalize(data.get("results", []), "/src")
        if os.path.exists(trivy_report):
            with open(trivy_report, encoding="utf-8") as fh:
                data = json.load(fh)
            findings += trivy_adapter.normalize(data.get("Results"))
        return findings


class BaselineUnreadable(Exception):
    """The baseline file exists but cannot be trusted as a refresh floor."""


def load_existing(path):
    """Returns None only when the file genuinely does not exist -- the
    "first onboarding, no baseline yet" case, which main() only accepts
    with --create. A file that exists but is empty, not JSON, missing its
    schema, or has a malformed entry raises BaselineUnreadable instead of
    returning None.

    docs/adr/0025 (SSDLC_Code_report.md H1): the original version of this
    function collapsed BOTH cases to None, and main() treated None as
    permission to create a fresh full-acceptance snapshot. That silently
    converts "the existing baseline is corrupt" into "every current
    finding is now accepted debt" -- exactly backwards, and asymmetric
    with evaluate-findings.py's OWN handling of the same file at
    evaluation time (there, unreadable correctly means "trust nothing,
    treat as empty" -- the safe direction for a step that BLOCKS on a
    mismatch, not one that's about to overwrite the file no one can read
    yet). A corrupt existing baseline must fail this script outright; the
    only way to reset it is the same explicit action DESIGN.md's docstring
    already named -- delete the file, then rerun with --create.
    """
    if not os.path.exists(path):
        return None
    try:
        with open(path, encoding="utf-8") as fh:
            document = json.load(fh)
    except (OSError, json.JSONDecodeError) as error:
        raise BaselineUnreadable(f"{path} exists but is not valid JSON: {error}") from error
    if not isinstance(document, dict):
        raise BaselineUnreadable(f"{path} does not contain a JSON object")
    entries = document.get("findings")
    if not isinstance(entries, list):
        raise BaselineUnreadable(f"{path} has no 'findings' list")
    result = {}
    for entry in entries:
        if not isinstance(entry, dict) or not isinstance(entry.get("tool"), str) or not isinstance(entry.get("fingerprint"), str):
            raise BaselineUnreadable(f"{path} contains a malformed finding entry")
        result[(entry["tool"], entry["fingerprint"])] = entry
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("workspace")
    parser.add_argument("output")
    parser.add_argument("--repo", default="")
    parser.add_argument("--commit", default="")
    # docs/adr/0025: required for first-time creation, so a caller can
    # never end up here by accident -- e.g. a typo'd --output path, or an
    # onboard-repo.sh run that silently failed to fetch a real existing
    # baseline, both used to look identical to "genuinely nothing exists
    # yet" and produced a full-acceptance snapshot either way.
    parser.add_argument("--create", action="store_true",
                         help="Allow creating a fresh baseline when none exists yet at <output>. "
                              "Required for first onboarding; omit for a refresh.")
    args = parser.parse_args()

    if not os.path.isdir(args.workspace):
        print(f"generate-baseline: {args.workspace} is not a directory", file=sys.stderr)
        return 2

    try:
        existing = load_existing(args.output)
    except BaselineUnreadable as error:
        print(
            f"generate-baseline: FATAL: {error} -- refusing to treat this as 'no baseline' and "
            "silently accept every current finding. Delete the file explicitly to start a fresh "
            "baseline, then rerun with --create.",
            file=sys.stderr,
        )
        return 2

    if existing is None and not args.create:
        print(
            f"generate-baseline: FATAL: no baseline exists at {args.output} -- pass --create to "
            "create one (first onboarding only; a refresh should always find an existing file)",
            file=sys.stderr,
        )
        return 2

    findings = scan_workspace(args.workspace)
    current = {(f["tool"], f["fingerprint"]): f for f in findings if f["tool"] != "gitleaks"}

    if existing is None:
        kept_keys = set(current.keys())
        print(f"generate-baseline: no existing baseline at {args.output} -- creating a fresh one ({len(kept_keys)} findings)")
    else:
        kept_keys = set(existing.keys()) & set(current.keys())
        dropped = set(existing.keys()) - kept_keys
        if dropped:
            print(f"generate-baseline: {len(dropped)} previously-baselined finding(s) no longer present -- baseline shrinks")
        print(f"generate-baseline: refreshed baseline at {args.output} ({len(kept_keys)} findings, was {len(existing)})")

    document = {
        "schema_version": 1,
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "repository": args.repo,
        "base_commit": args.commit,
        "findings": [
            {
                "tool": tool,
                "fingerprint": fingerprint,
                "rule_id": current[(tool, fingerprint)]["rule_id"],
                "severity": current[(tool, fingerprint)]["severity"],
            }
            for (tool, fingerprint) in sorted(kept_keys)
        ],
    }

    Path(args.output).parent.mkdir(parents=True, exist_ok=True)
    with open(args.output, "w", encoding="utf-8") as fh:
        json.dump(document, fh, indent=2, sort_keys=True)
        fh.write("\n")
    print(f"generate-baseline: wrote {args.output}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
