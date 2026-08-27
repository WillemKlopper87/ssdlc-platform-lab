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

No platform-specific IDE requirement exists today. Point your editor's Gitea/git integration at the
onboarded repo as you would any other Gitea-hosted project.

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
