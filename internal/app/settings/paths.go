// Package settings resolves the desktop app's typed settings and on-disk
// locations. Desktop-owned overrides use the HIVE_DESKTOP_ prefix; XDG and
// vendored Hive inputs remain external boundaries.
package settings

import (
	"os"
	"path/filepath"
	"sync"
)

const (
	EnvDataDir     = "HIVE_DESKTOP_DATA_DIR"
	EnvHiveDataDir = "HIVE_DESKTOP_HIVE_DATA_DIR"
	EnvConfigDir   = "HIVE_DESKTOP_CONFIG_DIR"
	EnvFlowsDir    = "HIVE_DESKTOP_FLOWS_DIR"
	EnvActionsPath = "HIVE_DESKTOP_ACTIONS_PATH"
	EnvMockMode    = "HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE"
	EnvE2EHarness  = "HIVE_DESKTOP_E2E_HARNESS"

	EnvHTTPEnabled = "HIVE_DESKTOP_HTTP_ENABLED"
	EnvHTTPHost    = "HIVE_DESKTOP_HTTP_HOST"
	EnvHTTPPort    = "HIVE_DESKTOP_HTTP_PORT"
)

// Paths is the immutable startup snapshot of every desktop-owned location.
// Runtime code receives this value instead of resolving process environment
// repeatedly.
type Paths struct {
	DataDir string
	// HiveDataDir holds hive.db, shared with the external hive CLI. It defaults
	// to DataDir; dev overrides it to the installed hive data dir so sessions
	// created in dev land in the real database while desktop state stays isolated.
	HiveDataDir          string
	StateDir             string
	ConfigDir            string
	ConfigPath           string
	FlowsDir             string
	ActionsPath          string
	SettingsPath         string
	CredentialsIndexPath string
	LogFile              string
	DataDirOverridden    bool
	ConfigDirOverridden  bool
}

// ResolvePaths applies explicit environment overrides over bootstrap values,
// then XDG defaults. mockMode only affects the isolated onboarding flow path.
func ResolvePaths(b Bootstrap, mockMode string) Paths {
	dataDir, dataEnv := os.LookupEnv(EnvDataDir)
	dataOverride := dataEnv && dataDir != ""
	if !dataOverride {
		dataDir = b.DataDir
		if dataDir == "" {
			dataHome := os.Getenv("XDG_DATA_HOME")
			if dataHome == "" {
				home, _ := os.UserHomeDir()
				dataHome = filepath.Join(home, ".local", "share")
			}
			dataDir = filepath.Join(dataHome, "hive")
		}
	}

	configDir, configEnv := os.LookupEnv(EnvConfigDir)
	configOverride := configEnv && configDir != ""
	if !configOverride {
		configDir = b.ConfigDir
		if configDir == "" {
			configHome := os.Getenv("XDG_CONFIG_HOME")
			if configHome == "" {
				home, _ := os.UserHomeDir()
				configHome = filepath.Join(home, ".config")
			}
			configDir = filepath.Join(configHome, "hive", "desktop")
		}
	}

	hiveDataDir, hiveEnv := os.LookupEnv(EnvHiveDataDir)
	if !hiveEnv || hiveDataDir == "" {
		hiveDataDir = dataDir
	}

	stateDir := filepath.Join(dataDir, "desktop")
	flowsDir := os.Getenv(EnvFlowsDir)
	if flowsDir == "" {
		if mockMode == MockOnboarding {
			flowsDir = onboardingFlowsDir()
		}
		if flowsDir == "" {
			flowsDir = filepath.Join(configDir, "flows")
		}
	}
	actionsPath := os.Getenv(EnvActionsPath)
	if actionsPath == "" {
		actionsPath = filepath.Join(configDir, "actions.yml")
	}
	return Paths{
		DataDir:              dataDir,
		HiveDataDir:          hiveDataDir,
		StateDir:             stateDir,
		ConfigDir:            configDir,
		ConfigPath:           filepath.Join(configDir, "profiles.yaml"),
		FlowsDir:             flowsDir,
		ActionsPath:          actionsPath,
		SettingsPath:         filepath.Join(configDir, settingsFileName),
		CredentialsIndexPath: filepath.Join(stateDir, "credentials.json"),
		LogFile:              filepath.Join(stateDir, logFileName),
		DataDirOverridden:    dataOverride || b.DataDir != "",
		ConfigDirOverridden:  configOverride || b.ConfigDir != "",
	}
}

func envMockMode() string {
	mode := os.Getenv(EnvMockMode)
	if mode == "" || mode == MockLive {
		return ""
	}
	return mode
}

func defaultPaths() Paths {
	b, _ := LoadBootstrap()
	return ResolvePaths(b, envMockMode())
}

// Package-level helpers are retained for isolated tests and e2e harnesses.
// Production runtime code uses the Paths snapshot injected from main.
func DataDir() string      { return defaultPaths().DataDir }
func StateDir() string     { return defaultPaths().StateDir }
func ConfigDir() string    { return defaultPaths().ConfigDir }
func FlowsDir() string     { return defaultPaths().FlowsDir }
func ActionsPath() string  { return defaultPaths().ActionsPath }
func SettingsPath() string { return defaultPaths().SettingsPath }
func MockMode() string     { return envMockMode() }

var onboardingFlowsDir = sync.OnceValue(func() string {
	dir, err := os.MkdirTemp("", "hive-desktop-onboarding-")
	if err != nil {
		return ""
	}
	return dir
})
