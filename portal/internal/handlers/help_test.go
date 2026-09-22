package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssdlc-portal/internal/shell"
)

func getHelp(t *testing.T, target string, s shell.Shell) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(shell.WithContext(req.Context(), s))
	rec := httptest.NewRecorder()
	Help()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	return rec.Body.String()
}

func TestHelp_RoleSpecificFirstSteps(t *testing.T) {
	dev := getHelp(t, "/help", shell.Shell{Operator: "dev2", Role: shell.RoleDeveloper})
	if !strings.Contains(dev, "first steps as a developer") || !strings.Contains(dev, "Why was my pull request blocked?") {
		t.Error("developer help page incomplete")
	}
	appr := getHelp(t, "/help", shell.Shell{Operator: "dev1", Role: shell.RoleApprover, IsApprover: true})
	if !strings.Contains(appr, "first steps as an approver") {
		t.Error("approver page should say 'an approver'")
	}
	adm := getHelp(t, "/help", shell.Shell{Operator: "gateadmin", Role: shell.RoleAdmin, IsAdmin: true})
	if !strings.Contains(adm, "first steps as an admin") {
		t.Error("admin page should say 'an admin'")
	}
}

func TestHelp_SearchFiltersAndEmptyState(t *testing.T) {
	body := getHelp(t, "/help?q=baseline", shell.Shell{Role: shell.RoleDeveloper})
	if !strings.Contains(body, "What is the baseline?") {
		t.Error("expected the baseline topic")
	}
	if strings.Contains(body, "Keyboard shortcuts") {
		t.Error("unrelated topics must be filtered out")
	}
	none := getHelp(t, "/help?q=zzzz-nothing", shell.Shell{Role: shell.RoleDeveloper})
	if !strings.Contains(none, "No topics match") {
		t.Error("expected the empty state")
	}
}

func TestHelp_QueryIsEscaped(t *testing.T) {
	body := getHelp(t, "/help?q=%3Cscript%3Ealert(1)%3C/script%3E", shell.Shell{Role: shell.RoleDeveloper})
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("the search term must be HTML-escaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("expected the escaped form of the term")
	}
}

func TestHelp_BlankQueryIsNormalPage(t *testing.T) {
	body := getHelp(t, "/help?q=%20%20", shell.Shell{Role: shell.RoleDeveloper})
	if !strings.Contains(body, "Common questions") || strings.Contains(body, "Results for") {
		t.Error("a blank query should show the normal page")
	}
}

func TestHelp_ActionButtonsAreRoleGated(t *testing.T) {
	dev := getHelp(t, "/help", shell.Shell{Operator: "dev2", Role: shell.RoleDeveloper})
	if strings.Contains(dev, `href="/onboarding"`) {
		t.Error("a developer must not get a button to the admin-only setup page")
	}
	if !strings.Contains(dev, "How do I add a project to the gate?") {
		t.Error("the setup topic must stay visible to everyone")
	}
	adm := getHelp(t, "/help", shell.Shell{Operator: "gateadmin", Role: shell.RoleAdmin, IsAdmin: true, IsApprover: true})
	if !strings.Contains(adm, `href="/onboarding"`) {
		t.Error("an admin should get the setup button")
	}
	outside := getHelp(t, "/help", shell.Shell{Operator: "siteadmin", Role: shell.RoleAdmin, IsAdmin: true})
	if strings.Contains(outside, `href="/onboarding"`) {
		t.Error("an admin outside the approvers team gets a 403 there, so no button")
	}
	if !strings.Contains(adm, `href="/exceptions"`) || !strings.Contains(outside, `href="/exceptions"`) {
		t.Error("admins should keep the Exceptions action buttons")
	}
}
