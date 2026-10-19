// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package markdown

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsMarkdown(t *testing.T) {
	for p, want := range map[string]bool{
		"README.md":        true,
		"docs/Guide.MD":    true,
		"notes.markdown":   true,
		"index.html":       false,
		"md":               false,
		"dir.md/notes.txt": false,
	} {
		assert.Equal(t, want, IsMarkdown(p), p)
	}
}

func TestPage(t *testing.T) {
	src := strings.Join([]string{
		"Intro",
		"",
		"# The *big* `idea`",
		"",
		"| a | b |",
		"|---|---|",
		"| 1 | 2 |",
		"",
		"- [x] done",
		"- ~~gone~~",
		"",
		"<script>alert(1)</script>",
		"",
		"[bad](javascript:alert(1)) https://tmphost.io",
		"",
		"Note[^1]",
		"",
		"[^1]: A footnote.",
	}, "\n")
	page := string(Page("docs/a b.md", []byte(src)))
	assert.Contains(t, page, "<title>The big idea</title>", "titled after the first h1")
	assert.Contains(t, page, `<h1 id="the-big-idea">The <em>big</em> <code>idea</code></h1>`)
	assert.Contains(t, page, "<table>")
	assert.Contains(t, page, `<input checked="" disabled="" type="checkbox">`)
	assert.Contains(t, page, "<del>gone</del>")
	assert.Contains(t, page, `<a href="https://tmphost.io">https://tmphost.io</a>`)
	assert.Contains(t, page, `class="footnotes"`)
	assert.NotContains(t, page, "<script>", "raw HTML is left out")
	assert.NotContains(t, page, "javascript:", "unsafe links are dropped")
	assert.Contains(t, page, `<span>a b.md</span><a href="./a%20b.md">Raw</a>`)
	assert.Contains(t, page, "prefers-color-scheme: dark")
}

func TestPage_Untitled(t *testing.T) {
	page := string(Page("<notes>.md", []byte("## Only a subheading\n")))
	assert.Contains(t, page, "<title>&lt;notes&gt;.md</title>", "titled after the file, escaped")
}
