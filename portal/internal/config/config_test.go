// portal/internal/config/config_test.go
package config

import (
	"strings"
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

func setRequiredEnv(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"PORTAL_GITEA_URL": "http://gitea:3500", "PORTAL_WOODPECKER_URL": "http://woodpecker:8000",
		"PORTAL_WOODPECKER_TOKEN": "w", "PORTAL_GITEA_ADMIN_TOKEN": "g",
		"PORTAL_OAUTH_CLIENT_ID": "x", "PORTAL_OAUTH_CLIENT_SECRET": "y",
		"PORTAL_SESSION_KEY": "0123456789abcdef0123456789abcdef", "PORTAL_EXCEPTIONS_REPO_OWNER": "ssdlc",
	} {
		t.Setenv(k, v)
	}
}

func TestLoad_SidecarBothSetTrimsSlash(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PORTAL_SIDECAR_URL", "http://sidecar:8282/")
	t.Setenv("PORTAL_SIDECAR_TOKEN", "t")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SidecarURL != "http://sidecar:8282" || cfg.SidecarToken != "t" {
		t.Errorf("sidecar: %q %q", cfg.SidecarURL, cfg.SidecarToken)
	}
}

func TestLoad_SidecarNeitherSetIsValid(t *testing.T) {
	setRequiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SidecarURL != "" || cfg.SidecarToken != "" {
		t.Errorf("want empty, got %q %q", cfg.SidecarURL, cfg.SidecarToken)
	}
}

func TestLoad_SidecarOnlyURLIsAnError(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PORTAL_SIDECAR_URL", "http://sidecar:8282")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "PORTAL_SIDECAR_TOKEN") {
		t.Fatalf("want error naming PORTAL_SIDECAR_TOKEN, got %v", err)
	}
}

func TestLoad_SidecarURLNeedsScheme(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PORTAL_SIDECAR_TOKEN", "t")
	for _, u := range []string{"sidecar:8282", "ftp://x"} {
		t.Setenv("PORTAL_SIDECAR_URL", u)
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "PORTAL_SIDECAR_URL must start with http") {
			t.Fatalf("%s: want error naming PORTAL_SIDECAR_URL and http, got %v", u, err)
		}
	}
	for _, u := range []string{"http://sidecar:8282", "https://x"} {
		t.Setenv("PORTAL_SIDECAR_URL", u)
		if _, err := Load(); err != nil {
			t.Errorf("%s should load: %v", u, err)
		}
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
