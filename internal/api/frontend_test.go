package api

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"footprint-serverful/internal/config"
)

func TestEmbeddedFrontend(t *testing.T) {
	e := New(nil, config.Config{Server: config.Server{IngestToken: "test-token"}})
	page := httptest.NewRecorder()
	e.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	if page.Code != 200 || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") || page.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("unexpected frontend response: %d %v", page.Code, page.Header())
	}
	assets := regexp.MustCompile(`(?:src|href)="\./(assets/[^"]+)"`).FindAllStringSubmatch(page.Body.String(), -1)
	if len(assets) == 0 {
		t.Fatal("frontend HTML does not reference relative build assets")
	}
	for _, asset := range assets {
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest("GET", "/"+asset[1], nil))
		if response.Code != 200 || response.Body.Len() == 0 || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("asset %s: status %d, headers %v", asset[1], response.Code, response.Header())
		}
	}
	head := httptest.NewRecorder()
	e.ServeHTTP(head, httptest.NewRequest("HEAD", "/", nil))
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Type") != page.Header().Get("Content-Type") {
		t.Fatalf("unexpected HEAD response: %d %v", head.Code, head.Header())
	}
	for _, path := range []string{"/assets/missing.js", "/projects/unknown", "/config.toml", "/LICENSE"} {
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 404 || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("missing resource %s: status %d, headers %v", path, response.Code, response.Header())
		}
	}
	request := httptest.NewRequest("HEAD", "/api/me", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	e.ServeHTTP(response, request)
	if response.Code != 405 || !strings.Contains(response.Header().Get("Allow"), "GET") {
		t.Fatalf("static routes changed API method handling: %d %v", response.Code, response.Header())
	}
}
