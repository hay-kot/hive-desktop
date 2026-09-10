package canvas

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sanitized form is what reaches the app's own webview, next to the Wails
// bindings, so these are exact expectations rather than "does not contain
// script": a policy that started escaping instead of dropping, or started
// keeping an element's character data, would still pass a substring check.
func TestSanitizeHTMLDropsHostileInput(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"script element and its content", `<p>a</p><script>alert(1)</script>`, `<p>a</p>`},
		{"event handler attribute", `<div onclick="alert(1)">x</div>`, `<div>x</div>`},
		{"handler on an allowed class", `<div class="hv-card" onmouseover="alert(1)">x</div>`, `<div class="hv-card">x</div>`},
		{"javascript href", `<a href="javascript:alert(1)">x</a>`, `x`},
		{"data uri href", `<a href="data:text/html,<script>alert(1)</script>">x</a>`, `x`},
		{"relative href", `<a href="/settings">x</a>`, `x`},
		{"image with an error handler", `<img src=x onerror=alert(1)>`, ``},
		{"remote image", `<img src="https://tracker.example/beacon.png">`, ``},
		{"iframe srcdoc", `<iframe srcdoc="&lt;script&gt;alert(1)&lt;/script&gt;"></iframe>`, ``},
		{"style attribute", `<div style="position:fixed;inset:0;background:red">x</div>`, `<div>x</div>`},
		{"style element", `<style>body{display:none}</style>`, ``},
		{"stylesheet link", `<link rel="stylesheet" href="https://x.example/e.css">`, ``},
		{"form and its controls", `<form action="https://x.example"><input name="password"><button>go</button></form>`, ``},
		{"svg wrapping a script", `<svg viewBox="0 0 10 10"><script>alert(1)</script></svg>`, `<svg viewBox="0 0 10 10"></svg>`},
		{"svg handler attribute", `<svg viewBox="0 0 10 10" onload="alert(1)"><circle r="2"/></svg>`, `<svg viewBox="0 0 10 10"><circle r="2"/></svg>`},
		{"mutation via svg foreignObject", `<svg viewBox="0 0 10 10"><foreignObject><img src=x onerror=alert(1)></foreignObject></svg>`, `<svg viewBox="0 0 10 10"></svg>`},
		{"mutation via svg desc", `<svg viewBox="0 0 10 10"><desc><style><!--</style><img src=x onerror=alert(1)></desc></svg>`, `<svg viewBox="0 0 10 10"></svg>`},
		{"mutation via svg title", `<svg viewBox="0 0 10 10"><title><a href="</title><img src=x onerror=alert(1)>"></title></svg>`, `<svg viewBox="0 0 10 10">&#34;&gt;</svg>`},
		{"mutation via svg cdata", `<svg viewBox="0 0 10 10"><![CDATA[</svg><img src=x onerror=alert(1)>]]></svg>`, `<svg viewBox="0 0 10 10">]]&gt;</svg>`},
		{"svg style element", `<svg viewBox="0 0 10 10"><style>body{display:none}</style><circle r="2"/></svg>`, `<svg viewBox="0 0 10 10"><circle r="2"/></svg>`},
		{"svg reaching out for a document", `<svg viewBox="0 0 10 10"><use href="#x"/><image href="https://x.example/a.png"/></svg>`, `<svg viewBox="0 0 10 10"></svg>`},
		{"agent-picked colours", `<svg viewBox="0 0 10 10"><path d="M0 0 L9 9" fill="red" stroke="#f00"/></svg>`, `<svg viewBox="0 0 10 10"><path d="M0 0 L9 9"/></svg>`},
		{"template smuggling an image", `<template><img src=x onerror=alert(1)></template>`, ``},
		{"mutation via math foreign content", `<math><mtext><table><mglyph><style><!--</style><img src=x onerror=alert(1)>`, ``},
		{"mutation via noscript title", `<noscript><p title="</noscript><img src=x onerror=alert(1)>">`, `&#34;&gt;`},
		{"split script tag", `<scr<script>ipt>alert(1)</script>`, `ipt&gt;alert(1)`},
		{"object and embed", `<object data="x"></object><embed src="x">`, ``},
		{"comment", `<p>a</p><!-- <script>alert(1)</script> -->`, `<p>a</p>`},
		{"unknown class", `<div class="hv-card grid-cols-2">x</div>`, `<div>x</div>`},
		{"tailwind reach", `<div class="fixed inset-0 bg-red-500">x</div>`, `<div>x</div>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SanitizeHTML(tc.src))
		})
	}
}

func TestSanitizeHTMLKeepsStructureAndVocabulary(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"a stat tile",
			`<div class="hv-grid hv-cols-3"><div class="hv-card hv-stat"><span class="hv-stat-value">42</span><span class="hv-stat-label">Open</span></div></div>`,
			`<div class="hv-grid hv-cols-3"><div class="hv-card hv-stat"><span class="hv-stat-value">42</span><span class="hv-stat-label">Open</span></div></div>`,
		},
		{
			"a toned badge",
			`<span class="hv-badge hv-success">passing</span>`,
			`<span class="hv-badge hv-success">passing</span>`,
		},
		{
			"a key/value list",
			`<dl class="hv-kv"><dt>Base</dt><dd class="hv-mono">main</dd></dl>`,
			`<dl class="hv-kv"><dt>Base</dt><dd class="hv-mono">main</dd></dl>`,
		},
		{"an http link", `<a href="https://example.com/pr/1">the PR</a>`, `<a href="https://example.com/pr/1">the PR</a>`},
		{"a mailto link", `<a href="mailto:a@example.com">mail</a>`, `<a href="mailto:a@example.com">mail</a>`},
		{"a table", `<table><tr><th scope="col">a</th><td colspan="2">b</td></tr></table>`, `<table><tr><th scope="col">a</th><td colspan="2">b</td></tr></table>`},
		{"a disclosure", `<details open><summary>s</summary><p>b</p></details>`, `<details open=""><summary>s</summary><p>b</p></details>`},
		{"entities in text", `<p>a &amp; b &lt;c&gt;</p>`, `<p>a &amp; b &lt;c&gt;</p>`},
		{
			"a diagram",
			`<svg viewBox="0 0 200 60"><g class="hv-accent"><rect class="hv-node" x="1" y="1" width="70" height="34" rx="6"/><text class="hv-label" x="36" y="22" text-anchor="middle">Poll</text></g><line class="hv-edge hv-dashed" x1="72" y1="18" x2="130" y2="18"/><polygon class="hv-arrow" points="130,14 138,18 130,22"/></svg>`,
			`<svg viewBox="0 0 200 60"><g class="hv-accent"><rect class="hv-node" x="1" y="1" width="70" height="34" rx="6"/><text class="hv-label" x="36" y="22" text-anchor="middle">Poll</text></g><line class="hv-edge hv-dashed" x1="72" y1="18" x2="130" y2="18"/><polygon class="hv-arrow" points="130,14 138,18 130,22"/></svg>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SanitizeHTML(tc.src))
		})
	}
}

