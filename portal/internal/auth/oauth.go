// portal/internal/auth/oauth.go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ssdlc-portal/internal/config"
	"ssdlc-portal/internal/session"
)

const cookieName = "ssdlc_portal_session"

// stateCookieName holds the short-lived CSRF state value between Login and
// Callback. It is a separate cookie from the session cookie so it can carry
// its own (much shorter) lifetime and be cleared independently.
const stateCookieName = "ssdlc_portal_oauth_state"

// stateCookieMaxAge bounds how long a user has to complete the OAuth
// round trip with Gitea before the state value expires.
const stateCookieMaxAge = 300 // seconds

type contextKey int

const ContextKeyToken contextKey = iota

type Handler struct {
	cfg config.Config
}

func NewHandler(cfg config.Config) *Handler { return &Handler{cfg: cfg} }

// newState generates a cryptographically random, URL-safe CSRF state token.
func newState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate state: %w", err)
	}
	return base64.URLEncoding.EncodeToString(buf), nil
}

// clearStateCookie removes the OAuth state cookie so it cannot be replayed,
// regardless of whether the callback succeeded or failed.
func clearStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// redirectURI computes the OAuth redirect_uri that both Login and Callback
// must agree on. When cfg.PublicURL is configured it is used verbatim (this
// is the expected production setup, and avoids trusting a proxy-forwarded
// Host header); otherwise it falls back to deriving the value from the
// incoming request, which keeps local development working without any
// extra configuration.
func (h *Handler) redirectURI(r *http.Request) string {
	if h.cfg.PublicURL != "" {
		return strings.TrimSuffix(h.cfg.PublicURL, "/") + "/oauth/callback"
	}
	return fmt.Sprintf("%s://%s/oauth/callback", scheme(r), r.Host)
}

// Login redirects to Gitea's own OAuth authorize endpoint. There is no
// portal-side password form; the portal never sees or stores a Gitea
// password (spec: "Login — Gitea OAuth button only").
//
// It also generates a random CSRF state value, stores it in a short-lived
// cookie, and includes it in the authorize request so Callback can verify
// the request it's completing was actually initiated by this browser.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	state, err := newState()
	if err != nil {
		http.Error(w, "could not start login", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   stateCookieMaxAge,
	})

	q := url.Values{
		"client_id":     {h.cfg.OAuthClientID},
		"redirect_uri":  {h.redirectURI(r)},
		"response_type": {"code"},
		"state":         {state},
	}
	http.Redirect(w, r, h.cfg.GiteaURL+"/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

// Callback exchanges the authorization code for an access token and seals
// it into the session cookie. The token itself is the only thing the
// portal stores about the operator — no separate user record.
//
// Before doing anything else it verifies the OAuth `state` query parameter
// against the value stashed in the state cookie by Login, using a
// constant-time comparison. This is what makes the flow CSRF-resistant: an
// attacker can send a victim a callback URL with a code bound to the
// attacker's own session, but cannot forge the victim's state cookie, so
// the comparison fails and the exchange never happens. The state cookie is
// cleared either way so it can't be replayed.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	// Read and validate the state before writing any response headers, then
	// clear the state cookie immediately — it must not survive this request
	// whether validation succeeds or fails, and Set-Cookie has no effect
	// once http.Error/http.Redirect has written the status line.
	stateParam := r.URL.Query().Get("state")
	stateCookie, cookieErr := r.Cookie(stateCookieName)
	clearStateCookie(w, r)

	if cookieErr != nil || stateCookie.Value == "" {
		http.Error(w, "missing or expired oauth state", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(stateParam), []byte(stateCookie.Value)) != 1 {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	form := url.Values{
		"client_id":     {h.cfg.OAuthClientID},
		"client_secret": {h.cfg.OAuthClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {h.redirectURI(r)},
	}
	resp, err := http.PostForm(h.cfg.GiteaURL+"/login/oauth/access_token", form)
	if err != nil {
		http.Error(w, "could not reach Gitea: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		http.Error(w, "Gitea did not return an access token", http.StatusBadGateway)
		return
	}

	encoded, err := session.Encode(tok.AccessToken, h.cfg.SessionKey)
	if err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// RequireAuth decodes the session cookie and, on success, injects the
// operator's real Gitea token into the request context so every
// downstream handler can call the Gitea/Woodpecker APIs as that operator
// — never as a shared service account. On any failure it redirects to
// /login rather than guessing at a degraded identity.
func (h *Handler) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		token, err := session.Decode(cookie.Value, h.cfg.SessionKey)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), ContextKeyToken, token)
		next(w, r.WithContext(ctx))
	}
}

func TokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(ContextKeyToken).(string)
	return token, ok
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
