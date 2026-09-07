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
	if findings[0].Severity != "CRITICAL" || findings[0].RuleID != "gitleaks/aws-access-token" ||
		findings[0].Location != "config.py:5" {
		t.Errorf("finding[0] = %+v", findings[0])
	}
	if findings[1].Severity != "HIGH" || findings[1].RuleID != "semgrep/sql-injection" {
		t.Errorf("finding[1] = %+v", findings[1])
	}
	if summary != (Summary{Critical: 1, High: 1, Medium: 0, Low: 1}) {
		t.Errorf("summary = %+v, want {1,1,0,1}", summary)
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
