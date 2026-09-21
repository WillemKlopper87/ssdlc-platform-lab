package help

import (
	"strings"
	"testing"

	"ssdlc-portal/internal/shell"
)

func TestTopicsAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, tp := range Topics() {
		if tp.ID == "" || tp.Question == "" || tp.Answer == "" || len(tp.Roles) == 0 {
			t.Errorf("incomplete topic %+v", tp)
		}
		if seen[tp.ID] {
			t.Errorf("duplicate id %q", tp.ID)
		}
		seen[tp.ID] = true
		if tp.GoTo != "" && !strings.HasPrefix(tp.GoTo, "/") {
			t.Errorf("topic %q GoTo must be a same-site path, got %q", tp.ID, tp.GoTo)
		}
	}
	for _, id := range []string{"blocked", "exception", "approve", "grade", "sla", "baseline", "signin", "shortcuts"} {
		if !seen[id] {
			t.Errorf("missing topic %q", id)
		}
	}
}

func TestFilter(t *testing.T) {
	all := Topics()
	if got := Filter(all, ""); len(got) != len(all) {
		t.Errorf("empty query should keep everything: %d vs %d", len(got), len(all))
	}
	got := Filter(all, "  BLOCKED ")
	if len(got) == 0 || got[0].ID != "blocked" {
		t.Errorf("search should be trimmed and case-insensitive; got %+v", got)
	}
	if got := Filter(all, "zzzz-no-such-word"); len(got) != 0 {
		t.Errorf("expected no results, got %d", len(got))
	}
	// answers are searched too, not just questions
	if got := Filter(all, "second person"); len(got) == 0 {
		t.Error("answer text should be searchable")
	}
}

func TestFirstSteps(t *testing.T) {
	for _, r := range []shell.Role{shell.RoleDeveloper, shell.RoleApprover, shell.RoleAdmin} {
		if steps := FirstSteps(r); len(steps) < 3 {
			t.Errorf("%s: want at least 3 first steps, got %v", r, steps)
		}
	}
	if FirstSteps("") == nil || len(FirstSteps("")) == 0 {
		t.Error("an unknown role falls back to the developer steps")
	}
}
