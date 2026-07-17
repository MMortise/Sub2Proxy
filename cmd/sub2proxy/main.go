// Command sub2proxy runs the control plane: it loads config.yaml, starts the
// embedded mihomo data plane, and serves the REST API + web UI.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wuxi/sub2proxy/internal/api"
	"github.com/wuxi/sub2proxy/internal/config"
	"github.com/wuxi/sub2proxy/internal/core"
	"github.com/wuxi/sub2proxy/web"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	configPath := flag.String("config", "/data/config.yaml", "path to config.yaml")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	api.Version = version

	cfg, err := config.Load(*configPath, func(msg string) { logger.Warn("config", "msg", msg) })
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}

	app := core.New(cfg, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Start(ctx); err != nil {
		logger.Error("start app", "err", err)
		os.Exit(1)
	}

	server := api.NewServer(app, cfg.AuthKey, web.FS())
	httpSrv := &http.Server{Addr: cfg.Listen, Handler: server.Handler()}

	go func() {
		logger.Info("listening", "addr", cfg.Listen, "version", version)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown", "err", err)
	}
	app.Shutdown()
}
