package canvas

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

// The html block kind: an agent writes structure, the app owns every pixel
// (ADR canvas-html-blocks-are-sanitized-in-go-and-styled-by-an-app-owned-class-vocabulary).
// Three sets below are the whole contract — the elements that survive, the
// attributes that survive, and the class vocabulary the stylesheet defines.
// They are declared once and feed three consumers: the sanitize policy, the
// write-time check that tells an agent what it got wrong, and the
// documentation test that holds them to the stylesheet and the MCP doc page.

// htmlElements is the structure an agent may write. Deny-by-default: an
// element absent here is dropped, and for the script-shaped ones bluemonday
// drops the character data with it. No img (a remote src in the webview is a
// beacon), no svg or math (the foreign-content parsing that mutation-XSS
// lives in), no form controls, and no button — the pane has exactly one
// interaction, and it is a link.
var htmlElements = []string{
	"a",
	"abbr",
	"article",
	"aside",
	"b",
	"blockquote",
	"br",
	"caption",
	"code",
	"col",
	"colgroup",
	"dd",
	"details",
	"div",
	"dl",
	"dt",
	"em",
	"figcaption",
	"figure",
	"footer",
	"h1",
	"h2",
	"h3",
	"h4",
	"h5",
	"h6",
	"header",
	"hr",
	"i",
	"kbd",
	"li",
	"mark",
	"ol",
	"p",
	"pre",
	"q",
	"s",
	"samp",
	"section",
	"small",
	"span",
	"strong",
	"sub",
	"summary",
	"sup",
	"table",
	"tbody",
	"td",
	"tfoot",
	"th",
	"thead",
	"time",
	"tr",
	"u",
	"ul",
	"var",
}

// htmlClasses is the styling contract. The agent picks a name from here and
// the app decides what it looks like, so a canvas follows the theme and two
// canvases written months apart still match. Anything else is not styling
// the app has an opinion about — it is a reach for the global Tailwind
// utilities — and is refused on write and stripped on render.
var htmlClasses = []string{
	// Layout.
	"hv-stack",
	"hv-row",
	"hv-grid",
	"hv-cols-2",
	"hv-cols-3",
	"hv-cols-4",
	// Containers.
	"hv-card",
	"hv-panel",
	"hv-callout",
	// Data.
	"hv-stat",
	"hv-stat-value",
	"hv-stat-label",
	"hv-kv",
	// Emphasis.
	"hv-badge",
	"hv-muted",
	"hv-mono",
	// Tones, for hv-callout and hv-badge.
	"hv-info",
	"hv-success",
	"hv-warn",
	"hv-error",
	"hv-accent",
}

// HTMLElements and HTMLClasses report the two allowlists, for the write-time
// error messages and the documentation test.
func HTMLElements() []string { return append([]string(nil), htmlElements...) }
func HTMLClasses() []string  { return append([]string(nil), htmlClasses...) }

// classAttrPattern matches a whole class attribute made only of vocabulary
// names, in any order. bluemonday matches an attribute policy against the
// entire value, so an attribute carrying one unknown name loses all of its
// classes rather than that one — acceptable because validateBlock refuses
// such a body on write, leaving this as the backstop for a file edited by
// hand or written by an older build.
var classAttrPattern = sync.OnceValue(func() *regexp.Regexp {
	quoted := make([]string, 0, len(htmlClasses))
	for _, c := range htmlClasses {
		quoted = append(quoted, regexp.QuoteMeta(c))
	}
	name := "(?:" + strings.Join(quoted, "|") + ")"
	return regexp.MustCompile(`^\s*` + name + `(?:\s+` + name + `)*\s*$`)
})

var htmlPolicy = sync.OnceValue(func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements(htmlElements...)
	p.AllowAttrs("class").Matching(classAttrPattern()).Globally()
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("colspan", "rowspan").Matching(regexp.MustCompile(`^[0-9]{1,3}$`)).OnElements("td", "th")
	p.AllowAttrs("scope").Matching(regexp.MustCompile(`^(?:row|col|rowgroup|colgroup)$`)).OnElements("th")
	p.AllowAttrs("open").Matching(regexp.MustCompile(`^(?:|open|true)$`)).OnElements("details")

	// The link block's rule, so a block never renders a link the pane's click
	// handler then refuses to open. Relative URLs stay out: there is nothing
	// in the webview for one to resolve against.
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(false)

	// v-html feeds this through innerHTML, which re-parses it. Foreign
	// content and templates are where a second parse disagrees with the
	// first, so drop what they carry rather than letting it re-enter as
	// markup.
	p.SkipElementsContent("svg", "math", "template", "form", "select", "textarea")
	return p
})

// SanitizeHTML renders an html block's source as markup that is safe to put
// in the app's own webview. The stored source is never touched — an agent
// reads back what it wrote, and the sanitized form is what leaves the app.
func SanitizeHTML(src string) string {
	return htmlPolicy().Sanitize(src)
}

// htmlAttrs is the attribute allowlist RejectedHTML reads, keyed by
// attribute name and listing the elements it is allowed on; an empty list
// means every allowed element. It restates what htmlPolicy grants because
// bluemonday cannot be asked what it would drop, and a silent drop is the
// one failure an agent cannot see. TestHTMLPolicyMatchesAttributeAllowlist
// holds the two together.
var htmlAttrs = map[string][]string{
	"class":   nil,
	"href":    {"a"},
	"colspan": {"td", "th"},
	"rowspan": {"td", "th"},
	"scope":   {"th"},
	"open":    {"details"},
}

// RejectedHTML names the first thing in src that SanitizeHTML would silently
// drop — an element, a class or an attribute — so a write fails with the
// name instead of rendering a canvas the agent believes is intact. An empty
// string means nothing is dropped.
func RejectedHTML(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	for {
		token := z.Next()
		if token == html.ErrorToken {
			return ""
		}
		if token != html.StartTagToken && token != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		tag := string(name)
		if !slices.Contains(htmlElements, tag) {
			return "the <" + tag + "> element"
		}
		for hasAttr {
			var rawKey, rawVal []byte
			rawKey, rawVal, hasAttr = z.TagAttr()
			attr := string(rawKey)
			on, ok := htmlAttrs[attr]
			if !ok || (len(on) > 0 && !slices.Contains(on, tag)) {
				return "the " + attr + " attribute on <" + tag + ">"
			}
			switch attr {
			case "class":
				for class := range strings.FieldsSeq(string(rawVal)) {
					if !slices.Contains(htmlClasses, class) {
						return "the class " + strconv.Quote(class)
					}
				}
			case "href":
				if reason := rejectedHref(string(rawVal)); reason != "" {
					return reason
				}
			}
		}
	}
}

// rejectedHref applies the link block's scheme rule to an anchor inside an
// html block: the policy drops the whole anchor for a scheme the pane would
// refuse to open, which reads to the agent as a link that vanished.
func rejectedHref(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "the unparseable link " + strconv.Quote(raw)
	}
	switch parsed.Scheme {
	case "http", "https", "mailto":
		return ""
	default:
		return "the link " + strconv.Quote(raw)
	}
}
