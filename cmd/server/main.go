// Command server boots the Stage-1 IM server: load config.toml, wire all
// layers via internal/app, serve HTTP+WS with graceful shutdown.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"golang-im-neo-system/internal/app"
	"golang-im-neo-system/internal/config"
)

func parseDurOr(s string, dflt time.Duration) time.Duration {
	if s == "" {
		return dflt
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return dflt
}

func main() {
	configPath := flag.String("config", "config.toml", "path to config.toml (SSOT)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	a, err := app.Build(cfg)
	if err != nil {
		log.Fatalf("build app: %v", err)
	}

	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      a.Engine,
		ReadTimeout:  parseDurOr(cfg.Server.ReadTimeout, 30*time.Second),
		WriteTimeout: parseDurOr(cfg.Server.WriteTimeout, 30*time.Second),
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		a.Logger.Info("starting Golang IM Neo System",
			zap.String("addr", cfg.Addr()),
			zap.String("mode", cfg.Server.Mode),
			zap.String("db", cfg.Database.Path))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Logger.Fatal("listen failed", zap.Error(err))
		}
	}()

	<-quit
	a.Logger.Info("shutdown signal received, draining...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		a.Logger.Error("http shutdown failed", zap.Error(err))
	}
	if err := a.Close(); err != nil {
		a.Logger.Error("app close failed", zap.Error(err))
	}
	a.Logger.Info("server stopped cleanly")
}
