package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	defaultR2Bucket    = "hive-desktop-releases"
	defaultR2AccountID = "bce6b95e4e84d92b1972d3b55b6cfaf6"
)

type publishOptions struct {
	version         releaseVersion
	skipNotarize    bool
	skipUpload      bool
	skipWeb         bool
	force           bool
	r2Bucket        string
	r2AccountID     string
	r2AccessKey     string
	r2SecretKey     string
	downloadBase    string
	signCertificate string
	signPassword    string
	signIdentity    string
	notaryKey       string
	notaryKeyID     string
	notaryIssuerID  string
}

type publisher struct {
	options           publishOptions
	workDir           string
	keychainPath      string
	originalKeychains []string
	// Stamped into every platform's binary, so all artifacts in a release
	// report the same provenance.
	commit    string
	buildDate string
}

// releaseArtifact is one published file and the manifest metadata describing it.
// A release produces one per platform key and registers them together.
type releaseArtifact struct {
	platformKey string // manifest platforms key, e.g. "linux-amd64"
	name        string // file name within the release prefix
	path        string // local path
	checksum    string // hex sha256
	size        int64
}

func publish(ctx context.Context, args []string) error {
	options, err := parsePublishOptions(args)
	if err != nil {
		return err
	}
	if !options.skipUpload {
		if err := validatePublishSource(ctx, options.version); err != nil {
			return err
		}
		manifests, err := readManifests(ctx)
		if err != nil {
			return err
		}
		if err := validateManifestAdvancement(options.version, manifests); err != nil {
			return err
		}
	}
	p := &publisher{options: options}
	if err := p.run(ctx); err != nil {
		return err
	}
	if options.skipUpload {
		return nil
	}
	return verifyLive(ctx, options.version)
}

