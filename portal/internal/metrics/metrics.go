// Package metrics writes the Prometheus text exposition format for gauges.
// It is deliberately tiny so the module keeps zero dependencies; the sidecar
// only ever exports gauges rebuilt from scratch on every poll.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type sample struct {
	labels string // rendered, sorted: {a="b",c="d"} or ""
	value  float64
}

type family struct {
	help    string
	samples []sample
}

type Snapshot struct {
	fams map[string]*family
}

func NewSnapshot() *Snapshot { return &Snapshot{fams: map[string]*family{}} }

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func renderLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+`="`+labelEscaper.Replace(labels[k])+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (s *Snapshot) Gauge(name, help string, labels map[string]string, value float64) {
	f, ok := s.fams[name]
	if !ok {
		f = &family{help: help}
		s.fams[name] = f
	}
	f.samples = append(f.samples, sample{labels: renderLabels(labels), value: value})
}

func (s *Snapshot) WriteTo(w io.Writer) (int64, error) {
	names := make([]string, 0, len(s.fams))
	for n := range s.fams {
		names = append(names, n)
	}
	sort.Strings(names)
	var total int64
	for _, n := range names {
		f := s.fams[n]
		// Copy the samples slice to avoid mutating the snapshot
		samplesCopy := make([]sample, len(f.samples))
		copy(samplesCopy, f.samples)
		sort.SliceStable(samplesCopy, func(i, j int) bool { return samplesCopy[i].labels < samplesCopy[j].labels })
		c, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", n, f.help, n)
		total += int64(c)
		if err != nil {
			return total, err
		}
		for _, sm := range samplesCopy {
			c, err := fmt.Fprintf(w, "%s%s %s\n", n, sm.labels, strconv.FormatFloat(sm.value, 'g', -1, 64))
			total += int64(c)
			if err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

// Store holds the most recent Snapshot and serves it.
type Store struct {
	mu   sync.RWMutex
	snap *Snapshot
}

func (s *Store) Set(snap *Snapshot) {
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
}

func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	snap := s.snap
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if snap != nil {
		snap.WriteTo(w)
	}
}
