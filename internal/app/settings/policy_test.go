package settings

import (
	"reflect"
	"strings"
	"testing"
)

const expectedSettingsLeafCount = 45

func TestFieldClassificationsCoverSettingsYAMLLeaves(t *testing.T) {
	leaves := yamlLeaves(reflect.TypeFor[Settings](), "")
	classifications, err := collectFieldClassifications(reflect.TypeFor[Settings]())
	if err != nil {
		t.Fatal(err)
	}
	if len(classifications) != expectedSettingsLeafCount {
		t.Fatalf("got %d settings leaf classifications, want %d", len(classifications), expectedSettingsLeafCount)
	}

	seen := make(map[string]FieldPolicy, len(classifications))
	for _, classification := range classifications {
		if _, exists := leaves[classification.Path]; !exists {
			t.Errorf("classification names unknown settings leaf %q", classification.Path)
		}
		if _, duplicate := seen[classification.Path]; duplicate {
			t.Errorf("settings leaf %q has duplicate classifications", classification.Path)
		}
		if !validFieldPolicy(classification.Policy) {
			t.Errorf("settings leaf %q has unknown policy %q", classification.Path, classification.Policy)
		}
		seen[classification.Path] = classification.Policy
	}

	for leaf := range leaves {
		if _, classified := seen[leaf]; !classified {
			t.Errorf("settings leaf %q has no classification", leaf)
		}
	}
}

func TestFieldClassificationCollectorRejectsInvalidTags(t *testing.T) {
	tests := map[string]reflect.Type{
		"missing policy":   reflect.TypeFor[missingPolicySettings](),
		"unknown policy":   reflect.TypeFor[unknownPolicySettings](),
		"duplicate path":   reflect.TypeFor[duplicatePathSettings](),
		"container policy": reflect.TypeFor[containerPolicySettings](),
	}
	for name, typ := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := collectFieldClassifications(typ); err == nil {
				t.Fatal("expected invalid settings policy schema")
			}
		})
	}
}

func TestFieldClassificationsPinImportantPolicies(t *testing.T) {
	policies := classificationPolicies(t)
	expected := map[string]FieldPolicy{
		"version":                            FieldPolicyRestart,
		"http.enabled":                       FieldPolicyRestart,
		"http.host":                          FieldPolicyRestart,
		"http.port":                          FieldPolicyRestart,
		"paths.tmux":                         FieldPolicyRestart,
		"polling.interval":                   FieldPolicyLive,
		"updates.enabled":                    FieldPolicyLive,
		"notifications.delivery":             FieldPolicyLive,
		"appearance.terminal_font_size":      FieldPolicyLive,
		"keybindings":                        FieldPolicyLive,
		"editor.command":                     FieldPolicyLive,
		"agent_workspaces.session_end_delay": FieldPolicyLive,
	}
	for path, want := range expected {
		if got := policies[path]; got != want {
			t.Errorf("%s policy = %q, want %q", path, got, want)
		}
	}
}

func TestFieldClassificationsAreImmutable(t *testing.T) {
	first := FieldClassifications()
	if len(first) == 0 {
		t.Fatal("no field classifications")
	}
	original := first[0]
	first[0] = FieldClassification{Path: "mutated", Policy: FieldPolicyLive}

	second := FieldClassifications()
	if second[0] != original {
		t.Fatalf("caller mutation changed field classifications: got %+v, want %+v", second[0], original)
	}
}

func TestYAMLLeavesTreatMapsAsOneLeaf(t *testing.T) {
	leaves := yamlLeaves(reflect.TypeFor[Settings](), "")
	if _, found := leaves["keybindings"]; !found {
		t.Fatal("keybindings map is not a YAML leaf")
	}
	for leaf := range leaves {
		if strings.HasPrefix(leaf, "keybindings.") {
			t.Fatalf("keybindings map was expanded into %q", leaf)
		}
	}
}

func classificationPolicies(t *testing.T) map[string]FieldPolicy {
	t.Helper()
	classifications := FieldClassifications()
	policies := make(map[string]FieldPolicy, len(classifications))
	for _, classification := range classifications {
		policies[classification.Path] = classification.Policy
	}
	return policies
}

func yamlLeaves(typ reflect.Type, prefix string) map[string]struct{} {
	leaves := make(map[string]struct{})
	for field := range typ.Fields() {
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if field.Type.Kind() != reflect.Struct {
			leaves[path] = struct{}{}
			continue
		}
		for child := range yamlLeaves(field.Type, path) {
			leaves[child] = struct{}{}
		}
	}
	return leaves
}

type missingPolicySettings struct {
	Field string `yaml:"field"`
}

type unknownPolicySettings struct {
	Field string `yaml:"field" policy:"deferred"`
}

type duplicatePathSettings struct {
	First  string `yaml:"field" policy:"live"`
	Second string `yaml:"field" policy:"restart"`
}

type containerPolicyChild struct {
	Field string `yaml:"field" policy:"live"`
}

type containerPolicySettings struct {
	Child containerPolicyChild `yaml:"child" policy:"live"`
}
