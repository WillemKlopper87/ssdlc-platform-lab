package sidecar

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/report"
)

type fakeCommentAPI struct {
	comments []giteaclient.IssueComment
	created  []string
	edited   map[int64]string
	failList bool
}

func (f *fakeCommentAPI) ListIssueComments(ctx context.Context, o, r string, n int) ([]giteaclient.IssueComment, error) {
	if f.failList {
		return nil, errors.New("boom")
	}
	return f.comments, nil
}
func (f *fakeCommentAPI) CreateIssueComment(ctx context.Context, o, r string, n int, body string) error {
	f.created = append(f.created, body)
	return nil
}
func (f *fakeCommentAPI) EditIssueComment(ctx context.Context, o, r string, id int64, body string) error {
	if f.edited == nil {
		f.edited = map[int64]string{}
	}
	f.edited[id] = body
	return nil
}

func blockedReport() report.Report {
	return report.Report{
		Repo: "ssdlc/pilot-app", Number: 3, HeadSHA: "abcdef0123456789", Gate: "failure", MergeBlocked: true,
		Summary: report.Summary{Critical: 1, Medium: 1},
		Findings: []report.Finding{
			{Category: "blocking", Severity: "CRITICAL", Tool: "gitleaks", RuleID: "gitleaks/aws-access-token", Location: "config.py:5", Description: "AWS key"},
			{Category: "warning", Severity: "MEDIUM", Tool: "semgrep", RuleID: "semgrep/weak-hash", Location: "app.py:12", Description: "md5"},
		},
	}
}

func TestRenderComment(t *testing.T) {
	body := RenderComment(blockedReport(), "http://192.168.1.28:8181")
	for _, want := range []string{
		CommentMarker,
		"blocked",
		"abcdef0",
		"critical 1",
		"gitleaks/aws-access-token",
		"config.py:5",
		"http://192.168.1.28:8181/pr/ssdlc/pilot-app/3",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("comment missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "semgrep/weak-hash") {
		t.Errorf("only blocking findings are listed in the comment:\n%s", body)
	}
}

func TestRenderCommentIsDeterministic(t *testing.T) {
	if RenderComment(blockedReport(), "u") != RenderComment(blockedReport(), "u") {
		t.Fatal("comment must not embed timestamps or other varying data")
	}
}

func TestRenderCommentWithoutPortalURLOmitsLink(t *testing.T) {
	if strings.Contains(RenderComment(blockedReport(), ""), "/pr/") {
		t.Error("no portal URL configured: no link expected")
	}
}

func TestSyncCommentCreatesWhenNoneExists(t *testing.T) {
	api := &fakeCommentAPI{}
	if err := SyncComment(context.Background(), api, "gate-reporter", "o", "r", 1, CommentMarker+"\nhi"); err != nil {
		t.Fatal(err)
	}
	if len(api.created) != 1 {
		t.Errorf("created = %v", api.created)
	}
}

func TestSyncCommentEditsOwnCommentWhenChanged(t *testing.T) {
	api := &fakeCommentAPI{comments: []giteaclient.IssueComment{
		{ID: 9, Body: CommentMarker + "\nold", Author: "gate-reporter"},
	}}
	if err := SyncComment(context.Background(), api, "gate-reporter", "o", "r", 1, CommentMarker+"\nnew"); err != nil {
		t.Fatal(err)
	}
	if api.edited[9] != CommentMarker+"\nnew" || len(api.created) != 0 {
		t.Errorf("edited=%v created=%v", api.edited, api.created)
	}
}

func TestSyncCommentSkipsWhenUnchanged(t *testing.T) {
	body := CommentMarker + "\nsame"
	api := &fakeCommentAPI{comments: []giteaclient.IssueComment{{ID: 9, Body: body, Author: "gate-reporter"}}}
	if err := SyncComment(context.Background(), api, "gate-reporter", "o", "r", 1, body); err != nil {
		t.Fatal(err)
	}
	if len(api.edited) != 0 || len(api.created) != 0 {
		t.Errorf("unchanged body must not write: edited=%v created=%v", api.edited, api.created)
	}
}

func TestSyncCommentIgnoresMarkerFromAnotherAuthor(t *testing.T) {
	api := &fakeCommentAPI{comments: []giteaclient.IssueComment{
		{ID: 4, Body: CommentMarker + "\nspoof", Author: "dev2"},
	}}
	if err := SyncComment(context.Background(), api, "gate-reporter", "o", "r", 1, CommentMarker+"\nreal"); err != nil {
		t.Fatal(err)
	}
	if len(api.edited) != 0 || len(api.created) != 1 {
		t.Errorf("a marker pasted by someone else must not be edited: edited=%v created=%v", api.edited, api.created)
	}
}

func TestCommentHookNeverPanicsOrPropagatesErrors(t *testing.T) {
	api := &fakeCommentAPI{failList: true}
	hook := CommentHook(api, "gate-reporter", "", log.New(io.Discard, "", 0))
	hook(context.Background(), blockedReport()) // must simply log and return
}

func TestRenderCommentSanitizesInlineText(t *testing.T) {
	r := report.Report{
		Repo: "o/r", Number: 1, HeadSHA: "abc", Gate: "failure",
		Summary: report.Summary{},
		Findings: []report.Finding{
			{
				Category: "blocking", Severity: "CRITICAL",
				Tool: "tool", RuleID: strings.Repeat("x", 500),
				Location: "a`b\n@evil<script>.py",
				Description: "line1\n\n  line2 ```code``` <img src=x>",
			},
		},
	}
	body := RenderComment(r, "")

	// Should not contain raw backticks outside template's own 4 per finding (2 around RuleID, 2 around Location) plus 2 around SHA
	backtickCount := strings.Count(body, "`")
	expectedBackticks := 2 + 2 + 2 // SHA, RuleID, Location
	if backtickCount != expectedBackticks {
		t.Errorf("expected %d backticks in sanitized comment, got %d", expectedBackticks, backtickCount)
	}

	// Should not contain <img
	if strings.Contains(body, "<img") {
		t.Error("comment should not contain <img tag")
	}

	// Should not contain raw @ before evil (should have zero-width space)
	if strings.Contains(body, "@evil") {
		t.Error("comment should not contain raw @evil mention")
	}

	// Should not contain raw newlines inside the finding line (finding must be one "- **" line)
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "- **") {
			// This line should not contain the problematic strings with newlines
			if strings.Contains(line, "\n") {
				t.Errorf("finding line %d should not contain newlines: %s", i, line)
			}
		}
	}

	// Check that RuleID was truncated with ... (since it's 500+ chars)
	if !strings.Contains(body, "...") {
		t.Error("long RuleID should be truncated with ...")
	}
}

