package flow

import (
	"strings"
	"testing"
)

func TestWebhookSourceConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		config  WebhookSourceConfig
		wantErr string
	}{
		{name: "simple path", config: WebhookSourceConfig{Path: "ci-alerts"}},
		{name: "nested path", config: WebhookSourceConfig{Path: "ci/deploys/prod_1"}},
		{name: "with secret", config: WebhookSourceConfig{Path: "ci", Secret: "s3cret-token_9"}},
		{name: "missing path", config: WebhookSourceConfig{}, wantErr: "path is required"},
		{name: "blank path", config: WebhookSourceConfig{Path: "   "}, wantErr: "path is required"},
		{name: "uppercase", config: WebhookSourceConfig{Path: "CI"}, wantErr: "invalid path"},
		{name: "leading slash", config: WebhookSourceConfig{Path: "/ci"}, wantErr: "invalid path"},
		{name: "trailing slash", config: WebhookSourceConfig{Path: "ci/"}, wantErr: "invalid path"},
		{name: "empty segment", config: WebhookSourceConfig{Path: "ci//x"}, wantErr: "invalid path"},
		{name: "leading dash segment", config: WebhookSourceConfig{Path: "-ci"}, wantErr: "invalid path"},
		{name: "spaces", config: WebhookSourceConfig{Path: "ci alerts"}, wantErr: "invalid path"},
		{name: "path too long", config: WebhookSourceConfig{Path: strings.Repeat("a", 129)}, wantErr: "exceeds 128"},
		{name: "secret too long", config: WebhookSourceConfig{Path: "ci", Secret: strings.Repeat("s", 129)}, wantErr: "secret exceeds 128"},
		{name: "secret with space", config: WebhookSourceConfig{Path: "ci", Secret: "no spaces"}, wantErr: "printable ASCII"},
		{name: "secret with control char", config: WebhookSourceConfig{Path: "ci", Secret: "a\nb"}, wantErr: "printable ASCII"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate(nil)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestWebhookSourcePorts(t *testing.T) {
	cfg := &WebhookSourceConfig{Path: "ci"}
	if cfg.Inputs() != 0 || cfg.Outputs() != 1 {
		t.Fatalf("webhook-source ports = %d in / %d out, want 0 / 1", cfg.Inputs(), cfg.Outputs())
	}
}

func TestWebhookSourceDecodesFromYAML(t *testing.T) {
	doc := `
version: 1
name: Hooks
nodes:
  - { id: in-hook, type: webhook-source, path: ci-alerts, secret: tok }
  - { id: out, type: feed }
wires:
  - { from: in-hook, to: out }
`
	f, _, err := parseFlow("hooks", []byte(doc), testRefs{})
	if err != nil {
		t.Fatalf("parseFlow: %v", err)
	}
	cfg, ok := f.Nodes[0].Config.(*WebhookSourceConfig)
	if !ok {
		t.Fatalf("node config = %T, want *WebhookSourceConfig", f.Nodes[0].Config)
	}
	if cfg.Path != "ci-alerts" || cfg.Secret != "tok" {
		t.Fatalf("decoded config = %+v", cfg)
	}
}