func parsePublishOptions(args []string) (publishOptions, error) {
	var versionText string
	options := publishOptions{
		r2Bucket:     envDefault("R2_BUCKET", defaultR2Bucket),
		r2AccountID:  envDefault("R2_ACCOUNT_ID", defaultR2AccountID),
		downloadBase: downloadBaseURL(),
	}
	for _, arg := range args {
		switch arg {
		case "--skip-notarize":
			options.skipNotarize = true
		case "--skip-upload":
			options.skipUpload = true
		case "--skip-web":
			options.skipWeb = true
		case "--force":
			options.force = true
		default:
			if strings.HasPrefix(arg, "-") {
				return publishOptions{}, fmt.Errorf("unknown flag %s", arg)
			}
			if versionText != "" {
				return publishOptions{}, errors.New("usage: release publish <version> [--skip-notarize] [--skip-upload] [--skip-web] [--force]")
			}
			versionText = arg
		}
	}
	if versionText == "" {
		return publishOptions{}, errors.New("usage: release publish <version> [--skip-notarize] [--skip-upload] [--skip-web] [--force]")
	}
	if options.skipNotarize && !options.skipUpload {
		return publishOptions{}, errors.New("--skip-notarize requires --skip-upload; public releases must be notarized")
	}
	version, err := parsePublishVersion(versionText)
	if err != nil {
		return publishOptions{}, fmt.Errorf("invalid version %q: %w", versionText, err)
	}
	options.version = version
	options.signCertificate = os.Getenv("MACOS_CERTIFICATE")
	options.signPassword = os.Getenv("MACOS_CERTIFICATE_PWD")
	options.signIdentity = os.Getenv("MACOS_SIGN_IDENTITY")
	options.notaryKey = os.Getenv("AC_API_KEY")
	options.notaryKeyID = os.Getenv("AC_API_KEY_ID")
	options.notaryIssuerID = os.Getenv("AC_API_ISSUER_ID")
	options.r2AccessKey = os.Getenv("R2_ACCESS_KEY_ID")
	options.r2SecretKey = os.Getenv("R2_SECRET_ACCESS_KEY")

	missing := missingValues(map[string]string{
		"MACOS_CERTIFICATE":     options.signCertificate,
		"MACOS_CERTIFICATE_PWD": options.signPassword,
		"MACOS_SIGN_IDENTITY":   options.signIdentity,
	})
	if !options.skipNotarize {
		missing = append(missing, missingValues(map[string]string{
			"AC_API_KEY":       options.notaryKey,
			"AC_API_KEY_ID":    options.notaryKeyID,
			"AC_API_ISSUER_ID": options.notaryIssuerID,
		})...)
	}
	if !options.skipUpload {
		missing = append(missing, missingValues(map[string]string{
			"R2_ACCESS_KEY_ID":     options.r2AccessKey,
			"R2_SECRET_ACCESS_KEY": options.r2SecretKey,
		})...)
	}
	if len(missing) > 0 {
		return publishOptions{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return options, nil
}

func missingValues(values map[string]string) []string {
	var missing []string
	for name, value := range values {
		if value == "" {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return missing
}

func envDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func (p *publisher) run(ctx context.Context) error {
	if err := p.preflight(ctx); err != nil {
		return err
	}
	workDir, err := os.MkdirTemp("", "hive-desktop-release.*")
	if err != nil {
		return fmt.Errorf("create release work directory: %w", err)
	}
	p.workDir = workDir
	defer func() {
		if err := p.cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: release cleanup failed: %v\n", err)
		}
	}()

	commit, err := gitHead(ctx)
	if err != nil {
		return err
	}
	p.commit = commit
	p.buildDate = time.Now().UTC().Format(time.RFC3339)

	fmt.Printf("==> releasing %s (channel: %s -> manifests: %s)\n", p.options.version, p.options.version.channel(), strings.Join(p.options.version.affectedChannels(), " "))
	// Deploy and verify the web landing page and worker before the app build, so a
	// broken or misconfigured backend aborts the release before any immutable
	// artifact is uploaded. The app upload is the only irreversible step.
	if p.webEnabled() {
		if err := p.deployWeb(ctx); err != nil {
			return err
		}
		if err := p.verifyWeb(ctx); err != nil {
			return err
		}
	}
	if err := p.build(ctx); err != nil {
		return err
	}
	if err := p.sign(ctx); err != nil {
		return err
	}
	if !p.options.skipNotarize {
		if err := p.notarize(ctx); err != nil {
			return err
		}
	} else {
		fmt.Println("==> skipping notarization (--skip-notarize)")
	}
	macArtifact, err := p.packageApp(ctx)
	if err != nil {
		return err
	}
	artifacts := []releaseArtifact{macArtifact}

	// A release covers every platform, so the manifest it writes is complete in
	// one write. That is what keeps the strict manifest-advancement rule usable:
	// a second, later publish topping up another platform would be rejected for
	// not advancing the version it just set.
	for _, arch := range linuxArches {
		artifact, err := p.buildLinux(ctx, arch)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifact)
	}

	if err := p.writeChecksums(artifacts); err != nil {
		return err
	}
	if p.options.skipUpload {
		fmt.Println("==> skipping upload (--skip-upload); artifacts:")
		for _, artifact := range artifacts {
			fmt.Printf("    %s\n", artifact.path)
		}
		return nil
	}
	// Re-check source state and manifests immediately before the irreversible
	// upload. The build and Apple notarization can take hours, so either may have
	// changed since the fail-fast validation at command startup.
	if err := validatePublishSource(ctx, p.options.version); err != nil {
		return err
	}
	manifests, err := readManifests(ctx)
	if err != nil {
		return err
	}
	if err := validateManifestAdvancement(p.options.version, manifests); err != nil {
		return err
	}
	return p.upload(ctx, artifacts)
}

// writeChecksums writes one SHA256SUMS covering every artifact in the release.
// A single release process writes it once, so it stays consistent with the
// immutable prefix it lives in.
func (p *publisher) writeChecksums(artifacts []releaseArtifact) error {
	var contents strings.Builder
	for _, artifact := range artifacts {
		fmt.Fprintf(&contents, "%s  %s\n", artifact.checksum, artifact.name)
	}
	return os.WriteFile(filepath.Join("desktop", "bin", "SHA256SUMS"), []byte(contents.String()), 0o644)
}

func (p *publisher) preflight(ctx context.Context) error {
	// docker is required unconditionally: every release publishes Linux too, and
	// the Linux binary is built in a container (the macOS host has no GTK4
	// headers for CGO to link against).
	tools := []string{"/usr/libexec/PlistBuddy", "codesign", "ditto", "docker", "mise", "openssl", "security", "/usr/bin/unzip"}
	if !p.options.skipNotarize {
		tools = append(tools, "xcrun")
	}
	if !p.options.skipUpload {
		tools = append(tools, "curl")
	}
	if p.webEnabled() {
		tools = append(tools, "node", "npm")
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required tool %s: %w", tool, err)
		}
	}
	if !p.options.skipUpload {
		output, err := commandOutput(ctx, "curl", "--help", "all")
		if err != nil {
			return err
		}
		if !strings.Contains(output, "--aws-sigv4") {
			return errors.New("curl with --aws-sigv4 support is required (curl >= 7.86)")
		}
	}
	return nil
}

func (p *publisher) cleanup() error {
	var cleanupErr error
	if p.keychainPath != "" {
		args := append([]string{"list-keychains", "-d", "user", "-s"}, p.originalKeychains...)
		if err := quietCommand(context.Background(), "security", args...); err != nil {
			cleanupErr = err
		}
		_ = quietCommand(context.Background(), "security", "delete-keychain", p.keychainPath)
	}
	if err := os.RemoveAll(p.workDir); cleanupErr == nil && err != nil {
		cleanupErr = err
	}
	return cleanupErr
}

func (p *publisher) webEnabled() bool {
	return !p.options.skipUpload && !p.options.skipWeb
}

func (p *publisher) deployWeb(ctx context.Context) error {
	fmt.Println("==> deploying web (landing page + worker)")
	for _, step := range [][]string{{"npm", "ci"}, {"npm", "run", "deploy"}} {
		command := exec.CommandContext(ctx, step[0], step[1:]...)
		command.Dir = "web"
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("web deploy (%s): %w", strings.Join(step, " "), err)
		}
	}
	return nil
}

