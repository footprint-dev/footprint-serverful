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
	Server Server `toml:"server"`
	Mapbox Mapbox `toml:"mapbox"`
}

type Server struct {
	Addr        string `toml:"addr"`
	DatabaseURL string `toml:"database_url"`
	IngestToken string `toml:"ingest_token"`
}

type Mapbox struct {
	PublicToken string `toml:"public_token"`
	Style       string `toml:"style"`
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()
	cfg := Config{
		Server: Server{Addr: ":8080"},
		Mapbox: Mapbox{Style: "mapbox://styles/mapbox/light-v11"},
	}
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
	if strings.TrimSpace(cfg.Server.DatabaseURL) == "" {
		return Config{}, errors.New("server.database_url is required")
	}
	if cfg.Server.IngestToken == "" || strings.ContainsFunc(cfg.Server.IngestToken, unicode.IsSpace) {
		return Config{}, errors.New("server.ingest_token must be a nonempty token without whitespace")
	}
	if cfg.Mapbox.PublicToken != "" && !strings.HasPrefix(cfg.Mapbox.PublicToken, "pk.") {
		return Config{}, errors.New("mapbox.public_token must be a public token starting with pk.")
	}
	if _, port, err := net.SplitHostPort(cfg.Server.Addr); err != nil || port == "" {
		return Config{}, errors.New("server.addr must use host:port format, for example :8080")
	}
	return cfg, nil
}
