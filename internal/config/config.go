package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	HTTPAddr    string `toml:"http_addr"`
	DatabaseURL string `toml:"database_url"`
	IngestToken string `toml:"ingest_token"`
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()
	cfg := Config{HTTPAddr: ":8080"}
	if err := toml.NewDecoder(file).DisallowUnknownFields().Decode(&cfg); err != nil {
		// Decoder diagnostics may quote the line containing a credential.
		var unknownFields *toml.StrictMissingError
		if errors.As(err, &unknownFields) {
			return Config{}, errors.New("unknown configuration field")
		}
		var decodeError *toml.DecodeError
		if errors.As(err, &decodeError) {
			line, column := decodeError.Position()
			return Config{}, fmt.Errorf("invalid TOML configuration at line %d, column %d", line, column)
		}
		return Config{}, errors.New("invalid TOML configuration")
	}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return Config{}, errors.New("database_url is required")
	}
	if cfg.IngestToken == "" || strings.ContainsFunc(cfg.IngestToken, unicode.IsSpace) {
		return Config{}, errors.New("ingest_token must be a nonempty token without whitespace")
	}
	if _, port, err := net.SplitHostPort(cfg.HTTPAddr); err != nil || port == "" {
		return Config{}, errors.New("http_addr must use host:port format, for example :8080")
	}
	return cfg, nil
}