func (p *publisher) verifyWeb(ctx context.Context) error {
	client := &http.Client{Timeout: 30 * time.Second}
	endpoint := siteBaseURL() + "/api/report"

	// The report route is POST-only, so a live worker answers GET with 405; a
	// missing worker or route answers 404. Retry briefly for edge propagation.
	var liveErr error
	for attempt := range 5 {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		status, err := probeStatus(ctx, client, http.MethodGet, endpoint, "")
		if err != nil {
			liveErr = err
			continue
		}
		if status == http.StatusMethodNotAllowed {
			liveErr = nil
			break
		}
		liveErr = fmt.Errorf("GET %s returned HTTP %d, want 405", endpoint, status)
	}
	if liveErr != nil {
		return fmt.Errorf("verify web worker: %w", liveErr)
	}

	token := os.Getenv("HIVE_DESKTOP_REPORT_TOKEN")
	if token == "" {
		fmt.Println("==> web deployed; problem reporting disabled (HIVE_DESKTOP_REPORT_TOKEN unset)")
		return nil
	}
	// An authenticated POST with a non-gzip body stops at the worker's gzip gate
	// (415), which is past the 401 (token) and 503 (reporting disabled) checks.
	// So a 415 proves the release token is accepted without writing a report.
	status, err := probeStatus(ctx, client, http.MethodPost, endpoint, token)
	if err != nil {
		return fmt.Errorf("verify web report token: %w", err)
	}
	if err := reportProbeResult(status); err != nil {
		return fmt.Errorf("verify web report token: %w", err)
	}
	fmt.Println("==> web deployed; report endpoint accepts the release token")
	return nil
}

func probeStatus(ctx context.Context, client *http.Client, method, url, bearer string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "hive-desktop-release/1")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, nil
}

func reportProbeResult(status int) error {
	switch status {
	case http.StatusUnsupportedMediaType:
		return nil
	case http.StatusUnauthorized:
		return errors.New("worker rejected the release token: HIVE_DESKTOP_REPORT_TOKEN does not match the worker's REPORT_TOKEN secret")
	case http.StatusServiceUnavailable:
		return errors.New("worker reports problem reporting disabled: set the REPORT_TOKEN secret on the worker (wrangler secret put REPORT_TOKEN)")
	default:
		return fmt.Errorf("unexpected status %d from the report endpoint", status)
	}
}

