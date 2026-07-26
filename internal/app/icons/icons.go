// Package icons holds the curated glyph set feed rows and sidebar entries
// draw from. It is a leaf: the flow package validates a feed node's icon and
// the webhook connector validates its own, and neither may import the other,
// so the set they share lives below both.
package icons

// feed is the scoped set of glyphs a feed node (sidebar entry) or a webhook
// source node (feed-item rows) may carry. It is intentionally small — a
// curated list rather than every available icon — and must stay in sync with
// the frontend's feed icon registry (desktop/frontend/src/lib/feedIcons.ts).
var feed = map[string]bool{
	"git-branch":       true,
	"git-pull-request": true,
	"circle-dot":       true,
	"message-square":   true,
	"at-sign":          true,
	"rss":              true,
	"webhook":          true,
	"bell":             true,
	"eye":              true,
	"star":             true,
	"bug":              true,
	"shield":           true,
	"zap":              true,
	"sparkles":         true,
	"flag":             true,
	"inbox":            true,
	"users":            true,
	"tag":              true,
	"package":          true,
	"rocket":           true,
	"clock":            true,
}

// ValidFeed reports whether name is a supported feed glyph. The empty string
// means "use the default" and is always allowed, so callers can validate an
// optional field without special-casing it.
func ValidFeed(name string) bool {
	return name == "" || feed[name]
}
