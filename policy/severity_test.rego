# policy/severity_test.rego — run via `conftest verify --policy policy`.
# Rego's own native test framework, not a hand-rolled harness -- these
# run with zero Docker, zero network, matching tests/unit/'s own
# no-privilege tier.

package main

test_critical_denies if {
	count(deny) > 0 with input as {
		"tool": "trivy", "rule_id": "CVE-1", "severity": "critical",
		"file": "requirements.txt", "line": null, "message": "x",
	}
}

test_high_denies if {
	count(deny) > 0 with input as {
		"tool": "semgrep", "rule_id": "r1", "severity": "high",
		"file": "app.py", "line": 5, "message": "x",
	}
}

test_medium_warns_not_denies if {
	count(deny) == 0 with input as {
		"tool": "semgrep", "rule_id": "r2", "severity": "medium",
		"file": "app.py", "line": 8, "message": "x",
	}
	count(warn) > 0 with input as {
		"tool": "semgrep", "rule_id": "r2", "severity": "medium",
		"file": "app.py", "line": 8, "message": "x",
	}
}

test_low_neither_denies_nor_warns if {
	count(deny) == 0 with input as {
		"tool": "semgrep", "rule_id": "r3", "severity": "low",
		"file": "app.py", "line": 1, "message": "x",
	}
	count(warn) == 0 with input as {
		"tool": "semgrep", "rule_id": "r3", "severity": "low",
		"file": "app.py", "line": 1, "message": "x",
	}
}

test_null_line_does_not_crash_the_message_formatter if {
	# Trivy findings carry no line number (docs/adr/0018) -- confirms
	# format_line's null-handling branch actually gets exercised, not
	# just written and assumed to work.
	count(deny) > 0 with input as {
		"tool": "trivy", "rule_id": "CVE-2", "severity": "critical",
		"file": "requirements.txt", "line": null, "message": "x",
	}
}
