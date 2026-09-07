package store

import (
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DBConfig holds the minimal database configuration needed by the store layer.
// This avoids an import cycle with internal/config (store is a lower layer).
type DBConfig struct {
	Path                   string
	MaxOpenConns           int
	MaxIdleConns           int
	BusyTimeout            string
	WalCheckpointTruncate  bool
}

// NewDB creates and configures the SQLite database with Gate-0 safety primitives:
// - PRAGMA journal_mode=WAL (40GB, single-writer, reader concurrency)
// - PRAGMA synchronous=FULL (physical durable, Gate 0-3)
// - PRAGMA busy_timeout, foreign_keys, wal_autocheckpoint
// - Single writer pool MaxOpenConns=1 (eliminates database is locked)
// - Read pool is the same handle with WAL — readers do not block writer.
// The function also auto-migrates all models and creates the UNIQUE(cov_id, stanza_id) dedup constraint.
func NewDB(cfg DBConfig) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0755); err != nil {
		return nil, fmt.Errorf("mkdir data dir: %w", err)
	}

	busy := cfg.BusyTimeout
	if busy == "" {
		busy = "5000"
	}

	// DSN with WAL-relevant query params. GORM sqlite driver passes DSN to mattn/go-sqlite3.
	// We also execute PRAGMAs post-open to guarantee they are applied regardless of driver defaults.
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=%s&_foreign_keys=on",
		cfg.Path, busy)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", cfg.Path, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}

	// Gate 0-3 & 0-4: single writer pool + WAL durability.
	// MaxOpenConns=1 guarantees only one writer at a time; MaxIdleConns=1 keeps the handle warm.
	// This is the sole write path — all writes go through the async batch worker (Sync-DB-First allocator also uses this handle).
	maxOpen := cfg.MaxOpenConns
	if maxOpen == 0 {
		maxOpen = 1
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle == 0 {
		maxIdle = 1
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)

	// PRAGMA enforcement — must be executed on the writer connection.
	// synchronous=FULL ensures WAL tail is fsynced on commit (crash-safe, Gate 0-3).
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA wal_autocheckpoint=1000",
		fmt.Sprintf("PRAGMA busy_timeout=%s", busy),
	}
	for _, p := range pragmas {
		if err := db.Exec(p).Error; err != nil {
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}

	// Auto-migrate all domain tables.
	if err := db.AutoMigrate(
		&User{},
		&UserSession{},
		&Friendship{},
		&Conversation{},
		&Message{},
		&MediaAsset{},
		&UserPost{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	// Gate 0-1 & 0-2: idempotency constraint UNIQUE(cov_id, stanza_id) for exactly-once semantics.
	if err := ensureExtraIndexes(db); err != nil {
		return nil, err
	}

	return db, nil
}

func ensureExtraIndexes(db *gorm.DB) error {
	// Message dedup: UNIQUE(cov_id, stanza_id) — client stanza_id replay delivers same seq without duplicate insert.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_cov_stanza ON messages(cov_id, stanza_id)`).Error; err != nil {
		return fmt.Errorf("create uq_cov_stanza: %w", err)
	}
	// Friendship partial unique (already via tag) but ensure correct WHERE clause on SQLite.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_active_friendship ON friendships(user_id, friend_id) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("ensure friendship index: %w", err)
	}
	// Session composite index for fast revoked check.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_user_class ON user_sessions(user_id, device_class, is_revoked)`).Error; err != nil {
		return fmt.Errorf("ensure session index: %w", err)
	}
	return nil
}

// Checkpoint executes PRAGMA wal_checkpoint(TRUNCATE) to reclaim WAL disk space.
// Called periodically when WAL exceeds threshold (e.g., 1GB) or during idle, per DATABASE_DESIGN_SPEC.md 1.3.
func Checkpoint(db *gorm.DB) error {
	return db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
}
