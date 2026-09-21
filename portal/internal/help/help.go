// Package help holds the portal's Help content. It is fixed text owned by
// the code (safe to render as HTML), filtered on the server so the page
// needs no JavaScript.
package help

import (
	"html/template"
	"strings"

	"ssdlc-portal/internal/shell"
)

type Topic struct {
	ID       string
	Question string
	Answer   template.HTML
	Roles    []string
	GoTo     string // same-site path, or empty
	GoLabel  string
}

var all = []string{"Developer", "Approver", "Admin"}

func Topics() []Topic {
	return []Topic{
		{ID: "blocked", Question: "Why was my pull request blocked?", Roles: []string{"Developer"},
			Answer: "The gate found at least one <b>new Critical or High</b> issue in the code you changed. Open the pull request to see each issue, why it matters and how to fix it. Existing problems in the <b>baseline</b> never block you. The pull request unblocks once the issues are fixed or an exception is approved."},
		{ID: "fix", Question: "How do I fix an issue?", Roles: []string{"Developer"},
			Answer: "Read the explanation and the suggested change, fix the code and push to your branch. The gate runs again in about a minute. If a fix is not possible right now, request an exception instead."},
		{ID: "exception", Question: "When should I request an exception?", Roles: []string{"Developer"},
			Answer: "Only when an issue is real but acceptable for now, for example test-only code, or a control that exists elsewhere. Explain <b>why</b>, choose an expiry (at most 90 days) and send it. It needs one approver who is not you, and it always expires, after which the issue blocks again.",
			GoTo:   "/exceptions", GoLabel: "Open Exceptions"},
		{ID: "approve", Question: "Who can approve my exception?", Roles: []string{"Developer", "Approver"},
			Answer: "Anyone in the approvers team except the person who asked. You can never approve your own request: it needs a second person. Approvers see a badge next to Exceptions in the sidebar."},
		{ID: "review", Question: "How do I review an exception?", Roles: []string{"Approver"},
			Answer: "Open <b>Exceptions</b>. Read the reason and the expiry. Approve if the risk is understood and temporary, decline if it should be fixed instead.",
			GoTo:   "/exceptions", GoLabel: "Open Exceptions"},
		{ID: "setup", Question: "How do I add a project to the gate?", Roles: []string{"Admin"},
			Answer: "Open <b>Admin</b>, enter the repository owner and name, and start setup. The portal activates the repository in the CI system, records today's findings as the baseline, adds the gate pipeline and turns on branch protection.",
			GoTo:   "/onboarding", GoLabel: "Set up a project"},
		{ID: "grade", Question: "What do the letter grades mean?", Roles: all,
			Answer: "Each project gets a grade from its open issues, weighted by severity: <b>A</b> is 90 or above, <b>B</b> 75, <b>C</b> 60, <b>D</b> 40 and <b>F</b> below 40. One open Critical issue is enough to drop a project to a D or F, on purpose. (Grades arrive with the Projects screens.)"},
		{ID: "sla", Question: "What is the \"fix within\" countdown?", Roles: all,
			Answer: "Each severity has a target: Critical 2 days, High 7, Medium 30, Low 90. The clock starts when the issue is first found and pauses while an approved exception is in force. Baseline issues have no clock. (The countdown arrives with the Issues screens.)"},
		{ID: "baseline", Question: "What is the baseline?", Roles: all,
			Answer: "When a project joins the gate, everything already in the code is recorded as the baseline. It never blocks merges, and it can only shrink: nothing can be added to it. Secrets are never baselined."},
		{ID: "signin", Question: "Do I need to sign in again for Gitea or Woodpecker?", Roles: all,
			Answer: "No separate account is needed: it is the same Gitea account. The Quick launch links open in a new tab. If Woodpecker asks you to authorise the first time, click <b>Authorize</b> once. After that it signs you in automatically."},
		{ID: "shortcuts", Question: "Keyboard shortcuts", Roles: all,
			Answer: "<kbd class=\"ssdlc-kbd\">Ctrl</kbd> <kbd class=\"ssdlc-kbd\">K</kbd> opens the jump palette to reach any page, or open Gitea or Woodpecker. <kbd class=\"ssdlc-kbd\">Esc</kbd> closes it."},
	}
}

// Filter keeps the topics whose question or answer contains q
// (case-insensitive, surrounding spaces ignored). An empty q keeps all.
func Filter(topics []Topic, q string) []Topic {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return topics
	}
	var out []Topic
	for _, t := range topics {
		if strings.Contains(strings.ToLower(t.Question+" "+string(t.Answer)), q) {
			out = append(out, t)
		}
	}
	return out
}

// FirstSteps is the short checklist shown at the top of the page for a role.
func FirstSteps(role shell.Role) []string {
	switch role {
	case shell.RoleApprover:
		return []string{
			"Open <b>Exceptions</b> when the badge shows a number",
			"Read the reason and the expiry, and judge the risk",
			"Approve or decline (never your own requests)",
			"Use the Overview to spot anything that is overdue",
		}
	case shell.RoleAdmin:
		return []string{
			"Open <b>Admin</b> to set up a project",
			"Enter the repository owner and name and run setup",
			"Tell the team the project is now behind the gate",
			"Watch for exceptions waiting in the sidebar badge",
		}
	default:
		return []string{
			"Open a pull request as usual in Gitea",
			"Watch the gate result on the pull request or in the Overview",
			"If it is blocked, fix the issue or request an exception",
			"Merge once the gate passes and a second person approves",
		}
	}
}
