package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultDownloadBase = "https://dl.hivedesktop.com"

const defaultSiteBase = "https://hivedesktop.com"

func siteBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("HIVE_DESKTOP_SITE_BASE")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return defaultSiteBase
}

var errManifestNotFound = errors.New("manifest not found")

type platformManifest struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type channelManifest struct {
	Channel   string                      `json:"channel"`
	Version   string                      `json:"version"`
	PubDate   string                      `json:"pub_date"`
	Platforms map[string]platformManifest `json:"platforms"`
}

func downloadBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("DL_BASE_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	if value := strings.TrimSpace(os.Getenv("HIVE_DESKTOP_MANIFEST_BASE")); value != "" {
		value = strings.TrimRight(value, "/")
		return strings.TrimSuffix(value, "/desktop/channels")
	}
	return defaultDownloadBase
}

func manifestBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("HIVE_DESKTOP_MANIFEST_BASE")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return downloadBaseURL() + "/desktop/channels"
}

func fetchManifest(ctx context.Context, client *http.Client, url string) (channelManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	if err := validateManifest(manifest); err != nil {
		return channelManifest{}, fmt.Errorf("decode %s: %w", url, err)
	}
	return manifest, nil
}

func validateManifest(manifest channelManifest) error {
	if manifest.Channel == "" || manifest.Version == "" || manifest.PubDate == "" || manifest.Platforms == nil {
		return errors.New("missing required manifest fields")
	}
	if _, err := time.Parse(time.RFC3339, manifest.PubDate); err != nil {
		return fmt.Errorf("invalid pub_date: %w", err)
	}
	platform, ok := manifest.Platforms["darwin-universal"]
	if !ok || platform.URL == "" || platform.SHA256 == "" || platform.Size < 1 {
		return errors.New("missing darwin-universal artifact metadata")
	}
	artifactURL, err := url.Parse(platform.URL)
	if err != nil || artifactURL.Host == "" || (artifactURL.Scheme != "http" && artifactURL.Scheme != "https") {
		return errors.New("invalid darwin-universal artifact URL")
	}
	checksum, err := hex.DecodeString(platform.SHA256)
	if err != nil || len(checksum) != sha256.Size {
		return errors.New("invalid darwin-universal SHA-256")
	}
	return nil
}

func readManifests(ctx context.Context) (map[string]channelManifest, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	manifests := make(map[string]channelManifest)
	for _, channel := range []string{"stable", "beta", "dev"} {
		manifest, err := fetchManifest(ctx, client, manifestBaseURL()+"/"+channel+"/latest.json")
		if errors.Is(err, errManifestNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if manifest.Channel != channel {
			return nil, fmt.Errorf("%s manifest declares channel %q", channel, manifest.Channel)
		}
		if _, err := parseVersion(manifest.Version); err != nil {
			return nil, fmt.Errorf("%s manifest version %q: %w", channel, manifest.Version, err)
		}
		manifests[channel] = manifest
	}
	return manifests, nil
}

func validateManifestAdvancement(candidate releaseVersion, manifests map[string]channelManifest) error {
	for _, channel := range candidate.affectedChannels() {
		manifest, ok := manifests[channel]
		if !ok {
			continue
		}
		current, err := parseVersion(manifest.Version)
		if err != nil {
			return fmt.Errorf("%s manifest version %q: %w", channel, manifest.Version, err)
		}
		if compareChannelRelease(candidate, current) <= 0 {
			return fmt.Errorf("candidate %s does not advance the affected %s manifest at %s", candidate, channel, manifest.Version)
		}
	}
	return nil
}

func releaseVersions(ctx context.Context) ([]releaseVersion, map[string]channelManifest, error) {
	output, err := exec.CommandContext(ctx, "git", "tag", "--list", "desktop-v*").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("list desktop release tags: %w", err)
	}

	var versions []releaseVersion
	for tag := range strings.Lines(string(output)) {
		tag = strings.TrimSpace(tag)
		version, err := parseVersion(tag)
		if err != nil {
			return nil, nil, fmt.Errorf("tag %q: %w", tag, err)
		}
		versions = append(versions, version)
	}
	manifests, err := readManifests(ctx)
	if err != nil {
		return nil, nil, err
	}
	for channel, manifest := range manifests {
		version, err := parseVersion(manifest.Version)
		if err != nil {
			return nil, nil, fmt.Errorf("%s manifest version %q: %w", channel, manifest.Version, err)
		}
		versions = append(versions, version)
	}
	return versions, manifests, nil
}

func verifyLive(ctx context.Context, version releaseVersion) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	var artifact platformManifest
	for _, channel := range version.affectedChannels() {
		manifest, err := fetchManifest(ctx, client, manifestBaseURL()+"/"+channel+"/latest.json")
		if err != nil {
			return err
		}
		if manifest.Channel != channel || manifest.Version != version.String() {
			return fmt.Errorf("%s manifest reports channel=%q version=%q, want channel=%q version=%q", channel, manifest.Channel, manifest.Version, channel, version.String())
		}
		platform, ok := manifest.Platforms["darwin-universal"]
		if !ok || platform.URL == "" || platform.SHA256 == "" || platform.Size < 1 {
			return fmt.Errorf("%s manifest has malformed darwin-universal platform", channel)
		}
		if artifact.URL != "" && platform != artifact {
			return fmt.Errorf("%s manifest artifact differs from the other affected manifests", channel)
		}
		artifact = platform
		fmt.Printf("manifest: %s -> %s\n", channel, manifest.Version)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return fmt.Errorf("create artifact request: %w", err)
	}
	req.Header.Set("User-Agent", "hive-desktop-release/1")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download artifact: HTTP %d", resp.StatusCode)
	}
	hash := sha256.New()
	n, err := io.Copy(hash, resp.Body)
	if err != nil {
		return fmt.Errorf("hash artifact: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != artifact.SHA256 {
		return fmt.Errorf("artifact checksum %s does not match manifest %s", actual, artifact.SHA256)
	}
	if n != artifact.Size {
		return fmt.Errorf("artifact size %d does not match manifest %d", n, artifact.Size)
	}
	fmt.Printf("artifact: %s\n", artifact.URL)
	fmt.Printf("size: %d\n", n)
	fmt.Printf("sha256: %s\n", actual)
	return nil
}
