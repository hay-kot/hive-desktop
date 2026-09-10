package canvas

import (
	"maps"
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
// Four sets below are the whole contract — the elements that survive, the
// svg elements that survive with them, the attributes that survive, and the
// class vocabulary the stylesheet defines. They are declared once and feed
// three consumers: the sanitize policy, the write-time check that tells an
// agent what it got wrong, and the documentation test that holds them to the
// stylesheet and the MCP doc page.

// htmlElements is the structure an agent may write. Deny-by-default: an
// element absent here is dropped, and for the script-shaped ones bluemonday
// drops the character data with it. No img (a remote src in the webview is a
// beacon), no math, no form controls, and no button — the pane has exactly
// one interaction, and it is a link.
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

// svgElements is the drawing half of the vocabulary: enough to place boxes,
// join them, and label both
// (ADR canvas-diagrams-are-a-narrow-svg-subset-the-class-vocabulary-colours).
// The rest of svg stays out, each for its own reason. desc, title and
// foreignObject are the integration points where a browser parses HTML again
// inside foreign content, which is where a second parse disagrees with the
// first; script and style execute; use and image fetch; defs and marker need
// ids and url() references, a second name space to be correct about for an
// arrowhead a polygon already draws.
var svgElements = []string{
	"circle",
	"ellipse",
	"g",
	"line",
	"path",
	"polygon",
	"polyline",
	"rect",
	"svg",
	"text",
	"tspan",
}

// svgShapes is everything inside the drawing: svgElements without the root.
// transform belongs to what is drawn, never to the frame around it.
var svgShapes = slices.DeleteFunc(slices.Clone(svgElements), func(el string) bool { return el == "svg" })

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
	// Diagram roles, inside an svg.
	"hv-node",
	"hv-edge",
	"hv-arrow",
	"hv-dashed",
	"hv-label",
	// Tones, for hv-callout, hv-badge and the diagram roles.
	"hv-info",
	"hv-success",
	"hv-warn",
	"hv-error",
	"hv-accent",
}

// HTMLElements and HTMLClasses report the two allowlists, for the write-time
// error messages and the documentation test.
func HTMLElements() []string {
	return append(append([]string(nil), htmlElements...), svgElements...)
}
func HTMLClasses() []string { return append([]string(nil), htmlClasses...) }

// attrRule is one attribute's contract: the elements it may appear on — an
// empty list means every allowed element — and the values it may carry. The
// sanitize policy and RejectedHTML are both built from it, so what a write
// is refused for and what a render drops cannot drift apart. A nil pattern
// belongs to the two attributes whose values are checked by rule rather than
// by shape: class against the vocabulary, href against the link block's URL
// rule, each so the refusal can name the value instead of the attribute.
type attrRule struct {
	on      []string
	pattern *regexp.Regexp
}

// svgNumber is a coordinate in the viewBox's own units. Units, percentages
// and exponents stay out: a diagram is drawn in one flat coordinate system,
// and the stylesheet scales the whole thing to the pane.
const svgNumber = `[+-]?(?:\d+(?:\.\d+)?|\.\d+)`

var (
	numberPattern     = regexp.MustCompile(`^` + svgNumber + `$`)
	numberListPattern = regexp.MustCompile(`^\s*` + svgNumber + `(?:[\s,]+` + svgNumber + `)*\s*$`)
	viewBoxPattern    = regexp.MustCompile(`^\s*` + svgNumber + `(?:[\s,]+` + svgNumber + `){3}\s*$`)
	pathDataPattern   = regexp.MustCompile(`^[MmLlHhVvCcSsQqTtAaZz0-9+\-.,\s]+$`)
	transformPattern  = regexp.MustCompile(`^\s*(?:(?:matrix|translate|scale|rotate|skewX|skewY)\(\s*` + svgNumber + `(?:[\s,]+` + svgNumber + `)*\s*\)\s*)+$`)
	anchorPattern     = regexp.MustCompile(`^(?:start|middle|end)$`)
	spanPattern       = regexp.MustCompile(`^[0-9]{1,3}$`)
	scopePattern      = regexp.MustCompile(`^(?:row|col|rowgroup|colgroup)$`)
	openPattern       = regexp.MustCompile(`^(?:|open|true)$`)
)

