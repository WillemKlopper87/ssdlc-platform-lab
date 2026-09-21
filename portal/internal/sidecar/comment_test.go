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
