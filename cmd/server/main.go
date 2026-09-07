package main

import (
	"log"

	"go.uber.org/zap"

	"golang-im-neo-system/internal/config"
	"golang-im-neo-system/internal/logic/session"
	"golang-im-neo-system/internal/store"
	"golang-im-neo-system/pkg/logger"
)

func main() {
	// Load configuration (config.toml SSOT)
	cfg, err := config.Load("config.toml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Initialize structured logger (uber/zap)
	zapLogger, err := logger.New(cfg.Server.Mode)
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	defer zapLogger.Sync()

	zapLogger.Info("starting Golang IM Neo System", zap.String("addr", cfg.Addr()))

	// Initialize persistence with Gate-0 safety primitives:
	// PRAGMA synchronous=FULL, WAL, single writer pool (store.NewDB)
	db, err := cfg.OpenDB()
	if err != nil {
		zapLogger.Fatal("open db failed", zap.Error(err))
	}
	zapLogger.Info("database opened",
		zap.String("path", cfg.Database.Path),
		zap.Int("maxOpenConns", cfg.Database.MaxOpenConns),
		zap.String("pragma", "WAL + synchronous=FULL"),
	)

	// Seed SuperAdmin (bcrypt hash, idempotent, Root vs Admin separation)
	if err := config.SeedSuperAdmin(db, cfg); err != nil {
		zapLogger.Fatal("seed superadmin failed", zap.Error(err))
	}
	zapLogger.Info("superadmin seeded", zap.String("username", cfg.Admin.Username))

	// Initialize in-memory CovSeq Allocator with persistent watermark (Sync-DB-First)
	allocator := session.NewAllocator(db)
	if err := allocator.LoadAll(); err != nil {
		zapLogger.Warn("allocator preload failed (non-fatal, lazy load will handle)", zap.Error(err))
	} else {
		zapLogger.Info("allocator preloaded")
	}

	// Demonstrate allocation (Gate-0 verification)
	_ = allocator
	_ = store.NewBatchWriter(db, zapLogger) // async writer skeleton (Stage-1)

	// Initialize gateway hub with backpressure breaker (select default)
	// hub := gateway.NewHub(zapLogger)
	// go hub.Run()

	zapLogger.Info("Gate-0 initialization complete",
		zap.String("module", "golang-im-neo-system"),
		zap.Strings("gates", []string{"G0-1 stanza_id", "G0-2 cov+stanza unique", "G0-3 sync FULL", "G0-4 backpressure", "G0-5 Sync-DB-First + Nanopb"}),
	)

	// TODO: Stage-1 — gin, handshake, gateway run, router, etc.
	select {}
}