// htmlAttrs is the one declaration the policy and RejectedHTML are both
// built from, so what is left to hold is that every rule in it reaches a
// real sanitize: a rule the policy loop passes over allows nothing and
// strips everything, and the write-time check would never see it.
func TestHTMLPolicyKeepsEveryDeclaredAttribute(t *testing.T) {
	sample := map[string]string{
		"class": "hv-card", "href": "https://example.com/", "colspan": "2",
		"rowspan": "2", "scope": "col", "open": "",
		"viewbox": "0 0 100 40", "transform": "translate(4 4)",
		"d": "M0 0 L9 9", "points": "0,0 9,9",
		"x": "1", "y": "1", "dx": "1", "dy": "1", "width": "10", "height": "10",
		"rx": "2", "ry": "2", "cx": "5", "cy": "5", "r": "4",
		"x1": "0", "y1": "0", "x2": "9", "y2": "9", "text-anchor": "middle",
	}
	for attr, rule := range htmlAttrs {
		value, ok := sample[attr]
		require.True(t, ok, "no sample value for the %s rule", attr)
		elements := rule.on
		if len(elements) == 0 {
			elements = []string{"div"}
		}
		for _, el := range elements {
			src := `<` + el + ` ` + attr + `="` + value + `">x</` + el + `>`
			// Lowercased because svgCasing spells viewBox the way SVG needs
			// it, and htmlAttrs is keyed by the name the tokenizer folds to.
			assert.Contains(t, strings.ToLower(SanitizeHTML(src)), attr+`="`, "policy drops %s on <%s>, which htmlAttrs allows", attr, el)
			assert.Empty(t, RejectedHTML(src), "the write check refuses %s on <%s>, which htmlAttrs allows", attr, el)
		}
	}

	for _, attr := range []string{"style", "id", "onclick", "onerror", "src", "target", "srcdoc", "data-x", "hidden"} {
		assert.NotContains(t, SanitizeHTML(`<div `+attr+`="v">x</div>`), attr, "%s must not survive", attr)
	}
}

