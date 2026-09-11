package settings

import (
	"reflect"
	"strings"
	"testing"
)

func TestFieldClassificationsCoverSettingsYAMLLeaves(t *testing.T) {
	leaves := yamlLeaves(reflect.TypeFor[Settings](), "")
	classifications := FieldClassifications()
	seen := make(map[string]FieldPolicy, len(classifications))
	for _, classification := range classifications {
		if _, exists := leaves[classification.Path]; !exists {
			t.Errorf("classification names unknown settings leaf %q", classification.Path)
		}
		if _, duplicate := seen[classification.Path]; duplicate {
			t.Errorf("settings leaf %q has duplicate classifications", classification.Path)
		}
		if classification.Policy != FieldPolicyLive && classification.Policy != FieldPolicyRestart && classification.Policy != FieldPolicyStartupOnly {
			t.Errorf("settings leaf %q has unknown policy %q", classification.Path, classification.Policy)
		}
		if classification.ApplyTarget == "" {
			t.Errorf("settings leaf %q has no apply target", classification.Path)
		}
		seen[classification.Path] = classification.Policy
	}

	for leaf := range leaves {
		if _, classified := seen[leaf]; !classified {
			t.Errorf("settings leaf %q has no classification", leaf)
		}
	}
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
