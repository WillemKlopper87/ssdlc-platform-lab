package exceptions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestValidateExpiry_RejectsOver90Days(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(91 * 24 * time.Hour)
	if err := ValidateExpiry(expiry, now); err == nil {
		t.Fatal("expected an error for an expiry more than 90 days out")
	}
}

func TestValidateExpiry_AcceptsExactly90Days(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(90 * 24 * time.Hour)
	if err := ValidateExpiry(expiry, now); err != nil {
		t.Errorf("expected 90 days exactly to be accepted, got error: %v", err)
	}
}

func TestValidateExpiry_RejectsPastExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(-1 * time.Hour)
	if err := ValidateExpiry(expiry, now); err == nil {
		t.Fatal("expected an error for an expiry already in the past")
	}
}

func TestRecordPath_IsDeterministicAndFilesystemSafe(t *testing.T) {
	r1 := Record{Repo: "gateadmin/gate-demo", FindingFingerprint: "gitleaks/aws-access-token:config.py:5"}
	r2 := Record{Repo: "gateadmin/gate-demo", FindingFingerprint: "gitleaks/aws-access-token:config.py:5"}
	path1 := RecordPath(r1)
	path2 := RecordPath(r2)
	if path1 != path2 {
		t.Error("RecordPath must be deterministic for the same repo+fingerprint")
	}
	for _, c := range path1 {
		if c == '/' {
			t.Errorf("RecordPath %q must not contain a raw '/' -- it becomes a single Gitea file path", path1)
		}
	}
	other := Record{Repo: "gateadmin/other-repo", FindingFingerprint: "gitleaks/aws-access-token:config.py:5"}
	if RecordPath(other) == path1 {
		t.Error("RecordPath must differ for a different repo with the same fingerprint")
	}
}

func TestStore_WriteThenList_RoundTrips(t *testing.T) {
	const contentsPrefix = "/api/v1/repos/gateadmin/exceptions/contents/"
	files := map[string][]byte{} // path (relative to contentsPrefix) -> raw content
	var putCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, contentsPrefix) {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		relPath := strings.TrimPrefix(r.URL.Path, contentsPrefix)

		switch r.Method {
		case http.MethodGet:
			if relPath == "" {
				// Directory listing (Store.List's ListDirectory call).
				var entries []string
				for p := range files {
					entries = append(entries, `{"path":"`+p+`","type":"file"}`)
				}
				w.Write([]byte("[" + strings.Join(entries, ",") + "]"))
				return
			}
			// Single-file GET (Store.Write's existing-SHA check, or a direct read).
			content, ok := files[relPath]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(`{"content":"` + base64Encode(content) + `","sha":"abc123"}`))
		case http.MethodPost:
			var body struct {
				Content string `json:"content"`
			}
			jsonDecode(r, &body)
			files[relPath] = base64Decode(body.Content)
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			putCalled = true
			var body struct {
				Content string `json:"content"`
				SHA     string `json:"sha"`
			}
			jsonDecode(r, &body)
			if body.SHA == "" {
				t.Errorf("PUT for %s must carry the existing record's sha", relPath)
			}
			files[relPath] = base64Decode(body.Content)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	gitea := giteaclient.New(srv.URL, "fake-token")
	store := NewStore(gitea, "gateadmin", "exceptions")

	rec := Record{
		Repo:               "gateadmin/gate-demo",
		FindingFingerprint: "gitleaks/aws-access-token:config.py:5",
		Severity:           "critical",
		Expiry:             time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
		Requester:          "alice",
		Ticket:             "TICKET-1",
		Approved:           false,
	}
	if err := store.Write(context.Background(), rec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	records, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	got := records[0]
	if got.Repo != rec.Repo || got.FindingFingerprint != rec.FindingFingerprint ||
		got.Requester != rec.Requester || got.Ticket != rec.Ticket || got.Approved != rec.Approved {
		t.Errorf("round-tripped record = %+v, want %+v", got, rec)
	}
	if !got.Expiry.Equal(rec.Expiry) {
		t.Errorf("Expiry = %v, want %v", got.Expiry, rec.Expiry)
	}

	// Second Write on the same Repo+FindingFingerprint models the approval
	// flow (Task 11): RecordPath resolves to the identical file, so this
	// must issue an HTTP PUT carrying the existing SHA, not a second POST.
	approved := rec
	approved.Approved = true
	approved.Approvers = []string{"bob"}
	if err := store.Write(context.Background(), approved); err != nil {
		t.Fatalf("second Write (approval): %v", err)
	}
	if !putCalled {
		t.Error("expected the second Write to issue a PUT against the existing record's path")
	}

	records, err = store.List(context.Background())
	if err != nil {
		t.Fatalf("List after approval: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records after approval, want 1 (update must replace, not duplicate)", len(records))
	}
	gotApproved := records[0]
	if !gotApproved.Approved {
		t.Error("expected Approved=true after the approval Write")
	}
	if len(gotApproved.Approvers) != 1 || gotApproved.Approvers[0] != "bob" {
		t.Errorf("Approvers = %v, want [bob]", gotApproved.Approvers)
	}
}

func TestStore_Ensure_CallsEnsureRepo(t *testing.T) {
	var ensureRepoCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/gateadmin/exceptions":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			ensureRepoCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gitea := giteaclient.New(srv.URL, "fake-token")
	store := NewStore(gitea, "gateadmin", "exceptions")
	if err := store.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !ensureRepoCalled {
		t.Error("expected Ensure to call EnsureRepo")
	}
}
