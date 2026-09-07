// portal/internal/auth/oauth.go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"ssdlc-portal/internal/config"
	"ssdlc-portal/internal/session"
)

const cookieName = "ssdlc_portal_session"

type contextKey int

const ContextKeyToken contextKey = iota

type Handler struct {
	cfg config.Config
}

func NewHandler(cfg config.Config) *Handler { return &Handler{cfg: cfg} }

// Login redirects to Gitea's own OAuth authorize endpoint. There is no
// portal-side password form; the portal never sees or stores a Gitea
// password (spec: "Login — Gitea OAuth button only").
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	redirectURI := fmt.Sprintf("%s://%s/oauth/callback", scheme(r), r.Host)
	q := url.Values{
		"client_id":     {h.cfg.OAuthClientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
	}
	http.Redirect(w, r, h.cfg.GiteaURL+"/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

// Callback exchanges the authorization code for an access token and seals
// it into the session cookie. The token itself is the only thing the
// portal stores about the operator — no separate user record.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	redirectURI := fmt.Sprintf("%s://%s/oauth/callback", scheme(r), r.Host)

	form := url.Values{
		"client_id":     {h.cfg.OAuthClientID},
		"client_secret": {h.cfg.OAuthClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
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
