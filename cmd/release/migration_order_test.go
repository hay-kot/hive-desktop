package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationOrderScript(t *testing.T) {
	repoRoot := testCommandOutput(t, "", "git", "rev-parse", "--show-toplevel")
	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "check-migration-order.sh"))
	require.NoError(t, err)

	dir := t.TempDir()
	migrationDir := filepath.Join(dir, "internal", "app", "data", "queries", "migrations")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
	require.NoError(t, os.MkdirAll(migrationDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "check-migration-order.sh"), script, 0o755))
	first := filepath.Join(migrationDir, "0001_baseline.up.sql")
	require.NoError(t, os.WriteFile(first, []byte("CREATE TABLE baseline (id INTEGER);\n"), 0o644))

	testCommandOutput(t, dir, "git", "init", "--quiet")
	testCommandOutput(t, dir, "git", "add", ".")
	testCommandOutput(t, dir, "git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", "baseline")
	testCommandOutput(t, dir, "git", "tag", "desktop-v0.1.0-dev.1")

	output, err := testMigrationOrder(t, dir)
	require.NoError(t, err, output)

	require.NoError(t, os.WriteFile(first, []byte("CREATE TABLE changed (id INTEGER);\n"), 0o644))
	output, err = testMigrationOrder(t, dir)
	require.Error(t, err)
	assert.Contains(t, output, "released migration was modified")

	require.NoError(t, os.WriteFile(first, []byte("CREATE TABLE baseline (id INTEGER);\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(migrationDir, "0003_gap.up.sql"), []byte("SELECT 1;\n"), 0o644))
	output, err = testMigrationOrder(t, dir)
	require.Error(t, err)
	assert.Contains(t, output, "expected 0002")
}

// A release tag cut before the internal/app/store move lists its migrations
// at the old path. Every check against such a tag takes the script's
// fallback, so the immutability checks have to hold through it.
func TestMigrationOrderScript_ChecksAgainstATagAtTheHistoricalPath(t *testing.T) {
	repoRoot := testCommandOutput(t, "", "git", "rev-parse", "--show-toplevel")
	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "check-migration-order.sh"))
	require.NoError(t, err)

	dir := t.TempDir()
	oldDir := filepath.Join(dir, "internal", "app", "store", "migrations")
	newDir := filepath.Join(dir, "internal", "app", "data", "queries", "migrations")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "check-migration-order.sh"), script, 0o755))
	baseline := []byte("CREATE TABLE baseline (id INTEGER);\n")
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "0001_baseline.up.sql"), baseline, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "0002_second.up.sql"), []byte("SELECT 2;\n"), 0o644))

	commit := func(msg string) {
		testCommandOutput(t, dir, "git", "add", "-A")
		testCommandOutput(t, dir, "git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", msg)
	}
	testCommandOutput(t, dir, "git", "init", "--quiet")
	commit("baseline")
	testCommandOutput(t, dir, "git", "tag", "desktop-v0.1.0-dev.1")

	require.NoError(t, os.MkdirAll(filepath.Dir(newDir), 0o755))
	testCommandOutput(t, dir, "git", "mv", oldDir, newDir)
	commit("move migrations")
	first := filepath.Join(newDir, "0001_baseline.up.sql")
	second := filepath.Join(newDir, "0002_second.up.sql")

	output, err := testMigrationOrder(t, dir)
	require.NoError(t, err, output)
	assert.Contains(t, output, "released history unchanged")

	require.NoError(t, os.WriteFile(first, []byte("CREATE TABLE changed (id INTEGER);\n"), 0o644))
	output, err = testMigrationOrder(t, dir)
	require.Error(t, err)
	assert.Contains(t, output, "released migration was modified")

	require.NoError(t, os.WriteFile(first, baseline, 0o644))
	require.NoError(t, os.Remove(second))
	output, err = testMigrationOrder(t, dir)
	require.Error(t, err)
	assert.Contains(t, output, "released migration was removed")
}

func testMigrationOrder(t *testing.T, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(dir, "scripts", "check-migration-order.sh"))
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func testCommandOutput(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return strings.TrimSpace(string(output))
}
