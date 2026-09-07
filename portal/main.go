// portal/main.go
package main

import (
	"log"
	"net/http"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/config"
	"ssdlc-portal/internal/handlers"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	authHandler := auth.NewHandler(cfg)

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	mux.HandleFunc("/login", authHandler.Login)
	mux.HandleFunc("/oauth/callback", authHandler.Callback)
	mux.HandleFunc("/logout", authHandler.Logout)
	mux.HandleFunc("/dashboard", authHandler.RequireAuth(handlers.Dashboard(cfg.GiteaURL)))
	mux.HandleFunc("/pr/{owner}/{repo}/{number}", authHandler.RequireAuth(
		handlers.PRReport(cfg.GiteaURL, cfg.WoodpeckerURL, cfg.WoodpeckerToken)))

	log.Printf("ssdlc-portal listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
