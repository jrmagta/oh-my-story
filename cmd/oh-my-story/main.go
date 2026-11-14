package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jrmagta/oh-my-story/internal/config"
	"github.com/jrmagta/oh-my-story/internal/db"
	"github.com/jrmagta/oh-my-story/internal/httpapi"
)

const usage = "usage: oh-my-story serve | migrate"

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
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error(os.Args[1], "err", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, cfg config.Config) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewHandler(pool),
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
