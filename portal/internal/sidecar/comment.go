package sidecar

import (
	"context"
	"fmt"
	"log"
	"strings"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/report"
)

// CommentMarker identifies the one comment this service maintains per PR.
const CommentMarker = "<!-- ssdlc-gate-report -->"

const maxCommentFindings = 10

type CommentAPI interface {
	ListIssueComments(ctx context.Context, owner, repo string, number int) ([]giteaclient.IssueComment, error)
	CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error
	EditIssueComment(ctx context.Context, owner, repo string, id int64, body string) error
}

// RenderComment is deterministic (no timestamps) so an unchanged result never
// causes an edit.
func RenderComment(r report.Report, portalURL string) string {
	var b strings.Builder
	b.WriteString(CommentMarker + "\n")
	verdict := map[string]string{
		"success": "passed", "failure": "blocked", "pending": "running", "none": "no result yet",
	}[r.Gate]
	if verdict == "" {
		verdict = "no result yet"
	}
	sha := r.HeadSHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	fmt.Fprintf(&b, "**SSDLC gate: %s** for `%s`\n\n", verdict, sha)
	fmt.Fprintf(&b, "Findings: critical %d, high %d, medium %d, low %d\n", r.Summary.Critical, r.Summary.High, r.Summary.Medium, r.Summary.Low)

	shown := 0
	for _, f := range r.Findings {
		if f.Category != "blocking" {
			continue
		}
		if shown == 0 {
			b.WriteString("\nBlocking:\n")
		}
		if shown == maxCommentFindings {
			b.WriteString("- ...and more (see the full report)\n")
			break
		}
		fmt.Fprintf(&b, "- **%s** `%s` at `%s`: %s\n", f.Severity, f.RuleID, f.Location, f.Description)
		shown++
	}
	if portalURL != "" {
		fmt.Fprintf(&b, "\n[Full report](%s/pr/%s/%d)\n", strings.TrimRight(portalURL, "/"), r.Repo, r.Number)
	}
	return b.String()
}

// SyncComment creates the sticky comment, or edits the one this bot already
// wrote. A marker pasted by another user is ignored: only comments by
// botLogin are ever edited.
func SyncComment(ctx context.Context, api CommentAPI, botLogin, owner, repo string, number int, body string) error {
	comments, err := api.ListIssueComments(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	for _, c := range comments {
		if c.Author == botLogin && strings.Contains(c.Body, CommentMarker) {
			if strings.TrimSpace(c.Body) == strings.TrimSpace(body) {
				return nil
			}
			return api.EditIssueComment(ctx, owner, repo, c.ID, body)
		}
	}
	return api.CreateIssueComment(ctx, owner, repo, number, body)
}

// CommentHook returns a Poller.AfterReport that keeps each PR's sticky
// comment current. Failures are logged and never propagate.
func CommentHook(api CommentAPI, botLogin, portalURL string, lg *log.Logger) func(ctx context.Context, r report.Report) {
	return func(ctx context.Context, r report.Report) {
		owner, repo, _ := strings.Cut(r.Repo, "/")
		if err := SyncComment(ctx, api, botLogin, owner, repo, r.Number, RenderComment(r, portalURL)); err != nil {
			lg.Printf("comment: %s#%d: %v", r.Repo, r.Number, err)
		}
	}
}
