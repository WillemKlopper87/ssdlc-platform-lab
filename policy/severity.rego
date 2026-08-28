# policy/severity.rego
#
# The severity-threshold gate DESIGN.md's Policy model names verbatim
# against framework §2.4's SLA table:
#
#   Critical -> block immediately
#   High     -> fail build
#   Medium   -> warn, track (does not block)
#   Low      -> log only (does not block)
#
# Input is ONE normalized finding record (docs/adr/0018's schema,
# produced by normalise/*_adapter.py) -- NOT an array of findings. Found
# live, via --trace, before trusting it: Conftest splits a top-level
# JSON array into SEPARATE documents automatically and evaluates this
# policy once per element, with `input` bound to that single element
# each time -- not once against the whole array with `input[_]`
# iterating findings, which was the first (wrong) version of this file
# and silently produced 0 failures against combined.json's 4 real
# findings (2 critical, 1 high, 1 medium) because `finding.severity` was
# being checked against `input[_]`'s individual STRING FIELD VALUES, not
# the finding object itself. This file has no idea what Gitleaks,
# Semgrep, or Trivy are; that's the normalize layer's job, done before
# this ever runs. Rego + Conftest, not a hand-rolled interpreter, per
# DESIGN.md's own component inventory ("writing a YAML mini-language and
# its evaluator is a classic avoidable mistake").
#
# Deliberately does NOT implement baseline/differential gating here, and
# still doesn't -- but not because that gap is unclosed anymore.
# docs/adr/0024/0025: policy-eval/evaluate-findings.py now splits findings
# into new vs. baselined (matched against .ssdlc/baseline.json) BEFORE
# calling Conftest at all -- only new_findings ever reaches this policy;
# a pre-existing finding never becomes `input` here in the first place.
# This file staying baseline-unaware is therefore the correct division of
# responsibility (severity policy vs. differential filtering are separate
# concerns), not an open gap. What's still genuinely absent, and still
# depends on the exceptions-repo infrastructure (Milestone 4, not built:
# DESIGN.md's D5), is the two-party EXCEPTION workflow for a finding that
# genuinely IS new -- there is still no accepted-risk path here other than
# fixing it or a reviewed change to this file itself. Tracked honestly in
# docs/TODO.md, not silently assumed solved.

package main

deny contains msg if {
	input.severity == "critical"
	msg := sprintf(
		"CRITICAL [%s/%s] %s:%s -- %s",
		[input.tool, input.rule_id, input.file, format_line(input.line), input.message],
	)
}

deny contains msg if {
	input.severity == "high"
	msg := sprintf(
		"HIGH [%s/%s] %s:%s -- %s",
		[input.tool, input.rule_id, input.file, format_line(input.line), input.message],
	)
}

warn contains msg if {
	input.severity == "medium"
	msg := sprintf(
		"MEDIUM [%s/%s] %s:%s -- %s",
		[input.tool, input.rule_id, input.file, format_line(input.line), input.message],
	)
}

# Low severity findings are deliberately not surfaced as either deny or
# warn -- framework §2.4's "log only" tier. They still exist in the
# normalized finding list for reporting; this policy just has nothing to
# say about them.

format_line(line) := "?" if {
	line == null
}

format_line(line) := sprintf("%v", [line]) if {
	line != null
}
