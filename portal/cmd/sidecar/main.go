// Command sidecar is the SSDLC reporting service: PR report data, Prometheus
// metrics, one sticky gate comment per PR, and dependency health. It is not in
// the merge-decision path and holds no durable state.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/metrics"
	"ssdlc-portal/internal/sidecar"
	"ssdlc-portal/internal/woodpeckerclient"
)

func main() {
	cfg, err := sidecar.LoadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	lg := log.New(os.Stderr, "sidecar: ", log.LstdFlags)

	gitea := giteaclient.New(cfg.GiteaURL, cfg.GiteaToken)
	wp := woodpeckerclient.New(cfg.WoodpeckerURL, cfg.WoodpeckerToken)
	store := &metrics.Store{}

	poller := &sidecar.Poller{
		B:     sidecar.Backends{Gitea: gitea, Woodpecker: wp},
		Org:   cfg.Org,
		Store: store,
		Now:   time.Now,
		Log:   lg,
	}
	if cfg.Comments {
		poller.AfterReport = sidecar.CommentHook(gitea, cfg.CommentUser, cfg.PortalURL, lg)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		poller.Run(ctx, cfg.PollInterval)
	}()

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: sidecar.NewServer(&sidecar.APIDeps{
			Token: cfg.APIToken, Metrics: store, Gitea: gitea, Woodpecker: wp, Now: time.Now,
			Health: &sidecar.HealthChecker{
				HTTP:          &http.Client{Timeout: 5 * time.Second},
				GiteaURL:      cfg.GiteaURL,
				WoodpeckerURL: cfg.WoodpeckerURL,
				Agents:        wp.ListAgents,
				Now:           time.Now,
			},
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	lg.Printf("listening on %s (org %q, poll every %s, comments %v)", cfg.ListenAddr, cfg.Org, cfg.PollInterval, cfg.Comments)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		stop()
		log.Fatal(err)
	}
	<-shutdownDone
	wg.Wait()
	lg.Printf("stopped")
}
