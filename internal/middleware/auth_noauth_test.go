package middleware

import (
	"net/http"
	"testing"
)

// TestIsNoAuthAPIBrandingLogoWildcard pins the public logo read path: the
// login page renders the workspace logo before authentication, so the
// global Auth middleware must let /api/v1/branding/logo/<tenant> through
// for GET and HEAD (browser/IM image prechecks), while nothing else under
// /api/v1/branding/ is implicitly public.
func TestIsNoAuthAPIBrandingLogoWildcard(t *testing.T) {
	cases := []struct {
		path   string
		method string
		want   bool
	}{
		{"/api/v1/branding/logo/10000", http.MethodGet, true},
		{"/api/v1/branding/logo/10000", http.MethodHead, true},
		{"/api/v1/branding/logo/10000", http.MethodPost, false},
		{"/api/v1/branding/logo/", http.MethodGet, true},
		// Sibling paths under /branding must stay authenticated — the
		// wildcard is scoped to the logo/ prefix, not /api/v1/branding/*.
		{"/api/v1/branding/logo-other", http.MethodGet, false},
		{"/api/v1/branding/other/10000", http.MethodGet, false},
		{"/api/v1/tenants/10000/branding/logo", http.MethodPost, false},
	}
	for _, tc := range cases {
		if got := isNoAuthAPI(tc.path, tc.method); got != tc.want {
			t.Errorf("isNoAuthAPI(%q, %q) = %v, want %v", tc.path, tc.method, got, tc.want)
		}
	}
}
