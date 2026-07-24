package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultManifestBase = "https://dl.hivedesktop.com/desktop/channels"

var versionPattern = regexp.MustCompile(`^(?:desktop-v)?([0-9]+)\.([0-9]+)\.([0-9]+)(?:-(dev|beta)\.([0-9]+))?$`)

type baseVersion struct {
	major int
	minor int
	patch int
}

type releaseVersion struct {
	base       baseVersion
	prerelease string
	number     int
}

type channelManifest struct {
	Channel string `json:"channel"`
	Version string `json:"version"`
}

func main() {
	if len(os.Args) != 2 || !validChannel(os.Args[1]) {
		fmt.Fprintf(os.Stderr, "usage: go run %s <dev|beta|stable>\n", os.Args[0])
		os.Exit(2)
	}

	versions, err := releaseVersions(manifestBaseURL())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(nextVersion(os.Args[1], versions))
}

func validChannel(channel string) bool {
	return channel == "dev" || channel == "beta" || channel == "stable"
}

func manifestBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("HIVE_DESKTOP_MANIFEST_BASE")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return defaultManifestBase
}

func releaseVersions(manifestBase string) ([]releaseVersion, error) {
	output, err := exec.Command("git", "tag", "--list", "desktop-v*").Output()
	if err != nil {
		return nil, fmt.Errorf("list desktop release tags: %w", err)
	}

	var versions []releaseVersion
	for tag := range strings.Lines(string(output)) {
		tag = strings.TrimSpace(tag)
		version, err := parseVersion(tag)
		if err != nil {
			return nil, fmt.Errorf("tag %q: %w", tag, err)
		}
		versions = append(versions, version)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	for _, channel := range []string{"stable", "beta", "dev"} {
		manifest, err := fetchManifest(client, manifestBase+"/"+channel+"/latest.json")
		if errors.Is(err, errManifestNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if manifest.Channel != channel {
			return nil, fmt.Errorf("%s manifest declares channel %q", channel, manifest.Channel)
		}
		version, err := parseVersion(manifest.Version)
		if err != nil {
			return nil, fmt.Errorf("%s manifest version %q: %w", channel, manifest.Version, err)
		}
		versions = append(versions, version)
	}
	return versions, nil
}

var errManifestNotFound = errors.New("manifest not found")

func fetchManifest(client *http.Client, url string) (channelManifest, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return channelManifest{}, fmt.Errorf("create manifest request %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "hive-desktop-release/1")

	resp, err := client.Do(req)
	if err != nil {
		return channelManifest{}, fmt.Errorf("read %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return channelManifest{}, errManifestNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return channelManifest{}, fmt.Errorf("read %s: HTTP %d", url, resp.StatusCode)
	}

	var manifest channelManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&manifest); err != nil {
		return channelManifest{}, fmt.Errorf("decode %s: %w", url, err)
	}
	return manifest, nil
}

func parseVersion(value string) (releaseVersion, error) {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return releaseVersion{}, errors.New("expected X.Y.Z or X.Y.Z-(dev|beta).N")
	}

	parts := make([]int, 4)
	for i, raw := range []string{match[1], match[2], match[3], match[5]} {
		if raw == "" {
			continue
		}
		part, err := strconv.Atoi(raw)
		if err != nil {
			return releaseVersion{}, fmt.Errorf("numeric component %q: %w", raw, err)
		}
		parts[i] = part
	}
	return releaseVersion{
		base:       baseVersion{major: parts[0], minor: parts[1], patch: parts[2]},
		prerelease: match[4],
		number:     parts[3],
	}, nil
}

func nextVersion(channel string, versions []releaseVersion) string {
	base := baseVersion{minor: 1}
	if len(versions) > 0 {
		base = versions[0].base
		for _, version := range versions[1:] {
			if compareBase(version.base, base) > 0 {
				base = version.base
			}
		}
	}

	var existing []releaseVersion
	for _, version := range versions {
		if compareBase(version.base, base) == 0 {
			existing = append(existing, version)
		}
	}

	hasStable := false
	hasBeta := false
	for _, version := range existing {
		hasStable = hasStable || version.prerelease == ""
		hasBeta = hasBeta || version.prerelease == "beta"
	}

	suffix := ""
	switch channel {
	case "dev":
		if hasStable || hasBeta {
			base.patch++
			suffix = "-dev.1"
		} else {
			suffix = fmt.Sprintf("-dev.%d", nextPrereleaseNumber(existing, "dev"))
		}
	case "beta":
		if hasStable {
			base.patch++
			suffix = "-beta.1"
		} else {
			suffix = fmt.Sprintf("-beta.%d", nextPrereleaseNumber(existing, "beta"))
		}
	case "stable":
		if hasStable {
			base.patch++
		}
	}
	return fmt.Sprintf("%d.%d.%d%s", base.major, base.minor, base.patch, suffix)
}

func nextPrereleaseNumber(versions []releaseVersion, prerelease string) int {
	maximum := 0
	for _, version := range versions {
		if version.prerelease == prerelease && version.number > maximum {
			maximum = version.number
		}
	}
	return maximum + 1
}

func compareBase(left, right baseVersion) int {
	for _, pair := range [][2]int{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}
