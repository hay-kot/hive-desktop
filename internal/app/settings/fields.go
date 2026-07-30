package settings

import (
	"fmt"
	"reflect"
	"strings"
)

// FieldChange is one settings field whose value differs between two snapshots.
// Field is the dotted YAML path ("polling.interval"); From and To are the
// values formatted for display, since a caller comparing snapshots is reporting
// to a human, not switching on the type.
type FieldChange struct {
	Field string
	From  string
	To    string
}

// Diff reports every field whose value differs, in declaration order. It walks
// the schema by reflection rather than comparing a hand-written list of fields,
// so a field added to Settings is compared without anyone remembering to.
func Diff(before, after Settings) []FieldChange {
	var changes []FieldChange
	walkFields(reflect.ValueOf(before), reflect.ValueOf(after), "", func(name string, from, to reflect.Value) {
		if equal(from, to) {
			return
		}
		changes = append(changes, FieldChange{Field: name, From: format(from), To: format(to)})
	})
	return changes
}

// FieldNames lists every dotted path Diff can report. It is what lets a
// consumer assert it has classified the whole schema.
func FieldNames() []string {
	var names []string
	zero := reflect.ValueOf(Settings{})
	walkFields(zero, zero, "", func(name string, _, _ reflect.Value) {
		names = append(names, name)
	})
	return names
}

// walkFields visits every leaf field of two Settings values in parallel. A
// nested struct recurses; a map or scalar is a leaf, because a settings map
// (keybindings, skills.targets) is edited and reported as a whole.
func walkFields(before, after reflect.Value, prefix string, visit func(name string, from, to reflect.Value)) {
	t := before.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := yamlName(field.Tag.Get("yaml"))
		if name == "" || name == "-" {
			continue
		}
		name = prefix + name
		from, to := before.Field(i), after.Field(i)
		if from.Kind() == reflect.Struct {
			walkFields(from, to, name+".", visit)
			continue
		}
		visit(name, from, to)
	}
}

func yamlName(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	return name
}

// equal treats an absent map and an empty one as the same value: an omitted
// keybindings section and a hand-written `keybindings: {}` both mean "no
// overrides", and reporting a change between them would announce an edit the
// user did not make.
func equal(from, to reflect.Value) bool {
	if from.Kind() == reflect.Map && from.Len() == 0 && to.Len() == 0 {
		return true
	}
	return reflect.DeepEqual(from.Interface(), to.Interface())
}

func format(v reflect.Value) string {
	if v.Kind() == reflect.Map && v.Len() == 0 {
		return ""
	}
	return fmt.Sprintf("%v", v.Interface())
}
