// Command adr manages the architecture decision records in docs/decisions.
//
// An ADR is identified by its filename, YYYY-MM-DD-slug.md. Nothing allocates a
// number, so concurrent branches cannot collide on one; `adr check` is the gate
// that keeps the ids, the index, and every citation in agreement.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

func main() {
	command := &cli.Command{
		Name:  "adr",
		Usage: "manage architecture decision records",
		Commands: []*cli.Command{
			{
				Name:      "new",
				Usage:     "create a new ADR from today's date and a title",
				ArgsUsage: "<title>",
				Action:    runNew,
			},
			{
				Name:   "index",
				Usage:  "regenerate docs/decisions/README.md",
				Action: runIndex,
			},
			{
				Name:   "check",
				Usage:  "verify ids, metadata, citations, and the index",
				Action: runCheck,
			},
		},
	}
	if err := command.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "adr:", err)
		os.Exit(1)
	}
}

func runNew(_ context.Context, cmd *cli.Command) error {
	title := strings.TrimSpace(strings.Join(cmd.Args().Slice(), " "))
	if title == "" {
		return fmt.Errorf("a title is required: adr new \"The decision, as a sentence\"")
	}
	slug := slugify(title)
	if !strings.Contains(slug, "-") {
		return fmt.Errorf("title %q slugs to %q; use a phrase, not one word", title, slug)
	}

	adrs, _, err := loadDir(decisionsDir)
	if err != nil {
		return err
	}
	for _, a := range adrs {
		if a.Slug == slug {
			return fmt.Errorf("slug %q is already taken by %s", slug, a.Name)
		}
	}

	name := fmt.Sprintf("%s-%s.md", time.Now().Format("2006-01-02"), slug)
	path := filepath.Join(decisionsDir, name)
	body := fmt.Sprintf(newTemplate, title, time.Now().Format("2006-01-02"))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	if err := writeIndex(); err != nil {
		return err
	}
	fmt.Println(path)
	fmt.Printf("cite it as: ADR %s\n", slug)
	return nil
}

func runIndex(context.Context, *cli.Command) error {
	return writeIndex()
}

func writeIndex() error {
	adrs, problems, err := loadDir(decisionsDir)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("cannot index a directory that does not pass `adr check`:\n  %s",
			strings.Join(problems, "\n  "))
	}
	return os.WriteFile(filepath.Join(decisionsDir, indexFile), []byte(renderIndex(adrs)), 0o644)
}

func runCheck(context.Context, *cli.Command) error {
	adrs, problems, err := loadDir(decisionsDir)
	if err != nil {
		return err
	}

	files, err := trackedTextFiles()
	if err != nil {
		return err
	}
	problems = append(problems, checkRefs(adrs, files)...)

	want := renderIndex(adrs)
	got, err := os.ReadFile(filepath.Join(decisionsDir, indexFile))
	switch {
	case err != nil && !os.IsNotExist(err):
		return err
	case string(got) != want:
		problems = append(problems, fmt.Sprintf(
			"%s/%s is out of date. Run 'mise run generate:adr' and commit the result.",
			decisionsDir, indexFile))
	}

	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		return fmt.Errorf("%d ADR problem(s)", len(problems))
	}
	fmt.Printf("adr: %d decisions, ids and citations consistent\n", len(adrs))
	return nil
}

// trackedTextFiles reads the checked-in files that may cite an ADR. It asks git
// rather than walking, so build output and node_modules never enter the scan.
func trackedTextFiles() ([]sourceFile, error) {
	out, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var files []sourceFile
	for rel := range strings.SplitSeq(string(out), "\x00") {
		if rel == "" {
			continue
		}
		info, err := os.Lstat(rel)
		// Symlinked docs (CLAUDE.md -> AGENTS.md) would be scanned twice and
		// reported against a path their content does not live at.
		if err != nil || info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			continue
		}
		raw, err := os.ReadFile(rel)
		if err != nil {
			return nil, err
		}
		if bytes.IndexByte(raw, 0) >= 0 {
			continue
		}
		files = append(files, sourceFile{Path: rel, Content: string(raw)})
	}
	return files, nil
}
