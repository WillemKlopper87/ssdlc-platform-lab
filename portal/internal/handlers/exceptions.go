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

// ExceptionRequestForm renders the request form, pre-filling Operator from
// the caller's authenticated session (via their own token, like
// ExceptionApprove and ExceptionsQueue do) purely for display -- it is
// never trusted as the identity that gets written; ExceptionRequestSubmit
// re-derives it independently from the session on submit.
func ExceptionRequestForm(gitea *giteaclient.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var operator string
		if token, ok := auth.TokenFromContext(r.Context()); ok {
			caller := giteaclient.New(gitea.BaseURL(), token)
			if username, err := caller.Username(r.Context()); err == nil {
				operator = username
			}
		}
		data := struct {
			ActiveNav   string
			Operator    string
			Repo        string
			Fingerprint string
		}{ActiveNav: "exceptions", Operator: operator, Repo: r.URL.Query().Get("repo"), Fingerprint: r.URL.Query().Get("fingerprint")}
		if err := exceptionRequestTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// ExceptionRequestSubmit derives Requester from the caller's own
// authenticated session -- never from the submitted form -- so a POST
// can't forge a different requester identity. That forgery would
// otherwise defeat ExceptionApprove's two-party rule entirely: approve
// the exception under your real account, but have "requested" it under
// someone else's forged name, and the self-approval check
// (approver == target.Requester) would never trip.
func ExceptionRequestSubmit(store *exceptions.Store, gitea *giteaclient.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		token, ok := auth.TokenFromContext(r.Context())
		if !ok || token == "" {
			http.Error(w, "not authenticated", http.StatusForbidden)
			return
		}
		caller := giteaclient.New(gitea.BaseURL(), token)
		requester, err := caller.Username(r.Context())
		if err != nil {
			http.Error(w, "could not identify requester: "+err.Error(), http.StatusBadGateway)
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
			Requester:          requester,
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

// ExceptionApprove enforces the requester-cannot-approve-their-own-request
// half of the two-party rule. The other half -- approver-team membership --
// is NOT checked here: it is enforced by wrapping this handler in
// handlers.RequireTeam (Task 7) at the routing layer in main.go, exactly
// like /onboarding already does, rather than duplicating RequireTeam's
// IsOnTeam check inline a second time. That's also why this signature no
// longer takes approverTeam/org -- RequireTeam owns that check and those
// parameters entirely.
func ExceptionApprove(store *exceptions.Store, gitea *giteaclient.Client) http.HandlerFunc {
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