func (p *publisher) build(ctx context.Context) error {
	fmt.Println("==> building universal .app")
	if os.Getenv("HIVE_DESKTOP_REPORT_TOKEN") == "" {
		fmt.Fprintln(os.Stderr, "warning: HIVE_DESKTOP_REPORT_TOKEN is empty; problem reporting will be disabled in this build")
	}
	command := exec.CommandContext(ctx, "mise", "x", "--", "wails3", "task", "darwin:package:universal")
	command.Dir = "desktop"
	command.Env = append(os.Environ(),
		"HIVE_DESKTOP_VERSION="+p.options.version.String(),
		"HIVE_DESKTOP_COMMIT="+p.commit,
		"HIVE_DESKTOP_DATE="+p.buildDate,
	)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build universal app: %w", err)
	}
	app := filepath.Join("desktop", "bin", "Hive.app")
	if info, err := os.Stat(app); err != nil || !info.IsDir() {
		return errors.New("build did not produce desktop/bin/Hive.app")
	}
	base := p.options.version.base
	baseText := fmt.Sprintf("%d.%d.%d", base.major, base.minor, base.patch)
	plistPath := filepath.Join(app, "Contents", "Info.plist")
	for _, key := range []string{"CFBundleShortVersionString", "CFBundleVersion"} {
		if err := runCommand(ctx, "/usr/libexec/PlistBuddy", "-c", "Set :"+key+" "+baseText, plistPath); err != nil {
			return err
		}
	}
	return nil
}

