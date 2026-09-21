package sidecar

import (
	"net/http"
	"net/http/httptest"
)

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
func newRequest() *http.Request               { return httptest.NewRequest("GET", "/metrics", nil) }
