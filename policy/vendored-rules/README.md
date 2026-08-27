# Vendored Semgrep-syntax rules

Rules used by the `sast` step in `pipelines/fast.woodpecker.yml`, loaded from this local
directory instead of pulling `p/security-audit`/`p/secrets` live from the Semgrep Registry at
scan time. Matches `DESIGN.md`'s *Rule-set updates* guidance: "Rule changes go through the same
gate as code" — this directory is reviewed by ordinary PR like any other file in this repo.

## Why not the Semgrep Registry directly

Semgrep's own registry rules (`github.com/semgrep/semgrep-rules`, and the `p/...` shorthands
that resolve from it) are distributed under the **Semgrep Rules License v1.0**
(`semgrep.dev/legal/rules-license`), which grants use "for your own internal business purposes"
but explicitly states: *"This license does not allow you to distribute the rules, or to make
them available to others as a service."* Copying those files into this repository — which exists
specifically to serve every onboarded team's own scanning — falls squarely under that
restriction. `DESIGN.md`'s own component inventory already flagged this risk before this
directory existed: *"Opengrep — Continuity plan, given the Semgrep Registry's licensing
history."*

## Source and provenance

- **Origin:** `github.com/opengrep/opengrep-rules` — the official Opengrep project's own fork of
  `semgrep-rules`, kept in the same Semgrep-compatible rule YAML syntax.
- **Commit pinned:** `f1d2b562b414783763fd02a6ed2736eaed622efa`
- **Vendored:** 2026-08-27
- **License:** LGPL-2.1 **plus a "Commons Clause" restriction** — see `LICENSE` in this
  directory for the exact text. Commons Clause blocks *selling* the software, defined as
  providing it to third parties for a fee whose value derives substantially from the software's
  own functionality (i.e., it exists to stop reselling this as a competing hosted product). It
  does **not** clearly restrict using it internally, as this platform does, to scan the same
  organization's own onboarded repositories.

  **This has not been reviewed by counsel.** Treat the internal-use reading above as a reasonable
  interpretation, not a legal clearance. Get a real legal opinion before this repository (or any
  product built on it) is used more broadly than an internal pilot, offered externally, or made
  public.

## Scope

Not the full `opengrep-rules` repository (thousands of framework-specific rules across hundreds
of libraries) — a deliberately bounded slice:

- `secrets/` — from `generic/secrets/`, the closest equivalent to `p/secrets`: broad,
  language-agnostic secret-pattern detection (cloud provider keys, API tokens, etc.).
- One directory per language (`python/`, `javascript/`, `typescript/`, `go/`, `java/`, `php/`,
  `ruby/`, `csharp/`, `bash/`, `c/`, `dockerfile/`) — each sourced from that language's own
  `lang/security/` (or `dockerfile/security/`) directory: core-language security issues
  (injection, unsafe deserialization, weak crypto, etc.), **not** the hundreds of
  framework-specific directories (`python/django/security`, `python/flask/security`,
  `javascript/angular/security`, and so on) that `opengrep-rules`/`semgrep-rules` also carry.
  Framework-specific coverage can be added deliberately, per framework, once a real onboarded
  repo needs it — not pulled in wholesale up front.

594 rule files, ~4.8 MB.

## Updating

Re-clone `opengrep-rules` at a new commit, diff against this directory, and open a normal PR —
same review discipline as any other policy change. Record the new commit SHA above. Do not
silently overwrite without noting what changed; a rule change here is a policy change per
`DESIGN.md`.
