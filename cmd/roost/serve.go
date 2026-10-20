package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/config"
	"github.com/manasseh-fikadu/roost/web"
)

// shutdownTimeout is how long in-flight requests get to finish after a stop signal.
const shutdownTimeout = 10 * time.Second

func serve(ctx context.Context, args []string, stderr io.Writer) error {
	cfg, err := config.Parse(args, os.Getenv, stderr)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))

	ui := web.FS()
	if ui == nil {
		logger.Warn("built without the web UI, so only /api is served; `make build` embeds it")
	}

	srv := &http.Server{
		Handler:           api.New(api.Options{Version: buildVersion(), UI: ui, Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	logger.Info("roost is running", "version", buildVersion(), "url", "http://"+ln.Addr().String())

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
