// Command vendorhive syncs the hive internal packages that this repo depends
// on into internal/hivecore, rewriting import paths. The hive commit is pinned
// in vendor.lock; hive pkg/ packages stay a normal go.mod dependency pinned to
// the same commit. Vendored code is read-only — change hive first, re-run this.
//
// Usage:
//
//	go run ./scripts/vendorhive [-repo-path /path/to/hive] [-update <ref>] [-skip-gomod]
//
// With -repo-path the source tree is materialized from that local checkout
// (git archive); otherwise the repo from vendor.lock is cloned shallowly.
// -update resolves <ref> to a commit SHA, rewrites vendor.lock, then syncs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	lockPath    = "scripts/vendorhive/vendor.lock"
	vendorDir   = "internal/hivecore"
	hiveModule  = "github.com/colonyops/hive"
	selfModule  = "github.com/hay-kot/hive-desktop"
	hivePrefix  = hiveModule + "/internal/"
	localPrefix = selfModule + "/" + vendorDir + "/"
)

type lockFile struct {
	Repo string `json:"repo"`
	Ref  string `json:"ref"`
}

func main() {
	repoPath := flag.String("repo-path", "", "local hive checkout to read from (default: clone)")
	update := flag.String("update", "", "resolve this ref, rewrite vendor.lock, then sync")
	skipGomod := flag.Bool("skip-gomod", false, "skip go get/go mod tidy after sync")
	flag.Parse()

	if err := run(*repoPath, *update, *skipGomod); err != nil {
		fmt.Fprintln(os.Stderr, "vendorhive:", err)
		os.Exit(1)
	}
}

func run(repoPath, update string, skipGomod bool) error {
	lock, err := readLock()
	if err != nil {
		return err
	}
	if update != "" {
		sha, err := resolveRef(repoPath, lock.Repo, update)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", update, err)
		}
		lock.Ref = sha
		if err := writeLock(lock); err != nil {
			return err
		}
		fmt.Println("pinned", sha)
	}

	src, cleanup, err := materialize(repoPath, lock)
	if err != nil {
		return err
	}
	defer cleanup()

	seeds, err := scanSeeds(".")
	if err != nil {
		return err
	}
	if len(seeds) == 0 {
		return fmt.Errorf("no %s imports found outside %s", localPrefix, vendorDir)
	}

	if err := os.RemoveAll(vendorDir); err != nil {
		return err
	}

	pkgs, err := vendorClosure(src, seeds)
	if err != nil {
		return err
	}
	if err := rewriteImports(vendorDir); err != nil {
		return err
	}
	if err := writeMeta(src, lock, pkgs); err != nil {
		return err
	}
	fmt.Printf("vendored %d packages from %s@%s\n", len(pkgs), lock.Repo, lock.Ref)

	if skipGomod {
		return nil
	}
	if err := runCmd(".", "go", "get", hiveModule+"@"+lock.Ref); err != nil {
		return err
	}
	return runCmd(".", "go", "mod", "tidy")
}

func readLock() (lockFile, error) {
	var l lockFile
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return l, err
	}
	return l, json.Unmarshal(b, &l)
}

func writeLock(l lockFile) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath, append(b, '\n'), 0o644)
}

func resolveRef(repoPath, repo, ref string) (string, error) {
	if repoPath != "" {
		out, err := exec.Command("git", "-C", repoPath, "rev-parse", ref+"^{commit}").Output()
		return strings.TrimSpace(string(out)), err
	}
	out, err := exec.Command("git", "ls-remote", "https://"+repo+".git", ref).Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("ref not found on remote")
	}
	return fields[0], nil
}

// materialize produces a directory containing hive's internal/ tree and
// LICENSE at the pinned ref.
func materialize(repoPath string, lock lockFile) (dir string, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "vendorhive-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }

	if repoPath != "" {
		cmd := exec.Command("git", "-C", repoPath, "archive", lock.Ref, "internal", "LICENSE")
		tar := exec.Command("tar", "-x", "-C", tmp)
		tar.Stdin, err = cmd.StdoutPipe()
		if err == nil {
			err = tar.Start()
		}
		if err == nil {
			err = cmd.Run()
		}
		if err == nil {
			err = tar.Wait()
		}
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("git archive from %s: %w", repoPath, err)
		}
		return tmp, cleanup, nil
	}

	steps := [][]string{
		{"git", "clone", "--depth", "1", "--no-checkout", "https://" + lock.Repo + ".git", tmp},
		{"git", "-C", tmp, "fetch", "--depth", "1", "origin", lock.Ref},
		{"git", "-C", tmp, "checkout", lock.Ref, "--", "internal", "LICENSE"},
	}
	for _, s := range steps {
		if err := runCmd(".", s[0], s[1:]...); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return tmp, cleanup, nil
}

