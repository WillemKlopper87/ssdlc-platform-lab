// Package grade turns open-issue counts into the project score and letter,
// and holds the fix-within targets. Weights and thresholds are proposals
// from the portal redesign spec and are deliberately kept in one place.
package grade

import "strings"

type Counts struct {
	Critical, High, Medium, Low int
}

// Score is 100 minus a weight per open issue. It is not clamped so callers
// can still order two failing projects.
func Score(c Counts) int {
	return 100 - 35*c.Critical - 18*c.High - 6*c.Medium - 2*c.Low
}

// Letter maps a score to A-F.
func Letter(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	}
	return "F"
}

// SLADays is the target number of days to fix an issue of the severity, or 0
// for an unknown severity (no clock).
func SLADays(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 2
	case "high":
		return 7
	case "medium":
		return 30
	case "low":
		return 90
	}
	return 0
}
