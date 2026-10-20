package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutingAndAuthentication(t *testing.T) {
	e := New(nil, "test-token")
	for _, tc := range []struct {
		method, path, authorization string
		status                      int
		code                        string
	}{
		{"GET", "/health", "", 200, ""},
		{"GET", "/api/me", "Bearer test-token", 200, ""},
		{"GET", "/api/me", "bearer test-token", 200, ""},
		{"GET", "/api/me", "", 401, "unauthorized"},
		{"GET", "/api/me", "Bearer wrong", 401, "unauthorized"},
		{"GET", "/api/me", "Bearer test-token extra", 401, "unauthorized"},
		{"POST", "/api/location", "Bearer test-token", 405, "method_not_allowed"},
		{"GET", "/api/unknown", "Bearer test-token", 404, "not_found"},
		{"GET", "/location", "Bearer test-token", 404, "not_found"},
	} {
		t.Run(tc.method+" "+tc.path+" "+tc.authorization, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("Authorization", tc.authorization)
			r.Header.Set("Token", "test-token")
			r.Header.Set("Cf-Access-Jwt-Assertion", "ignored")
			w := httptest.NewRecorder()
			e.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}
			if tc.code != "" {
				var result struct{ Error apiError }
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Error.Code != tc.code {
					t.Fatalf("unexpected error: %s", w.Body.String())
				}
			}
			if tc.status == 405 && !strings.Contains(w.Header().Get("Allow"), "PUT") {
				t.Fatal("missing Allow header")
			}
			if tc.status == 401 && w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing Bearer challenge")
			}
		})
	}
	w := httptest.NewRecorder()
	New(nil, "").ServeHTTP(w, httptest.NewRequest("GET", "/api/me", nil))
	if w.Code != 503 {
		t.Fatalf("unconfigured authentication: %d", w.Code)
	}
}
