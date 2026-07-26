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
				Flags: []cli.Flag{
					&cli.DurationFlag{
						Name: "wait",
						Usage: "keep retrying for this long before failing, for a launcher that " +
							"starts devserver and the app together (default: probe once)",
					},
				},
				Action: checkProxyAction(logger),
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

		// --wait exists for a launcher (.solo.yml) that starts devserver and the
		// app in the same breath: `go run ./cmd/devserver` has to link before it
		// binds, so a single probe would usually lose that race. Waiting is the
		// launcher's concern, not the task's — someone running desktop:dev by
		// hand wants to be told immediately, which is why the default is one
		// probe.
		deadline := time.Now().Add(cmd.Duration("wait"))
		for attempt := 0; ; attempt++ {
			switch devproxy.Probe(ctx, base) {
			case devproxy.StatusRunning:
				logger.Info().Str("api_base", base).Msg("development GitHub proxy is reachable")
				return nil
			case devproxy.StatusForeign:
				// Retrying cannot help: something else owns the port, and it is
				// not going to become devserver.
				return cli.Exit(base+" is answering but is not devserver", 1)
			case devproxy.StatusAbsent:
				if time.Now().After(deadline) {
					fmt.Fprint(os.Stderr, "\n"+devproxy.NotRunningHelp(base)+"\n")
					return cli.Exit("development GitHub proxy is not running", 1)
				}
				if attempt == 0 {
					logger.Info().Str("api_base", base).Msg("waiting for the development GitHub proxy")
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(probeInterval):
			}
		}
	}
}

// probeInterval paces the --wait retry loop. The target is loopback, so this is
// about not spinning rather than about network cost.
const probeInterval = 500 * time.Millisecond

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
