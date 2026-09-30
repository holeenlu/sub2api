package service

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The repository applies exponential backoff and jitter across instances. A
// server Retry-After is a lower bound, never a reason to retry earlier.
func catalogNextRetry(err error, interval int) time.Time {
	next := time.Now().Add(time.Duration(interval) * time.Second)
	var upstream *UpstreamModelSyncError
	if !errors.As(err, &upstream) {
		return next
	}
	raw := strings.TrimSpace(upstream.RetryAfter)
	if seconds, e := strconv.Atoi(raw); e == nil && seconds > 0 {
		if seconds > 86400 {
			seconds = 86400
		}
		at := time.Now().Add(time.Duration(seconds) * time.Second)
		if at.After(next) {
			next = at
		}
	} else if at, e := http.ParseTime(raw); e == nil && at.After(next) {
		next = at
	}
	return next
}
