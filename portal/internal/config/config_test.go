// portal/internal/config/config_test.go
package config

import (
	"testing"
)

func TestLoad_MissingRequiredVar(t *testing.T) {
	t.Setenv("PORTAL_GITEA_URL", "")
	t.Setenv("PORTAL_WOODPECKER_URL", "http://127.0.0.1:8000")
	t.Setenv("PORTAL_OAUTH_CLIENT_ID", "x")
	t.Setenv("PORTAL_OAUTH_CLIENT_SECRET", "y")
	t.Setenv("PORTAL_SESSION_KEY", "0123456789abcdef0123456789abcdef")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error when PORTAL_GITEA_URL is unset, got nil")
	}
}

func TestLoad_SessionKeyTooShort(t *testing.T) {
	t.Setenv("PORTAL_GITEA_URL", "http://127.0.0.1:3500")
	t.Setenv("PORTAL_WOODPECKER_URL", "http://127.0.0.1:8000")
	t.Setenv("PORTAL_OAUTH_CLIENT_ID", "x")
	t.Setenv("PORTAL_OAUTH_CLIENT_SECRET", "y")
	t.Setenv("PORTAL_SESSION_KEY", "tooshort")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error for a session key under 32 bytes, got nil")
	}
}

func TestLoad_Success(t *testing.T) {
	t.Setenv("PORTAL_GITEA_URL", "http://127.0.0.1:3500")
	t.Setenv("PORTAL_WOODPECKER_URL", "http://127.0.0.1:8000")
	t.Setenv("PORTAL_WOODPECKER_TOKEN", "wp-token")
	t.Setenv("PORTAL_GITEA_ADMIN_TOKEN", "gitea-admin-token")
	t.Setenv("PORTAL_OAUTH_CLIENT_ID", "x")
	t.Setenv("PORTAL_OAUTH_CLIENT_SECRET", "y")
	t.Setenv("PORTAL_SESSION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("PORTAL_APPROVER_TEAM", "security-officers")
	t.Setenv("PORTAL_EXCEPTIONS_REPO_OWNER", "gateadmin")
	t.Setenv("PORTAL_EXCEPTIONS_REPO_NAME", "exceptions")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GiteaURL != "http://127.0.0.1:3500" {
		t.Errorf("GiteaURL = %q", cfg.GiteaURL)
	}
	if len(cfg.SessionKey) != 32 {
		t.Errorf("SessionKey length = %d, want 32", len(cfg.SessionKey))
	}
}

func TestLoad_PublicURLsDefaultAndOverride(t *testing.T) {
	base := map[string]string{
		"PORTAL_GITEA_URL": "http://gitea:3500/", "PORTAL_WOODPECKER_URL": "http://woodpecker:8000",
		"PORTAL_WOODPECKER_TOKEN": "w", "PORTAL_GITEA_ADMIN_TOKEN": "g",
		"PORTAL_OAUTH_CLIENT_ID": "x", "PORTAL_OAUTH_CLIENT_SECRET": "y",
		"PORTAL_SESSION_KEY": "0123456789abcdef0123456789abcdef", "PORTAL_EXCEPTIONS_REPO_OWNER": "ssdlc",
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GiteaPublicURL != "http://gitea:3500" || cfg.WoodpeckerPublicURL != "http://woodpecker:8000" {
		t.Errorf("defaults: %q %q", cfg.GiteaPublicURL, cfg.WoodpeckerPublicURL)
	}

	t.Setenv("PORTAL_GITEA_PUBLIC_URL", "http://192.168.1.28:3500/")
	t.Setenv("PORTAL_WOODPECKER_PUBLIC_URL", "http://192.168.1.28:8000")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GiteaPublicURL != "http://192.168.1.28:3500" || cfg.WoodpeckerPublicURL != "http://192.168.1.28:8000" {
		t.Errorf("overrides: %q %q", cfg.GiteaPublicURL, cfg.WoodpeckerPublicURL)
	}
}
