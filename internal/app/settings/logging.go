package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
)

const (
	logFileName = "desktop.log"
	EnvLogLevel = "HIVE_DESKTOP_LOG_LEVEL"
)

// ResolveLogLevel validates the process log-level override once at startup.
func ResolveLogLevel() (zerolog.Level, error) {
	value := os.Getenv(EnvLogLevel)
	if value == "" {
		return zerolog.InfoLevel, nil
	}
	level, err := zerolog.ParseLevel(value)
	if err != nil {
		return zerolog.InfoLevel, fmt.Errorf("parse %s: %w", EnvLogLevel, err)
	}
	return level, nil
}

// NewLogger builds the root logger at the resolved immutable path and level.
func NewLogger(path string, level zerolog.Level) (zerolog.Logger, func(), error) {
	stderr := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		l := zerolog.New(stderr).With().Timestamp().Logger().Level(level)
		return l, func() {}, fmt.Errorf("create desktop log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		l := zerolog.New(stderr).With().Timestamp().Logger().Level(level)
		return l, func() {}, fmt.Errorf("open desktop log file: %w", err)
	}
	fileW := zerolog.ConsoleWriter{Out: f, NoColor: true, TimeFormat: time.RFC3339}
	l := zerolog.New(zerolog.MultiLevelWriter(fileW, stderr)).With().Timestamp().Logger().Level(level)
	return l, func() { _ = f.Close() }, nil
}

// LogFile is retained for tests and e2e helpers; runtime uses Paths.LogFile.
func LogFile() string { return defaultPaths().LogFile }
