//go:build !server

package wailsui

import "github.com/wailsapp/wails/v3/pkg/application"

// The roles whose accelerators the app takes back from the platform menu.
var detachedAccelerators = []application.Role{
	application.ZoomIn,
	application.ZoomOut,
	application.ResetZoom,
}

// buildMenu installs the platform's default application menu with the zoom
// accelerators stripped.
//
// Those roles scale the webview itself — `setMagnification:` on macOS — which
// resamples the whole window and leaves it scrolling rather than reflowing, so
// a zoomed terminal grows a horizontal scrollbar instead of rewrapping. A menu
// key equivalent is consumed by the platform before the webview sees the
// keydown, so removing the accelerator is what lets the frontend keymap claim
// ⌘+/⌘-/⌘0 for the terminal font ladder (ADR
// cmd-and-cmd-step-the-terminal-font-size-instead-of-magnifying-the-webview).
//
// The items stay in the View menu, clickable: magnification is the only way to
// scale the chrome outside a terminal, and nothing else in the app offers it.
func (u *UI) buildMenu() {
	menu := application.DefaultApplicationMenu()
	for _, role := range detachedAccelerators {
		if item := menu.FindByRole(role); item != nil {
			item.RemoveAccelerator()
		}
	}
	u.app.Menu.SetApplicationMenu(menu)
}
