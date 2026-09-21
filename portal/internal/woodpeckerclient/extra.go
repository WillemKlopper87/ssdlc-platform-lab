package woodpeckerclient

import (
	"context"
	"fmt"
	"time"
)

// Repo is a repository activated in Woodpecker.
type Repo struct {
	ID       int
	FullName string
}

// ListRepos returns the repositories the token's user can see that are
// active in Woodpecker.
func (c *Client) ListRepos(ctx context.Context) ([]Repo, error) {
	var raw []struct {
		ID       int    `json:"id"`
		FullName string `json:"full_name"`
	}
	if err := c.get(ctx, "/api/user/repos", &raw); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(raw))
	for _, r := range raw {
		out = append(out, Repo{ID: r.ID, FullName: r.FullName})
	}
	return out, nil
}

// LookupRepo resolves Woodpecker's own repo ID for owner/repo. Gitea's repo
// ID is a different number and must never be used here.
func (c *Client) LookupRepo(ctx context.Context, owner, repo string) (int, error) {
	var raw struct {
		ID int `json:"id"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/repos/lookup/%s/%s", owner, repo), &raw); err != nil {
		return 0, err
	}
	if raw.ID == 0 {
		return 0, fmt.Errorf("woodpecker: repo %s/%s not found or not active", owner, repo)
	}
	return raw.ID, nil
}

// Agent is a registered pipeline agent.
type Agent struct {
	Name        string
	LastContact time.Time
}

// ListAgents needs an admin token.
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	var raw []struct {
		Name        string `json:"name"`
		LastContact int64  `json:"last_contact"`
	}
	if err := c.get(ctx, "/api/agents", &raw); err != nil {
		return nil, err
	}
	out := make([]Agent, 0, len(raw))
	for _, a := range raw {
		out = append(out, Agent{Name: a.Name, LastContact: time.Unix(a.LastContact, 0)})
	}
	return out, nil
}
