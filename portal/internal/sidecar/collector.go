package sidecar

import (
	"time"

	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/report"
)

// BuildSnapshot turns the current open-PR reports into gauges. Everything is
// recomputed from Gitea/Woodpecker on each poll, so the service holds no state
// that can be lost across a restart.
func BuildSnapshot(reports []report.Report, now time.Time, pollOK bool) *metrics.Snapshot {
	s := metrics.NewSnapshot()

	type key struct{ repo, gate string }
	open := map[key]float64{}
	blocked := map[string]float64{}
	type fkey struct{ repo, category, severity, tool string }
	fcount := map[fkey]float64{}
	verdictSum, verdictN := map[string]float64{}, map[string]float64{}
	durSum, durN := map[string]float64{}, map[string]float64{}

	for _, r := range reports {
		open[key{r.Repo, r.Gate}]++
		if _, ok := blocked[r.Repo]; !ok {
			blocked[r.Repo] = 0
		}
		if r.MergeBlocked {
			blocked[r.Repo]++
		}
		for _, f := range r.Findings {
			sev := f.Severity
			if sev == "" {
				sev = "n/a"
			}
			fcount[fkey{r.Repo, f.Category, sev, f.Tool}]++
		}
		if r.PipelineFinished > 0 {
			if v := float64(r.PipelineFinished - r.CreatedAt.Unix()); v >= 0 {
				verdictSum[r.Repo] += v
				verdictN[r.Repo]++
			}
			if r.PipelineStarted > 0 && r.PipelineFinished >= r.PipelineStarted {
				durSum[r.Repo] += float64(r.PipelineFinished - r.PipelineStarted)
				durN[r.Repo]++
			}
		}
	}

	for k, n := range open {
		s.Gauge("ssdlc_open_pull_requests", "Open pull requests by repository and gate result.",
			map[string]string{"repo": k.repo, "gate": k.gate}, n)
	}
	for repo, n := range blocked {
		s.Gauge("ssdlc_pull_requests_blocked", "Open pull requests the gate is blocking.",
			map[string]string{"repo": repo}, n)
	}
	for k, n := range fcount {
		s.Gauge("ssdlc_open_findings", "Findings on open pull requests.",
			map[string]string{"repo": k.repo, "category": k.category, "severity": k.severity, "tool": k.tool}, n)
	}
	for repo, sum := range verdictSum {
		s.Gauge("ssdlc_pr_time_to_verdict_seconds_avg", "Average seconds from PR creation to a finished gate run.",
			map[string]string{"repo": repo}, sum/verdictN[repo])
	}
	for repo, sum := range durSum {
		s.Gauge("ssdlc_pipeline_duration_seconds_avg", "Average gate pipeline duration for open pull requests.",
			map[string]string{"repo": repo}, sum/durN[repo])
	}

	ok := 0.0
	if pollOK {
		ok = 1
	}
	s.Gauge("ssdlc_sidecar_poll_ok", "1 when every repository was read successfully in the last poll.", nil, ok)
	ts := 0.0
	if !now.IsZero() {
		ts = float64(now.Unix())
	}
	s.Gauge("ssdlc_sidecar_last_poll_timestamp_seconds", "Unix time of the last completed poll.", nil, ts)
	return s
}
