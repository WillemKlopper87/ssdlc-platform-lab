package sidecar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	GiteaURL, GiteaToken           string
	WoodpeckerURL, WoodpeckerToken string
	Org, ListenAddr, APIToken      string
	PortalURL, CommentUser         string
	PollInterval                   time.Duration
	Comments                       bool
}

// LoadConfig fails closed: a missing or weak value is a startup error, never
// a silent default, matching the portal's own configuration rules.
func LoadConfig(getenv func(string) string) (Config, error) {
	var problems []string
	required := func(name string) string {
		v := getenv(name)
		if v == "" {
			problems = append(problems, name+" is required")
		}
		return v
	}
	orDefault := func(name, def string) string {
		if v := getenv(name); v != "" {
			return v
		}
		return def
	}

	c := Config{
		GiteaURL:        strings.TrimRight(required("SIDECAR_GITEA_URL"), "/"),
		GiteaToken:      required("SIDECAR_GITEA_TOKEN"),
		WoodpeckerURL:   strings.TrimRight(required("SIDECAR_WOODPECKER_URL"), "/"),
		WoodpeckerToken: required("SIDECAR_WOODPECKER_TOKEN"),
		APIToken:        required("SIDECAR_API_TOKEN"),
		Org:             orDefault("SIDECAR_ORG", "ssdlc"),
		ListenAddr:      orDefault("SIDECAR_LISTEN_ADDR", ":8282"),
		PortalURL:       strings.TrimRight(getenv("SIDECAR_PORTAL_URL"), "/"),
		CommentUser:     orDefault("SIDECAR_COMMENT_USER", "gate-reporter"),
	}
	comments, err := strconv.ParseBool(orDefault("SIDECAR_COMMENTS", "true"))
	if err != nil {
		problems = append(problems, "SIDECAR_COMMENTS must be a boolean (true/false)")
	}
	c.Comments = comments
	if c.APIToken != "" && len(c.APIToken) < 16 {
		problems = append(problems, "SIDECAR_API_TOKEN must be at least 16 characters")
	}
	interval, err := time.ParseDuration(orDefault("SIDECAR_POLL_INTERVAL", "60s"))
	if err != nil || interval < 5*time.Second {
		problems = append(problems, "SIDECAR_POLL_INTERVAL must be a duration of at least 5s")
	}
	c.PollInterval = interval

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("sidecar configuration is invalid:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return c, nil
}
