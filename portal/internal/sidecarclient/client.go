// Package sidecarclient reads the reporting service's aggregated endpoints
// with the service token. It is optional: the portal only builds one when
// PORTAL_SIDECAR_URL is configured.
package sidecarclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ssdlc-portal/internal/projects"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

type ProjectsResponse struct {
	GeneratedAt time.Time          `json:"generated_at"`
	PollOK      bool               `json:"poll_ok"`
	Projects    []projects.Project `json:"projects"`
}

func (c *Client) Projects(ctx context.Context) (ProjectsResponse, error) {
	var out ProjectsResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/projects", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return out, fmt.Errorf("sidecar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return out, fmt.Errorf("sidecar: status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return out, fmt.Errorf("sidecar: decode: %w", err)
	}
	return out, nil
}
