// portal/main.go
package main

import (
	"context"
	"log"
	"mime"
	"net/http"
	"time"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/config"
	"ssdlc-portal/internal/exceptions"
	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/handlers"
	"ssdlc-portal/internal/shell"
	"ssdlc-portal/internal/sidecarclient"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	exceptionsStore := exceptions.NewStore(giteaclient.New(cfg.GiteaURL, cfg.GiteaAdminToken), cfg.ExceptionsRepoOwner, cfg.ExceptionsRepoName)
	if err := exceptionsStore.Ensure(context.Background()); err != nil {
		log.Fatal(err)
	}

	authHandler := auth.NewHandler(cfg)

	// Fonts are served by the portal itself; the minimal runtime image has no
	// system mime database, so register the type explicitly.
	mime.AddExtensionType(".woff2", "font/woff2")

	shellBuilder := &shell.Builder{
		NewIdentity:   func(token string) shell.Identity { return giteaclient.New(cfg.GiteaURL, token) },
		Records:       exceptionsStore.List,
		Org:           cfg.ExceptionsRepoOwner,
		Team:          cfg.ApproverTeam,
		GiteaURL:      cfg.GiteaPublicURL,
		WoodpeckerURL: cfg.WoodpeckerPublicURL,
		TTL:           30 * time.Second,
		Now:           time.Now,
	}
	var projectsSource handlers.ProjectsSource // nil = reporting service not configured
	if cfg.SidecarURL != "" {
		projectsSource = sidecarclient.New(cfg.SidecarURL, cfg.SidecarToken)
	}
	// authed = require a signed-in user, then compute the page chrome for them.
	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return authHandler.RequireAuth(shellBuilder.Middleware(h))
	}

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	// Bare "/" has no page of its own; send visitors to the dashboard (which
	// itself redirects to Gitea sign-in when there is no session).
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})
	mux.HandleFunc("/login", authHandler.Login)
	mux.HandleFunc("/oauth/callback", authHandler.Callback)
	mux.HandleFunc("/logout", authHandler.Logout)
	mux.HandleFunc("/dashboard", authed(handlers.Dashboard(cfg.GiteaURL)))
	mux.HandleFunc("/help", authed(handlers.Help()))
	mux.HandleFunc("/projects", authed(handlers.Projects(projectsSource)))
	mux.HandleFunc("/pr/{owner}/{repo}/{number}", authed(
		handlers.PRReport(cfg.GiteaURL, cfg.WoodpeckerURL, cfg.WoodpeckerToken)))
	// Onboarding runs onboard-repo.sh with an admin-scoped Gitea token.
	// Requests are authenticated first (RequireAuth), then the page chrome
	// is computed (shell middleware), then RequireTeam checks membership of
	// cfg.ApproverTeam in the cfg.ExceptionsRepoOwner org before the
	// handler runs. That org is used because the platform is
	// single-tenant per instance.
	onboardingGitea := giteaclient.New(cfg.GiteaURL, "")
	mux.HandleFunc("/onboarding", authed(handlers.RequireTeam(
		onboardingGitea, cfg.ExceptionsRepoOwner, cfg.ApproverTeam,
		handlers.OnboardingForm())))
	mux.HandleFunc("/onboarding/start", authed(handlers.RequireTeam(
		onboardingGitea, cfg.ExceptionsRepoOwner, cfg.ApproverTeam,
		handlers.OnboardingStream("../scripts/onboard-repo.sh", []string{
			"GITEA_URL=" + cfg.GiteaURL,
			"WOODPECKER_URL=" + cfg.WoodpeckerURL,
			"GITEA_ADMIN_TOKEN=" + cfg.GiteaAdminToken,
			"WOODPECKER_TOKEN=" + cfg.WoodpeckerToken,
		}))))

	mux.HandleFunc("/exceptions", authed(
		handlers.ExceptionsQueue(exceptionsStore, onboardingGitea, cfg.ExceptionsRepoOwner)))
	mux.HandleFunc("/exceptions/request", authed(handlers.ExceptionRequestForm(onboardingGitea)))
	mux.HandleFunc("/exceptions/submit", authed(handlers.ExceptionRequestSubmit(exceptionsStore, onboardingGitea)))
	// Approval is gated behind approver-team membership via the same
	// RequireTeam middleware /onboarding uses -- ExceptionApprove itself
	// only enforces the self-approval half of the two-party rule, so the
	// team-membership half isn't duplicated inline a second time.
	mux.HandleFunc("/exceptions/approve", authed(handlers.RequireTeam(
		onboardingGitea, cfg.ExceptionsRepoOwner, cfg.ApproverTeam,
		handlers.ExceptionApprove(exceptionsStore, onboardingGitea))))
	// Decline carries no self-approval risk, so it only needs the
	// approver-team gate, not ExceptionApprove's requester check too.
	mux.HandleFunc("/exceptions/decline", authed(handlers.RequireTeam(
		onboardingGitea, cfg.ExceptionsRepoOwner, cfg.ApproverTeam,
		handlers.ExceptionDecline(exceptionsStore))))

	log.Printf("ssdlc-portal listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
