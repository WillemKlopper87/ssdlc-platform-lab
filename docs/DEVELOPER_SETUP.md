# Developer setup

For a developer working in a repository already onboarded to the platform (see
[`ONBOARDING.md`](ONBOARDING.md) for how a repo gets onboarded — that part is a platform-admin action,
not something you do here).

## The short version

Clone, code, push. That is the entire required interaction — the pipeline template, policy, and
approval rules were committed into the repo during onboarding; you do not configure or invoke any
scanner yourself. If a push or PR fails the gate, see [SECURITY.md](../SECURITY.md) for what a
failure means and how to respond.

## Optional local checks (`pre-commit`)

Not a security boundary — `git commit --no-verify` defeats it, and it only runs if installed at all.
Framework §3.2's own errata note (`DESIGN.md`, Appendix) is explicit that pre-commit is a
developer-experience feature, not a control, and should never be presented as one in ISO evidence.
The server-side gate ([`GATE_CONTRACT.md`](GATE_CONTRACT.md)) remains authoritative regardless of
whether you run this.

Install the pinned `pre-commit` version (`docs/PINNED_VERSIONS.md`), then:

```sh
pre-commit install
pre-commit run --all-files
```

`.pre-commit-config.yaml` currently runs, deliberately kept small per its own header comment ("this
only catches simple mistakes before a PR is opened" — the central gate still makes the merge
decision):

- `check-yaml`, `end-of-file-fixer`, `trailing-whitespace` (`pre-commit-hooks`)
- `gitleaks`, pinned to the same version (`v8.30.1`) the server-side scan uses — catching a secret
  before it's committed at all is strictly better than catching it in CI, since by the time Gitleaks
  fires in CI the credential may already be in Gitea's object store, webhook payloads, and build logs
  (DESIGN.md's D7).

Adding a language-specific linter (ruff, eslint, gosec) here is reasonable and matches DESIGN.md's
architecture diagram's "pre-commit (Gitleaks, Semgrep, ruff/eslint/gosec)" line, but keep it fast —
this runs on every commit, not every PR.

## IDE

No platform-specific IDE requirement exists — this is a standard Gitea remote, so any editor's
built-in Git support works with no configuration. VS Code specifically:

**Works with no extension at all.** Clone, branch, commit, push and open a PR (via the URL Gitea
prints after a push) all go through VS Code's built-in Source Control view against this repo's
Gitea remote exactly as they would against any other git host.

**Optional: the official "Gitea for VSCode" extension** (`gitea.gitea-for-vscode`, VS Code ≥ 1.105,
also works in Cursor/VSCodium/Windsurf) adds a Pull Requests view — inline diffs, comments, and
approve/request-changes from the editor — plus a Workflow Runs view for **Gitea Actions**
specifically. This platform's CI is Woodpecker, not Gitea Actions, so that Workflow Runs view stays
empty here; it is not where you watch a pipeline. What the extension's PR view *does* surface
correctly is the PR's Gitea commit status — the same `ssdlc/security-gate/<event>/<workflow>` pass/
fail check anyone sees on gitea.com/<owner>/<repo>/pulls/<n>, since that's server-side Gitea state,
not an Actions-specific feature. Configure it against this instance's URL via the extension's
`gitea-for-vscode.baseUrl` setting (default is `https://gitea.com`) and a personal access token or
OAuth sign-in.

For the pipeline's own logs — the Semgrep/Trivy/Gitleaks step output the commit status summarizes —
open the run in Woodpecker's own web UI; nothing in-editor surfaces that today.

## AI coding assistants (Continue + Ollama)

**Not deployed in this environment.** DESIGN.md names `Continue` + `Ollama` as the intended
internal-first LLM code-review pairing (framework §4's mandate — see
[`AI_ASSISTANT_POLICY.md`](AI_ASSISTANT_POLICY.md) for the policy itself), but no Ollama host exists
in `compose/minimal` and this remains an open question in `DESIGN.md`'s *Open questions* (model choice,
host sizing — a useful 7B-class quantised model wants 6-8 GB by itself, which collides with other
services on a shared box).

Until that exists: the framework's internal-first review requirement is **not currently enforced by
tooling**. Read [`AI_ASSISTANT_POLICY.md`](AI_ASSISTANT_POLICY.md) for what the policy asks of you in
the meantime if you use an external AI coding assistant against this codebase.

## What actually gates your PR

The full list is [`GATE_CONTRACT.md`](GATE_CONTRACT.md) and [`POLICY.md`](POLICY.md); in short, on
every push and PR: Gitleaks (secrets — always Critical, never exempted), Semgrep (SAST), Trivy
(dependencies), an approval-validity re-check independent of Gitea's own stored approval state, and
inline PR comments from reviewdog (Semgrep findings only, non-blocking, for faster feedback than
waiting on the full gate result).

## If something looks wrong with the platform itself

Do not weaken the pipeline or work around a gate you believe is misconfigured. See [SECURITY.md](../SECURITY.md)'s
"Reporting a platform weakness" section.
