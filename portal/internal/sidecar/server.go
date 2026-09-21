package sidecar

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/report"
)

type APIDeps struct {
	Token      string
	Metrics    *metrics.Store
	Gitea      report.Gitea
	Woodpecker report.Woodpecker
	Health     *HealthChecker
	Now        func() time.Time
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (d *APIDeps) auth(next http.HandlerFunc) http.HandlerFunc {
	want := []byte("Bearer " + d.Token)
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// NewServer wires the HTTP routes. /healthz and /metrics are open because the
// port is not published outside the Docker network; /api/* needs the token.
func NewServer(d *APIDeps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /metrics", d.Metrics)
	mux.HandleFunc("GET /api/v1/reports/{owner}/{repo}/{number}", d.auth(func(w http.ResponseWriter, r *http.Request) {
		number, err := strconv.Atoi(r.PathValue("number"))
		if err != nil || number < 1 {
			http.Error(w, "pull request number must be a positive integer", http.StatusBadRequest)
			return
		}
		rep, err := report.Build(r.Context(), d.Gitea, d.Woodpecker, r.PathValue("owner"), r.PathValue("repo"), number, d.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, rep)
	}))
	mux.HandleFunc("GET /api/v1/health", d.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, d.Health.Run(r.Context()))
	}))
	return mux
}
