package wailsui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/hay-kot/hive-desktop/internal/app"
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

func TestTrayItemLabel(t *testing.T) {
	cases := map[string]struct {
		item app.MenuBarItem
		want string
	}{
		"forge item": {
			item: app.MenuBarItem{Title: "Add retry budget", Repo: "acme/api", Number: 412, Reason: "review_requested", Unread: true},
			want: "  ● acme/api #412 Add retry budget · review",
		},
		"title only": {
			item: app.MenuBarItem{Title: "Rotate credentials"},
			want: "    Rotate credentials",
		},
		"unknown reason is left off": {
			item: app.MenuBarItem{Title: "CI", Repo: "acme/api", Reason: "ci_activity"},
			want: "    acme/api CI",
		},
		"long title": {
			item: app.MenuBarItem{Title: strings.Repeat("x", 70)},
			want: "    " + strings.Repeat("x", trayTitleLimit-1) + "…",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, trayItemLabel(tc.item))
		})
	}
}

func TestTraySummary(t *testing.T) {
	assert.Equal(t, "4 unread", traySummary(app.MenuBarSnapshot{OtherUnread: 4}))
	assert.Equal(t, "5 pinned · 4 unread elsewhere", traySummary(app.MenuBarSnapshot{
		Pinned:      []app.MenuBarFeedView{{Total: 2}, {Total: 3}},
		OtherUnread: 4,
	}))
}

func TestTrayUpdated(t *testing.T) {
	now := time.Date(2026, 9, 24, 16, 0, 0, 0, time.UTC)
	assert.Equal(t, "Not polled yet", trayUpdated(time.Time{}, now))
	assert.Equal(t, "Updated at 3:04 PM", trayUpdated(time.Date(2026, 9, 24, 15, 4, 0, 0, time.UTC), now))
	assert.Equal(t, "Updated Sep 23, 3:04 PM", trayUpdated(time.Date(2026, 9, 23, 15, 4, 0, 0, time.UTC), now))
}

func TestTrayFeedHeader(t *testing.T) {
	inFolder := app.MenuBarFeedView{ProfileName: "Work", Folder: "Deps", Name: "Renovate PRs", Unread: 3}
	assert.Equal(t, "Work › Deps › Renovate PRs (3 unread)", trayFeedHeader(inFolder))

	topLevel := app.MenuBarFeedView{ProfileName: "Work", Name: "Open PRs"}
	assert.Equal(t, "Work › Open PRs", trayFeedHeader(topLevel))
}
