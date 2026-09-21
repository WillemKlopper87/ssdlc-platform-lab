package sidecar

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func validEnv() map[string]string {
	return map[string]string{
		"SIDECAR_GITEA_URL": "http://g:3500", "SIDECAR_GITEA_TOKEN": "t1",
		"SIDECAR_WOODPECKER_URL": "http://w:8000", "SIDECAR_WOODPECKER_TOKEN": "t2",
		"SIDECAR_API_TOKEN": "0123456789abcdef0123",
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(env(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Org != "ssdlc" || c.ListenAddr != ":8282" || c.PollInterval != 60*time.Second || !c.Comments || c.CommentUser != "gate-reporter" {
		t.Errorf("defaults wrong: %+v", c)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	m := validEnv()
	m["SIDECAR_ORG"], m["SIDECAR_POLL_INTERVAL"], m["SIDECAR_COMMENTS"] = "acme", "30s", "false"
	c, err := LoadConfig(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.Org != "acme" || c.PollInterval != 30*time.Second || c.Comments {
		t.Errorf("overrides wrong: %+v", c)
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	m := validEnv()
	delete(m, "SIDECAR_GITEA_TOKEN")
	m["SIDECAR_API_TOKEN"] = "short"
	m["SIDECAR_POLL_INTERVAL"] = "1s"
	_, err := LoadConfig(env(m))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"SIDECAR_GITEA_TOKEN", "SIDECAR_API_TOKEN", "SIDECAR_POLL_INTERVAL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}
