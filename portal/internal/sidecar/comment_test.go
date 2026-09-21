package sidecar

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"unicode/utf8"

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

	// Backtick count: exactly 2 for SHA (in header) + 2 for RuleID backticks + 2 for Location backticks = 6 total
	backtickCount := strings.Count(body, "`")
	expectedBackticks := 2 + 2 + 2 // 2 for SHA, 2 for RuleID, 2 for Location
	if backtickCount != expectedBackticks {
		t.Errorf("expected %d backticks (2 SHA + 2 RuleID + 2 Location), got %d", expectedBackticks, backtickCount)
	}

	// Should not contain <img
	if strings.Contains(body, "<img") {
		t.Error("comment should not contain <img tag")
	}

	// Should not contain raw @evil without zero-width space
	if strings.Contains(body, "@evil") {
		t.Error("comment should not contain raw @evil mention")
	}
	// Should contain @​evil (with zero-width space after @)
	if !strings.Contains(body, "@​evil") {
		t.Error("comment should contain @​evil with zero-width space after @")
	}

	// Count lines starting with "- **" should equal the number of blocking findings (1)
	lines := strings.Split(body, "\n")
	findingLineCount := 0
	var findingLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "- **") {
			findingLineCount++
			findingLine = line
		}
	}
	if findingLineCount != 1 {
		t.Errorf("expected 1 finding line starting with '- **', got %d", findingLineCount)
	}

	// The single finding line should contain the sanitized location and description
	if findingLineCount == 1 {
		// Should contain sanitized location (backtick becomes quote, newlines removed, @evil escaped, <> escaped)
		if !strings.Contains(findingLine, "a'b") {
			t.Errorf("finding line should contain sanitized location with quote: %s", findingLine)
		}
		if !strings.Contains(findingLine, "&lt;") {
			t.Errorf("finding line should contain &lt; (escaped <): %s", findingLine)
		}
		// Should contain sanitized description (newlines collapsed to space)
		if !strings.Contains(findingLine, "line1 line2") {
			t.Errorf("finding line should contain 'line1 line2' (newlines collapsed): %s", findingLine)
		}
	}

	// RuleID should be exactly 120 runes + "..." (500 exceeds 120 max, so gets truncated to 120+...)
	expectedRuleID := strings.Repeat("x", 120) + "..."
	if !strings.Contains(body, "`"+expectedRuleID+"`") {
		t.Errorf("RuleID should be truncated to exactly 120 runes with ...: expected `%s`", expectedRuleID)
	}
}

func TestSanitizeInline_WhitespaceCollapsing(t *testing.T) {
	// Test 1: Newlines and tabs collapse to single space
	got := sanitizeInline("a \n\t b", 1000)
	if got != "a b" {
		t.Errorf("whitespace should collapse to single space: expected 'a b', got %q", got)
	}

	// Test 2: Multiple consecutive spaces and newlines collapse to single space
	got = sanitizeInline("line1\n\n  line2\ttab\r\nline3", 1000)
	expected := "line1 line2 tab line3"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}

	// Test 3: Backtick becomes single quote
	got = sanitizeInline("a`b", 1000)
	if got != "a'b" {
		t.Errorf("backtick should become quote: expected 'a'b', got %q", got)
	}

	// Test 4: Angle brackets escaped
	got = sanitizeInline("<b>", 1000)
	if got != "&lt;b&gt;" {
		t.Errorf("angle brackets should be escaped: expected '&lt;b&gt;', got %q", got)
	}

	// Test 5: @ followed by zero-width space
	got = sanitizeInline("@x", 1000)
	if got != "@​x" {
		t.Errorf("@ should be followed by zero-width space: expected '@\\u200bx', got %q", got)
	}
}

func TestSanitizeInline_TruncationOnRuneBoundary(t *testing.T) {
	// Use emoji (multibyte rune) to verify rune-boundary truncation, not byte truncation
	// emoji is 4 bytes each but 1 rune each
	input := "abc😀def😀ghi"
	got := sanitizeInline(input, 7)

	// Expected: keep 7 runes "abc😀def" then add "..." (total 10 runes)
	expected := "abc😀def..."
	if got != expected {
		t.Errorf("truncation at rune boundary: expected %q, got %q", expected, got)
	}

	// Verify result is valid UTF-8 (no corruption from byte-level truncation)
	if !utf8.ValidString(got) {
		t.Errorf("result should be valid UTF-8: got %q", got)
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
