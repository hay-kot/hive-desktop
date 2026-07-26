// Command devtools prepares an isolated worktree-local Hive Desktop
// development instance and writes the environment mise loads before launch.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v3"

	"github.com/hay-kot/hive-desktop/cmd/internal/devproxy"
)

const envLogLevel = "HIVE_DESKTOP_DEVTOOLS_LOG_LEVEL"

func main() {
	logger := newConsoleLogger(os.Stderr, zerolog.InfoLevel)
	command := newDevtoolsCommand(&logger)
	if err := command.Run(context.Background(), os.Args); err != nil {
		logger.Error().Err(err).Msg("devtools failed")
		os.Exit(exitCode(err))
	}
}

func newConsoleLogger(out io.Writer, level zerolog.Level) zerolog.Logger {
	writer := zerolog.ConsoleWriter{Out: out, TimeFormat: time.Kitchen}
	return zerolog.New(writer).With().Timestamp().Logger().Level(level)
}

func newDevtoolsCommand(logger *zerolog.Logger) *cli.Command {
	return &cli.Command{
		Name:        "devtools",
		Usage:       "manage an isolated Hive Desktop development instance",
		Description: "Prepares reusable worktree-local data and config and writes launch.env for mise. Wails runs directly from the mise task with that environment.",
		// Main owns structured error logging and process exit codes.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:             "log-level",
				Usage:            "set log verbosity (trace, debug, info, warn, error, fatal, panic, disabled)",
				Value:            zerolog.InfoLevel.String(),
				Sources:          cli.EnvVars(envLogLevel),
				Local:            true,
				ValidateDefaults: true,
				Validator: func(value string) error {
					_, err := zerolog.ParseLevel(value)
					return err
				},
			},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			level, err := zerolog.ParseLevel(cmd.String("log-level"))
			if err != nil {
				return nil, err
			}
			*logger = newConsoleLogger(os.Stderr, level)
			return ctx, nil
		},
		Commands: []*cli.Command{
			{
				Name:        "prepare",
				Usage:       "prepare or reuse this worktree's development instance",
				Description: "Seeds .hive-desktop on first use and writes launch.env with isolated paths and non-conflicting Wails/Vite ports.",
				Action:      devtoolsAction(logger, func(tools *devtools) error { return tools.withLock(func() error { return tools.prepare(false) }) }),
			},
			{
				Name:        "fresh",
				Usage:       "delete and reseed this worktree's development instance",
				Description: "Safely removes the marked instance and launch.env, then snapshots installed state and allocates fresh framework ports.",
				Action:      devtoolsAction(logger, func(tools *devtools) error { return tools.withLock(func() error { return tools.prepare(true) }) }),
			},
			{
				Name:        "reset",
				Usage:       "remove this worktree's development instance",
				Description: "Safely removes the marked .hive-desktop directory and generated launch.env without starting Wails.",
				Action:      devtoolsAction(logger, func(tools *devtools) error { return tools.withLock(tools.reset) }),
			},
			{
				Name:        "check-proxy",
				Usage:       "verify the development GitHub proxy is reachable",
				Description: "Reads the effective HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE and fails with instructions when no devserver answers there. Development is proxied by default, so this turns connection-refused-on-every-GitHub-call into one actionable message before Wails starts. An empty value means direct-to-GitHub and passes.",
				Action:      checkProxyAction(logger),
			},
		},
	}
}

// checkProxyAction is the desktop:dev preflight. It reads the *effective*
// environment — mise has already layered launch.env and overrides.env by the
// time the task runs — so opting out in overrides.env silently disables the
// check rather than needing a separate task.
func checkProxyAction(logger *zerolog.Logger) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		if cmd.NArg() != 0 {
			return cli.Exit(cmd.Name+" does not accept positional arguments", 2)
		}
		base := strings.TrimSpace(os.Getenv(devproxy.EnvAPIBase))
		if base == "" {
			logger.Debug().Msg("no GitHub API base override; talking to api.github.com directly")
			return nil
		}

		// One probe, no retry. devserver binds in the time it takes to link one
		// Go binary, while the tab this guards goes on to build Wails bindings
		// and Vite before anything reaches GitHub — so a proxy that is genuinely
		// starting has always won that race by the time the app would need it.
		// What is left to catch is the proxy nobody started, and for that the
		// only useful behaviour is to say so immediately.
		switch devproxy.Probe(ctx, base) {
		case devproxy.StatusRunning:
			logger.Info().Str("api_base", base).Msg("development GitHub proxy is reachable")
			return nil
		case devproxy.StatusForeign:
			return cli.Exit(base+" is answering but is not devserver", 1)
		default:
			fmt.Fprint(os.Stderr, "\n"+notRunningHelp(base)+"\n")
			return cli.Exit("development GitHub proxy is not running", 1)
		}
	}
}

// notRunningHelp is the guidance for a redirected instance with no proxy behind
// it. Every dev run is proxied by default (ADR 0017), so this is the one failure
// the default path can produce, and it is worth spelling out both ways out
// rather than leaving connection-refused errors to be interpreted.
func notRunningHelp(baseURL string) string {
	return fmt.Sprintf(`This worktree routes GitHub through the development proxy, but nothing is
listening at %s.

Start it (once per machine — every worktree shares it):

    mise run devserver

Or run against real GitHub instead, by putting this in the gitignored
overrides.env beside launch.env:

    %s=""
`, baseURL, devproxy.EnvAPIBase)
}

func devtoolsAction(logger *zerolog.Logger, action func(*devtools) error) cli.ActionFunc {
	return func(_ context.Context, cmd *cli.Command) error {
		if cmd.NArg() != 0 {
			return cli.Exit(cmd.Name+" does not accept positional arguments", 2)
		}
		worktree, err := findWorktree()
		if err != nil {
			return err
		}
		return action(newDevtools(worktree, *logger))
	}
}

func exitCode(err error) int {
	if cliExit, ok := errors.AsType[cli.ExitCoder](err); ok {
		return cliExit.ExitCode()
	}
	return 1
}
