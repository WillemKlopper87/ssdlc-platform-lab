// portal/internal/auth/oauth_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ssdlc-portal/internal/config"
	"ssdlc-portal/internal/session"
)

func TestLogin_RedirectsToGiteaAuthorize(t *testing.T) {
	cfg := config.Config{
		GiteaURL:      "http://gitea.example",
		OAuthClientID: "client-123",
		SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
	}
	h := NewHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("bad Location header: %v", err)
	}
	if !strings.HasPrefix(loc.String(), "http://gitea.example/login/oauth/authorize") {
		t.Errorf("Location = %q, want it to start with the Gitea authorize endpoint", loc.String())
	}
	if loc.Query().Get("client_id") != "client-123" {
		t.Errorf("client_id = %q, want client-123", loc.Query().Get("client_id"))
	}
	if loc.Query().Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", loc.Query().Get("response_type"))
	}

	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("Location has no state param, want a non-empty CSRF state value")
	}

	var stateCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == stateCookieName {
			stateCookie = c
		}
	}
	if stateCookie == nil {
		t.Fatal("no oauth state cookie was set")
	}
	if stateCookie.Value != state {
		t.Errorf("state cookie value = %q, want it to match the state param %q", stateCookie.Value, state)
	}
	if !stateCookie.HttpOnly {
		t.Error("state cookie must be HttpOnly")
	}
	if stateCookie.SameSite != http.SameSiteLaxMode {
		t.Error("state cookie must be SameSite=Lax")
	}
}

func TestLogin_UsesConfiguredPublicURLForRedirectURI(t *testing.T) {
	cfg := config.Config{
		GiteaURL:      "http://gitea.example",
		OAuthClientID: "client-123",
		SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
		PublicURL:     "https://portal.example.com",
	}
	h := NewHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Host = "attacker-controlled.example" // must be ignored when PublicURL is set
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("bad Location header: %v", err)
	}
	want := "https://portal.example.com/oauth/callback"
	if got := loc.Query().Get("redirect_uri"); got != want {
		t.Errorf("redirect_uri = %q, want %q (derived from PublicURL, not Host)", got, want)
	}
}

func TestCallback_ExchangesCodeAndSetsSessionCookie(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/oauth/access_token" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"gto_realtoken123","token_type":"bearer","expires_in":3600}`))
	}))
	defer fakeGitea.Close()

	cfg := config.Config{
		GiteaURL:          fakeGitea.URL,
		OAuthClientID:     "client-123",
		OAuthClientSecret: "secret-456",
		SessionKey:        []byte("0123456789abcdef0123456789abcdef"),
	}
	h := NewHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=abc123&state=matching-state", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "matching-state"})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == cookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie was set")
	}
	token, err := session.Decode(sessionCookie.Value, cfg.SessionKey)
	if err != nil {
		t.Fatalf("could not decode session cookie: %v", err)
	}
	if token != "gto_realtoken123" {
		t.Errorf("decoded token = %q, want gto_realtoken123", token)
	}
	if !sessionCookie.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Error("session cookie must be SameSite=Lax")
	}
}

func TestCallback_RejectsMismatchedState(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("token exchange must not happen when state does not match")
	}))
	defer fakeGitea.Close()

	cfg := config.Config{
		GiteaURL:          fakeGitea.URL,
		OAuthClientID:     "client-123",
		OAuthClientSecret: "secret-456",
		SessionKey:        []byte("0123456789abcdef0123456789abcdef"),
	}
	h := NewHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=abc123&state=attacker-state", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "victim-state"})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			t.Error("no session cookie should be set when state does not match")
		}
	}
}

func TestCallback_RejectsMissingStateCookie(t *testing.T) {
	fakeGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("token exchange must not happen when there is no state cookie")
	}))
	defer fakeGitea.Close()

	cfg := config.Config{
		GiteaURL:          fakeGitea.URL,
		OAuthClientID:     "client-123",
		OAuthClientSecret: "secret-456",
		SessionKey:        []byte("0123456789abcdef0123456789abcdef"),
	}
	h := NewHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=abc123&state=whatever", nil)
	// No state cookie added.
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			t.Error("no session cookie should be set when there is no state cookie")
		}
	}
}

func TestRequireAuth_NoCookieRedirectsToLogin(t *testing.T) {
	cfg := config.Config{SessionKey: []byte("0123456789abcdef0123456789abcdef")}
	h := NewHandler(cfg)

	called := false
	protected := h.RequireAuth(func(w http.ResponseWriter, r *http.Request) { called = true })

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	protected(rec, req)

	if called {
		t.Error("the protected handler ran without a valid session")
	}
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect to login)", rec.Code, http.StatusFound)
	}
}

func TestRequireAuth_ValidCookiePassesTokenThroughContext(t *testing.T) {
	cfg := config.Config{SessionKey: []byte("0123456789abcdef0123456789abcdef")}
	h := NewHandler(cfg)

	encoded, err := session.Encode("gto_realtoken123", cfg.SessionKey)
	if err != nil {
		t.Fatal(err)
	}

	var gotToken string
	protected := h.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		gotToken, _ = TokenFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: encoded})
	rec := httptest.NewRecorder()
	protected(rec, req)

	if gotToken != "gto_realtoken123" {
		t.Errorf("token in context = %q, want gto_realtoken123", gotToken)
	}
}
