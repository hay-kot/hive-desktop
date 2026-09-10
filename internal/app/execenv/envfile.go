package execenv

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/rs/zerolog"
)

// desktopPrefix is the settings namespace. A name under it belongs to
// settings.yaml, which is the file that named this one, so honouring it here
// would apply on a later reload and not at startup — the app disagreeing with
// itself, which is the failure this feature exists to stop.
const desktopPrefix = "HIVE_DESKTOP_"

// reserved reports whether the app owns a name outright. PATH is the resolver's
// answer, layered shell-first over the inherited value and the package-manager
// prefixes; a file that set it would either lose silently or reorder that.
func reserved(name string) bool {
	return name == "PATH" || strings.HasPrefix(name, desktopPrefix)
}

// ApplyFile seeds this process's environment from an env file, once, at
// startup. Every later reader — this package's Getenv and Environ, hive's
// config load, an internal/app/secrets `env:` reference — then answers from the
// process environment as it always has, so the file adds a source of values
// without adding a resolver
// (ADR the-desktop-seeds-its-environment-from-a-file-the-settings-name).
//
// A name this process already carries wins: presence, not a non-empty value, so
// a launch can opt out of a file's value by setting it to nothing. A missing
// file is a no-op. A file that does not parse applies nothing at all — half of
// an environment is worse than none — and the error names the line, never the
// value on it.
func ApplyFile(path string, logger zerolog.Logger) error {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		logger.Debug().Str("path", path).Msg("no environment file")
		return nil
	case err != nil:
		return fmt.Errorf("read environment file %s: %w", path, err)
	}

	values, err := parseEnvFile(data)
	if err != nil {
		return fmt.Errorf("parse environment file %s: %w", path, err)
	}

	var applied, skipped, refused []string
	for _, name := range slices.Sorted(maps.Keys(values)) {
		switch {
		case reserved(name):
			refused = append(refused, name)
		case alreadySet(name):
			skipped = append(skipped, name)
		default:
			if err := os.Setenv(name, values[name]); err != nil {
				return fmt.Errorf("set %s from environment file %s: %w", name, path, err)
			}
			applied = append(applied, name)
		}
	}

	if len(refused) > 0 {
		logger.Warn().Str("path", path).Strs("variables", refused).
			Msg("environment file sets variables the app owns; ignored")
	}
	if len(skipped) > 0 {
		logger.Debug().Strs("variables", skipped).
			Msg("environment file variables already set for this launch; kept")
	}
	if len(applied) == 0 {
		logger.Debug().Str("path", path).Msg("environment file set nothing this launch did not already have")
		return nil
	}
	logger.Info().Str("path", path).Strs("variables", applied).
		Msg("seeded the process environment from the environment file")
	return nil
}

// alreadySet is presence, not a non-empty value, and is named apart from
// Environ's local `defined` only because a package-level name would shadow it.
func alreadySet(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}

// parseEnvFile reads NAME=value lines. `export ` is accepted because the file
// is what people paste out of a shell startup file. A value is single-quoted
// (literal), double-quoted (Go escapes) or bare, and a bare value's trailing
// `# comment` is a comment rather than part of it — the convention every dotenv
// reader follows, and the one a user writing this by hand expects.
func parseEnvFile(data []byte) (map[string]string, error) {
	values := make(map[string]string)
	for number, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, rest, found := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		name = strings.TrimSpace(name)
		if !found || !isEnvName(name) {
			return nil, fmt.Errorf("line %d: expected NAME=value", number+1)
		}
		if _, duplicate := values[name]; duplicate {
			return nil, fmt.Errorf("line %d: %s is set twice", number+1, name)
		}
		value, err := parseValue(strings.TrimLeft(rest, " \t"))
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %w", number+1, name, err)
		}
		values[name] = value
	}
	return values, nil
}

var errUnterminated = errors.New("a quoted value must close on the same line")

func parseValue(raw string) (string, error) {
	if raw == "" || (raw[0] != '"' && raw[0] != '\'') {
		return trimBare(raw), nil
	}

	quote := raw[0]
	var value strings.Builder
	escaped := false
	for index := 1; index < len(raw); index++ {
		char := raw[index]
		switch {
		case escaped:
			value.WriteString(unescape(char))
			escaped = false
		// Only a double-quoted value takes escapes: inside single quotes a
		// backslash is a backslash, which is what makes them the way to write a
		// value the parser must not touch.
		case char == '\\' && quote == '"':
			escaped = true
		case char == quote:
			if tail := strings.TrimSpace(raw[index+1:]); tail != "" && !strings.HasPrefix(tail, "#") {
				return "", errors.New("a quoted value must end the line")
			}
			return value.String(), nil
		default:
			value.WriteByte(char)
		}
	}
	return "", errUnterminated
}

// trimBare cuts a bare value at a `#` that follows whitespace. Without the
// whitespace rule a URL fragment or a colour would lose its tail.
func trimBare(raw string) string {
	for index := 1; index < len(raw); index++ {
		if raw[index] == '#' && (raw[index-1] == ' ' || raw[index-1] == '\t') {
			raw = raw[:index]
			break
		}
	}
	return strings.TrimRight(raw, " \t")
}

// unescape expands the escapes a double-quoted value may carry. Anything else
// stands for itself, so a Windows path written with single backslashes survives
// rather than losing them.
func unescape(char byte) string {
	switch char {
	case 'n':
		return "\n"
	case 'r':
		return "\r"
	case 't':
		return "\t"
	default:
		return string(char)
	}
}
