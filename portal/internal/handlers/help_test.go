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
}
