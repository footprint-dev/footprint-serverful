package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"

	"footprint-serverful/internal/api"
	"footprint-serverful/internal/config"
	"footprint-serverful/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	path := flag.String("config", "config.toml", "Path to the TOML configuration file")
	probe := flag.Bool("healthcheck", false, "Check the configured HTTP service and exit")
	flag.Parse()
	cfg, err := config.Load(*path)
	if err == nil {
		if *probe {
			err = healthcheck(cfg.Server.Addr)
		} else {
			err = run(cfg)
		}
	}
	if err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	db, err := store.Open(startup, cfg.Server.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(startup); err != nil {
		// Driver errors can include connection strings or database contents.
		return errors.New("database schema initialization failed")
	}
	cancel()
	e := api.New(db, cfg)
	e.Logger = slog.Default()
	serverConfig := echo.StartConfig{
		Address: cfg.Server.Addr, HideBanner: true, GracefulTimeout: 10 * time.Second,
		BeforeServeFunc: func(server *http.Server) error {
			server.ReadHeaderTimeout = 5 * time.Second
			server.ReadTimeout = 30 * time.Second
			server.WriteTimeout = 35 * time.Second
			server.IdleTimeout = 60 * time.Second
			return nil
		},
	}
	return serverConfig.Start(ctx, e)
}

func healthcheck(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	target := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/health"}
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(target.String())
	if err != nil {
		return errors.New("HTTP health check failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("HTTP service is unhealthy")
	}
	return nil
}
