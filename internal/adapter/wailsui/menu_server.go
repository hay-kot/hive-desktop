//go:build server

package wailsui

// The headless build renders in a browser, which owns its own zoom and has no
// application menu to take the accelerators back from.
func (u *UI) buildMenu() {}
