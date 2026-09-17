//go:build darwin && !server

package wailsui

import "github.com/wailsapp/wails/v3/pkg/application"

var detachedAccelerators = []application.Role{
	application.ZoomIn,
	application.ZoomOut,
	application.ResetZoom,
}

// buildMenu installs the default application menu without its zoom
// accelerators. A menu key equivalent is consumed before the webview sees the
// keydown, so the frontend keymap can only bind ⌘+/⌘-/⌘0 once the menu lets go
// of them (ADR the-zoom-chords-step-the-terminal-text-size-instead-of-magnifying-the-webview).
// The items stay clickable: magnification is the only way to scale the chrome.
func (u *UI) buildMenu() {
	menu := application.DefaultApplicationMenu()
	for _, role := range detachedAccelerators {
		if item := menu.FindByRole(role); item != nil {
			item.RemoveAccelerator()
		}
	}
	u.app.Menu.SetApplicationMenu(menu)
}
