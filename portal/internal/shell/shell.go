// Package shell computes what the page chrome needs for one signed-in
// person: who they are, what role they hold, how many approvals wait for
// them, and where Gitea and Woodpecker live. The result is only used to
// decide what the sidebar shows; every action is still authorised on the
// server by its own handler.
package shell

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/exceptions"
)

type Role string

const (
	RoleDeveloper Role = "developer"
	RoleApprover  Role = "approver"
	RoleAdmin     Role = "admin"
)

type Shell struct {
	Operator         string
	Role             Role
	IsApprover       bool
	IsAdmin          bool
	PendingApprovals int
	GiteaURL         string
	WoodpeckerURL    string
}

func (s Shell) RoleLabel() string {
	switch s.Role {
	case RoleAdmin:
		return "Admin"
	case RoleApprover:
		return "Approver"
	default:
		return "Developer"
	}
}

// Identity is the slice of the Gitea client the shell needs, called with
// the signed-in user's own token.
type Identity interface {
	CurrentUser(ctx context.Context) (login string, isAdmin bool, err error)
	IsOnTeam(ctx context.Context, org, team string) (bool, error)
}

type cacheEntry struct {
	shell   Shell
	expires time.Time
}

type Builder struct {
	NewIdentity   func(token string) Identity
	Records       func(ctx context.Context) ([]exceptions.Record, error)
	Org, Team     string
	GiteaURL      string
	WoodpeckerURL string
	TTL           time.Duration
	Now           func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
}

func cacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// For builds the shell for one token. It never fails: any lookup that does
// not work simply leaves that part of the chrome out (no badge, developer
// view), so a Gitea hiccup cannot take a page down.
func (b *Builder) For(ctx context.Context, token string) Shell {
	key := cacheKey(token)
	now := b.Now()
	b.mu.Lock()
	if e, ok := b.cache[key]; ok && now.Before(e.expires) {
		b.mu.Unlock()
		return e.shell
	}
	b.mu.Unlock()

	s := Shell{Role: RoleDeveloper, GiteaURL: b.GiteaURL, WoodpeckerURL: b.WoodpeckerURL}
	id := b.NewIdentity(token)
	login, admin, err := id.CurrentUser(ctx)
	if err != nil {
		// A transient Gitea failure must not stick for a whole TTL.
		return s
	}
	s.Operator, s.IsAdmin = login, admin
	if member, err := id.IsOnTeam(ctx, b.Org, b.Team); err == nil {
		s.IsApprover = member
	}
	switch {
	case s.IsAdmin:
		s.Role = RoleAdmin
	case s.IsApprover:
		s.Role = RoleApprover
	}
	if s.IsApprover {
		if recs, err := b.Records(ctx); err == nil {
			s.PendingApprovals = exceptions.CountPending(recs, login, now)
		}
	}

	b.mu.Lock()
	if b.cache == nil {
		b.cache = map[string]cacheEntry{}
	}
	b.cache[key] = cacheEntry{shell: s, expires: now.Add(b.TTL)}
	b.mu.Unlock()
	return s
}

type ctxKey struct{}

func WithContext(ctx context.Context, s Shell) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext returns the shell the middleware stored, or a zero Shell
// (developer view, no links) when there is none.
func FromContext(ctx context.Context) Shell {
	s, _ := ctx.Value(ctxKey{}).(Shell)
	return s
}

// Middleware must run inside the authentication wrapper so the user's token
// is already in the request context.
func (b *Builder) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token, ok := auth.TokenFromContext(r.Context()); ok && token != "" {
			r = r.WithContext(WithContext(r.Context(), b.For(r.Context(), token)))
		}
		next(w, r)
	}
}
