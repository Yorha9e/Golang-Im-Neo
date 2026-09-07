package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New creates a Zap structured logger with sensible defaults for the IM system.
// It uses JSON encoding in production and console in debug, with ISO8601 timestamps.
func New(mode string) (*zap.Logger, error) {
	var cfg zap.Config
	if mode == "release" || mode == "production" {
		cfg = zap.NewProductionConfig()
		cfg.Encoding = "json"
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.Encoding = "console"
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	return cfg.Build()
}

// Must is a helper that panics on error (convenient for startup).
func Must(mode string) *zap.Logger {
	l, err := New(mode)
	if err != nil {
		panic(err)
	}
	return l
}
