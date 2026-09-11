package configstate

import (
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

func TestTruncateUTF8PreservesRuneBoundaries(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("界", 100)
	truncated := truncateUTF8(value, 256)
	if len(truncated) > 256 {
		t.Fatalf("summary exceeds byte limit: %d", len(truncated))
	}
	if !utf8.ValidString(truncated) {
		t.Fatalf("summary splits a rune: %q", truncated)
	}
}
