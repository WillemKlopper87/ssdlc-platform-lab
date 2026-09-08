// portal/internal/handlers/exceptions.go
package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"
	"time"

	"ssdlc-portal/internal/auth"
	"ssdlc-portal/internal/exceptions"
	"ssdlc-portal/internal/giteaclient"
)

// templateDir is declared in dashboard.go (Task 6) and reused here.
var exceptionsTmpl = template.Must(template.ParseFiles(
	filepath.Join(templateDir, "layout.html"), filepath.Join(templateDir, "exceptions.html"),
))
var exceptionRequestTmpl = template.Must(template.ParseFiles(
	filepath.Join(templateDir, "layout.html"), filepath.Join(templateDir, "exception_request.html"),
))

func ExceptionRequestForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			ActiveNav   string
			Operator    string
			Repo        string
			Fingerprint string
		}{ActiveNav: "exceptions", Repo: r.URL.Query().Get("repo"), Fingerprint: r.URL.Query().Get("fingerprint")}
		if err := exceptionRequestTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func ExceptionRequestSubmit(store *exceptions.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		expiry, err := time.Parse("2006-01-02", r.FormValue("expiry"))
		if err != nil {
			http.Error(w, "invalid expiry date: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := exceptions.ValidateExpiry(expiry, time.Now().UTC()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		rec := exceptions.Record{
			Repo:               r.FormValue("repo"),
			FindingFingerprint: r.FormValue("fingerprint"),
			Severity:           r.FormValue("severity"),
			Expiry:             expiry,
			Requester:          r.FormValue("requester"),
			Ticket:             r.FormValue("ticket"),
			Approved:           false,
		}
		if err := store.Write(r.Context(), rec); err != nil {
			http.Error(w, "could not save exception request: "+err.Error(), http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, "/exceptions", http.StatusFound)
	}
}

// ExceptionApprove enforces the two-party rule server-side: the approver
// must not be the requester, and must belong to approverTeam (read live
// from Gitea via the approver's OWN token, never cached) -- this is the
// actual security property the whole feature exists for.
func ExceptionApprove(store *exceptions.Store, gitea *giteaclient.Client, approverTeam, org string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		repo := r.FormValue("repo")
		fingerprint := r.FormValue("fingerprint")

		token, ok := auth.TokenFromContext(r.Context())
		if !ok {
			http.Error(w, "not authenticated", http.StatusForbidden)
			return
		}
		caller := giteaclient.New(gitea.BaseURL(), token)

		records, err := store.List(r.Context())
		if err != nil {
			http.Error(w, "could not read exceptions: "+err.Error(), http.StatusBadGateway)
			return
		}
		var target *exceptions.Record
		for i := range records {
			if records[i].Repo == repo && records[i].FindingFingerprint == fingerprint {
				target = &records[i]
			}
		}
		if target == nil {
			http.Error(w, "no such pending exception", http.StatusNotFound)
			return
		}

		approver, err := caller.Username(r.Context())
		if err != nil {
			http.Error(w, "could not identify approver: "+err.Error(), http.StatusBadGateway)
			return
		}
		if approver == target.Requester {
			http.Error(w, "the requester cannot also approve their own exception", http.StatusForbidden)
			return
		}
		onTeam, err := caller.IsOnTeam(r.Context(), org, approverTeam)
		if err != nil || !onTeam {
			http.Error(w, "approver is not a member of the "+approverTeam+" team", http.StatusForbidden)
			return
		}

		target.Approvers = append(target.Approvers, approver)
		target.Approved = true
		if err := store.Write(r.Context(), *target); err != nil {
			http.Error(w, "could not finalize exception: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func ExceptionsQueue(store *exceptions.Store, gitea *giteaclient.Client, owner string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		records, err := store.List(r.Context())
		if err != nil {
			http.Error(w, "could not read exceptions: "+err.Error(), http.StatusBadGateway)
			return
		}
		var pending, active []exceptions.Record
		for _, rec := range records {
			if rec.Approved {
				active = append(active, rec)
			} else {
				pending = append(pending, rec)
			}
		}

		var operator string
		if token, ok := auth.TokenFromContext(r.Context()); ok {
			caller := giteaclient.New(gitea.BaseURL(), token)
			if username, err := caller.Username(r.Context()); err == nil {
				operator = username
			}
		}

		data := struct {
			ActiveNav string
			Operator  string
			Pending   []exceptions.Record
			Active    []exceptions.Record
		}{ActiveNav: "exceptions", Operator: operator, Pending: pending, Active: active}
		if err := exceptionsTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