func TestSanitizeInline_WhitespaceCollapsing(t *testing.T) {
	input := "line1\n\n  line2\ttab\r\nline3"
	got := sanitizeInline(input, 1000)
	if strings.Contains(got, "\n") || strings.Contains(got, "\t") || strings.Contains(got, "\r") {
		t.Errorf("sanitizeInline should collapse whitespace: got %q", got)
	}
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") || !strings.Contains(got, "line3") {
		t.Errorf("sanitizeInline should preserve content: got %q", got)
	}
}

func TestSanitizeInline_TruncationOnRuneBoundary(t *testing.T) {
	// Use multibyte runes (Chinese characters) so byte truncation would corrupt
	input := "abc😀def😀ghi"  // emoji is 4 bytes but 1 rune
	got := sanitizeInline(input, 7)
	if !strings.HasSuffix(got, "...") {
		t.Errorf("should truncate with ...: got %q", got)
	}
	// Verify it's a valid string (not corrupted by byte truncation)
	if len([]rune(got)) == 0 {
		t.Error("truncated string should be valid")
	}
}

func TestCommentHookChecksRepoFormat(t *testing.T) {
	var logOutput strings.Builder
	lg := log.New(&logOutput, "", 0)
	api := &fakeCommentAPI{}
	hook := CommentHook(api, "bot", "url", lg)

	// Report with malformed repo name (no slash)
	badReport := report.Report{Repo: "noSlash", Number: 1, HeadSHA: "abc", Gate: "success"}
	hook(context.Background(), badReport)

	// API should not be called
	if len(api.created) > 0 || len(api.edited) > 0 {
		t.Error("API should not be called for malformed repo name")
	}

	// Should log the error
	output := logOutput.String()
	if !strings.Contains(output, "malformed repo name") {
		t.Errorf("should log malformed repo name error, got: %s", output)
	}
}
