package config

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
)

// SeedSuperAdmin ensures the SuperAdmin from config.toml exists in the database.
// It uses bcrypt hashing (Cost from config) and is idempotent — if the admin already exists, it is left untouched.
// This implements the Gate-0 requirement: "启动时自动检测管理员账号，通过 bcrypt 哈希注入数据库" and Root vs Admin separation.
func SeedSuperAdmin(db *gorm.DB, cfg *Config) error {
	var count int64
	if err := db.Model(&store.User{}).Where("username = ?", cfg.Admin.Username).Count(&count).Error; err != nil {
		return fmt.Errorf("count admin: %w", err)
	}
	if count > 0 {
		// Admin already seeded — do not overwrite (Root vs Admin authority separation, VULNERABILITIES_AND_FIXES.md Gate 0-5).
		return nil
	}

	cost := cfg.Security.BcryptCost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Admin.Password), cost)
	if err != nil {
		return fmt.Errorf("bcrypt hash: %w", err)
	}

	admin := store.User{
		ID:           "u_superadmin_001",
		Username:     cfg.Admin.Username,
		PasswordHash: string(hash),
		Role:         "superadmin",
		Status:       1,
		TokenVersion: 1,
	}
	if err := db.Create(&admin).Error; err != nil {
		return fmt.Errorf("create superadmin: %w", err)
	}
	return nil
}
