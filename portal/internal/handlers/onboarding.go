// portal/internal/handlers/onboarding.go
package handlers

import (
	"bufio"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

// templateDir is declared once in dashboard.go (Task 6) and reused here —
// do not redeclare it.
var onboardingTmpl = template.Must(template.ParseFiles(
	filepath.Join(templateDir, "layout.html"), filepath.Join(templateDir, "onboarding.html"),
))

func OnboardingForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := struct{ ActiveNav, Operator string }{ActiveNav: "onboarding"}
		if err := onboardingTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// OnboardingStream runs scriptPath as `sh scriptPath owner repo`, streaming
// its combined stdout+stderr to the browser as Server-Sent Events as it is
// produced -- the wizard shows the real onboard-repo.sh output live,
// including a real failure if step 3 (say) genuinely fails, rather than a
// canned progress bar that can't represent that.
//
// The subprocess inherits the parent's full environment (os.Environ())
// plus extraEnv, rather than replacing it -- the real onboard-repo.sh
// shells out to git/curl/gh found via PATH, and setting cmd.Env to ONLY
// extraEnv would silently drop PATH and break every such call in
// production, invisibly to a unit test using a trivial fake script.
func OnboardingStream(scriptPath string, extraEnv []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		owner := r.FormValue("owner")
		repo := r.FormValue("repo")
		if owner == "" || repo == "" {
			http.Error(w, "owner and repo are required", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		cmd := exec.CommandContext(r.Context(), "sh", scriptPath, owner, repo)
		cmd.Env = append(os.Environ(), extraEnv...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			fmt.Fprintf(w, "data: ERROR: %s\n\n", err.Error())
			flusher.Flush()
			return
		}
		cmd.Stderr = cmd.Stdout // combined stream, same order the operator would see in a real terminal

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(w, "data: ERROR: could not start onboarding: %s\n\n", err.Error())
			flusher.Flush()
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
			flusher.Flush()
		}

		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(w, "data: ONBOARDING FAILED: %s\n\n", err.Error())
		} else {
			fmt.Fprintf(w, "data: ONBOARDING COMPLETE\n\n")
		}
		flusher.Flush()
	}
}
