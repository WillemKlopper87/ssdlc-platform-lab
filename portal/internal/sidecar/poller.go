package sidecar

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/report"
	"ssdlc-portal/internal/woodpeckerclient"
)

type Backends struct {
	Gitea interface {
		report.Gitea
		ListOpenPullRequests(ctx context.Context, owner, repo string) ([]giteaclient.PRDetail, error)
	}
	Woodpecker interface {
		report.Woodpecker
		ListRepos(ctx context.Context) ([]woodpeckerclient.Repo, error)
	}
}

// Latest is the poller's most recent view, for the projects endpoint. A zero
// GeneratedAt means no poll has completed yet.
type Latest struct {
	GeneratedAt time.Time
	PollOK      bool
	Repos       []string        // the org's repositories, in Woodpecker's order
	Failed      map[string]bool // repositories whose pull requests could not be listed
	Reports     []report.Report
}

type Poller struct {
	B     Backends
	Org   string
	Store *metrics.Store
	Now   func() time.Time
	Log   *log.Logger
	// AfterReport, when set, is called for every successfully built report
	// (the sticky-comment sync hooks in here). Its failures are its own to log.
	AfterReport func(ctx context.Context, r report.Report)

	// Last poll that reached the repository list. Once is only called from a
	// single goroutine (Run), so no locking is needed.
	lastReports []report.Report
	lastTime    time.Time

	// mu guards latest, which the HTTP handler reads while Once writes it.
	mu     sync.RWMutex
	latest Latest
}

// Latest returns a copy of the most recent snapshot; safe for concurrent use.
func (p *Poller) Latest() Latest {
	p.mu.RLock()
	defer p.mu.RUnlock()
	l := p.latest
	l.Repos = append([]string(nil), l.Repos...)
	l.Reports = append([]report.Report(nil), l.Reports...)
	l.Failed = make(map[string]bool, len(p.latest.Failed))
	for k, v := range p.latest.Failed {
		l.Failed[k] = v
	}
	return l
}

func (p *Poller) setLatest(l Latest) {
	p.mu.Lock()
	p.latest = l
	p.mu.Unlock()
}

// Once performs a single poll. It returns an error only when the repository
// list itself cannot be read; a single repository failing is logged, marks
// ssdlc_sidecar_poll_ok 0 and leaves the others intact.
func (p *Poller) Once(ctx context.Context) error {
	now := p.Now()
	repos, err := p.B.Woodpecker.ListRepos(ctx)
	if err != nil {
		// Keep serving the last good data (with its own timestamp) rather
		// than an empty snapshot that would read as "no open PRs".
		p.Store.Set(BuildSnapshot(p.lastReports, p.lastTime, false))
		prev := p.Latest()
		prev.PollOK = false
		p.setLatest(prev)
		return fmt.Errorf("sidecar: list repositories: %w", err)
	}

	var reports []report.Report
	var orgRepos []string
	failed := map[string]bool{}
	allOK := true
	prefix := p.Org + "/"
	for _, repo := range repos {
		if !strings.HasPrefix(repo.FullName, prefix) {
			continue
		}
		orgRepos = append(orgRepos, repo.FullName)
		owner, name, _ := strings.Cut(repo.FullName, "/")
		prs, err := p.B.Gitea.ListOpenPullRequests(ctx, owner, name)
		if err != nil {
			p.Log.Printf("poll: list PRs for %s: %v", repo.FullName, err)
			allOK = false
			failed[repo.FullName] = true
			continue
		}
		for _, pr := range prs {
			r, err := report.Build(ctx, p.B.Gitea, p.B.Woodpecker, owner, name, pr.Number, now)
			if err != nil {
				p.Log.Printf("poll: report for %s#%d: %v", repo.FullName, pr.Number, err)
				allOK = false
				failed[repo.FullName] = true
				continue
			}
			if r.FindingsUnavailable {
				allOK = false
			}
			reports = append(reports, r)
		}
	}
	p.lastReports, p.lastTime = reports, now
	p.setLatest(Latest{GeneratedAt: now, PollOK: allOK, Repos: orgRepos, Failed: failed, Reports: reports})
	p.Store.Set(BuildSnapshot(reports, now, allOK))
	// Hooks run after publication so a slow or panicking hook cannot hold
	// back metrics.
	if p.AfterReport != nil {
		for _, r := range reports {
			p.runHook(ctx, r)
		}
	}
	return nil
}

func (p *Poller) runHook(ctx context.Context, r report.Report) {
	defer func() {
		if rec := recover(); rec != nil {
			p.Log.Printf("poll: AfterReport panic for %s#%d: %v", r.Repo, r.Number, rec)
		}
	}()
	p.AfterReport(ctx, r)
}

// Run polls immediately and then every interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	for {
		if err := p.Once(ctx); err != nil {
			p.Log.Print(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
