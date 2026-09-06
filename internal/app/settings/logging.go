package settings

import (
	"fmt"
	"io"
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
//
// Each extra writer is an additional arm of the same MultiLevelWriter and
// receives the encoded JSON event, not the console rendering the two built-in
// arms produce — the two ConsoleWriters parse that same JSON to pretty-print
// it. That is the seam a log bridge attaches to: a zerolog.Hook is handed only
// the level and the message, while an arm sees the event's fields.
//
// An extra writer must not fail the write or block: it is a tap on the log
// pipeline, and an error here would be an error about an error.
func NewLogger(path string, level zerolog.Level, extra ...io.Writer) (zerolog.Logger, func(), error) {
	stderr := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	build := func(writers ...io.Writer) zerolog.Logger {
		return zerolog.New(zerolog.MultiLevelWriter(writers...)).With().Timestamp().Logger().Level(level)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return build(append([]io.Writer{stderr}, extra...)...), func() {}, fmt.Errorf("create desktop log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return build(append([]io.Writer{stderr}, extra...)...), func() {}, fmt.Errorf("open desktop log file: %w", err)
	}
	fileW := zerolog.ConsoleWriter{Out: f, NoColor: true, TimeFormat: time.RFC3339}
	l := build(append([]io.Writer{fileW, stderr}, extra...)...)
	return l, func() { _ = f.Close() }, nil
}

// LogFile is retained for tests and e2e helpers; runtime uses Paths.LogFile.
func LogFile() string { return defaultPaths().LogFile }
