package middleware

import "testing"

func TestRequestLogPathHidesImageCapability(t *testing.T) {
	for path, want := range map[string]string{

		"/v1/responses":           "/v1/responses",
		"/api/v1/settings/public": "/api/v1/settings/public",
	} {
		if got := requestLogPath(path); got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
	}
}
