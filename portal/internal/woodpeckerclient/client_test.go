package woodpeckerclient

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPipelines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/repos/1/pipelines" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`[{"number":12,"status":"failure","commit":"abc123","event":"pr"}]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	pipelines, err := c.ListPipelines(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(pipelines) != 1 || pipelines[0].Number != 12 || pipelines[0].Status != "failure" {
		t.Errorf("unexpected pipelines: %+v", pipelines)
	}
}

func TestGetStepLog_DecodesBase64Lines(t *testing.T) {
	line1 := base64.StdEncoding.EncodeToString([]byte("  FAIL  CRITICAL [gitleaks/aws-access-token] config.py:5 -- example\n"))
	line2 := base64.StdEncoding.EncodeToString([]byte("policy-eval: 1 finding(s) normalized -- critical=1 high=0 medium=0 low=0\n"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"data":"` + line1 + `"},{"data":"` + line2 + `"}]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "fake-token")
	log, err := c.GetStepLog(context.Background(), 1, 12, 59)
	if err != nil {
		t.Fatalf("GetStepLog: %v", err)
	}
	if !contains(log, "FAIL  CRITICAL [gitleaks/aws-access-token]") ||
		!contains(log, "critical=1 high=0 medium=0 low=0") {
		t.Errorf("decoded log missing expected content:\n%s", log)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
