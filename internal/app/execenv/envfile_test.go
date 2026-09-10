package execenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeEnvFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// absent makes a variable unset for one test rather than empty: t.Setenv
// registers the restore, and the Unsetenv is what removes it.
func absent(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	require.NoError(t, os.Unsetenv(name))
}

func TestParseEnvFileReadsTheShapesPeopleWrite(t *testing.T) {
	values, err := parseEnvFile([]byte(`
# a comment
HIVE_DEFAULT_AGENT=pi
export EDITOR=nvim
QUOTED="two words"
LITERAL='keep $HOME and \n as written'
ESCAPED="line\nbreak"
TRAILING=value   # why this value
EMPTY=
SPACED = padded
`))

	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"HIVE_DEFAULT_AGENT": "pi",
		"EDITOR":             "nvim",
		"QUOTED":             "two words",
		"LITERAL":            `keep $HOME and \n as written`,
		"ESCAPED":            "line\nbreak",
		"TRAILING":           "value",
		"EMPTY":              "",
		"SPACED":             "padded",
	}, values)
}

// A `#` is only a comment when something separates it from the value; a token
// or a URL fragment that contains one is not a comment.
func TestParseEnvFileKeepsAHashInsideAValue(t *testing.T) {
	values, err := parseEnvFile([]byte("TOKEN=abc#123\nURL=https://example.com/x#frag\n"))

	require.NoError(t, err)
	assert.Equal(t, "abc#123", values["TOKEN"])
	assert.Equal(t, "https://example.com/x#frag", values["URL"])
}

func TestParseEnvFileRejectsTheWholeFile(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"no assignment", "HIVE_DEFAULT_AGENT pi\n", "line 1: expected NAME=value"},
		{"not a name", "hive-default-agent=pi\n", "line 1: expected NAME=value"},
		{"set twice", "A=1\nB=2\nA=3\n", "line 3: A is set twice"},
		{"unterminated", "A=\"open\n", "line 1: A: a quoted value must close on the same line"},
		{"junk after the quote", "A=\"closed\" and more\n", "line 1: A: a quoted value must end the line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseEnvFile([]byte(tc.body))
			require.Error(t, err)
			assert.EqualError(t, err, tc.want)
		})
	}
}

// The value is where the secret is, so a parse failure may name the line and
// the variable and nothing else.
func TestParseEnvFileErrorsDoNotCarryTheValue(t *testing.T) {
	_, err := parseEnvFile([]byte("TOKEN=\"sk-live-do-not-log\n"))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sk-live-do-not-log")
}

func TestApplyFileSeedsTheProcessEnvironment(t *testing.T) {
	absent(t, "HIVE_DEFAULT_AGENT")
	path := writeEnvFile(t, "HIVE_DEFAULT_AGENT=pi\n")

	require.NoError(t, ApplyFile(path, zerolog.Nop()))
	assert.Equal(t, "pi", os.Getenv("HIVE_DEFAULT_AGENT"))
}

// A launch that names a variable is more specific than a file, the same order
// the login shell probe already follows.
func TestApplyFileLeavesALaunchVariableAlone(t *testing.T) {
	t.Setenv("HIVE_DEFAULT_AGENT", "codex")
	path := writeEnvFile(t, "HIVE_DEFAULT_AGENT=pi\n")

	require.NoError(t, ApplyFile(path, zerolog.Nop()))
	assert.Equal(t, "codex", os.Getenv("HIVE_DEFAULT_AGENT"))
}

// Defined means present, not non-empty: setting a variable to nothing is how a
// launch opts out of a value, and the file must not refill it.
func TestApplyFileTreatsAnEmptyLaunchVariableAsSet(t *testing.T) {
	t.Setenv("HIVE_DEFAULT_AGENT", "")
	path := writeEnvFile(t, "HIVE_DEFAULT_AGENT=pi\n")

	require.NoError(t, ApplyFile(path, zerolog.Nop()))
	assert.Empty(t, os.Getenv("HIVE_DEFAULT_AGENT"))
}

// PATH belongs to the resolver and HIVE_DESKTOP_* to settings.yaml. Neither is
// an error: the rest of the file still applies.
func TestApplyFileIgnoresTheNamesTheAppOwns(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	absent(t, "HIVE_DESKTOP_POLLING_INTERVAL")
	absent(t, "HIVE_TEST_ADOPTED")
	path := writeEnvFile(t, "PATH=/nowhere\nHIVE_DESKTOP_POLLING_INTERVAL=1h\nHIVE_TEST_ADOPTED=yes\n")

	require.NoError(t, ApplyFile(path, zerolog.Nop()))
	assert.Equal(t, "/usr/bin", os.Getenv("PATH"))
	assert.Empty(t, os.Getenv("HIVE_DESKTOP_POLLING_INTERVAL"))
	assert.Equal(t, "yes", os.Getenv("HIVE_TEST_ADOPTED"), "a name the app does not own still applies")
}

// A typo degrades to "nothing changed" rather than to half an environment.
func TestApplyFileAppliesNothingWhenTheFileIsBroken(t *testing.T) {
	absent(t, "HIVE_TEST_FIRST")
	path := writeEnvFile(t, "HIVE_TEST_FIRST=yes\nBROKEN LINE\n")

	require.Error(t, ApplyFile(path, zerolog.Nop()))
	_, set := os.LookupEnv("HIVE_TEST_FIRST")
	assert.False(t, set, "the line before the bad one is not applied either")
}

func TestApplyFileWithoutAFileIsANoOp(t *testing.T) {
	assert.NoError(t, ApplyFile(filepath.Join(t.TempDir(), "absent", ".env"), zerolog.Nop()))
}
