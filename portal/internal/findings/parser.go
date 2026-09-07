package findings

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Finding struct {
	Severity    string
	RuleID      string
	Location    string
	Description string
}

type Summary struct {
	Critical, High, Medium, Low int
}

// Matches lines like:
//   FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- description here
var findingLine = regexp.MustCompile(`^\s*(?:FAIL|PASS)\s+(CRITICAL|HIGH|MEDIUM|LOW)\s+\[([^\]]+)\]\s+(\S+)\s+--\s+(.+)$`)

// Matches: policy-eval: 3 finding(s) normalized -- critical=1 high=1 medium=0 low=1
var summaryLine = regexp.MustCompile(`^policy-eval:\s+\d+\s+finding\(s\)\s+normalized\s+--\s+critical=(\d+)\s+high=(\d+)\s+medium=(\d+)\s+low=(\d+)`)

// Parse extracts structured findings and the tally from the
// policy-eval-findings step's own stdout — the exact text the gate
// decision was made from, not a re-derivation from raw scanner JSON.
func Parse(log string) ([]Finding, Summary, error) {
	var findings []Finding
	var summary Summary

	for _, line := range strings.Split(log, "\n") {
		if m := findingLine.FindStringSubmatch(line); m != nil {
			findings = append(findings, Finding{
				Severity: m[1], RuleID: m[2], Location: m[3], Description: m[4],
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