// A value the policy would strip is as invisible to the agent as an element
// it would drop, so the shape rules are part of the write-time contract too.
func TestSanitizeHTMLDropsValuesOutsideTheShapeRules(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"a percentage width", `<rect width="50%" height="4"/>`, `<rect height="4"/>`},
		{"a unit on a coordinate", `<circle cx="4px" cy="4" r="2"/>`, `<circle cy="4" r="2"/>`},
		{"a url in a transform", `<g transform="url(#x)"><circle r="2"/></g>`, `<g><circle r="2"/></g>`},
		{"a script call in path data", `<path d="alert(1)"/>`, `<path/>`},
		{"a colspan that is not a number", `<td colspan="one">x</td>`, `<td>x</td>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SanitizeHTML(tc.src))
			assert.NotEmpty(t, RejectedHTML(tc.src), "the write check must refuse what the policy strips")
		})
	}
}

func TestRejectedHTMLNamesTheFirstOffender(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"clean", `<div class="hv-card"><p>a <strong>b</strong></p></div>`, ""},
		{"clean without any class", `<section><h3>Results</h3><ul><li>a</li></ul></section>`, ""},
		{"plain text", `no markup at all`, ""},
		{"script", `<p>a</p><script>alert(1)</script>`, "the <script> element"},
		{"image", `<img src="https://x.example/a.png">`, "the <img> element"},
		{"button", `<button>go</button>`, "the <button> element"},
		{"unknown class", `<div class="hv-card grid-cols-2">x</div>`, `the class "grid-cols-2"`},
		{"tailwind reach", `<div class="fixed inset-0">x</div>`, `the class "fixed"`},
		{"style attribute", `<div style="color:red">x</div>`, "the style attribute on <div>"},
		{"event handler", `<div class="hv-card" onclick="x()">x</div>`, "the onclick attribute on <div>"},
		{"href off an anchor", `<div href="https://x.example">x</div>`, "the href attribute on <div>"},
		{"good href", `<a href="https://x.example/pr/1">x</a>`, ""},
		{"mailto href", `<a href="mailto:a@example.com">x</a>`, ""},
		{"javascript href", `<a href="javascript:alert(1)">x</a>`, `the link "javascript:alert(1)"`},
		{"relative href", `<a href="/settings">x</a>`, `the link "/settings"`},
		{"element beats class", `<script class="nope">x</script>`, "the <script> element"},
		{"a clean diagram", `<svg viewBox="0 0 40 20"><rect class="hv-node" width="10" height="10"/></svg>`, ""},
		{"svg without a viewBox", `<svg><circle r="2"/></svg>`, "an <svg> with no viewBox"},
		{"svg sizing itself", `<svg viewBox="0 0 40 20" width="400" height="200"></svg>`, "the width attribute on <svg>"},
		{"an agent-picked colour", `<path d="M0 0" fill="red"/>`, "the fill attribute on <path>"},
		{"a stroke weight", `<line x1="0" y1="0" x2="9" y2="9" stroke-width="4"/>`, "the stroke-width attribute on <line>"},
		{"a percentage coordinate", `<rect width="50%" height="4"/>`, `the width value "50%" on <rect>`},
		{"a url in a transform", `<g transform="url(#x)"></g>`, `the transform value "url(#x)" on <g>`},
		{"defs for a marker", `<svg viewBox="0 0 40 20"><defs></defs></svg>`, "the <defs> element"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, RejectedHTML(tc.src))
		})
	}
}

// The doc page is the only place an agent learns what it may write, so an
// element or class the policy allows and the page omits is a capability
// nobody can use. The stylesheet is held the other way too: a class it styles
// but the sanitizer strips is a layout the agent cannot see failing.
func TestHTMLVocabularyIsStyledAndDocumented(t *testing.T) {
	css := readRepoFile(t, "desktop/frontend/src/styles/canvas-html.css")
	doc := readRepoFile(t, "internal/app/mcpcatalog/docs/hive-canvas.md")

	for _, class := range HTMLClasses() {
		assert.Contains(t, css, "."+class, "canvas-html.css does not style %s", class)
		assert.Contains(t, doc, "`"+class+"`", "hive-canvas.md does not document %s", class)
	}
	for _, element := range HTMLElements() {
		assert.Contains(t, doc, "`"+element+"`", "hive-canvas.md does not document <%s>", element)
	}

	// hv-html is the pane's own root scope, applied by AgentCanvasPane rather
	// than chosen by the agent, so it is the one selector outside the
	// vocabulary.
	for _, match := range regexp.MustCompile(`\.hv-[a-z0-9-]+`).FindAllString(css, -1) {
		class := strings.TrimPrefix(match, ".")
		if class == "hv-html" {
			continue
		}
		assert.Contains(t, HTMLClasses(), class, "canvas-html.css styles %s, which the sanitizer strips", class)
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", rel))
	require.NoError(t, err)
	return string(data)
}
