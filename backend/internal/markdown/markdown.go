// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package markdown renders Markdown files as readable HTML pages, for
// public files asked for with ?format=html.
package markdown

import (
	"bytes"
	"html/template"
	"path"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// FormatHTML is the ?format= value that asks for a Markdown file as HTML.
const FormatHTML = "html"

// ContentType is the media type of a rendered page.
const ContentType = "text/html; charset=utf-8"

// _md leaves raw HTML and javascript: links out (goldmark's default), so a
// rendered page holds only the markup Markdown itself produces.
var _md = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Footnote),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// IsMarkdown reports whether the file at p is Markdown, by its extension.
func IsMarkdown(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// Page renders src as a standalone HTML page. The page is titled after the
// document's first top-level heading, else after the file at p, and links
// back to the raw file.
func Page(p string, src []byte) []byte {
	doc := _md.Parser().Parse(text.NewReader(src))
	name := path.Base(p)
	title := firstHeading(doc, src)
	if title == "" {
		title = name
	}
	var body bytes.Buffer
	// Writing to a bytes.Buffer cannot fail.
	_ = _md.Renderer().Render(&body, src, doc)
	var page bytes.Buffer
	_ = _page.Execute(&page, map[string]any{
		"Title": title,
		"Name":  name,
		// Escaped by goldmark; raw HTML in the source was left out.
		"Body": template.HTML(body.String()),
	})
	return page.Bytes()
}

// firstHeading returns the text of the document's first level-1 heading.
func firstHeading(doc ast.Node, src []byte) string {
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if h, ok := n.(*ast.Heading); ok && h.Level == 1 {
			var b strings.Builder
			_ = ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if t, ok := n.(*ast.Text); ok && entering {
					b.Write(t.Segment.Value(src))
				}
				return ast.WalkContinue, nil
			})
			return strings.TrimSpace(b.String())
		}
	}
	return ""
}

// _page is a reading layout in the spirit of Medium: a serif body at a
// large size in a wide centred column, sans-serif headings, and light or
// dark colours following the visitor's system. It loads nothing from
// elsewhere; the page is served sandboxed.
var _page = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>{{.Title}}</title>
<style>
:root {
  --bg: #ffffff;
  --fg: #242424;
  --muted: #6b6b6b;
  --rule: #e6e6e6;
  --link: #1a8917;
  --code-bg: #f6f6f6;
  --quote: #242424;
  --mark: #fff3a3;
  --serif: Charter, "Bitstream Charter", "Sitka Text", Cambria, Georgia, serif;
  --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  --mono: ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #121212;
    --fg: #e6e6e6;
    --muted: #a0a0a0;
    --rule: #2e2e2e;
    --link: #5cc95a;
    --code-bg: #1e1e1e;
    --quote: #e6e6e6;
    --mark: #5c5200;
  }
}
* { box-sizing: border-box; }
html { -webkit-text-size-adjust: 100%; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--fg);
  font-family: var(--serif);
  font-size: 20px;
  line-height: 1.6;
  -webkit-font-smoothing: antialiased;
  text-rendering: optimizeLegibility;
}
.bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  max-width: 960px;
  margin: 0 auto;
  padding: 20px 24px;
  font-family: var(--sans);
  font-size: 14px;
  color: var(--muted);
  border-bottom: 1px solid var(--rule);
}
.bar span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar a { color: var(--muted); text-decoration: none; border: 1px solid var(--rule); border-radius: 999px; padding: 4px 14px; }
.bar a:hover { color: var(--fg); border-color: var(--muted); }
article {
  max-width: 960px;
  margin: 0 auto;
  padding: 48px 24px 96px;
  overflow-wrap: break-word;
}
article > :first-child { margin-top: 0; }
h1, h2, h3, h4, h5, h6 {
  font-family: var(--sans);
  font-weight: 700;
  line-height: 1.25;
  letter-spacing: -0.016em;
  margin: 1.8em 0 0.5em;
}
h1 { font-size: 2.4em; letter-spacing: -0.022em; margin-bottom: 0.6em; }
h2 { font-size: 1.6em; }
h3 { font-size: 1.3em; }
h4 { font-size: 1.1em; }
h5, h6 { font-size: 1em; }
h6 { color: var(--muted); }
p, ul, ol, dl, table, pre, blockquote, figure { margin: 0 0 1.4em; }
ul, ol { padding-left: 1.6em; }
li { margin: 0.3em 0; }
li > p { margin: 0.3em 0; }
li > ul, li > ol { margin: 0.3em 0; }
a { color: inherit; text-decoration: underline; text-decoration-color: var(--muted); text-underline-offset: 3px; }
a:hover { color: var(--link); text-decoration-color: var(--link); }
strong { font-weight: 700; }
mark { background: var(--mark); color: inherit; }
hr { border: 0; margin: 2.6em 0; text-align: center; height: auto; }
hr::before { content: "\2026"; display: block; color: var(--muted); font-size: 1.6em; letter-spacing: 0.6em; line-height: 1; }
blockquote {
  margin-left: 0;
  padding-left: 1.2em;
  border-left: 3px solid var(--fg);
  color: var(--quote);
  font-style: italic;
}
blockquote > :last-child { margin-bottom: 0; }
img { max-width: 100%; height: auto; display: block; margin: 0 auto; }
code {
  font-family: var(--mono);
  font-size: 0.8em;
  background: var(--code-bg);
  border-radius: 4px;
  padding: 0.15em 0.35em;
}
pre {
  font-family: var(--mono);
  font-size: 16px;
  line-height: 1.5;
  background: var(--code-bg);
  border-radius: 6px;
  padding: 20px 24px;
  overflow-x: auto;
  tab-size: 4;
}
pre code { font-size: inherit; background: none; padding: 0; border-radius: 0; }
table {
  display: block;
  max-width: 100%;
  overflow-x: auto;
  border-collapse: collapse;
  font-family: var(--sans);
  font-size: 16px;
}
th, td { border: 1px solid var(--rule); padding: 8px 14px; text-align: left; }
th { font-weight: 600; background: var(--code-bg); }
input[type="checkbox"] { margin: 0 0.5em 0 0; vertical-align: middle; }
li:has(> input[type="checkbox"]) { list-style: none; margin-left: -1.4em; }
del { color: var(--muted); }
.footnotes { font-size: 0.85em; color: var(--muted); }
.footnotes hr { margin: 2em 0 1em; border-top: 1px solid var(--rule); }
.footnotes hr::before { content: none; }
@media (max-width: 600px) {
  body { font-size: 18px; }
  .bar { padding: 14px 16px; }
  article { padding: 32px 16px 64px; }
  h1 { font-size: 2em; }
  pre { padding: 16px; font-size: 14px; }
}
</style>
</head>
<body>
<header class="bar"><span>{{.Name}}</span><a href="./{{.Name}}">Raw</a></header>
<article>
{{.Body}}
</article>
</body>
</html>
`))
