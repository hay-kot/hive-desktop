package fonts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Both fixtures are subsets of CaskaydiaMono Nerd Font (OFL-1.1), renamed off
// the reserved font name and cut to the probe glyphs. They differ
// only in one advance width, so nothing but the measurement separates them:
// each reports isFixedPitch=0, which is what a Nerd Font patched face reports
// once its double-width icon glyphs are added.
const (
	monospaceFixture    = "monospace-patched.ttf"
	proportionalFixture = "proportional.ttf"
)

func fixtureDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}
	return dir
}

// The regression this package exists for: filtering on the post table's
// isFixedPitch flag alone hides every Nerd Font patched face, which is exactly
// what a terminal user has installed.
func TestScanClassifiesPatchedFaceAsMonospace(t *testing.T) {
	got := scan([]string{fixtureDir(t, monospaceFixture)})

	require.Equal(t, []string{"Fixture Mono Patched"}, got)
}

func TestScanExcludesProportionalFaces(t *testing.T) {
	got := scan([]string{fixtureDir(t, proportionalFixture)})

	require.Empty(t, got)
}

func TestScanDeduplicatesAndSortsFamilies(t *testing.T) {
	dir := fixtureDir(t, monospaceFixture, proportionalFixture)
	// A second copy of the same family, as the four weight files of one family
	// would be.
	data, err := os.ReadFile(filepath.Join("testdata", monospaceFixture))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "copy-bold.ttf"), data, 0o600))

	require.Equal(t, []string{"Fixture Mono Patched"}, scan([]string{dir}))
}

func TestScanFindsFontsInNestedDirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "vendor", "deep")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	data, err := os.ReadFile(filepath.Join("testdata", monospaceFixture))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(nested, monospaceFixture), data, 0o600))

	require.Equal(t, []string{"Fixture Mono Patched"}, scan([]string{root}))
}

// More font files than workers, so the queue actually backs up. A result
// channel sized from the file count would wedge here once collections pushed
// the family count past it.
func TestScanCompletesUnderContention(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("testdata", monospaceFixture))
	require.NoError(t, err)
	for i := range 200 {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("face-%d.ttf", i)), data, 0o600))
	}

	require.Equal(t, []string{"Fixture Mono Patched"}, scan([]string{dir}))
}

func TestScanSkipsUnreadableAndNonFontFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.ttf"), []byte("not a font"), 0o600))

	require.Empty(t, scan([]string{dir}))
}

func TestScanIgnoresMissingDirectories(t *testing.T) {
	require.Empty(t, scan([]string{filepath.Join(t.TempDir(), "absent")}))
}

// The settings pane asks on every open, and a scan parses every font file on
// the machine.
func TestMonospaceScansOnce(t *testing.T) {
	scans := 0
	lister := &Lister{scan: func() []string {
		scans++
		return []string{"Fixture Mono Patched"}
	}}

	require.Equal(t, []string{"Fixture Mono Patched"}, lister.Monospace())
	require.Equal(t, []string{"Fixture Mono Patched"}, lister.Monospace())
	require.Equal(t, 1, scans)
}

// Every name handed to the frontend has to be one CSS can resolve: non-empty,
// and never a macOS system-reserved face like ".SF NS Mono".
func TestMonospaceNamesAreSelectable(t *testing.T) {
	for _, family := range NewLister().Monospace() {
		require.NotEmpty(t, family)
		require.False(t, strings.HasPrefix(family, "."), "system-reserved family %q", family)
	}
}

func TestSearchPathsAreAbsolute(t *testing.T) {
	for _, path := range searchPaths() {
		require.True(t, filepath.IsAbs(path), "font search path %q must be absolute", path)
	}
}
