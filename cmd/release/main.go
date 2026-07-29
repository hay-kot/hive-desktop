// Command release manages Hive Desktop versions and publishes signed macOS releases.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/urfave/cli/v3"
)

func main() {
	command := newReleaseCommand()
	if err := command.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func newReleaseCommand() *cli.Command {
	return &cli.Command{
		Name:  "release",
		Usage: "manage Hive Desktop versions and publish signed macOS releases",
		Description: "Selects and validates versions against both Git tags and live channel manifests, " +
			"then builds, signs, notarizes, uploads, and verifies desktop releases.",
		Commands: []*cli.Command{
			{
				Name:      "next",
				Usage:     "print the next version for a release channel",
				ArgsUsage: "<dev|beta|stable>",
				Description: "Selects the next version from all local desktop-v* tags and the live stable, " +
					"beta, and dev manifests. A missing live manifest (HTTP 404) is an empty channel.",
				Action: withRepoRoot(func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() != 1 || !validChannel(cmd.Args().First()) {
						return cli.Exit("expected exactly one channel: dev, beta, or stable", 2)
					}
					versions, _, err := releaseVersions(ctx)
					if err != nil {
						return err
					}
					fmt.Println(nextVersion(cmd.Args().First(), versions))
					return nil
				}),
			},
			{
				Name:      "prepare",
				Usage:     "select and validate a release candidate",
				ArgsUsage: "<dev|beta|stable> [version]",
				Description: "Requires a clean current main, selects the next version when version is omitted, validates advancement " +
					"across every affected live manifest, rejects existing local or origin tags, and prints the commit and manifest cascade.",
				Action: withRepoRoot(func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 1 || cmd.NArg() > 2 || !validChannel(cmd.Args().First()) {
						return cli.Exit("expected a channel (dev, beta, or stable) and optional version", 2)
					}
					return prepare(ctx, cmd.Args().First(), cmd.Args().Get(1))
				}),
			},
			{
				Name:      "publish",
				Usage:     "build, sign, notarize, and publish a release",
				ArgsUsage: "<version>",
				Description: "Public publishing requires a clean current main or a matching CI tag on main; the command builds the universal macOS app, signs it, " +
					"notarizes and staples it, packages and verifies it, uploads immutable artifacts to R2, updates the channel cascade, and verifies the public artifact. " +
					"For local publishing, run this through `mise run release:desktop -- <version>` so mise loads credentials.",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "skip-notarize", Usage: "skip notarization and stapling (requires --skip-upload)"},
					&cli.BoolFlag{Name: "skip-upload", Usage: "build and package without publishing"},
					&cli.BoolFlag{Name: "skip-web", Usage: "skip deploying and verifying the web landing page and worker"},
					&cli.BoolFlag{Name: "force", Usage: "permit overwriting an existing immutable release"},
				},
				Action: withRepoRoot(func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() != 1 {
						return cli.Exit("expected exactly one version", 2)
					}
					args := []string{cmd.Args().First()}
					for _, flag := range []string{"skip-notarize", "skip-upload", "skip-web", "force"} {
						if cmd.Bool(flag) {
							args = append(args, "--"+flag)
						}
					}
					return publish(ctx, args)
				}),
			},
			{
				Name:      "github",
				Usage:     "push the release tag and create the GitHub release",
				ArgsUsage: "<version>",
				Description: "Records a published version on GitHub: pushes the lightweight desktop-v<version> tag and creates a GitHub Release whose notes " +
					"capture the commits since the previous desktop release tag (dev and beta are marked prerelease). Downloads still come from R2 (decision 0003); " +
					"this attaches no artifacts. Idempotent — safe to re-run to record a release whose GitHub step failed after the R2 upload. Requires an authenticated gh.",
				Action: withRepoRoot(func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() != 1 {
						return cli.Exit("expected exactly one version", 2)
					}
					version, err := parsePublishVersion(cmd.Args().First())
					if err != nil {
						return fmt.Errorf("invalid version %q: %w", cmd.Args().First(), err)
					}
					if err := validatePublishSource(ctx, version); err != nil {
						return err
					}
					return publishGitHubRelease(ctx, version)
				}),
			},
			{
				Name:      "verify",
				Usage:     "verify live manifests and the public artifact",
				ArgsUsage: "<version>",
				Description: "Requires every affected manifest to agree on the version and artifact metadata, " +
					"then downloads the artifact and verifies its size and SHA-256 checksum.",
				Action: withRepoRoot(func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() != 1 {
						return cli.Exit("expected exactly one version", 2)
					}
					version, err := parsePublishVersion(cmd.Args().First())
					if err != nil {
						return fmt.Errorf("invalid version %q: %w", cmd.Args().First(), err)
					}
					return verifyLive(ctx, version)
				}),
			},
		},
	}
}

func withRepoRoot(action cli.ActionFunc) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		root, err := commandOutput(ctx, "git", "rev-parse", "--show-toplevel")
		if err != nil {
			return err
		}
		if err := os.Chdir(strings.TrimSpace(root)); err != nil {
			return fmt.Errorf("change to repository root: %w", err)
		}
		return action(ctx, cmd)
	}
}

func prepare(ctx context.Context, channel, candidate string) error {
	if err := validatePrepareSource(ctx); err != nil {
		return err
	}
	versions, manifests, err := releaseVersions(ctx)
	if err != nil {
		return err
	}
	if candidate == "" {
		candidate = nextVersion(channel, versions)
	}
	version, err := parsePublishVersion(candidate)
	if err != nil {
		return fmt.Errorf("invalid candidate %q: %w", candidate, err)
	}
	if version.channel() != channel {
		return fmt.Errorf("candidate %s belongs to %s, not %s", candidate, version.channel(), channel)
	}
	if err := validateManifestAdvancement(version, manifests); err != nil {
		return err
	}

	tag := "desktop-v" + version.String()
	if exists, err := localTagExists(ctx, tag); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("tag %s already exists locally", tag)
	}
	remote, err := commandOutput(ctx, "git", "ls-remote", "--tags", "origin", "refs/tags/"+tag)
	if err != nil {
		return fmt.Errorf("check origin tag %s: %w", tag, err)
	}
	if strings.TrimSpace(remote) != "" {
		return fmt.Errorf("tag %s already exists on origin", tag)
	}

	commit, err := commandOutput(ctx, "git", "show", "-s", "--format=%H%n%s", "HEAD")
	if err != nil {
		return err
	}
	lines := strings.SplitN(strings.TrimSpace(commit), "\n", 2)
	fmt.Printf("candidate: %s\n", version.String())
	fmt.Printf("channel: %s\n", channel)
	fmt.Printf("commit: %s\n", lines[0])
	if len(lines) == 2 {
		fmt.Printf("subject: %s\n", lines[1])
	}
	fmt.Printf("manifests: %s\n", strings.Join(version.affectedChannels(), "+"))
	for _, currentChannel := range []string{"stable", "beta", "dev"} {
		if manifest, ok := manifests[currentChannel]; ok {
			fmt.Printf("current-%s: %s\n", currentChannel, manifest.Version)
		} else {
			fmt.Printf("current-%s: empty\n", currentChannel)
		}
	}
	return nil
}

func localTagExists(ctx context.Context, tag string) (bool, error) {
	output, err := commandOutput(ctx, "git", "tag", "--list", tag)
	if err != nil {
		return false, fmt.Errorf("check local tag %s: %w", tag, err)
	}
	return strings.TrimSpace(output) != "", nil
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.Output()
	if err != nil {
		exitErr := new(exec.ExitError)
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("%s: %s", name, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return string(output), nil
}
