package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"footprint-serverful/internal/config"
)

func TestRoutingAndAuthentication(t *testing.T) {
	e := New(nil, config.Config{
		Server: config.Server{IngestToken: "test-token"},
		Mapbox: config.Mapbox{PublicToken: "pk.test"},
	})
	for _, tc := range []struct {
		method, path, authorization string
		status                      int
		code                        string
	}{
		{"GET", "/health", "", 200, ""},
		{"GET", "/api/me", "Bearer test-token", 200, ""},
		{"GET", "/api/me", "bearer test-token", 200, ""},
		{"GET", "/api/me", "", 401, "unauthorized"},
		{"GET", "/api/console-config", "", 401, "unauthorized"},
		{"GET", "/api/console-config", "Bearer wrong", 401, "unauthorized"},
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
			if tc.status == 401 && strings.Contains(w.Body.String(), "pk.test") {
				t.Fatal("unauthenticated response disclosed the Mapbox token")
			}
		})
	}
	w := httptest.NewRecorder()
	New(nil, config.Config{}).ServeHTTP(w, httptest.NewRequest("GET", "/api/me", nil))
	if w.Code != 503 {
		t.Fatalf("unconfigured authentication: %d", w.Code)
	}
}

func TestConsoleConfig(t *testing.T) {
	for _, tc := range []struct {
		name, settings, token, style string
	}{
		{"unconfigured map", "", "", "mapbox://styles/mapbox/light-v11"},
		{"configured map", "[mapbox]\npublic_token = 'pk.public'\nstyle = 'mapbox://styles/example/custom'", "pk.public", "mapbox://styles/example/custom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			content := "[server]\ndatabase_url = 'postgres://user:private-password@localhost/footprint'\ningest_token = 'private-token'\n" + tc.settings
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/api/console-config", nil)
			request.Header.Set("Authorization", "Bearer private-token")
			New(nil, cfg).ServeHTTP(response, request)
			if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status %d, body %s", response.Code, response.Body.String())
			}
			var result map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"mapboxToken": tc.token, "mapboxStyle": tc.style, "localPreview": false}
			if !reflect.DeepEqual(result, want) {
				t.Fatalf("unexpected map configuration: %s", response.Body.String())
			}
		})
	}
}
