package main

import (
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
		{checksum: strings.Repeat("b", 64), name: "Hive-1.2.3-linux-amd64.tar.gz"},
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(filepath.Join("desktop", "bin", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("a", 64) + "  Hive-1.2.3-darwin-universal.zip\n" +
		strings.Repeat("b", 64) + "  Hive-1.2.3-linux-amd64.tar.gz\n"
	if string(contents) != want {
		t.Fatalf("SHA256SUMS = %q, want %q", contents, want)
	}
}
