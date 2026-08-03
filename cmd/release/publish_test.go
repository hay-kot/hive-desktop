package main

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePublishOptionsRequiresUploadSkipWithNotarySkip(t *testing.T) {
	t.Parallel()

	_, err := parsePublishOptions([]string{"1.2.3-dev.1", "--skip-notarize"})
	if err == nil || !strings.Contains(err.Error(), "requires --skip-upload") {
		t.Fatalf("parsePublishOptions() error = %v", err)
	}
}

func TestParsePublishOptionsAllowsLocalUnnotarizedBuild(t *testing.T) {
	for _, name := range []string{"MACOS_CERTIFICATE", "MACOS_CERTIFICATE_PWD", "MACOS_SIGN_IDENTITY"} {
		t.Setenv(name, "test")
	}

	options, err := parsePublishOptions([]string{"1.2.3-dev.1", "--skip-notarize", "--skip-upload"})
	if err != nil {
		t.Fatal(err)
	}
	if !options.skipNotarize || !options.skipUpload {
		t.Fatalf("unexpected options: %#v", options)
	}
}

// The two-space separator is what `sha256sum -c` requires; the install
// instructions in docs/distribution.md pipe SHA256SUMS straight into it.
func TestWriteChecksumsMatchesSha256sumFormat(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join("desktop", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	publisher := &publisher{}
	err := publisher.writeChecksums([]releaseArtifact{
		{checksum: strings.Repeat("a", 64), name: "Hive-1.2.3-darwin-universal.zip"},
		{checksum: strings.Repeat("b", 64), name: "Hive-1.2.3-darwin-universal.dmg"},
		{checksum: strings.Repeat("c", 64), name: "Hive-1.2.3-linux-amd64.tar.gz"},
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(filepath.Join("desktop", "bin", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("a", 64) + "  Hive-1.2.3-darwin-universal.zip\n" +
		strings.Repeat("b", 64) + "  Hive-1.2.3-darwin-universal.dmg\n" +
		strings.Repeat("c", 64) + "  Hive-1.2.3-linux-amd64.tar.gz\n"
	if string(contents) != want {
		t.Fatalf("SHA256SUMS = %q, want %q", contents, want)
	}
}

// The zip and the DMG share a platform key; they must land in different fields
// of one entry, because the updater reads url and only url.
func TestPlatformManifestsPairUpdateAndInstaller(t *testing.T) {
	t.Parallel()

	platforms, err := platformManifests("https://dl.example.com", "desktop/releases/1.2.3", []releaseArtifact{
		{platformKey: "darwin-universal", role: updateArtifact, name: "Hive-1.2.3-darwin-universal.zip", checksum: strings.Repeat("a", 64), size: 10},
		{platformKey: "darwin-universal", role: installerArtifact, name: "Hive-1.2.3-darwin-universal.dmg", checksum: strings.Repeat("b", 64), size: 20},
		{platformKey: "linux-amd64", role: updateArtifact, name: "Hive-1.2.3-linux-amd64.tar.gz", checksum: strings.Repeat("c", 64), size: 30},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]platformManifest{
		"darwin-universal": {
			URL:             "https://dl.example.com/desktop/releases/1.2.3/Hive-1.2.3-darwin-universal.zip",
			SHA256:          strings.Repeat("a", 64),
			Size:            10,
			InstallerURL:    "https://dl.example.com/desktop/releases/1.2.3/Hive-1.2.3-darwin-universal.dmg",
			InstallerSHA256: strings.Repeat("b", 64),
			InstallerSize:   20,
		},
		"linux-amd64": {
			URL:    "https://dl.example.com/desktop/releases/1.2.3/Hive-1.2.3-linux-amd64.tar.gz",
			SHA256: strings.Repeat("c", 64),
			Size:   30,
		},
	}
	if !maps.Equal(platforms, want) {
		t.Fatalf("platformManifests() = %#v, want %#v", platforms, want)
	}
}

// An artifact with no role would otherwise publish a manifest entry missing
// either the update URL or the installer URL, silently.
func TestPlatformManifestsRejectsARolelessArtifact(t *testing.T) {
	t.Parallel()

	_, err := platformManifests("https://dl.example.com", "desktop/releases/1.2.3", []releaseArtifact{
		{platformKey: "darwin-universal", name: "Hive-1.2.3-darwin-universal.zip", checksum: strings.Repeat("a", 64), size: 10},
	})
	if err == nil {
		t.Fatal("platformManifests accepted an artifact with no role")
	}
}
