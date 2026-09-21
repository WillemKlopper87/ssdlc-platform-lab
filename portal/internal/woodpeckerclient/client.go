package woodpeckerclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("woodpecker: GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("woodpecker: GET %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type Pipeline struct {
	Number   int
	Status   string
	Commit   string
	Event    string
	Started  int64
	Finished int64
}

func (c *Client) ListPipelines(ctx context.Context, repoID int) ([]Pipeline, error) {
	var raw []struct {
		Number   int    `json:"number"`
		Status   string `json:"status"`
		Commit   string `json:"commit"`
		Event    string `json:"event"`
		Started  int64  `json:"started"`
		Finished int64  `json:"finished"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/repos/%d/pipelines", repoID), &raw); err != nil {
		return nil, err
	}
	out := make([]Pipeline, 0, len(raw))
	for _, p := range raw {
		out = append(out, Pipeline{
			Number: p.Number, Status: p.Status, Commit: p.Commit, Event: p.Event,
			Started: p.Started, Finished: p.Finished,
		})
	}
	return out, nil
}

type Step struct {
	ID    int
	Name  string
	State string
}

func (c *Client) ListSteps(ctx context.Context, repoID, pipelineNumber int) ([]Step, error) {
	var raw struct {
		Workflows []struct {
			Children []struct {
				ID    int    `json:"id"`
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"children"`
		} `json:"workflows"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/repos/%d/pipelines/%d", repoID, pipelineNumber), &raw); err != nil {
		return nil, err
	}
	var out []Step
	for _, wf := range raw.Workflows {
		for _, s := range wf.Children {
			out = append(out, Step{ID: s.ID, Name: s.Name, State: s.State})
		}
	}
	return out, nil
}

// GetStepLog returns a step's full log as plain text, one line per log
// entry. Woodpecker's log API returns a JSON array of {"data": "<base64>"}
// entries, confirmed live against a real 3.18.0 server — not plain text
// and not one JSON object, both of which look plausible until you check.
func (c *Client) GetStepLog(ctx context.Context, repoID, pipelineNumber, stepID int) (string, error) {
	var entries []struct {
		Data string `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/repos/%d/logs/%d/%d", repoID, pipelineNumber, stepID), &entries); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		decoded, err := base64.StdEncoding.DecodeString(e.Data)
		if err != nil {
			continue // a single malformed line shouldn't hide the rest of a real log
		}
		b.Write(decoded)
	}
	return b.String(), nil
}
