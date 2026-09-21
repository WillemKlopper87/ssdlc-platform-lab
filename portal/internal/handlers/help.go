package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"

	"ssdlc-portal/internal/help"
	"ssdlc-portal/internal/shell"
)

var helpTmpl = template.Must(template.ParseFiles(
	filepath.Join(templateDir, "layout.html"), filepath.Join(templateDir, "help.html"),
))

// Help renders the role-aware help page. Searching is a plain GET (?q=), so
// it works without JavaScript.
func Help() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := shell.FromContext(r.Context())
		q := r.URL.Query().Get("q")

		steps := make([]template.HTML, 0, 4)
		for _, step := range help.FirstSteps(s.Role) {
			steps = append(steps, template.HTML(step)) // fixed text owned by the code
		}
		article := "a"
		if s.Role == shell.RoleApprover || s.Role == shell.RoleAdmin {
			article = "an"
		}
		roleWord := "developer"
		switch s.Role {
		case shell.RoleApprover:
			roleWord = "approver"
		case shell.RoleAdmin:
			roleWord = "admin"
		}

		topics := help.Filter(help.Topics(), q)
		data := struct {
			ActiveNav string
			Operator  string
			Shell     shell.Shell
			Query     string
			Article   string
			RoleWord  string
			Steps     []template.HTML
			Topics    []help.Topic
			Open      bool
		}{"help", s.Operator, s, q, article, roleWord, steps, topics, q != ""}
		if err := helpTmpl.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
