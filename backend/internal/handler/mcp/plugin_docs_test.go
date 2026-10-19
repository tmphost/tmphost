// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The plugins under plugin/ describe this server's tools to the agent; check
// they don't drift from what it registers.

var (
	// `mcp.AddTool(server, &mcp.Tool{Name: "file"`.
	_toolRe = regexp.MustCompile(`mcp\.AddTool\(server, &mcp\.Tool\{\s*Name:\s*"([^"]+)"`)
	// A leading table cell holding a backticked identifier.
	_docRowRe = regexp.MustCompile("(?m)^\\| `([a-zA-Z]+)`")
)

// repoRoot walks up from the test's directory to the one holding the
// Gemini CLI extension's manifest.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "gemini-extension.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no gemini-extension.json above the test")
		dir = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func matches(re *regexp.Regexp, s string) []string {
	var names []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		names = append(names, m[1])
	}
	return names
}

func TestPluginDocsListExactlyTheRegisteredTools(t *testing.T) {
	registered := matches(_toolRe, readFile(t, "mcp.go"))
	require.NotEmpty(t, registered, "no tools found in mcp.go; has the registration format changed?")

	documented := matches(_docRowRe, readFile(t, filepath.Join(repoRoot(t), "plugin", "gemini", "README.md")))
	assert.ElementsMatch(t, registered, documented)
}

func TestPluginDocsNameEveryAction(t *testing.T) {
	root := repoRoot(t)
	docs := []string{
		"plugin/claude/skills/tmphost/SKILL.md",
		"plugin/gemini/GEMINI.md",
		"plugin/gemini/README.md",
	}
	for _, doc := range docs {
		t.Run(doc, func(t *testing.T) {
			text := readFile(t, filepath.Join(root, doc))
			for _, action := range inputSchema().Properties["action"].Enum {
				assert.Contains(t, text, "`"+action.(string)+"`", "%s doesn't document the %q action", doc, action)
			}
		})
	}
}
