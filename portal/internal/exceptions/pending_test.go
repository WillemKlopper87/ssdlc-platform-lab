package exceptions

import (
	"testing"
	"time"
)

func TestCountPending(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	future, past := now.Add(24*time.Hour), now.Add(-24*time.Hour)
	recs := []Record{
		{Requester: "dev2", Expiry: future},                 // pending, someone else's: counts
		{Requester: "dev3", Expiry: future},                 // pending, counts
		{Requester: "dev1", Expiry: future},                 // my own request: excluded
		{Requester: "dev2", Expiry: future, Approved: true}, // decided
		{Requester: "dev2", Expiry: future, Declined: true}, // decided
		{Requester: "dev2", Expiry: past},                   // expired request: excluded
	}
	if got := CountPending(recs, "dev1", now); got != 2 {
		t.Errorf("CountPending = %d, want 2", got)
	}
	if got := CountPending(nil, "dev1", now); got != 0 {
		t.Errorf("empty = %d", got)
	}
}
