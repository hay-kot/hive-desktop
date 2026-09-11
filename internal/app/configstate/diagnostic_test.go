package configstate

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSummaryUsesFixedMessages(t *testing.T) {
	t.Parallel()

	summary := Summary(Diagnostic{
		Stage:  Decode,
		Reason: InvalidDocument,
		Line:   12,
		Column: 4,
	})
	if summary != "Configuration document is invalid (line 12, column 4)" {
		t.Fatalf("unexpected summary: %q", summary)
	}

	opaque := Summary(Diagnostic{Stage: Stage("秘密"), Reason: Reason("password=secret")})
	if opaque != "Configuration could not be processed" {
		t.Fatalf("opaque diagnostic leaked into summary: %q", opaque)
	}
}

func TestSummaryDoesNotDeriveProseFromOpaqueInput(t *testing.T) {
	t.Parallel()

	opaque := "秘密 https://example.invalid/path?token=secret command: rm -rf /"
	summary := Summary(Diagnostic{Stage: Stage(opaque), Reason: Reason(opaque)})
	assert := func(fragment string) {
		t.Helper()
		if strings.Contains(summary, fragment) {
			t.Fatalf("summary leaked opaque input %q: %q", fragment, summary)
		}
	}
	assert("秘密")
	assert("https://")
	assert("token")
	assert("command")
}

func TestTruncateUTF8PreservesRuneBoundaries(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{255, 256, 257} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			value := strings.Repeat("界", 100)
			truncated := truncateUTF8(value, limit)
			if len(truncated) > limit {
				t.Fatalf("summary exceeds byte limit: %d > %d", len(truncated), limit)
			}
			if len(truncated) != 255 {
				t.Fatalf("summary did not retain the maximum complete runes: %d", len(truncated))
			}
			if !utf8.ValidString(truncated) {
				t.Fatalf("summary splits a rune: %q", truncated)
			}
		})
	}
}
