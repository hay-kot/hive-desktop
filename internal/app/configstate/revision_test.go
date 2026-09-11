package configstate

import (
	"strings"
	"testing"
)

func TestAggregateRevisionSortsParts(t *testing.T) {
	t.Parallel()

	first := AggregateRevision([]RevisionPart{
		{Key: "flows/beta", State: Valid, Revision: "beta"},
		{Key: "flows/alpha", State: Missing, Revision: "alpha"},
	})
	second := AggregateRevision([]RevisionPart{
		{Key: "flows/alpha", State: Missing, Revision: "alpha"},
		{Key: "flows/beta", State: Valid, Revision: "beta"},
	})
	if first != second {
		t.Fatalf("aggregate revision depends on input order: %q != %q", first, second)
	}
}

func TestAggregateRevisionFramesParts(t *testing.T) {
	t.Parallel()

	first := AggregateRevision([]RevisionPart{{Key: "a", State: Valid, Revision: "bc"}})
	second := AggregateRevision([]RevisionPart{{Key: "ab", State: Valid, Revision: "c"}})
	if first == second {
		t.Fatal("aggregate revision did not frame logical fields")
	}
}

func TestCandidateRevisionIncludesDependencies(t *testing.T) {
	t.Parallel()

	own := BytesRevision([]byte("flow"))
	withoutDependency := CandidateRevision(own, nil)
	withActions := CandidateRevision(own, map[Source]Revision{Actions: BytesRevision([]byte("actions"))})
	if withoutDependency == withActions {
		t.Fatal("candidate revision ignored dependency-only change")
	}
}

func TestAggregateRevisionDoesNotUseAbsolutePaths(t *testing.T) {
	t.Parallel()

	path := "/Users/example/.config/hive/desktop/flows/main.yaml"
	revision := AggregateRevision([]RevisionPart{{
		Key:      "flows/main.yaml",
		State:    Valid,
		Revision: BytesRevision([]byte("name: main\n")),
	}})
	if strings.Contains(string(revision), path) {
		t.Fatalf("revision contains absolute path: %q", revision)
	}
}