func gitHead(ctx context.Context) (string, error) {
	output, err := commandOutput(ctx, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve build commit: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func (p *publisher) sign(ctx context.Context) error {
	fmt.Println("==> importing Developer ID certificate into an ephemeral keychain")
	p.keychainPath = filepath.Join(p.workDir, "signing.keychain-db")
	password, err := commandOutput(ctx, "openssl", "rand", "-base64", "24")
	if err != nil {
		return err
	}
	password = strings.TrimSpace(password)
	keychains, err := commandOutput(ctx, "security", "list-keychains", "-d", "user")
	if err != nil {
		return err
	}
	for line := range strings.Lines(keychains) {
		if path := strings.Trim(strings.TrimSpace(line), `"`); path != "" {
			p.originalKeychains = append(p.originalKeychains, path)
		}
	}
	certPath := filepath.Join(p.workDir, "cert.p12")
	if err := decodeSecret(p.options.signCertificate, certPath); err != nil {
		return fmt.Errorf("decode MACOS_CERTIFICATE: %w", err)
	}
	if err := runCommand(ctx, "security", "create-keychain", "-p", password, p.keychainPath); err != nil {
		return err
	}
	if err := runCommand(ctx, "security", "set-keychain-settings", "-lut", "3600", p.keychainPath); err != nil {
		return err
	}
	if err := runCommand(ctx, "security", "unlock-keychain", "-p", password, p.keychainPath); err != nil {
		return err
	}
	if err := runCommand(ctx, "security", "import", certPath, "-P", p.options.signPassword, "-k", p.keychainPath, "-T", "/usr/bin/codesign"); err != nil {
		return err
	}
	if err := quietCommand(ctx, "security", "set-key-partition-list", "-S", "apple-tool:,apple:,codesign:", "-s", "-k", password, p.keychainPath); err != nil {
		return err
	}
	args := append([]string{"list-keychains", "-d", "user", "-s", p.keychainPath}, p.originalKeychains...)
	if err := runCommand(ctx, "security", args...); err != nil {
		return err
	}
	if err := os.Remove(certPath); err != nil {
		return err
	}

	fmt.Println("==> codesigning (Developer ID, hardened runtime)")
	app := filepath.Join("desktop", "bin", "Hive.app")
	if err := runCommand(ctx, "codesign", "--force", "--deep", "--timestamp", "--options", "runtime", "--entitlements", filepath.Join("desktop", "build", "darwin", "entitlements.plist"), "--sign", p.options.signIdentity, app); err != nil {
		return err
	}
	return runCommand(ctx, "codesign", "--verify", "--strict", "--verbose=2", app)
}

func decodeSecret(value, path string) error {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return err
	}
	return os.WriteFile(path, decoded, 0o600)
}

type notaryResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (p *publisher) notarize(ctx context.Context) error {
	fmt.Println("==> notarizing")
	keyPath := filepath.Join(p.workDir, "ac_api_key.p8")
	if err := decodeSecret(p.options.notaryKey, keyPath); err != nil {
		return fmt.Errorf("decode AC_API_KEY: %w", err)
	}
	archive := filepath.Join(p.workDir, "notarize.zip")
	app := filepath.Join("desktop", "bin", "Hive.app")
	if err := runCommand(ctx, "ditto", "-c", "-k", "--keepParent", app, archive); err != nil {
		return err
	}
	output, err := commandOutput(ctx, "xcrun", "notarytool", "submit", archive, "--key", keyPath, "--key-id", p.options.notaryKeyID, "--issuer", p.options.notaryIssuerID, "--output-format", "json")
	if err != nil {
		return err
	}
	var submission notaryResponse
	if err := json.Unmarshal([]byte(output), &submission); err != nil {
		return fmt.Errorf("decode notary submission: %w", err)
	}
	if submission.ID == "" {
		return errors.New("decode notary submission: missing id")
	}
	fmt.Printf("    submission: %s\n", submission.ID)

	deadline := time.Now().Add(2 * time.Hour)
	consecutiveErrors := 0
	for time.Now().Before(deadline) {
		output, err := commandOutput(ctx, "xcrun", "notarytool", "info", submission.ID, "--key", keyPath, "--key-id", p.options.notaryKeyID, "--issuer", p.options.notaryIssuerID, "--output-format", "json")
		if err != nil {
			consecutiveErrors++
			fmt.Fprintf(os.Stderr, "    status check failed (%d/10)\n", consecutiveErrors)
			if consecutiveErrors >= 10 {
				return errors.New("too many notarytool errors")
			}
		} else {
			consecutiveErrors = 0
			var info notaryResponse
			if err := json.Unmarshal([]byte(output), &info); err != nil {
				return fmt.Errorf("decode notarization status: %w", err)
			}
			fmt.Printf("    status: %s\n", info.Status)
			switch info.Status {
			case "Accepted":
				if err := runCommand(ctx, "xcrun", "stapler", "staple", app); err != nil {
					return err
				}
				return runCommand(ctx, "xcrun", "stapler", "validate", app)
			case "In Progress":
			case "Invalid", "Rejected":
				_ = runCommand(ctx, "xcrun", "notarytool", "log", submission.ID, "--key", keyPath, "--key-id", p.options.notaryKeyID, "--issuer", p.options.notaryIssuerID)
				return fmt.Errorf("notarization failed: %s", info.Status)
			default:
				return fmt.Errorf("unexpected notarization status: %s", info.Status)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
		}
	}
	return errors.New("timed out waiting for notarization")
}

func (p *publisher) packageApp(ctx context.Context) (releaseArtifact, error) {
	zipName := fmt.Sprintf("Hive-%s-darwin-universal.zip", p.options.version)
	zipPath := filepath.Join("desktop", "bin", zipName)
	fmt.Printf("==> packaging %s\n", zipName)
	// ditto must run in desktop/bin so the archive has Hive.app at its root.
	command := exec.CommandContext(ctx, "ditto", "-c", "-k", "--norsrc", "--noextattr", "--noacl", "--keepParent", "Hive.app", zipName)
	command.Dir = filepath.Join("desktop", "bin")
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return releaseArtifact{}, fmt.Errorf("package app: %w", err)
	}
	checksum, size, err := fileChecksum(zipPath)
	if err != nil {
		return releaseArtifact{}, err
	}
	fmt.Printf("%s  %s\n", checksum, zipName)

	fmt.Println("==> verifying packaged app after plain ZIP extraction")
	extracted := filepath.Join(p.workDir, "extracted")
	if err := os.MkdirAll(extracted, 0o755); err != nil {
		return releaseArtifact{}, err
	}
	if err := runCommand(ctx, "/usr/bin/unzip", "-q", zipPath, "-d", extracted); err != nil {
		return releaseArtifact{}, err
	}
	extractedApp := filepath.Join(extracted, "Hive.app")
	err = filepath.WalkDir(extractedApp, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), "._") {
			return fmt.Errorf("packaged app contains signature-breaking AppleDouble file: %s", path)
		}
		return nil
	})
	if err != nil {
		return releaseArtifact{}, err
	}
	if err := runCommand(ctx, "codesign", "--verify", "--deep", "--strict", "--verbose=2", extractedApp); err != nil {
		return releaseArtifact{}, err
	}
	if !p.options.skipNotarize {
		if err := runCommand(ctx, "xcrun", "stapler", "validate", extractedApp); err != nil {
			return releaseArtifact{}, err
		}
	}
	return releaseArtifact{
		platformKey: "darwin-universal",
		name:        zipName,
		path:        zipPath,
		checksum:    checksum,
		size:        size,
	}, nil
}

