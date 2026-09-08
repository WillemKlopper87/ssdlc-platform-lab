// Package exceptions bootstraps and manages the exceptions repo -- the one
// place in the SSDLC portal where durable state is written. State lives
// entirely as Gitea commits to a dedicated "exceptions" repo, not in a
// portal-side database.
package exceptions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ssdlc-portal/internal/giteaclient"
)

type Record struct {
	Repo               string    `json:"repo"`
	FindingFingerprint string    `json:"finding_fingerprint"`
	Severity           string    `json:"severity"`
	Expiry             time.Time `json:"expiry"`
	Requester          string    `json:"requester"`
	Approvers          []string  `json:"approvers"`
	Ticket             string    `json:"ticket"`
	Approved           bool      `json:"approved"`

	// Path is the Gitea repo path this record was actually read from
	// (populated by List). It is deliberately excluded from the persisted
	// JSON -- it describes where the record lives, not what it is. Write
	// uses it, when set, to overwrite the exact file a record came from
	// instead of recomputing RecordPath, so a record whose file predates
	// (or otherwise doesn't match) the current content-addressed naming
	// scheme still gets updated in place rather than duplicated under a
	// second path.
	Path string `json:"-"`
}

const maxExpiry = 90 * 24 * time.Hour

// ValidateExpiry enforces framework §3's hard cap, server-side -- not just
// a form's max attribute, which a direct API call would bypass.
func ValidateExpiry(expiry, now time.Time) error {
	if expiry.Before(now) {
		return fmt.Errorf("exceptions: expiry %s is already in the past", expiry)
	}
	if expiry.After(now.Add(maxExpiry)) {
		return fmt.Errorf("exceptions: expiry %s is more than 90 days from now", expiry)
	}
	return nil
}

// RecordPath derives a deterministic, filesystem-safe filename from a
// record's repo + fingerprint so Write is idempotent (re-approving the
// same finding updates the same file rather than creating a duplicate).
func RecordPath(r Record) string {
	sum := sha256.Sum256([]byte(r.Repo + "|" + r.FindingFingerprint))
	return hex.EncodeToString(sum[:]) + ".json"
}

type Store struct {
	gitea *giteaclient.Client
	owner string
	repo  string
}

func NewStore(gitea *giteaclient.Client, owner, repo string) *Store {
	return &Store{gitea: gitea, owner: owner, repo: repo}
}

func (s *Store) Ensure(ctx context.Context) error {
	return s.gitea.EnsureRepo(ctx, s.owner, s.repo)
}

// Write commits r to its deterministic path. Because RecordPath is
// content-addressed by repo+fingerprint, this naturally overwrites a
// prior record for the same finding rather than creating a second one
// (used both for the initial request and for the later approval, which
// updates the same file with Approved=true and an appended approver).
func (s *Store) Write(ctx context.Context, r Record) error {
	path := r.Path
	if path == "" {
		path = RecordPath(r)
	}
	_, existingSHA, err := s.gitea.GetFileContent(ctx, s.owner, s.repo, path)
	if err != nil {
		return fmt.Errorf("exceptions: check existing record: %w", err)
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("exceptions: marshal record: %w", err)
	}
	message := fmt.Sprintf("exception: %s for %s (requested by %s)", r.FindingFingerprint, r.Repo, r.Requester)
	if r.Approved {
		message = fmt.Sprintf("exception: %s for %s (approved by %s)", r.FindingFingerprint, r.Repo, strings.Join(r.Approvers, ", "))
	}
	return s.gitea.PutFileContent(ctx, s.owner, s.repo, path, body, message, existingSHA)
}

// List reads every exception record currently committed, using
// giteaclient's directory listing rather than requiring the caller to
// already know every path (there is no other way to discover records
// written by someone else's request).
func (s *Store) List(ctx context.Context) ([]Record, error) {
	paths, err := s.gitea.ListDirectory(ctx, s.owner, s.repo, "")
	if err != nil {
		return nil, fmt.Errorf("exceptions: list directory: %w", err)
	}
	var out []Record
	for _, p := range paths {
		content, _, err := s.gitea.GetFileContent(ctx, s.owner, s.repo, p)
		if err != nil {
			return nil, fmt.Errorf("exceptions: read %s: %w", p, err)
		}
		if content == nil {
			continue
		}
		var r Record
		if err := json.Unmarshal(content, &r); err != nil {
			return nil, fmt.Errorf("exceptions: parse %s: %w", p, err)
		}
		r.Path = p
		out = append(out, r)
	}
	return out, nil
}
