# Writing and testing policy

How the severity gate is written, tested, and extended. The threshold table itself is framework
§2.4, reproduced in [`DESIGN.md`](DESIGN.md#policy-model); this document is about the mechanics of
`policy/severity.rego` and the normalisation layer that feeds it, live-tested end to end in
[SADR-0018](adr/0018-unified-findings-gate-live-and-self-contamination.md).

## The pipeline

```
scanner report (Gitleaks / Semgrep / Trivy JSON)
      │
      ▼
normalise/<tool>_adapter.py   -- one per tool, independently testable
      │  produces: {tool, rule_id, severity, file, line, message,
      │             fingerprint, raw_severity}
      ▼
policy-eval/evaluate-findings.py
      │  collects all findings, hands the combined list to Conftest
      ▼
policy/severity.rego          -- ONE reviewable policy, evaluated by Conftest
      │
      ▼
exit 0 (no Critical/High) / exit 1 (blocks) / exit 2 (fail closed -- could not evaluate)
```

## The normalisation contract

One schema, regardless of source tool — this is DESIGN.md's whole point in avoiding "a tool upgrade
that changes its severity mapping silently changes what the gate blocks":

| Field | Meaning |
|---|---|
| `tool` | `"gitleaks"` \| `"semgrep"` \| `"trivy"` |
| `rule_id` | The tool's own rule/CVE identifier |
| `severity` | One of `"critical"`, `"high"`, `"medium"`, `"low"` — always this vocabulary, never the tool's own |
| `file` | Path relative to the scanned workspace |
| `line` | Line number, or `null` when the tool doesn't report one (Trivy's dependency findings never do) |
| `message` | Human-readable description |
| `fingerprint` | Stable identity for the finding — used for future baseline/dedup work, not yet consumed by anything (see [`EXCEPTIONS.md`](EXCEPTIONS.md)) |
| `raw_severity` | The tool's own severity string/score, kept for audit — `null` where the tool has none (Gitleaks) |

**Each adapter is independently tested** against recorded fixtures in `normalise/testdata/` — not
against a live scanner run, so a fixture never silently drifts with a tool upgrade. See
`tests/unit/test_normalise.py`.

