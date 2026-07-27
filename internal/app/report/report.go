// Package report assembles a redacted diagnostic bundle for in-app bug
// reports and compresses it for upload. Redaction is the load-bearing part:
// config is included only after every secret-bearing field is scrubbed.
package report

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

const (
	MaxUploadBytes = 5 * 1024 * 1024

	maxLogTailBytes     = 256 * 1024
	maxDescriptionRunes = 5000
	maxContactRunes     = 254
)

// Build is the ldflags-stamped identity of the running binary, supplied by the
// adapter because -X binds to package main.
type Build struct {
	Version string
	Commit  string
	Date    string
}

type Options struct {
	Description string
	Contact     string
	IncludeLogs bool
	Channel     string
}

type Bundle struct {
	ReportID    string         `json:"report_id"`
	GeneratedAt time.Time      `json:"generated_at"`
	Description string         `json:"description,omitempty"`
	Contact     string         `json:"contact,omitempty"`
	Build       BuildInfo      `json:"build"`
	Logs        *LogTail       `json:"logs,omitempty"`
	Config      ConfigSnapshot `json:"config"`
}

type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	Channel   string `json:"channel,omitempty"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
	NumCPU    int    `json:"num_cpu"`
}

type LogTail struct {
	Path      string `json:"path"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
}

type ConfigSnapshot struct {
	Settings any        `json:"settings,omitempty"`
	Flows    []FlowFile `json:"flows,omitempty"`
	Actions  any        `json:"actions,omitempty"`
	Accounts []string   `json:"accounts,omitempty"`
}

type FlowFile struct {
	Name    string `json:"name"`
	Content any    `json:"content"`
}

type Assembler struct {
	paths settings.Paths
	build Build
}

func NewAssembler(paths settings.Paths, build Build) *Assembler {
	return &Assembler{paths: paths, build: build}
}

func (a *Assembler) Assemble(id string, at time.Time, opts Options) *Bundle {
	b := &Bundle{
		ReportID:    id,
		GeneratedAt: at,
		Description: scrubText(truncateRunes(strings.TrimSpace(opts.Description), maxDescriptionRunes)),
		Contact:     scrubText(truncateRunes(strings.TrimSpace(opts.Contact), maxContactRunes)),
		Build:       a.buildInfo(opts.Channel),
		Config:      a.configSnapshot(),
	}
	if opts.IncludeLogs {
		if tail := readLogTail(a.paths.LogFile, maxLogTailBytes); tail != nil {
			b.Logs = tail
		}
	}
	return b
}

func (a *Assembler) buildInfo(channel string) BuildInfo {
	return BuildInfo{
		Version:   a.build.Version,
		Commit:    a.build.Commit,
		Date:      a.build.Date,
		Channel:   channel,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		NumCPU:    runtime.NumCPU(),
	}
}

func (a *Assembler) configSnapshot() ConfigSnapshot {
	return ConfigSnapshot{
		Settings: redactedYAML(a.paths.SettingsPath),
		Actions:  redactedYAML(a.paths.ActionsPath),
		Flows:    redactedFlows(a.paths.FlowsDir),
		Accounts: readAccounts(a.paths.CredentialsIndexPath),
	}
}

func GzipJSON(b *Bundle) ([]byte, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// readLogTail returns the last max bytes of the log with a partial leading
// line dropped, and secret-looking values scrubbed. A missing log is nil.
func readLogTail(path string, max int64) *LogTail {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil
	}

	start, truncated := int64(0), false
	if info.Size() > max {
		start, truncated = info.Size()-max, true
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}

	content := string(data)
	if truncated {
		if i := strings.IndexByte(content, '\n'); i >= 0 {
			content = content[i+1:]
		}
	}
	content = scrubText(content)
	return &LogTail{Path: path, Bytes: len(content), Truncated: truncated, Content: content}
}

func redactedYAML(path string) any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	return Redact(doc)
}

func redactedFlows(dir string) []FlowFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var flows []FlowFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		if strings.HasSuffix(name, ".ui.yaml") || strings.HasSuffix(name, ".sidebar.yaml") {
			continue
		}
		if content := redactedYAML(filepath.Join(dir, name)); content != nil {
			flows = append(flows, FlowFile{Name: name, Content: content})
		}
	}
	return flows
}

func readAccounts(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var index struct {
		Refs []string `json:"refs"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil
	}
	return index.Refs
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
