package giteaclient

import (
	"fmt"
	"net/url"
)

// escapeSegment makes one caller-supplied value safe to use as a single URL
// path segment. "." and ".." are refused because PathEscape leaves them
// unchanged and they would be resolved as relative path steps upstream.
func escapeSegment(s string) (string, error) {
	if s == "." || s == ".." {
		return "", fmt.Errorf("gitea: invalid path segment %q", s)
	}
	return url.PathEscape(s), nil
}

// repoPath returns "/api/v1/repos/<owner>/<repo>" with both segments escaped.
func repoPath(owner, repo string) (string, error) {
	o, err := escapeSegment(owner)
	if err != nil {
		return "", err
	}
	r, err := escapeSegment(repo)
	if err != nil {
		return "", err
	}
	return "/api/v1/repos/" + o + "/" + r, nil
}