var htmlAttrs = map[string]attrRule{
	"class":   {},
	"href":    {on: []string{"a"}},
	"colspan": {on: []string{"td", "th"}, pattern: spanPattern},
	"rowspan": {on: []string{"td", "th"}, pattern: spanPattern},
	"scope":   {on: []string{"th"}, pattern: scopePattern},
	"open":    {on: []string{"details"}, pattern: openPattern},

	// Geometry. width and height are absent from svg on purpose: how big a
	// diagram is belongs to the pane the user resized, not to the agent, so
	// the viewBox is the only size it states.
	"viewbox":     {on: []string{"svg"}, pattern: viewBoxPattern},
	"transform":   {on: svgShapes, pattern: transformPattern},
	"d":           {on: []string{"path"}, pattern: pathDataPattern},
	"points":      {on: []string{"polyline", "polygon"}, pattern: numberListPattern},
	"x":           {on: []string{"rect", "text", "tspan"}, pattern: numberPattern},
	"y":           {on: []string{"rect", "text", "tspan"}, pattern: numberPattern},
	"dx":          {on: []string{"text", "tspan"}, pattern: numberPattern},
	"dy":          {on: []string{"text", "tspan"}, pattern: numberPattern},
	"width":       {on: []string{"rect"}, pattern: numberPattern},
	"height":      {on: []string{"rect"}, pattern: numberPattern},
	"rx":          {on: []string{"rect", "ellipse"}, pattern: numberPattern},
	"ry":          {on: []string{"rect", "ellipse"}, pattern: numberPattern},
	"cx":          {on: []string{"circle", "ellipse"}, pattern: numberPattern},
	"cy":          {on: []string{"circle", "ellipse"}, pattern: numberPattern},
	"r":           {on: []string{"circle"}, pattern: numberPattern},
	"x1":          {on: []string{"line"}, pattern: numberPattern},
	"y1":          {on: []string{"line"}, pattern: numberPattern},
	"x2":          {on: []string{"line"}, pattern: numberPattern},
	"y2":          {on: []string{"line"}, pattern: numberPattern},
	"text-anchor": {on: []string{"text", "tspan"}, pattern: anchorPattern},
}

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
	p.AllowElements(svgElements...)
	// A <g> or a <text> carries no attribute of its own when a class does
	// the work, and bluemonday unwraps an attribute-less element unless it
	// is told otherwise.
	p.AllowNoAttrs().OnElements(svgElements...)

	p.AllowAttrs("class").Matching(classAttrPattern()).Globally()
	p.AllowAttrs("href").OnElements("a")
	for _, attr := range slices.Sorted(maps.Keys(htmlAttrs)) {
		rule := htmlAttrs[attr]
		if rule.pattern == nil {
			continue
		}
		allowed := p.AllowAttrs(attr).Matching(rule.pattern)
		if len(rule.on) == 0 {
			allowed.Globally()
		} else {
			allowed.OnElements(rule.on...)
		}
	}

	// The link block's rule, so a block never renders a link the pane's click
	// handler then refuses to open. Relative URLs stay out: there is nothing
	// in the webview for one to resolve against.
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(false)

	// v-html feeds this through innerHTML, which re-parses it. These are the
	// elements a second parse reads differently from the tokenizer this
	// policy runs on — math and template outright, and the three svg
	// integration points, where markup inside foreign content becomes HTML
	// again. Drop what they carry rather than letting it re-enter as markup.
	p.SkipElementsContent("math", "template", "form", "select", "textarea", "foreignobject", "desc", "title")
	return p
})

// svgCasing restores the one camelCase name in the vocabulary. SVG attribute
// names are case-sensitive and the HTML tokenizer folds them, so the policy
// emits viewbox; a browser is meant to fold it back when it re-parses the
// markup as foreign content, but jsdom does not, and a viewBox that does not
// apply is a diagram with no aspect ratio to scale by. Emitting the real name
// means nothing downstream has to. Text tokens escape their quotes, so the
// sequence this matches only ever occurs in an attribute position.
var svgCasing = strings.NewReplacer(` viewbox="`, ` viewBox="`)

// SanitizeHTML renders an html block's source as markup that is safe to put
// in the app's own webview. The stored source is never touched — an agent
// reads back what it wrote, and the sanitized form is what leaves the app.
func SanitizeHTML(src string) string {
	return svgCasing.Replace(htmlPolicy().Sanitize(src))
}

// RejectedHTML names the first thing in src that would not survive to the
// pane — an element, a class, an attribute or a value SanitizeHTML silently
// drops, or an svg with no viewBox, which the stylesheet cannot size — so a
// write fails with the name instead of rendering a canvas the agent believes
// is intact. An empty string means the source reaches the pane whole.
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
		if !slices.Contains(htmlElements, tag) && !slices.Contains(svgElements, tag) {
			return "the <" + tag + "> element"
		}
		hasViewBox := tag != "svg"
		for hasAttr {
			var rawKey, rawVal []byte
			rawKey, rawVal, hasAttr = z.TagAttr()
			attr, value := string(rawKey), string(rawVal)
			rule, ok := htmlAttrs[attr]
			if !ok || (len(rule.on) > 0 && !slices.Contains(rule.on, tag)) {
				return "the " + attr + " attribute on <" + tag + ">"
			}
			if attr == "viewbox" {
				hasViewBox = true
			}
			switch attr {
			case "class":
				for class := range strings.FieldsSeq(value) {
					if !slices.Contains(htmlClasses, class) {
						return "the class " + strconv.Quote(class)
					}
				}
			case "href":
				if reason := rejectedHref(value); reason != "" {
					return reason
				}
			default:
				if !rule.pattern.MatchString(value) {
					return "the " + attr + " value " + strconv.Quote(value) + " on <" + tag + ">"
				}
			}
		}
		if !hasViewBox {
			return "an <svg> with no viewBox"
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