func fileChecksum(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func (p *publisher) upload(ctx context.Context, artifacts []releaseArtifact) error {
	releasePrefix := "desktop/releases/" + p.options.version.String()
	for _, artifact := range artifacts {
		exists, err := p.r2Exists(ctx, releasePrefix+"/"+artifact.name)
		if err != nil {
			return err
		}
		if exists && !p.options.force {
			return fmt.Errorf("release %s already has %s in the bucket (immutable); use --force to overwrite", p.options.version, artifact.name)
		}
	}

	fmt.Printf("==> uploading artifacts to r2://%s/%s/\n", p.options.r2Bucket, releasePrefix)
	for _, artifact := range artifacts {
		if err := p.r2Put(ctx, releasePrefix+"/"+artifact.name, artifact.path, artifactContentType(artifact.name), "public, max-age=31536000, immutable"); err != nil {
			return err
		}
	}
	if err := p.r2Put(ctx, releasePrefix+"/SHA256SUMS", filepath.Join("desktop", "bin", "SHA256SUMS"), "text/plain", "public, max-age=31536000, immutable"); err != nil {
		return err
	}

	platforms := make(map[string]platformManifest, len(artifacts))
	for _, artifact := range artifacts {
		platforms[artifact.platformKey] = platformManifest{
			URL:    fmt.Sprintf("%s/%s/%s", p.options.downloadBase, releasePrefix, artifact.name),
			SHA256: artifact.checksum,
			Size:   artifact.size,
		}
	}

	pubDate := time.Now().UTC().Format(time.RFC3339)
	for _, channel := range p.options.version.affectedChannels() {
		fmt.Printf("==> writing channel manifest: %s\n", channel)
		manifest := channelManifest{
			Channel:   channel,
			Version:   p.options.version.String(),
			PubDate:   pubDate,
			Platforms: platforms,
		}
		contents, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		manifestPath := filepath.Join(p.workDir, "latest-"+channel+".json")
		if err := os.WriteFile(manifestPath, append(contents, '\n'), 0o644); err != nil {
			return err
		}
		if err := p.r2Put(ctx, "desktop/channels/"+channel+"/latest.json", manifestPath, "application/json", "no-cache"); err != nil {
			return err
		}
	}
	fmt.Printf("Release %s published to the %s channel.\n", p.options.version, p.options.version.channel())
	for _, artifact := range artifacts {
		fmt.Printf("  %s: %s/%s/%s\n", artifact.platformKey, p.options.downloadBase, releasePrefix, artifact.name)
	}
	fmt.Printf("  manifests updated: %s\n", strings.Join(p.options.version.affectedChannels(), " "))
	return nil
}

func artifactContentType(name string) string {
	if strings.HasSuffix(name, ".zip") {
		return "application/zip"
	}
	return "application/gzip"
}

func (p *publisher) r2Endpoint(key string) string {
	return fmt.Sprintf("https://%s.r2.cloudflarestorage.com/%s/%s", p.options.r2AccountID, p.options.r2Bucket, key)
}

func (p *publisher) r2AuthArgs() []string {
	return []string{"--aws-sigv4", "aws:amz:auto:s3", "--user", p.options.r2AccessKey + ":" + p.options.r2SecretKey}
}

func (p *publisher) r2Exists(ctx context.Context, key string) (bool, error) {
	args := []string{"--silent", "--show-error", "--output", "/dev/null", "--write-out", "%{http_code}", "--head"}
	args = append(args, p.r2AuthArgs()...)
	args = append(args, p.r2Endpoint(key))
	output, err := commandOutput(ctx, "curl", args...)
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(output) {
	case strconv.Itoa(200):
		return true, nil
	case strconv.Itoa(404):
		return false, nil
	default:
		return false, fmt.Errorf("check R2 object: HTTP %s", strings.TrimSpace(output))
	}
}

func (p *publisher) r2Put(ctx context.Context, key, path, contentType, cacheControl string) error {
	args := []string{"--fail", "--silent", "--show-error", "--request", "PUT", "--upload-file", path, "--header", "Content-Type: " + contentType, "--header", "Cache-Control: " + cacheControl}
	args = append(args, p.r2AuthArgs()...)
	args = append(args, p.r2Endpoint(key))
	return runCommand(ctx, "curl", args...)
}

func runCommand(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func quietCommand(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %s", name, strings.TrimSpace(string(output)))
	}
	return nil
}