// scanSeeds walks this repo's Go files outside the vendor dir and collects
// hivecore package paths from their import declarations.
func scanSeeds(root string) (map[string]bool, error) {
	seeds := map[string]bool{}
	skip := map[string]bool{".git": true, "node_modules": true, vendorDir: true}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(path)
		if d.IsDir() {
			if skip[rel] || skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		for _, imp := range fileImports(path) {
			if p, ok := strings.CutPrefix(imp, localPrefix); ok {
				seeds[p] = true
			}
		}
		return nil
	})
	return seeds, err
}

// vendorClosure copies the seed packages and, transitively, every hive
// internal package imported by copied files (tests included). Returns the
// sorted package list.
func vendorClosure(src string, seeds map[string]bool) ([]string, error) {
	queue := make([]string, 0, len(seeds))
	for p := range seeds {
		queue = append(queue, p)
	}
	sort.Strings(queue)
	done := map[string]bool{}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if done[p] {
			continue
		}
		done[p] = true
		srcDir := filepath.Join(src, "internal", filepath.FromSlash(p))
		dstDir := filepath.Join(vendorDir, filepath.FromSlash(p))
		more, err := copyPackage(srcDir, dstDir)
		if err != nil {
			return nil, fmt.Errorf("package %s: %w", p, err)
		}
		sort.Strings(more)
		queue = append(queue, more...)
	}
	pkgs := make([]string, 0, len(done))
	for p := range done {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	return pkgs, nil
}

// copyPackage copies one package directory: its files, testdata, and asset
// subdirectories containing no Go code. Subdirectories with Go files are
// separate packages and are vendored only if imported. Returns the hive
// internal packages imported by the copied Go files.
func copyPackage(srcDir, dstDir string) ([]string, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, err
	}
	var imports []string
	for _, e := range entries {
		srcPath := filepath.Join(srcDir, e.Name())
		dstPath := filepath.Join(dstDir, e.Name())
		if e.IsDir() {
			if e.Name() == "testdata" || !treeHasGo(srcPath) {
				if err := copyTree(srcPath, dstPath); err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return nil, err
		}
		if strings.HasSuffix(e.Name(), ".go") {
			for _, imp := range fileImports(dstPath) {
				if p, ok := strings.CutPrefix(imp, hivePrefix); ok {
					imports = append(imports, p)
				}
			}
		}
	}
	return imports, nil
}

func treeHasGo(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// fileImports returns the import paths of a Go file. Parse errors yield nil;
// the subsequent build will surface them with better messages.
func fileImports(path string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		out = append(out, strings.Trim(imp.Path.Value, `"`))
	}
	return out
}

// rewriteImports maps hive internal import paths to their vendored location
// in every vendored Go file.
func rewriteImports(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		nb := strings.ReplaceAll(string(b), hivePrefix, localPrefix)
		if nb == string(b) {
			return nil
		}
		return os.WriteFile(path, []byte(nb), 0o644)
	})
}

func writeMeta(src string, lock lockFile, pkgs []string) error {
	if err := copyFile(filepath.Join(src, "LICENSE"), filepath.Join(vendorDir, "LICENSE")); err != nil {
		return fmt.Errorf("copy LICENSE: %w", err)
	}
	var b strings.Builder
	b.WriteString("# Vendored hive core\n\n")
	fmt.Fprintf(&b, "Vendored from `%s` at `%s` by `scripts/vendorhive`.\n\n", lock.Repo, lock.Ref)
	b.WriteString("**Do not edit anything in this tree.** Change hive first, then re-run\n`mise run vendor`. CI fails on drift. License: see LICENSE (MIT, upstream).\n\nPackages:\n\n")
	for _, p := range pkgs {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	return os.WriteFile(filepath.Join(vendorDir, "VENDOR.md"), []byte(b.String()), 0o644)
}

func runCmd(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
