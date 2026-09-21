// portal/main.go
package main

import (
	"context"
	"log"
	"net/http"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/config"
	"ssdlc-portal/internal/exceptions"
	"ssdlc-portal/internal/giteaclient"
	"ssdlc-portal/internal/handlers"
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
	mux.HandleFunc("/dashboard", authHandler.RequireAuth(handlers.Dashboard(cfg.GiteaURL)))
	mux.HandleFunc("/pr/{owner}/{repo}/{number}", authHandler.RequireAuth(
		handlers.PRReport(cfg.GiteaURL, cfg.WoodpeckerURL, cfg.WoodpeckerToken)))
	// Onboarding runs onboard-repo.sh with an admin-scoped Gitea token, so
	// both routes are gated behind membership in cfg.ApproverTeam before
	// authHandler.RequireAuth's session check ever reaches them. The org
	// used for the team-membership check is cfg.ExceptionsRepoOwner: this
	// platform is single-tenant-per-instance, and ExceptionsRepoOwner is
	// already the org that owns the exceptions repo and is wired elsewhere
	// as "the platform operators' org" for this Gitea instance — there is
	// no other org name available in config to use instead.
	onboardingGitea := giteaclient.New(cfg.GiteaURL, "")
	mux.HandleFunc("/onboarding", authHandler.RequireAuth(handlers.RequireTeam(
		onboardingGitea, cfg.ExceptionsRepoOwner, cfg.ApproverTeam,
		handlers.OnboardingForm())))
	mux.HandleFunc("/onboarding/start", authHandler.RequireAuth(handlers.RequireTeam(
		onboardingGitea, cfg.ExceptionsRepoOwner, cfg.ApproverTeam,
		handlers.OnboardingStream("../scripts/onboard-repo.sh", []string{
			"GITEA_URL=" + cfg.GiteaURL,
			"WOODPECKER_URL=" + cfg.WoodpeckerURL,
			"GITEA_ADMIN_TOKEN=" + cfg.GiteaAdminToken,
			"WOODPECKER_TOKEN=" + cfg.WoodpeckerToken,
		}))))

	mux.HandleFunc("/exceptions", authHandler.RequireAuth(
		handlers.ExceptionsQueue(exceptionsStore, onboardingGitea, cfg.ExceptionsRepoOwner)))
	mux.HandleFunc("/exceptions/request", authHandler.RequireAuth(handlers.ExceptionRequestForm(onboardingGitea)))
	mux.HandleFunc("/exceptions/submit", authHandler.RequireAuth(handlers.ExceptionRequestSubmit(exceptionsStore, onboardingGitea)))
	// Approval is gated behind approver-team membership via the same
	// RequireTeam middleware /onboarding uses -- ExceptionApprove itself
	// only enforces the self-approval half of the two-party rule, so the
	// team-membership half isn't duplicated inline a second time.
	mux.HandleFunc("/exceptions/approve", authHandler.RequireAuth(handlers.RequireTeam(
		onboardingGitea, cfg.ExceptionsRepoOwner, cfg.ApproverTeam,
		handlers.ExceptionApprove(exceptionsStore, onboardingGitea))))
	// Decline carries no self-approval risk, so it only needs the
	// approver-team gate, not ExceptionApprove's requester check too.
	mux.HandleFunc("/exceptions/decline", authHandler.RequireAuth(handlers.RequireTeam(
		onboardingGitea, cfg.ExceptionsRepoOwner, cfg.ApproverTeam,
		handlers.ExceptionDecline(exceptionsStore))))

	log.Printf("ssdlc-portal listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
