package shell

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/exceptions"
)

type fakeIdentity struct {
	login   string
	admin   bool
	member  bool
	userErr error
	teamErr error
	calls   *int
}

func (f fakeIdentity) CurrentUser(ctx context.Context) (string, bool, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.login, f.admin, f.userErr
}
func (f fakeIdentity) IsOnTeam(ctx context.Context, org, team string) (bool, error) {
	return f.member, f.teamErr
}

func builder(id fakeIdentity, recs []exceptions.Record, recErr error) *Builder {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	return &Builder{
		NewIdentity: func(token string) Identity { return id },
		Records:     func(ctx context.Context) ([]exceptions.Record, error) { return recs, recErr },
		Org:         "ssdlc", Team: "approvers",
		GiteaURL: "http://g:3500", WoodpeckerURL: "http://w:8000",
		TTL: 30 * time.Second, Now: func() time.Time { return now },
	}
}

var pendingRecs = []exceptions.Record{{Requester: "dev2", Expiry: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}}

func TestFor_Roles(t *testing.T) {
	ctx := context.Background()
	dev := builder(fakeIdentity{login: "dev2"}, pendingRecs, nil).For(ctx, "t1")
	if dev.Role != RoleDeveloper || dev.IsApprover || dev.IsAdmin || dev.PendingApprovals != 0 || dev.Operator != "dev2" {
		t.Errorf("developer = %+v", dev)
	}
	appr := builder(fakeIdentity{login: "dev1", member: true}, pendingRecs, nil).For(ctx, "t2")
	if appr.Role != RoleApprover || !appr.IsApprover || appr.IsAdmin || appr.PendingApprovals != 1 {
		t.Errorf("approver = %+v", appr)
	}
	adm := builder(fakeIdentity{login: "gateadmin", admin: true, member: true}, pendingRecs, nil).For(ctx, "t3")
	if adm.Role != RoleAdmin || !adm.IsAdmin || !adm.IsApprover || adm.RoleLabel() != "Admin" {
		t.Errorf("admin = %+v", adm)
	}
	if adm.GiteaURL != "http://g:3500" || adm.WoodpeckerURL != "http://w:8000" {
		t.Errorf("urls = %q %q", adm.GiteaURL, adm.WoodpeckerURL)
	}
}

func TestFor_FailsSoft(t *testing.T) {
	ctx := context.Background()
	s := builder(fakeIdentity{userErr: errors.New("boom")}, nil, nil).For(ctx, "t")
	if s.Role != RoleDeveloper || s.Operator != "" || s.GiteaURL == "" {
		t.Errorf("identity failure should give the developer view with links: %+v", s)
	}
	s = builder(fakeIdentity{login: "dev1", member: true}, nil, errors.New("store down")).For(ctx, "t")
	if !s.IsApprover || s.PendingApprovals != 0 {
		t.Errorf("a store failure must only lose the badge: %+v", s)
	}
	s = builder(fakeIdentity{login: "dev1", teamErr: errors.New("teams down")}, nil, nil).For(ctx, "t")
	if s.IsApprover || s.Operator != "dev1" {
		t.Errorf("a team lookup failure must not grant approver: %+v", s)
	}
}

func TestFor_CachesPerToken(t *testing.T) {
	calls := 0
	b := builder(fakeIdentity{login: "dev2", calls: &calls}, nil, nil)
	ctx := context.Background()
	b.For(ctx, "same")
	b.For(ctx, "same")
	if calls != 1 {
		t.Errorf("identity calls = %d, want 1 (cached)", calls)
	}
	b.For(ctx, "other")
	if calls != 2 {
		t.Errorf("a different token must not share the cache; calls = %d", calls)
	}
	// TTL expiry
	later := b.Now().Add(31 * time.Second)
	b.Now = func() time.Time { return later }
	b.For(ctx, "same")
	if calls != 3 {
		t.Errorf("entry should expire after the TTL; calls = %d", calls)
	}
}

func TestMiddleware_PutsShellInContext(t *testing.T) {
	b := builder(fakeIdentity{login: "dev1", member: true}, pendingRecs, nil)
	var got Shell
	h := b.Middleware(func(w http.ResponseWriter, r *http.Request) { got = FromContext(r.Context()) })
	req := httptest.NewRequest("GET", "/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyToken, "tok"))
	h(httptest.NewRecorder(), req)
	if got.Operator != "dev1" || !got.IsApprover {
		t.Errorf("shell in context = %+v", got)
	}

	// No token in the context: the request is still served, with a zero shell.
	got = Shell{Operator: "stale"}
	h(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if got.Operator != "" {
		t.Errorf("without a session the shell must be empty, got %+v", got)
	}
}

func TestWithContextRoundTrip(t *testing.T) {
	s := Shell{Operator: "x", Role: RoleAdmin}
	if FromContext(WithContext(context.Background(), s)).Operator != "x" {
		t.Error("round trip failed")
	}
	if FromContext(context.Background()).Operator != "" {
		t.Error("empty context must give a zero Shell")
	}
}

func TestFor_AdminWithoutTeamMembershipIsNotApprover(t *testing.T) {
	s := builder(fakeIdentity{login: "gateadmin", admin: true}, pendingRecs, nil).For(context.Background(), "ta")
	if !s.IsAdmin || s.IsApprover || s.PendingApprovals != 0 || s.Role != RoleAdmin {
		t.Errorf("admin outside the approvers team = %+v", s)
	}
}

type flakyIdentity struct {
	calls *int
}

func (f flakyIdentity) CurrentUser(ctx context.Context) (string, bool, error) {
	*f.calls++
	if *f.calls == 1 {
		return "", false, errors.New("transient")
	}
	return "dev2", false, nil
}
func (f flakyIdentity) IsOnTeam(ctx context.Context, org, team string) (bool, error) {
	return false, nil
}

func TestFor_DoesNotCacheIdentityFailure(t *testing.T) {
	calls := 0
	b := builder(fakeIdentity{}, nil, nil)
	b.NewIdentity = func(string) Identity { return flakyIdentity{calls: &calls} }
	ctx := context.Background()
	if first := b.For(ctx, "tok"); first.Operator != "" {
		t.Fatalf("first call should fail soft: %+v", first)
	}
	if second := b.For(ctx, "tok"); second.Operator != "dev2" || calls != 2 {
		t.Errorf("failure was cached: %+v calls=%d", second, calls)
	}
}
