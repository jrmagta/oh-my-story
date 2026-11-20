package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jrmagta/oh-my-story/internal/config"
	"github.com/jrmagta/oh-my-story/internal/db"
	"github.com/jrmagta/oh-my-story/internal/httpapi"
	"github.com/jrmagta/oh-my-story/internal/live"
	"github.com/jrmagta/oh-my-story/internal/mail"
	"github.com/jrmagta/oh-my-story/web"
)

const usage = "usage: oh-my-story serve | migrate | healthcheck"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		err = serve(ctx, cfg)
	case "migrate":
		var n int
		n, err = db.Migrate(ctx, cfg.DatabaseURL)
		if err == nil {
			slog.Info("migrations applied", "count", n)
		}
	case "healthcheck":
		err = healthcheck(ctx, cfg)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error(os.Args[1], "err", err)
		os.Exit(1)
	}
}

// healthcheck asks the local server for /healthz. It exists because the
// distroless runtime image has no curl for a container health check.
func healthcheck(ctx context.Context, cfg config.Config) error {
	host, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/healthz", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("/healthz returned %d", res.StatusCode)
	}
	return nil
}

func serve(ctx context.Context, cfg config.Config) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	var sender mail.Sender = mail.Log{}
	if cfg.SMTP.Addr != "" {
		sender = mail.SMTP{Addr: cfg.SMTP.Addr, Username: cfg.SMTP.Username, Password: cfg.SMTP.Password, From: cfg.SMTP.From}
	}
	var sso *httpapi.OIDC
	if cfg.OIDC != nil {
		if sso, err = httpapi.NewOIDC(ctx, *cfg.OIDC, cfg.BaseURL); err != nil {
			return fmt.Errorf("oidc discovery at %s: %w", cfg.OIDC.DiscoveryURL, err)
		}
		slog.Info("sso enabled", "issuer", cfg.OIDC.Issuer)
	}

	hub := live.New(pool)
	hub.Start(ctx)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(cfg, pool, sender, sso, hub, web.Handler(web.Dist())),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", cfg.HTTPAddr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
