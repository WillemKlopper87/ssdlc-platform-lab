package giteaclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) (status int, err error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("gitea: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("gitea: decode %s %s: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

// RawGet exposes the client's authenticated GET for endpoints not worth a
// dedicated typed method.
func (c *Client) RawGet(ctx context.Context, path string, out any) (int, error) {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) Username(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/user", nil, &user); err != nil {
		return "", err
	}
	return user.Login, nil
}

type PullRequest struct {
	Number     int
	Title      string
	HTMLURL    string
	State      string
	Author     string
	Repo       string
	HeadSHA    string
	HeadBranch string
	CreatedAt  time.Time
}

type giteaPR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	CreatedAt time.Time `json:"created_at"`
}

// ListMyPullRequests lists open PRs across every repo the authenticated
// operator can see. Gitea has no single "my PRs across all repos"
// endpoint, so this lists searchable repos, then lists PRs per repo — an
// N+1 pattern that's fine at this fleet's size and matches the portal's
// "no owned state, call the API live" constraint rather than caching.
func (c *Client) ListMyPullRequests(ctx context.Context) ([]PullRequest, error) {
	var repos struct {
		Data []struct {
			FullName string `json:"full_name"`
		} `json:"data"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/repos/search", nil, &repos); err != nil {
		return nil, err
	}

	var all []PullRequest
	for _, repo := range repos.Data {
		var prs []giteaPR
		if _, err := c.do(ctx, http.MethodGet, "/api/v1/repos/"+repo.FullName+"/pulls", nil, &prs); err != nil {
			return nil, err
		}
		for _, pr := range prs {
			all = append(all, PullRequest{
				Number: pr.Number, Title: pr.Title, HTMLURL: pr.HTMLURL, State: pr.State,
				Author: pr.User.Login, Repo: repo.FullName,
				HeadSHA: pr.Head.SHA, HeadBranch: pr.Head.Ref, CreatedAt: pr.CreatedAt,
			})
		}
	}
	return all, nil
}

type CommitStatus struct {
	State   string
	Context string
}

func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, sha string) ([]CommitStatus, error) {
	var resp struct {
		Statuses []struct {
			Status  string `json:"status"`
			Context string `json:"context"`
		} `json:"statuses"`
	}
	base, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/commits/%s/status", base, url.PathEscape(sha))
	if _, err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]CommitStatus, 0, len(resp.Statuses))
	for _, s := range resp.Statuses {
		out = append(out, CommitStatus{State: s.Status, Context: s.Context})
	}
	return out, nil
}

type Team struct {
	Name         string
	Organization string
}

func (c *Client) ListMyTeams(ctx context.Context) ([]Team, error) {
	var teams []struct {
		Name string `json:"name"`
		Org  struct {
			Username string `json:"username"`
		} `json:"organization"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/user/teams", nil, &teams); err != nil {
		return nil, err
	}
	out := make([]Team, 0, len(teams))
	for _, t := range teams {
		out = append(out, Team{Name: t.Name, Organization: t.Org.Username})
	}
	return out, nil
}

func (c *Client) IsOnTeam(ctx context.Context, org, teamName string) (bool, error) {
	teams, err := c.ListMyTeams(ctx)
	if err != nil {
		return false, err
	}
	for _, t := range teams {
		if t.Organization == org && t.Name == teamName {
			return true, nil
		}
	}
	return false, nil
}

// GetFileContent returns a repo file's content and its blob SHA (needed
// to update it later without a lost-update race). A 404 is not an error
// here — "the file doesn't exist yet" is an expected, common case for the
// exceptions repo — it's reported as (nil, "", nil).
func (c *Client) GetFileContent(ctx context.Context, owner, repo, path string) ([]byte, string, error) {
	var resp struct {
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	status, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", owner, repo, path), nil, &resp)
	if err != nil {
		return nil, "", err
	}
	if status == http.StatusNotFound {
		return nil, "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.Content)
	if err != nil {
		return nil, "", fmt.Errorf("gitea: decode file content: %w", err)
	}
	return decoded, resp.SHA, nil
}

// PutFileContent creates or updates a file via a single commit. Pass
// existingSHA == "" to create a new file; pass the SHA from a prior
// GetFileContent to update one — Gitea rejects an update whose SHA
// doesn't match the current blob, which is the platform's optimistic-
// concurrency guard against two approvals racing on the same file.
func (c *Client) PutFileContent(ctx context.Context, owner, repo, path string, content []byte, message, existingSHA string) error {
	payload := map[string]any{
		"content": base64.StdEncoding.EncodeToString(content),
		"message": message,
		"branch":  "main",
	}
	if existingSHA != "" {
		payload["sha"] = existingSHA
	}
	body, _ := json.Marshal(payload)
	method := http.MethodPost
	if existingSHA != "" {
		method = http.MethodPut
	}
	status, err := c.do(ctx, method, fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", owner, repo, path), bytes.NewReader(body), nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("gitea: put file content: unexpected status %d", status)
	}
	return nil
}

// EnsureRepo creates owner/name if it doesn't already exist. Idempotent —
// safe to call on every portal startup or exceptions-repo access.
func (c *Client) EnsureRepo(ctx context.Context, owner, name string) error {
	status, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s", owner, name), nil, nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"name":      name,
		"private":   false,
		"auto_init": true,
	})
	status, err = c.do(ctx, http.MethodPost, "/api/v1/user/repos", bytes.NewReader(body), nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("gitea: create repo %s/%s: unexpected status %d", owner, name, status)
	}
	return nil
}

// ListDirectory returns every file path directly inside a repo directory
// (non-recursive — sufficient for the exceptions repo's flat layout). A
// 404 (directory doesn't exist) is reported as an empty slice, not an
// error, matching GetFileContent's treatment of a missing file.
func (c *Client) ListDirectory(ctx context.Context, owner, repo, dir string) ([]string, error) {
	var entries []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	status, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s", owner, repo, dir), nil, &entries)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	var out []string
	for _, e := range entries {
		if e.Type == "file" {
			out = append(out, e.Path)
		}
	}
	return out, nil
}
