package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, content, message string
	}{
		{"missing database", `ingest_token = "private-token"`, "database_url"},
		{"missing token", `database_url = "postgres://user:private-password@db/footprint"`, "ingest_token"},
		{"whitespace token", "database_url = 'postgres://db/footprint'\ningest_token = 'private token'", "ingest_token"},
		{"invalid address", "database_url = 'postgres://db/footprint'\ningest_token = 'private-token'\nhttp_addr = 'invalid'", "http_addr"},
		{"syntax error", `ingest_token = "private-token`, "invalid TOML"},
		{"unknown field", `ingest_tokne = "private-token"`, "unknown configuration field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("unexpected configuration error: %v", err)
			}
			for _, secret := range []string{"private-token", "private-password", "private token"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("configuration error disclosed a credential")
				}
			}
		})
	}
}
