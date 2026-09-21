package giteaclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCurrentUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "token tok" {
			t.Fatalf("auth header = %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"login":"gateadmin","is_admin":true}`))
	}))
	defer srv.Close()

	login, admin, err := New(srv.URL, "tok").CurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if login != "gateadmin" || !admin {
		t.Errorf("got %q admin=%v", login, admin)
	}
}

func TestCurrentUser_NonAdminAndError(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"login":"dev2"}`))
	}))
	defer ok.Close()
	if _, admin, err := New(ok.URL, "t").CurrentUser(context.Background()); err != nil || admin {
		t.Errorf("admin=%v err=%v", admin, err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer bad.Close()
	if _, _, err := New(bad.URL, "t").CurrentUser(context.Background()); err == nil {
		t.Fatal("expected an error on 401")
	}
}
