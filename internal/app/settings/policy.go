package settings

import (
	"fmt"
	"reflect"
	"strings"
)

const fieldPolicyTag = "policy"

// FieldPolicy describes when an accepted setting takes effect.
type FieldPolicy string

const (
	FieldPolicyLive    FieldPolicy = "live"
	FieldPolicyRestart FieldPolicy = "restart"
)

// FieldClassification assigns one reconciliation policy to a settings.yaml leaf.
type FieldClassification struct {
	Path   string
	Policy FieldPolicy
}

// FieldClassifications returns the policy for every settings.yaml leaf.
func FieldClassifications() []FieldClassification {
	classifications, err := collectFieldClassifications(reflect.TypeFor[Settings]())
	if err != nil {
		panic(err)
	}
	return append([]FieldClassification(nil), classifications...)
}

func collectFieldClassifications(typ reflect.Type) ([]FieldClassification, error) {
	var classifications []FieldClassification
	seen := make(map[string]struct{})
	var issues []string
	appendFieldClassifications(typ, "", seen, &classifications, &issues)
	if len(issues) > 0 {
		return nil, fmt.Errorf("settings policy schema is invalid: %s", strings.Join(issues, "; "))
	}
	return classifications, nil
}

func appendFieldClassifications(typ reflect.Type, prefix string, seen map[string]struct{}, classifications *[]FieldClassification, issues *[]string) {
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
		policy := FieldPolicy(field.Tag.Get(fieldPolicyTag))
		if field.Type.Kind() == reflect.Struct {
			if policy != "" {
				*issues = append(*issues, fmt.Sprintf("container %q has policy %q", path, policy))
			}
			appendFieldClassifications(field.Type, path, seen, classifications, issues)
			continue
		}

		if policy == "" {
			*issues = append(*issues, fmt.Sprintf("leaf %q has no policy", path))
			continue
		}
		if !validFieldPolicy(policy) {
			*issues = append(*issues, fmt.Sprintf("leaf %q has unknown policy %q", path, policy))
			continue
		}
		if _, exists := seen[path]; exists {
			*issues = append(*issues, fmt.Sprintf("leaf %q has duplicate policy", path))
			continue
		}
		seen[path] = struct{}{}
		*classifications = append(*classifications, FieldClassification{Path: path, Policy: policy})
	}
}

func validFieldPolicy(policy FieldPolicy) bool {
	switch policy {
	case FieldPolicyLive, FieldPolicyRestart:
		return true
	default:
		return false
	}
}