**Gitleaks is a special case, deliberately, not an oversight.** A Gitleaks finding carries no
severity field at all — there is nothing to map, because framework policy makes this correct rather
than a gap: a verified secret is *always* Critical, always blocks, and is never baselined or
exempted (framework §2.4's "Verified secret" row). `gitleaks_adapter.py` hardcodes `"severity":
"critical"` for exactly this reason, confirmed against a real Gitleaks v8.30.1 run rather than
assumed from documentation.

## The Rego policy

`policy/severity.rego` implements the threshold table verbatim:

| Severity | Action |
|---|---|
| `critical` | `deny` — blocks the merge |
| `high` | `deny` — blocks the merge |
| `medium` | `warn` — visible, does not block |
| `low` | neither `deny` nor `warn` — logged only, per framework §2.4's "log only" tier |

**The input shape is one finding, not an array — this is the sharpest edge in the whole pipeline, and
it bit the first draft of this file silently.** Conftest splits a top-level JSON array into *separate
documents* and evaluates the policy once per element, with `input` bound to that single finding each
time. The first version of `severity.rego` assumed `input` was the whole array and iterated it with
`input[_]`; it silently produced **zero failures** against a fixture with two Critical and one High
finding, because it was comparing `input[_]`'s individual string field values against `"critical"`,
never the finding object's `.severity` field. Found only by running Conftest with `--trace` and
reading what it actually bound `input` to, not by reasoning about it from the Rego source. Any new
rule added to this policy must write `input.severity`, `input.tool`, etc. — never assume an array.

`format_line` exists solely to keep the message formatter from crashing on Trivy's `null` line
numbers — exercised directly by `severity_test.rego`'s
`test_null_line_does_not_crash_the_message_formatter`.

## Adding a new severity rule

1. Add the `deny`/`warn` rule to `policy/severity.rego`, reading fields off `input` directly (never
   `input[_]`).
2. Add a case to `policy/severity_test.rego` covering it — both the positive case (the rule fires)
   and, where relevant, a negative case (a lower severity does not).
3. Run it standalone, no Docker, no network:
   ```sh
   conftest verify --policy policy
   ```
   This is wired into `tests/unit/run-unit-tests.sh`'s `conftest verify` step and skips gracefully
   (with a warning) if `conftest` isn't on `PATH` — see `docs/PINNED_VERSIONS.md` for the pinned
   version to install.
4. Run the Python-level test too: `python3 tests/unit/test_evaluate_findings.py`. This exercises the
   full `evaluate-findings.py` → Conftest → parsed-output path, not just the Rego in isolation —
   which is what caught the self-contamination bug in SADR-0018 (a policy problem one layer up from
   Rego: the scanners were flagging the platform's *own* report files, not a policy logic error).

## Adding a new scanner

1. Write `normalise/<tool>_adapter.py` with a `normalize(raw_report)` function returning the schema
   above. Look at `gitleaks_adapter.py` for the pattern — handle "no report file" and "empty report"
   as the non-error cases they are, not exceptions.
2. Add fixtures to `normalise/testdata/` and a corresponding case to `tests/unit/test_normalise.py`.
3. Wire it into `policy-eval/evaluate-findings.py`'s `collect_findings` and add a `--<tool>` CLI flag.
4. Add the scanner step to `pipelines/fast.woodpecker.yml`, writing its report into the shared
   workspace under a name that won't collide with another step's output — see the note below.
5. If the scanner has its own built-in secret/pattern detector (Semgrep's `p/secrets`, Trivy's
   filesystem scan), **exclude every other step's report filename from its scan target explicitly**.
   This is not hypothetical: SADR-0018 found Semgrep's `p/secrets` ruleset flagging
   `gitleaks-report.json` — the *prior* step's own output, sitting in the same shared workspace — as
   a leaked Facebook OAuth token, which would have permanently blocked every PR regardless of actual
   code. The fix was `--exclude=gitleaks-report.json --exclude=semgrep-report.sarif` on the Semgrep
   invocations; the same class of self-match applies to any new step that runs after others in a
   shared workspace and has its own default secret/pattern detection.
6. Vendor any ruleset the scanner pulls from a registry, per DESIGN.md's *Rule-set updates* — pulling
   live means an upstream rule change can block every PR in the estate with no review and no
   rollback. This is not yet done for Semgrep's `p/security-audit`/`p/secrets` configs (see
   `docs/TODO.md`).

## What this layer deliberately does not do

**Baseline/differential gating exists (SADR-0024/0025), separate from exceptions.** `evaluate-findings.py
--baseline` splits findings into new (still gates exactly as below) and pre-existing debt (reported,
never blocking) by consulting `.ssdlc/baseline.json`, generated at onboarding by
`scripts/generate-baseline.py`. Secrets are never baselined regardless of what that file claims — a
Gitleaks finding always evaluates as new. What's still absent is the two-party *exception* path (a
human accepting a genuinely NEW finding, time-boxed, with expiry) — that depends on the exceptions-repo
infrastructure named in [`EXCEPTIONS.md`](EXCEPTIONS.md), and is unrelated to baseline gating.

**No exception/suppression consumption.** `fingerprint` is computed and carried through the pipeline,
but nothing reads it yet to honour a risk acceptance or a `.ssdlc/suppressions.yaml` entry (DESIGN.md's
"two distinct mechanisms" — false-positive suppression vs. risk acceptance). Both are Milestone 4 work.

## Fail-closed behaviour, by design

- `evaluate-findings.py` exits 2 (not 0, not 1) if a scanner report is unreadable or malformed, or if
  Conftest itself fails to start or produces unparseable output. Tested directly:
  `tests/unit/test_evaluate_findings.py`'s "a Conftest crash fails closed even if it prints JSON" and
  "malformed scanner JSON fails closed" cases.
- Conftest's own exit code 1 (findings present) is treated as a *successful evaluation* that failed
  the policy — distinct from a genuine crash. Only an unparseable or unexpected exit code is fatal.
- An absent report file (a scanner that found nothing, or wasn't run this pipeline) is not an error —
  `load_report` returns `None` and that tool simply contributes zero findings.
