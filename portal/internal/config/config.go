// portal/internal/config/config.go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	GiteaURL            string
	WoodpeckerURL       string
	WoodpeckerToken     string
	OAuthClientID       string
	OAuthClientSecret   string
	SessionKey          []byte
	ApproverTeam        string
	ExceptionsRepoOwner string
	ExceptionsRepoName  string
	ListenAddr          string
	// PublicURL, when set, is the portal's externally-reachable base URL
	// (e.g. "https://portal.example.com"). It is optional: when unset, the
	// OAuth redirect_uri is derived from the incoming request's Host header
	// instead (see internal/auth). Setting it avoids trusting a
	// proxy-forwarded Host header for the OAuth redirect_uri.
	PublicURL string
}

// Load reads the portal's configuration from the environment. It fails
// closed: a missing required variable or a too-short session key is a
// startup error, never a silent default, matching this platform's own
// "missing/placeholder secret is fatal" discipline elsewhere in the repo.
func Load() (Config, error) {
	var problems []string

	get := func(name string) string { return os.Getenv(name) }
	required := func(name string) string {
		v := get(name)
		if v == "" {
			problems = append(problems, fmt.Sprintf("%s is required", name))
		}
		return v
	}

	cfg := Config{
		GiteaURL:            required("PORTAL_GITEA_URL"),
		WoodpeckerURL:       required("PORTAL_WOODPECKER_URL"),
		WoodpeckerToken:     required("PORTAL_WOODPECKER_TOKEN"),
		OAuthClientID:       required("PORTAL_OAUTH_CLIENT_ID"),
		OAuthClientSecret:   required("PORTAL_OAUTH_CLIENT_SECRET"),
		ApproverTeam:        get("PORTAL_APPROVER_TEAM"),
		ExceptionsRepoOwner: required("PORTAL_EXCEPTIONS_REPO_OWNER"),
		ExceptionsRepoName:  get("PORTAL_EXCEPTIONS_REPO_NAME"),
		ListenAddr:          get("PORTAL_LISTEN_ADDR"),
		PublicURL:           get("PORTAL_PUBLIC_URL"),
	}
	if cfg.ExceptionsRepoName == "" {
		cfg.ExceptionsRepoName = "exceptions"
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8181"
	}

	sessionKey := required("PORTAL_SESSION_KEY")
	if sessionKey != "" && len(sessionKey) < 32 {
		problems = append(problems, "PORTAL_SESSION_KEY must be at least 32 bytes")
	}
	cfg.SessionKey = []byte(sessionKey)

	if len(problems) > 0 {
		msg := "portal configuration is invalid:"
		for _, p := range problems {
			msg += "\n  - " + p
		}
		return Config{}, fmt.Errorf("%s", msg)
	}
	return cfg, nil
}
