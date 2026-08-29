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
		{"svg wrapping a script", `<svg><script>alert(1)</script></svg>`, ``},
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SanitizeHTML(tc.src))
		})
	}
}

// htmlAttrs restates what the policy grants, and a drift between the two
// would either refuse a write the policy would have kept or — the direction
// that matters — accept one it silently strips.
func TestHTMLPolicyMatchesAttributeAllowlist(t *testing.T) {
	sample := map[string]string{
		"class": "hv-card", "href": "https://example.com/", "colspan": "2",
		"rowspan": "2", "scope": "col", "open": "",
	}
	for attr, elements := range htmlAttrs {
		if len(elements) == 0 {
			elements = []string{"div"}
		}
		for _, el := range elements {
			src := `<` + el + ` ` + attr + `="` + sample[attr] + `">x</` + el + `>`
			assert.Contains(t, SanitizeHTML(src), attr+`="`, "policy drops %s on <%s>, which htmlAttrs claims is allowed", attr, el)
		}
	}

	for _, attr := range []string{"style", "id", "onclick", "onerror", "src", "target", "srcdoc", "data-x", "hidden"} {
		assert.NotContains(t, SanitizeHTML(`<div `+attr+`="v">x</div>`), attr, "%s must not survive", attr)
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
