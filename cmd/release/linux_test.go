package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTarball builds a gzipped tar with the given entries, for the negative
// cases writeBinaryTarball cannot itself produce.
func writeTarball(t *testing.T, path string, headers []*tar.Header) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	zip := gzip.NewWriter(file)
	archive := tar.NewWriter(zip)
	for _, header := range headers {
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := archive.Write(make([]byte, header.Size)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
}

// The updater renames the archive's single entry straight over the running
// binary, so the packaged shape is a correctness requirement, not a convention.
func TestWriteBinaryTarballProducesOneExecutableEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binary := filepath.Join(dir, "hive-desktop")
	if err := os.WriteFile(binary, []byte("ELF-ish payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	tarball := filepath.Join(dir, "out.tar.gz")
	if err := writeBinaryTarball(tarball, binary, "hive-desktop", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyBinaryTarball(tarball, "hive-desktop"); err != nil {
		t.Fatalf("freshly written tarball failed verification: %v", err)
	}

	file, err := os.Open(tarball)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	zip, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewReader(zip)
	header, err := archive.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != "hive-desktop" {
		t.Fatalf("entry name = %q, want hive-desktop", header.Name)
	}
	if header.FileInfo().Mode().Perm()&0o111 == 0 {
		t.Fatalf("entry mode %v lost the executable bit", header.FileInfo().Mode())
	}
	if _, err := archive.Next(); err == nil {
		t.Fatal("archive has a second entry; the updater rejects multi-entry archives")
	}
}

func TestVerifyBinaryTarballRejectsBadShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		headers []*tar.Header
		want    string
	}{
		{
			name: "two entries",
			headers: []*tar.Header{
				{Name: "hive-desktop", Mode: 0o755, Size: 1, Typeflag: tar.TypeReg},
				{Name: "README", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg},
			},
			want: "exactly one entry",
		},
		{
			// A wrapper directory would make the helper rename a directory over
			// the executable path.
			name:    "wrapper directory",
			headers: []*tar.Header{{Name: "hive-1.0", Mode: 0o755, Typeflag: tar.TypeDir}},
			want:    "want \"hive-desktop\"",
		},
		{
			name:    "wrong entry name",
			headers: []*tar.Header{{Name: "hive", Mode: 0o755, Size: 1, Typeflag: tar.TypeReg}},
			want:    "want \"hive-desktop\"",
		},
		{
			name:    "not executable",
			headers: []*tar.Header{{Name: "hive-desktop", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}},
			want:    "not executable",
		},
		{
			name:    "empty archive",
			headers: nil,
			want:    "is empty",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "out.tar.gz")
			writeTarball(t, path, testCase.headers)
			err := verifyBinaryTarball(path, "hive-desktop")
			if err == nil {
				t.Fatal("expected verification to fail")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %q does not mention %q", err, testCase.want)
			}
		})
	}
}

// A build that lost its ldflags reports "dev", which releaseChannel rejects, so
// the app would ship with its updater permanently disabled.
func TestFileContainsDetectsVersionStamp(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(path, []byte("\x00\x01padding1.4.0-dev.2padding\x00"), 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := fileContains(path, "1.4.0-dev.2")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("stamped version not found in binary")
	}
	found, err = fileContains(path, "9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("unstamped version reported as present")
	}
}

func TestLinuxArchesArePublished(t *testing.T) {
	t.Parallel()

	// A release publishes every platform in one manifest write; if this list
	// changes, docs/decisions/0027 and the manifest schema need updating too.
	want := []string{"amd64", "arm64"}
	if len(linuxArches) != len(want) {
		t.Fatalf("linuxArches = %v, want %v", linuxArches, want)
	}
	for i, arch := range want {
		if linuxArches[i] != arch {
			t.Fatalf("linuxArches = %v, want %v", linuxArches, want)
		}
	}
}
