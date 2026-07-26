package wailsui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTrayProfilesIncludesValidAndInvalidFlows(t *testing.T) {
	summaries := []FlowSummary{
		{ID: "broken", Valid: false},
		{ID: "triage-id", Name: "Triage", Enabled: false, Valid: true},
	}

	assert.Equal(t, []trayProfile{
		{ID: "broken", Label: "broken (invalid)"},
		{ID: "triage-id", Label: "Triage", Enabled: false, Valid: true},
	}, trayProfiles(summaries))
}
