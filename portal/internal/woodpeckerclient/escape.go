package woodpeckerclient

import (
	"fmt"
	"net/url"
)

// escapeSegment makes one caller-supplied value safe to use as a single URL
// path segment. "." and ".." are refused because PathEscape leaves them
// unchanged and they would be resolved as relative path steps upstream.
func escapeSegment(s string) (string, error) {
	if s == "." || s == ".." {
		return "", fmt.Errorf("woodpecker: invalid path segment %q", s)
	}
	return url.PathEscape(s), nil
}
