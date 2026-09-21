package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/shell"
)

func renderDashboardAs(t *testing.T, s shell.Shell) string {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/user":
			w.Write([]byte(`{"login":"` + s.Operator + `"}`))
		case "/api/v1/repos/search":
			w.Write([]byte(`{"data":[]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer fake.Close()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req = req.WithContext(shell.WithContext(req.Context(), s))
	rec := httptest.NewRecorder()
	Dashboard(fake.URL)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestLayout_DeveloperSidebar(t *testing.T) {
	body := renderDashboardAs(t, shell.Shell{
		Operator: "dev2", Role: shell.RoleDeveloper,
		GiteaURL: "http://192.168.1.28:3500", WoodpeckerURL: "http://192.168.1.28:8000",
	})
	for _, want := range []string{
		`href="/dashboard"`, "Overview", `href="/exceptions"`, `href="/help"`,
		`href="http://192.168.1.28:3500"`, `href="http://192.168.1.28:8000"`,
		"Quick launch", "dev2", "Developer", "Projects", "Issues",
		`/static/shell.js`, `/static/fonts.css`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{`href="/onboarding"`, "fonts.googleapis.com", "unpkg.com", "ssdlc-nav-badge", "htmx"} {
		if strings.Contains(body, bad) {
			t.Errorf("developer view must not contain %q", bad)
		}
	}
	if !strings.Contains(body, `href="/projects"`) {
		t.Error("Projects must be a link")
	}
	if strings.Contains(body, `class="disabled" title="Arrives in the next step of the portal redesign">Projects`) {
		t.Error("Projects must no longer be a disabled placeholder")
	}
	// Screens that do not exist yet are visible but not links.
	if strings.Contains(body, `href="/issues"`) {
		t.Error("Issues must stay a disabled placeholder, not a link")
	}
	if !strings.Contains(body, `class="disabled" title="Arrives in the next step of the portal redesign">Issues`) {
		t.Error("Issues must still be disabled")
	}
}

func TestLayout_ProjectsActiveOnProjectsPage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req = req.WithContext(shell.WithContext(req.Context(), shell.Shell{Operator: "dev2"}))
	rec := httptest.NewRecorder()
	Projects(nil)(rec, req)
	if !strings.Contains(rec.Body.String(), `href="/projects" data-pal-item data-pal-sub="page" class="active"`) {
		t.Errorf("Projects link must be active; body:\n%s", rec.Body.String())
	}
}

func TestLayout_ApproverBadgeAndAdminEntry(t *testing.T) {
	approver := renderDashboardAs(t, shell.Shell{Operator: "dev1", Role: shell.RoleApprover, IsApprover: true, PendingApprovals: 2, GiteaURL: "http://g", WoodpeckerURL: "http://w"})
	if !strings.Contains(approver, `ssdlc-nav-badge">2<`) {
		t.Errorf("approver should see the pending badge; body:\n%s", approver)
	}
	if strings.Contains(approver, `href="/onboarding"`) {
		t.Error("an approver who is not an admin must not see Admin")
	}

	admin := renderDashboardAs(t, shell.Shell{Operator: "gateadmin", Role: shell.RoleAdmin, IsApprover: true, IsAdmin: true, GiteaURL: "http://g", WoodpeckerURL: "http://w"})
	if !strings.Contains(admin, `href="/onboarding"`) || !strings.Contains(admin, "Admin") {
		t.Error("an admin should see the Admin entry")
	}
	if strings.Contains(admin, "ssdlc-nav-badge") {
		t.Error("no badge when nothing is pending")
	}
}

func TestLayout_NoShellStillRenders(t *testing.T) {
	body := renderDashboardAs(t, shell.Shell{})
	if !strings.Contains(body, "Overview") {
		t.Error("the page must render with an empty shell (developer view, no links)")
	}
	if strings.Contains(body, "Quick launch") {
		t.Error("no Quick launch block without URLs")
	}
}

func TestLayout_LinksAreEscaped(t *testing.T) {
	body := renderDashboardAs(t, shell.Shell{Operator: `<script>x</script>`, GiteaURL: "http://g", WoodpeckerURL: "http://w"})
	if strings.Contains(body, "<script>x</script>") {
		t.Error("operator name must be HTML-escaped")
	}
}

func TestLayout_AdminEntryNeedsTeamMembership(t *testing.T) {
	notMember := renderDashboardAs(t, shell.Shell{Operator: "gateadmin", Role: shell.RoleAdmin, IsAdmin: true, GiteaURL: "http://g", WoodpeckerURL: "http://w"})
	if strings.Contains(notMember, `href="/onboarding"`) {
		t.Error("an admin outside the approvers team cannot open /onboarding, so must not see the link")
	}
	member := renderDashboardAs(t, shell.Shell{Operator: "gateadmin", Role: shell.RoleAdmin, IsAdmin: true, IsApprover: true, GiteaURL: "http://g", WoodpeckerURL: "http://w"})
	if !strings.Contains(member, `href="/onboarding"`) {
		t.Error("an admin in the approvers team should see the Admin entry")
	}
}
