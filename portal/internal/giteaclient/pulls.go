package giteaclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PRDetail is one pull request's identity and head commit.
type PRDetail struct {
	Number    int
	Title     string
	State     string
	HTMLURL   string
	Author    string
	HeadSHA   string
	CreatedAt time.Time
}

func toDetail(p giteaPR) PRDetail {
	return PRDetail{
		Number: p.Number, Title: p.Title, State: p.State, HTMLURL: p.HTMLURL,
		Author: p.User.Login, HeadSHA: p.Head.SHA, CreatedAt: p.CreatedAt,
	}
}

func statusError(method, path string, status int) error {
	if status >= 300 {
		return fmt.Errorf("gitea: %s %s: status %d", method, path, status)
	}
	return nil
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PRDetail, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d", owner, repo, number)
	var pr giteaPR
	status, err := c.do(ctx, http.MethodGet, path, nil, &pr)
	if err != nil {
		return PRDetail{}, err
	}
	if err := statusError(http.MethodGet, path, status); err != nil {
		return PRDetail{}, err
	}
	return toDetail(pr), nil
}

func (c *Client) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]PRDetail, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls?state=open&limit=50", owner, repo)
	var prs []giteaPR
	status, err := c.do(ctx, http.MethodGet, path, nil, &prs)
	if err != nil {
		return nil, err
	}
	if err := statusError(http.MethodGet, path, status); err != nil {
		return nil, err
	}
	out := make([]PRDetail, 0, len(prs))
	for _, p := range prs {
		out = append(out, toDetail(p))
	}
	return out, nil
}

// IssueComment is a comment on an issue or pull request.
type IssueComment struct {
	ID     int64
	Body   string
	Author string
}

func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error) {
	const pageSize = 50
	const maxPages = 20
	var out []IssueComment

	for page := 1; page <= maxPages; page++ {
		path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments?limit=%d&page=%d", owner, repo, number, pageSize, page)
		var raw []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		status, err := c.do(ctx, http.MethodGet, path, nil, &raw)
		if err != nil {
			return nil, err
		}
		if err := statusError(http.MethodGet, path, status); err != nil {
			return nil, err
		}

		// Append this page's comments
		for _, r := range raw {
			out = append(out, IssueComment{ID: r.ID, Body: r.Body, Author: r.User.Login})
		}

		// Stop if this page returned fewer than pageSize items
		if len(raw) < pageSize {
			break
		}
	}

	return out, nil
}

func (c *Client) sendBody(ctx context.Context, method, path, body string) error {
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}
	status, err := c.do(ctx, method, path, bytes.NewReader(payload), nil)
	if err != nil {
		return err
	}
	return statusError(method, path, status)
}

func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	return c.sendBody(ctx, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, number), body)
}

func (c *Client) EditIssueComment(ctx context.Context, owner, repo string, id int64, body string) error {
	return c.sendBody(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/repos/%s/%s/issues/comments/%d", owner, repo, id), body)
}
