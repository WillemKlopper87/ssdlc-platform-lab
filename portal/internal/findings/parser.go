package findings

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Category is which of evaluate-findings.py's four buckets a finding
// landed in -- blocking (FAIL, a new Critical/High), warning (WARN, a new
// Medium, doesn't block), baselined (pre-existing debt, matched
// .ssdlc/baseline.json), or excepted (covered by an approved, unexpired
// two-party exception record).
type Category string

const (
	CategoryBlocking  Category = "blocking"
	CategoryWarning   Category = "warning"
	CategoryBaselined Category = "baselined"
	CategoryExcepted  Category = "excepted"
)

type Finding struct {
	Category    Category
	Severity    string
	Tool        string
	RuleID      string
	Location    string
	Description string
}

type Summary struct {
	Critical, High, Medium, Low int
}

// Matches lines like:
//   FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- description here
//   WARN  MEDIUM [semgrep/weak-hash] app.py:12 -- description here
var findingLine = regexp.MustCompile(`^\s*(FAIL|PASS|WARN)\s+(CRITICAL|HIGH|MEDIUM|LOW)\s+\[([^\]]+)\]\s+(\S+)\s+--\s+(.+)$`)

// Matches lines like:
//   BASELINE  [semgrep/subprocess-shell-true] service.py:118 -- description
//   EXCEPTION  [tool/rule] file:line -- description
var suppressedLine = regexp.MustCompile(`^\s*(BASELINE|EXCEPTION)\s+\[([^\]]+)\]\s+(\S+)\s+--\s+(.+)$`)

// Matches: policy-eval: 3 finding(s) normalized -- critical=1 high=1 medium=0 low=1
var summaryLine = regexp.MustCompile(`^policy-eval:\s+\d+\s+finding\(s\)\s+normalized\s+--\s+critical=(\d+)\s+high=(\d+)\s+medium=(\d+)\s+low=(\d+)`)

// toolOf splits a "tool/rule-id" RuleID into just the tool name; a RuleID
// with no "/" (shouldn't happen in practice) is returned as-is.
func toolOf(ruleID string) string {
	tool, _, found := strings.Cut(ruleID, "/")
	if !found {
		return ruleID
	}
	return tool
}

// Parse extracts structured findings and the tally from the
// policy-eval-findings step's own stdout — the exact text the gate
// decision was made from, not a re-derivation from raw scanner JSON.
func Parse(log string) ([]Finding, Summary, error) {
	var findings []Finding
	var summary Summary

	for _, line := range strings.Split(log, "\n") {
		if m := findingLine.FindStringSubmatch(line); m != nil {
			category := CategoryBlocking
			if m[1] == "WARN" {
				category = CategoryWarning
			}
			findings = append(findings, Finding{
				Category: category, Severity: m[2], Tool: toolOf(m[3]), RuleID: m[3],
				Location: m[4], Description: m[5],
			})
			continue
		}
		if m := suppressedLine.FindStringSubmatch(line); m != nil {
			category := CategoryBaselined
			if m[1] == "EXCEPTION" {
				category = CategoryExcepted
			}
			findings = append(findings, Finding{
				Category: category, Tool: toolOf(m[2]), RuleID: m[2], Location: m[3], Description: m[4],
			})
			continue
		}
		if m := summaryLine.FindStringSubmatch(line); m != nil {
			var err error
			if summary.Critical, err = strconv.Atoi(m[1]); err != nil {
				return nil, Summary{}, fmt.Errorf("findings: parse critical count: %w", err)
			}
			if summary.High, err = strconv.Atoi(m[2]); err != nil {
				return nil, Summary{}, fmt.Errorf("findings: parse high count: %w", err)
			}
			if summary.Medium, err = strconv.Atoi(m[3]); err != nil {
				return nil, Summary{}, fmt.Errorf("findings: parse medium count: %w", err)
			}
			if summary.Low, err = strconv.Atoi(m[4]); err != nil {
				return nil, Summary{}, fmt.Errorf("findings: parse low count: %w", err)
			}
		}
	}
	return findings, summary, nil
}
