// portal/internal/webutil/statusbadge.go
package webutil

// Badge is the one place a gate/pipeline/finding status becomes a color and
// a label. Every template renders status through this function so the
// mapping can never drift between screens.
type Badge struct {
	Label    string
	CSSClass string
}

func StatusBadge(status string) Badge {
	switch status {
	case "success":
		return Badge{"Passed", "ssdlc-badge-success"}
	case "failure":
		return Badge{"Blocked", "ssdlc-badge-critical"}
	case "pending", "running":
		return Badge{"Running", "ssdlc-badge-warning"}
	default:
		return Badge{status, "ssdlc-badge-neutral"}
	}
}
