package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidConfiguration(t *testing.T) {
	database := "[server]\ndatabase_url = 'postgres://user:private-password@db/footprint'\n"
	token := "ingest_token = 'private-token'\n"
	for _, tc := range []struct {
		name, content, message string
	}{
		{"missing database", "[server]\n" + token, "server.database_url"},
		{"missing token", database, "server.ingest_token"},
		{"whitespace token", database + "ingest_token = 'private token'", "server.ingest_token"},
		{"secret Mapbox token", database + token + "[mapbox]\npublic_token = 'sk.private-mapbox-token'", "mapbox.public_token"},
		{"invalid address", database + token + "addr = 'invalid'", "server.addr"},
		{"syntax error", "[server]\ningest_token = 'private-token", "invalid TOML"},
		{"unknown field", "[server]\ningest_tokne = 'private-token'", "unknown configuration field"},
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
			for _, secret := range []string{"private-token", "private-password", "private token", "sk.private-mapbox-token"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("configuration error disclosed a credential")
				}
			}
		})
	}
}
