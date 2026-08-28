# SADR-0023: The trusted-runner bundle's secret detection was silently inert

**Status:** Accepted, implemented.

**Date:** 2026-08-28
**Milestone:** 3 -- trusted pilot foundation
**Security & Privacy Impact:** Critical

## Question

`docs/TODO.md` tracked an open, deliberately-unresolved finding from SADR-0022's
live testing: a real (non-allowlisted) AWS-shaped access key produced **zero**
Gitleaks findings when scanned in a workspace with no `.git` directory, while
the same key in a git-initialized directory was caught correctly. Was this a
throwaway-test artifact, or a real gap in `gate-bundle/run-pilot-bundle.py`'s
actual `gitleaks detect` invocation?

## What was confirmed, directly

`gitleaks detect --help` states the mechanism plainly: `--no-git` "treat[s] git
repo as a regular directory and scan those files" -- meaning **without** that
flag, gitleaks does not scan working-tree files at all. It scans **git commit
history** via `git log`, which requires a real `.git` directory to exist.

Live-tested directly (not inferred) against three cases, with a realistic,
non-allowlisted key ("AKIAIOSFODNN7EXAMPLE" turned out to be a red herring
early in this investigation -- it is AWS's own published documentation
example key, and gitleaks' default config allowlists it specifically, which
is why an earlier check mistakenly looked inconclusive):

| Workspace | Flag | Result |
|---|---|---|
| No `.git` | (none) | `fatal: not a git repository` -> `0 commits scanned` -> **0 findings**, exit success |
| Real `.git`, one commit | (none) | `1 commits scanned` -> **1 finding**, correctly caught |
| No `.git` | `--no-git` | Scans working-tree files directly -> **1 finding**, correctly caught |

`scripts/trusted-gate-runner.py`'s `checkout_head()` downloads Gitea's
`/repos/{owner}/{repo}/archive/{head_sha}.tar.gz` and extracts it. Confirmed
directly this produces **no `.git` directory** -- a Gitea archive tarball is
the same kind of plain source-tree snapshot `git archive` itself produces,
deliberately excluding VCS metadata. `gate-bundle/run-pilot-bundle.py`'s
`gitleaks detect --source <workspace> ...` invocation did not pass `--no-git`.

**The combination means the trusted-runner bundle's secret detection was
completely, silently inert against its own real input shape.** Every
attestation the durable trust boundary (SADR-0017) was designed around would
have reported `"secrets": "success"` regardless of what secrets were actually
present in the scanned code -- not degraded detection, zero detection, with
no error surfaced anywhere in the pipeline.

## Why the fast gate is unaffected

`pipelines/fast.woodpecker.yml`'s own `secrets` step runs the identical
`gitleaks detect` command, also without `--no-git`, against `.` inside a
Woodpecker pipeline step. That workspace **is** a real `git clone`
(Woodpecker's own `clone` step), so scanning commit history is not just
harmless there -- it's strictly more thorough than a working-tree-only scan,
catching a secret introduced in an earlier commit and later removed. Adding
`--no-git` to the fast gate's own invocation would be a regression, not a
fix; this SADR deliberately does not touch it.

## Decision

Add `--no-git` to `gate-bundle/run-pilot-bundle.py`'s gitleaks invocation
only. One flag, scoped to the one gitleaks call whose workspace genuinely has
no git history to scan.

## Verification

- Live, end to end, through the real built image: rebuilt
  `gate-bundle/Dockerfile`, ran the fixed bundle against a workspace shaped
  exactly like `checkout_head()`'s real output (no `.git`, a single file)
  with a real non-allowlisted secret. Confirmed the gitleaks report now
  contains the finding, `policy-eval` correctly evaluates it as
  `CRITICAL [gitleaks/aws-access-token]`, and the bundle's final
  `result.json` correctly reports `"decision": "fail"`.
- Unit: `tests/unit/test_gate_bundle.py` now asserts `--no-git` is present in
  the gitleaks command specifically, so this cannot silently regress.
- Full `tests/unit/` tier re-run clean.

## Consequences

- Closes the specific finding tracked in `docs/TODO.md` since SADR-0022 --
  confirmed real, not a test artifact, and fixed before the trusted runner
  is ever deployed.
- No fast-gate change needed or made; that path was never affected.
- A reminder for whoever next adds a scanner to the bundle: **the bundle's
  workspace is not a git checkout.** Any tool whose default behavior assumes
  git context (history-based scanning, `git blame` attribution, etc.) needs
  the same scrutiny this SADR gave gitleaks specifically -- check the tool's
  actual behavior against a no-`.git` directory before trusting it, the same
  way this finding was confirmed rather than assumed.
