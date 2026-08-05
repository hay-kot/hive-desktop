package configmigrate

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// renameStep is a synthetic Set whose single real migration renames old_name
// to new_name, used to prove Apply actually transforms a below-current
// document rather than just bumping the version stamp.
func renameStep() Set {
	return Set{
		Name:                "widget",
		Baseline:            1,
		Current:             2,
		AllowMissingVersion: true,
		Migrations: []Migration{
			{To: 2, Migrate: func(doc map[string]any) error {
				if v, ok := doc["old_name"]; ok {
					doc["new_name"] = v
					delete(doc, "old_name")
				}
				return nil
			}},
		},
	}
}

func decodeDoc(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	return doc
}

func TestValidate_RegisteredSets(t *testing.T) {
	t.Parallel()

	sets := []Set{SettingsSet, FlowSet, ActionsSet, MCPLibrarySet, AgentWorkspaceSet}
	for _, s := range sets {
		t.Run(s.Name, func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, s.Validate())
		})
	}
}

// The MCP cut-over (ADR mcp-replaces-the-agent-facing-http-api) retired the hive-http-api skill slug. A workspace
// still declaring it fails to open outright — resolveSkills refuses a slug no
// prompt id backs — so the rename has to happen before the manifest is loaded.
func TestAgentWorkspace_RenamesTheRetiredHTTPAPISkill(t *testing.T) {
	t.Parallel()

	raw := []byte("version: 1\nname: Hive\nagent: claude\nautonomy: ask\nskills:\n  - hive-flows\n  - hive-http-api\n  - hive-settings\n")

	migrated, changed, err := AgentWorkspaceSet.Apply(raw)
	require.NoError(t, err)
	require.True(t, changed)

	doc := decodeDoc(t, migrated)
	assert.Equal(t, AgentWorkspaceSet.Current, doc["version"])
	assert.Equal(t, []any{"hive-flows", "hive-mcp", "hive-settings"}, doc["skills"],
		"the slug is renamed in place, keeping its position")
}

// A manifest naming both would otherwise declare hive-mcp twice, which the
// workspace validator rejects as a duplicate — turning a migration into the
// breakage it exists to prevent.
func TestAgentWorkspace_SkillRenameDoesNotDuplicate(t *testing.T) {
	t.Parallel()

	raw := []byte("version: 1\nname: Hive\nagent: claude\nautonomy: ask\nskills:\n  - hive-http-api\n  - hive-mcp\n")

	migrated, changed, err := AgentWorkspaceSet.Apply(raw)
	require.NoError(t, err)
	require.True(t, changed)

	assert.Equal(t, []any{"hive-mcp"}, decodeDoc(t, migrated)["skills"])
}

// A manifest with no skills list at all is the common case, and must migrate
// to the new version without growing an empty key.
func TestAgentWorkspace_SkillRenameToleratesNoSkills(t *testing.T) {
	t.Parallel()

	migrated, changed, err := AgentWorkspaceSet.Apply([]byte("version: 1\nname: Hive\nagent: claude\nautonomy: ask\n"))
	require.NoError(t, err)
	require.True(t, changed)

	doc := decodeDoc(t, migrated)
	assert.Equal(t, AgentWorkspaceSet.Current, doc["version"])
	assert.NotContains(t, doc, "skills")
}

func TestValidate_GapChainRejected(t *testing.T) {
	t.Parallel()

	s := Set{
		Name:       "gappy",
		Baseline:   1,
		Current:    3,
		Migrations: []Migration{{To: 3}},
	}
	assert.Error(t, s.Validate())
}

func TestValidate_DuplicateStepRejected(t *testing.T) {
	t.Parallel()

	s := Set{
		Name:     "dupe",
		Baseline: 1,
		Current:  2,
		Migrations: []Migration{
			{To: 2},
			{To: 2},
		},
	}
	assert.Error(t, s.Validate())
}

func TestValidate_NoOpChainIsValid(t *testing.T) {
	t.Parallel()

	s := Set{Name: "flat", Baseline: 1, Current: 1}
	assert.NoError(t, s.Validate())
}

func TestApply_EmptyInput(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{"nil": nil, "empty": {}, "whitespace": []byte("   \n")} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			migrated, changed, err := SettingsSet.Apply(raw)
			require.NoError(t, err)
			assert.Nil(t, migrated)
			assert.False(t, changed)
		})
	}
}

