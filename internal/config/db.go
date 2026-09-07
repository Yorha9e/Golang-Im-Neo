package config

import (
	"golang-im-neo-system/internal/store"

	"gorm.io/gorm"
)

// OpenDB opens the database using the store layer with Gate-0 safety primitives.
// It converts the config's database block into the store's DBConfig to avoid an import cycle.
func (c *Config) OpenDB() (*gorm.DB, error) {
	return store.NewDB(store.DBConfig{
		Path:                  c.Database.Path,
		MaxOpenConns:          c.Database.MaxOpenConns,
		MaxIdleConns:          c.Database.MaxIdleConns,
		BusyTimeout:           c.Database.BusyTimeout,
		WalCheckpointTruncate: c.Database.WalCheckpointTruncate,
	})
}
