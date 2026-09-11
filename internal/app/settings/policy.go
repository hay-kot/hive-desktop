package settings

// FieldPolicy describes when an accepted setting takes effect.
type FieldPolicy string

const (
	FieldPolicyLive        FieldPolicy = "live"
	FieldPolicyRestart     FieldPolicy = "restart"
	FieldPolicyStartupOnly FieldPolicy = "startup_only"
)

// FieldClassification assigns one reconciliation policy and apply target to a settings.yaml leaf.
type FieldClassification struct {
	Path        string
	Policy      FieldPolicy
	ApplyTarget string
}

var fieldClassifications = []FieldClassification{
	{"version", FieldPolicyStartupOnly, "YAML migration metadata"},
	{"polling.interval", FieldPolicyLive, "Producer interval and all GitHub search TTLs, subsequent ticks"},
	{"updates.enabled", FieldPolicyLive, "Adapter updater ticker; no cancellation of an install in progress"},
	{"updates.channel", FieldPolicyRestart, "Existing updater provider/channel"},
	{"notifications.enabled", FieldPolicyLive, "Next notification eligibility"},
	{"notifications.delivery", FieldPolicyLive, "Next notification routing"},
	{"notifications.sound", FieldPolicyLive, "Next notification sound"},
	{"appearance.theme", FieldPolicyLive, "Mounted theme and first-paint cache"},
	{"appearance.font_family", FieldPolicyLive, "Mounted UI typography"},
	{"appearance.mono_font_family", FieldPolicyLive, "Mounted monospace UI typography"},
	{"appearance.terminal_font_size", FieldPolicyLive, "Mounted terminal options/refit"},
	{"appearance.terminal_font_family", FieldPolicyLive, "Mounted terminal options/refit"},
	{"appearance.terminal_font_weight", FieldPolicyLive, "Mounted terminal normal weight"},
	{"appearance.terminal_font_weight_bold", FieldPolicyLive, "Mounted terminal bold weight"},
	{"appearance.terminal_line_height", FieldPolicyLive, "Existing frontend normalization/renderer policy"},
	{"appearance.terminal_letter_spacing", FieldPolicyLive, "Existing frontend normalization/renderer policy"},
	{"appearance.terminal_show_windows", FieldPolicyLive, "Code sidebar tree"},
	{"appearance.terminal_show_status_bar", FieldPolicyLive, "Terminal status bar"},
	{"appearance.terminal_pool_size", FieldPolicyLive, "Existing bounded attach-pool policy"},
	{"profiles.order", FieldPolicyLive, "FlowStore order and views, no runner replay"},
	{"keybindings", FieldPolicyLive, "Mounted keymap including pane dispatch"},
	{"paths.tmux", FieldPolicyRestart, "Current resolver and sessions unchanged"},
	{"editor.command", FieldPolicyLive, "Subsequent open-in-editor operation"},
	{"agent_workspaces.dir", FieldPolicyRestart, "Immutable resolved root and current watch set"},
	{"agent_workspaces.session_end_delay", FieldPolicyLive, "New end requests only"},
	{"http.enabled", FieldPolicyRestart, "Existing shared listener"},
	{"http.host", FieldPolicyRestart, "Existing bind"},
	{"http.port", FieldPolicyRestart, "Existing bind, with startup auto-port adoption"},
	{"telemetry.enabled", FieldPolicyRestart, "Existing providers/exporters/log arms"},
	{"telemetry.endpoint", FieldPolicyRestart, "No live secret-reference resolution"},
	{"telemetry.instance_id", FieldPolicyRestart, "Existing exporter destination identity"},
	{"telemetry.token", FieldPolicyRestart, "No live secret-reference resolution"},
	{"development.mocks.mode", FieldPolicyRestart, "Existing composition"},
	{"development.instance.id", FieldPolicyRestart, "Existing process identity"},
	{"development.github.api_base", FieldPolicyRestart, "Existing clients/caches"},
	{"development.vite.host", FieldPolicyRestart, "Existing dev server composition"},
	{"development.vite.port", FieldPolicyRestart, "Existing dev server composition"},
	{"development.wails.host", FieldPolicyRestart, "Existing dev server composition"},
	{"development.wails.port", FieldPolicyRestart, "Existing dev server composition"},
	{"development.pprof.enabled", FieldPolicyRestart, "Existing mounted route"},
	{"development.perf.enabled", FieldPolicyRestart, "Existing recorder"},
	{"development.metrics.enabled", FieldPolicyRestart, "Existing scrape route"},
	{"development.devtools.enabled", FieldPolicyRestart, "Existing service"},
	{"development.debug.pause_ingest", FieldPolicyRestart, "Existing producer wiring"},
	{"development.debug.pause_commit", FieldPolicyRestart, "Existing DB wrapper"},
}

// FieldClassifications returns the policy for every settings.yaml leaf.
func FieldClassifications() []FieldClassification {
	return append([]FieldClassification(nil), fieldClassifications...)
}