func TestApply_CorruptInputFailsDecodeNotPanic(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		migrated, changed, err := SettingsSet.Apply([]byte("foo: [unterminated"))
		assert.Error(t, err)
		assert.False(t, changed)
		assert.Nil(t, migrated)
	})
}

func TestApply_VersionAboveCurrentRejected(t *testing.T) {
	t.Parallel()

	migrated, changed, err := renameStep().Apply([]byte("version: 3\n"))
	require.ErrorIs(t, err, ErrVersionTooNew)
	assert.False(t, changed)
	assert.Nil(t, migrated)
}

func TestApply_VersionBelowBaselineRejected(t *testing.T) {
	t.Parallel()

	_, _, err := renameStep().Apply([]byte("version: 0\n"))
	assert.Error(t, err)
}

func TestApply_NonIntegerVersionRejected(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{
		"string": []byte("version: \"two\"\n"),
		"float":  []byte("version: 1.5\n"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			migrated, changed, err := renameStep().Apply(raw)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrVersionTooNew)
			assert.False(t, changed)
			assert.Nil(t, migrated)
		})
	}
}

func TestApply_VersionEqualCurrentIsNoOp(t *testing.T) {
	t.Parallel()

	s := renameStep()
	raw := []byte("version: 2\nnew_name: foo\n")
	migrated, changed, err := s.Apply(raw)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, raw, migrated)
}

func TestApply_AbsentVersionDefaultsToBaseline(t *testing.T) {
	t.Parallel()

	// Baseline == Current: an absent version defaults to Baseline, which is
	// already current, so this is a no-op (the Current == Baseline no-op
	// invariant every registered Set relies on before its first real step).
	s := Set{Name: "flat", Baseline: 1, Current: 1, AllowMissingVersion: true}
	raw := []byte("some_key: value\n")
	migrated, changed, err := s.Apply(raw)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, raw, migrated)
}

func TestApply_AbsentVersionRejectedWhenRequired(t *testing.T) {
	t.Parallel()

	migrated, changed, err := renameStep().Apply([]byte("old_name: hello\n"))
	require.NoError(t, err)
	require.True(t, changed)
	require.NotNil(t, migrated)

	required := renameStep()
	required.AllowMissingVersion = false
	migrated, changed, err = required.Apply([]byte("old_name: hello\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version field is required")
	assert.False(t, changed)
	assert.Nil(t, migrated)
}

func TestApply_MultipleDocumentsRejected(t *testing.T) {
	t.Parallel()

	migrated, changed, err := renameStep().Apply([]byte("old_name: hello\n---\nother: value\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple YAML documents")
	assert.False(t, changed)
	assert.Nil(t, migrated)
}

func TestApply_StepErrorAbortsWithNoBytes(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	s := Set{
		Name:                "broken",
		Baseline:            1,
		Current:             2,
		AllowMissingVersion: true,
		Migrations: []Migration{
			{To: 2, Migrate: func(doc map[string]any) error { return boom }},
		},
	}

	migrated, changed, err := s.Apply([]byte("k: v\n"))
	require.ErrorIs(t, err, boom)
	assert.False(t, changed)
	assert.Nil(t, migrated)
}

func TestApply_RenameStepTransformsAndRestampsVersion(t *testing.T) {
	t.Parallel()

	s := renameStep()
	migrated, changed, err := s.Apply([]byte("old_name: hello\n"))
	require.NoError(t, err)
	assert.True(t, changed)
	require.NotNil(t, migrated)

	doc := decodeDoc(t, migrated)
	assert.Equal(t, 2, doc["version"])
	assert.Equal(t, "hello", doc["new_name"])
	_, hasOldName := doc["old_name"]
	assert.False(t, hasOldName, "old_name should be removed by the rename step")
}

func TestApply_IsIdempotent(t *testing.T) {
	t.Parallel()

	s := renameStep()
	migrated, changed, err := s.Apply([]byte("old_name: hello\n"))
	require.NoError(t, err)
	require.True(t, changed)

	_, changedAgain, err := s.Apply(migrated)
	require.NoError(t, err)
	assert.False(t, changedAgain, "re-applying to an already-migrated document must be a no-op")
}
