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
	fb, dev := FirstSteps(""), FirstSteps(shell.RoleDeveloper)
	if len(fb) == 0 || len(fb) != len(dev) {
		t.Fatalf("an unknown role falls back to the developer steps: %v vs %v", fb, dev)
	}
	for i := range fb {
		if fb[i] != dev[i] {
			t.Errorf("step %d differs: %q vs %q", i, fb[i], dev[i])
		}
	}
}

func TestFilterIgnoresMarkup(t *testing.T) {
	all := Topics()
	for _, q := range []string{"kbd", "class", "ssdlc"} {
		if got := Filter(all, q); len(got) != 0 {
			t.Errorf("%q matched markup in %d topics", q, len(got))
		}
	}
	if got := Filter(all, "second person"); len(got) == 0 {
		t.Error("plain answer text should still match")
	}
}

func TestPlainText(t *testing.T) {
	if got := plainText("a <b>bold</b> <kbd class=\"x\">k</kbd>"); got != "a bold k" {
		t.Errorf("plainText = %q", got)
	}
}

func TestUnbuiltFeatureTopicsAreHedged(t *testing.T) {
	for _, tp := range Topics() {
		text := plainText(tp.Answer)
		if strings.Contains(text, "about a minute") {
			t.Errorf("%s: unconfirmed timing", tp.ID)
		}
		if tp.ID == "grade" && (!strings.HasPrefix(text, "Shown on the Projects page.") || !strings.Contains(text, "not enforced yet")) {
			t.Errorf("%s: must point at Projects and stay not enforced: %q", tp.ID, text)
		}
		if tp.ID == "sla" {
			if !strings.HasPrefix(text, "Coming with") || !strings.Contains(text, "not enforced yet") {
				t.Errorf("%s: must be flagged as coming and not enforced: %q", tp.ID, text)
			}
		}
	}
}

func TestAppliesTo(t *testing.T) {
	setup := Topic{GoTo: "/onboarding"}
	exc := Topic{GoTo: "/exceptions"}
	for _, c := range []struct {
		name string
		tp   Topic
		s    shell.Shell
		want bool
	}{
		{"developer, setup", setup, shell.Shell{}, false},
		{"approver, setup", setup, shell.Shell{IsApprover: true}, false},
		{"admin outside approvers, setup", setup, shell.Shell{IsAdmin: true}, false},
		{"admin in approvers, setup", setup, shell.Shell{IsAdmin: true, IsApprover: true}, true},
		{"developer, exceptions", exc, shell.Shell{}, true},
		{"admin outside approvers, exceptions", exc, shell.Shell{IsAdmin: true}, true},
	} {
		if got := c.tp.AppliesTo(c.s); got != c.want {
			t.Errorf("%s: AppliesTo = %v, want %v", c.name, got, c.want)
		}
	}
}
