package exceptions

import "time"

// CountPending is the number of requests waiting for the given approver:
// undecided, not expired, and not requested by that person (nobody approves
// their own request).
func CountPending(records []Record, user string, now time.Time) int {
	n := 0
	for _, r := range records {
		if r.Approved || r.Declined || r.Requester == user || !r.Expiry.After(now) {
			continue
		}
		n++
	}
	return n
}
