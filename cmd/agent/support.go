package main

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/liki/liki-agent/internal/domain"
	"github.com/liki/liki-agent/internal/platform/config"
)

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type randomIDs struct{}

func (randomIDs) New(prefix string) domain.ID {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return domain.ID(prefix + "_fallback")
	}
	return domain.ID(prefix + "_" + hex.EncodeToString(buf))
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	options := &slog.HandlerOptions{Level: level}
	if strings.ToLower(cfg.LogFormat) == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, options))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, options))
}
