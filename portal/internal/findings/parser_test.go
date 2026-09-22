package findings

import "testing"

const sampleLog = `+ python3 policy-eval/evaluate-findings.py
  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- Identified a pattern that may indicate AWS credentials, risking unauthorized cloud resource access and data breaches on AWS platforms.
  FAIL  HIGH [semgrep/sql-injection] app.py:42 -- Tainted SQL string built from request input.
  PASS  LOW [trivy/CVE-2026-1234] requirements.txt -- Low-severity dependency vulnerability, does not block.
policy-eval: 3 finding(s) normalized -- critical=1 high=1 medium=0 low=1
`

func TestParse_ExtractsFindingsAndSummary(t *testing.T) {
	findings, summary, err := Parse(sampleLog)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("got %d findings, want 3: %+v", len(findings), findings)
	}
	if findings[0].Category != CategoryBlocking || findings[0].Severity != "CRITICAL" ||
		findings[0].Tool != "gitleaks" || findings[0].RuleID != "gitleaks/aws-access-token" ||
		findings[0].Location != "config.py:5" {
		t.Errorf("finding[0] = %+v", findings[0])
	}
	if findings[1].Category != CategoryBlocking || findings[1].Severity != "HIGH" ||
		findings[1].Tool != "semgrep" || findings[1].RuleID != "semgrep/sql-injection" {
		t.Errorf("finding[1] = %+v", findings[1])
	}
	if findings[2].Category != CategoryBlocking || findings[2].Severity != "LOW" || findings[2].Tool != "trivy" {
		t.Errorf("finding[2] = %+v", findings[2])
	}
	if summary != (Summary{Critical: 1, High: 1, Medium: 0, Low: 1}) {
		t.Errorf("summary = %+v, want {1,1,0,1}", summary)
	}
}

const suppressedLog = `+ python3 policy-eval/evaluate-findings.py
policy-eval: 3 finding(s) normalized -- critical=0 high=0 medium=1 low=0
policy-eval: 1 finding(s) match the baseline (.ssdlc/baseline.json) -- pre-existing debt, not evaluated for blocking
  BASELINE  [semgrep/subprocess-shell-true] service.py:118 -- shell=True propagates current shell settings
policy-eval: 1 finding(s) covered by an approved exception -- not evaluated for blocking
  EXCEPTION  [trivy/CVE-2024-6221] requirements.txt:? -- medium, logged not blocking
  WARN  MEDIUM [semgrep/weak-hash] app.py:9 -- md5 is not a cryptographically secure hash
policy-eval: PASS -- no new Critical/High findings
`

func TestParse_CapturesWarnedBaselinedAndExceptedFindings(t *testing.T) {
	findings, summary, err := Parse(suppressedLog)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("got %d findings, want 3: %+v", len(findings), findings)
	}

	baseline := findings[0]
	if baseline.Category != CategoryBaselined || baseline.Tool != "semgrep" ||
		baseline.RuleID != "semgrep/subprocess-shell-true" || baseline.Location != "service.py:118" {
		t.Errorf("baseline finding = %+v", baseline)
	}

	excepted := findings[1]
	if excepted.Category != CategoryExcepted || excepted.Tool != "trivy" ||
		excepted.RuleID != "trivy/CVE-2024-6221" || excepted.Location != "requirements.txt:?" {
		t.Errorf("excepted finding = %+v", excepted)
	}

	warned := findings[2]
	if warned.Category != CategoryWarning || warned.Severity != "MEDIUM" || warned.Tool != "semgrep" ||
		warned.RuleID != "semgrep/weak-hash" {
		t.Errorf("warned finding = %+v", warned)
	}

	if summary != (Summary{Medium: 1}) {
		t.Errorf("summary = %+v, want {Medium: 1}", summary)
	}
}

func TestToolOf(t *testing.T) {
	if got := toolOf("gitleaks/aws-access-token"); got != "gitleaks" {
		t.Errorf("toolOf = %q, want gitleaks", got)
	}
	if got := toolOf("no-slash-rule"); got != "no-slash-rule" {
		t.Errorf("toolOf with no slash = %q, want unchanged", got)
	}
}

func TestParse_NoFindingsLine(t *testing.T) {
	findings, summary, err := Parse("policy-eval: 0 finding(s) normalized -- critical=0 high=0 medium=0 low=0\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("got %d findings, want 0", len(findings))
	}
	if summary != (Summary{}) {
		t.Errorf("summary = %+v, want zero value", summary)
	}
}
