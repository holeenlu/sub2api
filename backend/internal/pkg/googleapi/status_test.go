package googleapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPStatusToGoogleStatus(t *testing.T) {
	cases := map[int]string{
		http.StatusBadRequest:            "INVALID_ARGUMENT",
		http.StatusUnauthorized:          "UNAUTHENTICATED",
		http.StatusForbidden:             "PERMISSION_DENIED",
		http.StatusNotFound:              "NOT_FOUND",
		http.StatusRequestTimeout:        "DEADLINE_EXCEEDED",
		http.StatusUnsupportedMediaType:  "INVALID_ARGUMENT",
		http.StatusTooManyRequests:       "RESOURCE_EXHAUSTED",
		499:                              "CANCELLED",
		http.StatusInternalServerError:   "INTERNAL",
		http.StatusBadGateway:            "INTERNAL",
		http.StatusRequestEntityTooLarge: "UNKNOWN",
	}
	for status, want := range cases {
		require.Equalf(t, want, HTTPStatusToGoogleStatus(status), "status %d", status)
	}
}
