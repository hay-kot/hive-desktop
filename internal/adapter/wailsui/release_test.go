package wailsui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShortCommit(t *testing.T) {
	require.Equal(t, "abc1234", ShortCommit("abc1234def567890"))
	require.Equal(t, "abc12", ShortCommit("abc12"))
	require.Equal(t, "HEAD", ShortCommit("HEAD"))
	require.Empty(t, ShortCommit(""))
}

func TestReleaseURL(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"released semver", "1.2.3", "https://github.com/hay-kot/hive-desktop/releases/tag/desktop-v1.2.3"},
		{"leading v tolerated", "v0.4.0", "https://github.com/hay-kot/hive-desktop/releases/tag/desktop-v0.4.0"},
		{"surrounding whitespace", "  1.0.0 ", "https://github.com/hay-kot/hive-desktop/releases/tag/desktop-v1.0.0"},
		{"beta prerelease", "1.2.3-beta.1", "https://github.com/hay-kot/hive-desktop/releases/tag/desktop-v1.2.3-beta.1"},
		{"dev prerelease", "1.2.3-dev.4", "https://github.com/hay-kot/hive-desktop/releases/tag/desktop-v1.2.3-dev.4"},
		{"dev build", "dev", ""},
		{"empty", "", ""},
		{"pseudo version", "v0.0.0-20240101000000-abcdef123456", ""},
		{"non-numeric", "1.2.x", ""},
		{"missing patch", "1.2", ""},
		{"devel", "(devel)", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ReleaseURL(tt.version))
		})
	}
}

func TestReleaseChannel(t *testing.T) {
	tests := []struct {
		version string
		channel string
		ok      bool
	}{
		{"1.2.3", "stable", true},
		{"v1.2.3", "stable", true},
		{"1.2.3-beta.1", "beta", true},
		{"1.2.3-dev.4", "dev", true},
		{"dev", "", false},
		{"", "", false},
		{"(devel)", "", false},
		{"v0.0.0-20240101000000-abcdef123456", "", false},
		{"1.2.3-rc.1", "", false},
		{"1.2.3-beta", "", false},
		{"1.2", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			channel, ok := ReleaseChannel(tt.version)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.channel, channel)
		})
	}
}

func TestSystemServiceBuild(t *testing.T) {
	// The package defaults ("dev") mean no release link is offered, and the
	// short "HEAD" commit passes through untouched.
	info := NewSystemService(nil, "dev", "HEAD", "now").Build()
	require.Equal(t, "dev", info.Version)
	require.Equal(t, "HEAD", info.Commit)
	require.Equal(t, "https://github.com/hay-kot/hive-desktop", info.RepoURL)
	require.Empty(t, info.ReleaseURL)
}
