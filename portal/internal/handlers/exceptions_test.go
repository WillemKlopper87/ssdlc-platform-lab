// portal/internal/handlers/exceptions_test.go
package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/exceptions"
	"ssdlc-portal/internal/giteaclient"
)

func jsonDecode(r *http.Request, v any) {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		panic(err)
	}
}

func base64Decode(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// fakeExceptionsGitea models just enough of Gitea's contents + teams API
// for these tests: an in-memory directory of exception-record JSON files,
// plus a configurable team-membership answer.
func fakeExceptionsGitea(t *testing.T, username string, onApproverTeam bool, seedRecords map[string]string) *httptest.Server {
	t.Helper()
	const contentsPrefix = "/api/v1/repos/gateadmin/exceptions/contents/"
	files := map[string][]byte{}
	for path, content := range seedRecords {
		files[path] = []byte(content)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/user":
			w.Write([]byte(`{"login":"` + username + `"}`))
		case r.URL.Path == "/api/v1/user/teams":
			if onApproverTeam {
				w.Write([]byte(`[{"name":"security-officers","organization":{"username":"gateadmin"}}]`))
			} else {
				w.Write([]byte(`[]`))
			}
		case strings.HasPrefix(r.URL.Path, contentsPrefix):
			relPath := strings.TrimPrefix(r.URL.Path, contentsPrefix)
			switch r.Method {
			case http.MethodGet:
				if relPath == "" {
					var entries []string
					for p := range files {
						entries = append(entries, `{"path":"`+p+`","type":"file"}`)
					}
					w.Write([]byte("[" + strings.Join(entries, ",") + "]"))
					return
				}
				content, ok := files[relPath]
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Write([]byte(`{"content":"` + base64Encode(content) + `","sha":"abc123"}`))
			case http.MethodPost, http.MethodPut:
				var body struct {
					Content string `json:"content"`
				}
				jsonDecode(r, &body)
				files[relPath] = base64Decode(body.Content)
				w.WriteHeader(http.StatusCreated)
			default:
				t.Fatalf("unexpected method %s on %s", r.Method, r.URL.Path)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
}

const pendingRecordJSON = `{
	"repo": "gateadmin/gate-demo",
	"finding_fingerprint": "f1",
	"severity": "critical",
	"expiry": "2099-01-01T00:00:00Z",
	"requester": "alice",
	"approvers": [],
	"ticket": "TICKET-1",
	"approved": false
}`

func TestExceptionApprove_RejectsSelfApproval(t *testing.T) {
	srv := fakeExceptionsGitea(t, "alice", true, map[string]string{"seed1.json": pendingRecordJSON})
	defer srv.Close()

	store := exceptions.NewStore(giteaclient.New(srv.URL, "token"), "gateadmin", "exceptions")
	handler := ExceptionApprove(store, giteaclient.New(srv.URL, "alice-token"))

	form := url.Values{"repo": {"gateadmin/gate-demo"}, "fingerprint": {"f1"}}
	req := httptest.NewRequest(http.MethodPost, "/exceptions/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "alice-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (self-approval must be rejected)", rec.Code, http.StatusForbidden)
	}
}

// TestExceptionApprove_RejectsNonApproverTeamMember exercises the full
// /exceptions/approve route shape -- RequireTeam wrapping ExceptionApprove,
// exactly as main.go wires it -- rather than calling the bare handler,
// since the team-membership check now lives entirely in RequireTeam and
// ExceptionApprove itself no longer performs it (avoiding the duplicate
// IsOnTeam check the plan calls out).
func TestExceptionApprove_RejectsNonApproverTeamMember(t *testing.T) {
	srv := fakeExceptionsGitea(t, "carol", false, map[string]string{"seed1.json": pendingRecordJSON})
	defer srv.Close()

	store := exceptions.NewStore(giteaclient.New(srv.URL, "token"), "gateadmin", "exceptions")
	gitea := giteaclient.New(srv.URL, "")
	handler := RequireTeam(gitea, "gateadmin", "security-officers", ExceptionApprove(store, gitea))

	form := url.Values{"repo": {"gateadmin/gate-demo"}, "fingerprint": {"f1"}}
	req := httptest.NewRequest(http.MethodPost, "/exceptions/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "carol-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (non-approver-team member must be rejected)", rec.Code, http.StatusForbidden)
	}
}

// TestExceptionApprove_AcceptsDistinctApproverOnTeam also goes through the
// RequireTeam-wrapped route (see comment above) to prove the two checks
// compose correctly end-to-end: a distinct, approver-team member succeeds.
func TestExceptionApprove_AcceptsDistinctApproverOnTeam(t *testing.T) {
	srv := fakeExceptionsGitea(t, "bob", true, map[string]string{"seed1.json": pendingRecordJSON})
	defer srv.Close()

	store := exceptions.NewStore(giteaclient.New(srv.URL, "token"), "gateadmin", "exceptions")
	gitea := giteaclient.New(srv.URL, "")
	handler := RequireTeam(gitea, "gateadmin", "security-officers", ExceptionApprove(store, gitea))

	form := url.Values{"repo": {"gateadmin/gate-demo"}, "fingerprint": {"f1"}}
	req := httptest.NewRequest(http.MethodPost, "/exceptions/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "bob-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s, want 200 for a valid distinct approver", rec.Code, rec.Body.String())
	}

	records, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List after approval: %v", err)
	}
	if len(records) != 1 || !records[0].Approved || len(records[0].Approvers) != 1 || records[0].Approvers[0] != "bob" {
		t.Errorf("record after approval = %+v, want Approved=true, Approvers=[bob]", records[0])
	}
}

func TestExceptionApprove_NoSuchPendingExceptionIs404(t *testing.T) {
	srv := fakeExceptionsGitea(t, "bob", true, map[string]string{})
	defer srv.Close()

	store := exceptions.NewStore(giteaclient.New(srv.URL, "token"), "gateadmin", "exceptions")
	handler := ExceptionApprove(store, giteaclient.New(srv.URL, "bob-token"))

	form := url.Values{"repo": {"gateadmin/gate-demo"}, "fingerprint": {"does-not-exist"}}
	req := httptest.NewRequest(http.MethodPost, "/exceptions/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "bob-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d for a nonexistent pending exception", rec.Code, http.StatusNotFound)
	}
}

func TestExceptionRequestSubmit_RejectsExpiryOver90Days(t *testing.T) {
	srv := fakeExceptionsGitea(t, "alice", false, map[string]string{})
	defer srv.Close()
	store := exceptions.NewStore(giteaclient.New(srv.URL, "token"), "gateadmin", "exceptions")
	gitea := giteaclient.New(srv.URL, "")
	handler := ExceptionRequestSubmit(store, gitea)

	form := url.Values{
		"repo": {"gateadmin/gate-demo"}, "fingerprint": {"f1"}, "severity": {"critical"},
		"ticket": {"TICKET-1"}, "requester": {"alice"},
		"expiry": {"2099-01-01"},
	}
	req := httptest.NewRequest(http.MethodPost, "/exceptions/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "alice-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for an expiry more than 90 days out", rec.Code, http.StatusBadRequest)
	}
}

// TestExceptionRequestSubmit_IgnoresForgedRequesterField is the regression
// test for the requester-spoofing gap: a POST claiming to be from "eve"
// while authenticated as "alice" must be recorded as requested by "alice"
// -- the session-derived identity -- never the form's claimed value. If
// the forged value survived into the record, an attacker could request an
// exception "as" someone else, then approve it themselves under their own
// (different) username, defeating ExceptionApprove's self-approval check
// entirely.
func TestExceptionRequestSubmit_IgnoresForgedRequesterField(t *testing.T) {
	srv := fakeExceptionsGitea(t, "alice", false, map[string]string{})
	defer srv.Close()
	store := exceptions.NewStore(giteaclient.New(srv.URL, "token"), "gateadmin", "exceptions")
	gitea := giteaclient.New(srv.URL, "")
	handler := ExceptionRequestSubmit(store, gitea)

	form := url.Values{
		"repo": {"gateadmin/gate-demo"}, "fingerprint": {"f1"}, "severity": {"critical"},
		"ticket": {"TICKET-1"}, "requester": {"eve"}, // forged: authenticated caller is "alice"
		"expiry": {"2026-10-01"},
	}
	req := httptest.NewRequest(http.MethodPost, "/exceptions/submit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "alice-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, body = %s, want 302 redirect on success", rec.Code, rec.Body.String())
	}

	records, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List after submit: %v", err)
	}
	if len(records) != 1 || records[0].Requester != "alice" {
		t.Errorf("record.Requester = %q, want %q (session identity, not the forged form value)",
			recordsRequester(records), "alice")
	}
}

func recordsRequester(records []exceptions.Record) string {
	if len(records) == 0 {
		return "<no records>"
	}
	return records[0].Requester
}

func TestExceptionsQueue_SeparatesPendingAndActive(t *testing.T) {
	activeRecordJSON := `{
		"repo": "gateadmin/gate-demo", "finding_fingerprint": "f2", "severity": "high",
		"expiry": "2099-01-01T00:00:00Z", "requester": "alice", "approvers": ["bob"],
		"ticket": "TICKET-2", "approved": true
	}`
	srv := fakeExceptionsGitea(t, "carol", false, map[string]string{
		"pending.json": pendingRecordJSON,
		"active.json":  activeRecordJSON,
	})
	defer srv.Close()

	store := exceptions.NewStore(giteaclient.New(srv.URL, "token"), "gateadmin", "exceptions")
	handler := ExceptionsQueue(store, giteaclient.New(srv.URL, "carol-token"), "gateadmin")

	req := httptest.NewRequest(http.MethodGet, "/exceptions", nil)
	ctx := context.WithValue(req.Context(), auth.ContextKeyToken, "carol-token")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "f1") || !strings.Contains(body, "f2") {
		t.Errorf("queue page missing one of the two records; body:\n%s", body)
	}
}
