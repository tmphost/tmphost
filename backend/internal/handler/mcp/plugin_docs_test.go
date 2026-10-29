// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The plugins under plugin/ describe this server's tools to the agent; check
// they don't drift from what it registers.

// A leading table cell holding a backticked identifier.
var _docRowRe = regexp.MustCompile("(?m)^\\| `([a-zA-Z]+)`")

// registeredTools names the tools the server lists.
func registeredTools(t *testing.T) []string {
	t.Helper()
	rs, err := newEnv(t).session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := make([]string, len(rs.Tools))
	for i, tool := range rs.Tools {
		names[i] = tool.Name
	}
	return names
}

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
	registered := registeredTools(t)
	documented := matches(_docRowRe, readFile(t, filepath.Join(repoRoot(t), "plugin", "gemini", "README.md")))
	assert.ElementsMatch(t, registered, documented)
}

func TestPluginDocsNameEveryTool(t *testing.T) {
	root := repoRoot(t)
	registered := registeredTools(t)
	docs := []string{
		"backend/internal/handler/mcp/skills/tmphost/SKILL.md",
		"plugin/claude/skills/tmphost/SKILL.md",
		"plugin/codex/skills/tmphost/SKILL.md",
		"plugin/codex/README.md",
		"plugin/cursor/skills/tmphost/SKILL.md",
		"plugin/cursor/rules/prefer-tmphost-mcp.mdc",
		"plugin/cursor/README.md",
		"plugin/gemini/GEMINI.md",
		"plugin/gemini/README.md",
	}
	for _, doc := range docs {
		t.Run(doc, func(t *testing.T) {
			text := readFile(t, filepath.Join(root, doc))
			for _, tool := range registered {
				assert.Contains(t, text, "`"+tool+"`", "%s doesn't document the %q tool", doc, tool)
			}
		})
	}
}

// The review team tests the Codex plugin with the cases in its manifest; each
// names the tools it expects, which must be ones the server registers.
func TestCodexReviewCasesNameRegisteredTools(t *testing.T) {
	var manifest struct {
		Extensions struct {
			OpenAI struct {
				Review struct {
					TestCases struct {
						Positive []struct {
							ToolsTriggered string `json:"tools_triggered"`
						} `json:"positive"`
					} `json:"test_cases"`
				} `json:"review"`
			} `json:"com.openai"`
		} `json:"extensions"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(repoRoot(t), "plugin", "codex", "plugin.json"))), &manifest))

	registered := registeredTools(t)
	cases := manifest.Extensions.OpenAI.Review.TestCases.Positive
	require.NotEmpty(t, cases)
	for i, c := range cases {
		for _, tool := range strings.Split(c.ToolsTriggered, ",") {
			assert.Contains(t, registered, strings.TrimSpace(tool), "positive case %d expects a tool the server doesn't register", i)
		}
	}
}

// The plugin directory lists the plugin with .claude-plugin/icon.png: a
// square PNG, 512 to 2048 px a side, under 2 MB.
func TestPluginIcon(t *testing.T) {
	path := filepath.Join(repoRoot(t), "plugin", "claude", ".claude-plugin", "icon.png")
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	cfg, format, err := image.DecodeConfig(f)
	require.NoError(t, err)
	assert.Equal(t, "png", format)
	assert.Equal(t, cfg.Width, cfg.Height, "square")
	assert.GreaterOrEqual(t, cfg.Width, 512)
	assert.LessOrEqual(t, cfg.Width, 2048)
	info, err := f.Stat()
	require.NoError(t, err)
	assert.Less(t, info.Size(), int64(2<<20))
}

// The plugin directory shows its privacy policy, from the manifest and the
// plugin's README.
func TestPluginPrivacyPolicy(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "plugin", "claude")
	var manifest struct {
		Icon             string `json:"icon"`
		PrivacyPolicyURL string `json:"privacyPolicyUrl"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"))), &manifest))
	assert.Equal(t, "https://tmphost.io/privacy", manifest.PrivacyPolicyURL)
	assert.Equal(t, "./.claude-plugin/icon.png", manifest.Icon)
	assert.Contains(t, readFile(t, filepath.Join(dir, "README.md")), "[Privacy Policy](https://tmphost.io/privacy)")
}
