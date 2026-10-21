// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// specPath is the OpenAPI document at the repository root.
const specPath = "../../../../openapi.yaml"

// specOnlyPaths are documented in openapi.yaml but registered outside
// New.
var specOnlyPaths = map[string]bool{"/mcp": true}

// adminPrefix is where the root-only admin routes live; they are kept out
// of openapi.yaml on purpose.
const adminPrefix = AdminPrefix + "/"

var (
	specParam  = regexp.MustCompile(`\{[^}]+\}`)
	routeParam = regexp.MustCompile(`:[^/]+|\*`)
)

// loadSpec returns the documented operations as path -> methods, with
// path parameters normalized to {}.
func loadSpec(t *testing.T) map[string]map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	require.NoError(t, err)
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	ops := map[string]map[string]bool{}
	for path, item := range doc.Paths {
		methods := map[string]bool{}
		for m := range item {
			if m != "parameters" {
				methods[strings.ToUpper(m)] = true
			}
		}
		ops[specParam.ReplaceAllString(path, "{}")] = methods
	}
	return ops
}

// TestOpenAPISpec keeps openapi.yaml in step with the routes: every
// registered path is documented, and every documented operation exists.
func TestOpenAPISpec(t *testing.T) {
	spec := loadSpec(t)
	app, _ := newApp(t)

	registered := map[string]map[string]bool{}
	for _, r := range app.GetRoutes(true) {
		path := routeParam.ReplaceAllString(r.Path, "{}")
		if registered[path] == nil {
			registered[path] = map[string]bool{}
		}
		registered[path][r.Method] = true
	}

	for path := range registered {
		if strings.HasPrefix(path, adminPrefix) {
			assert.NotContains(t, spec, path, "admin route %s must stay out of openapi.yaml", path)
			continue
		}
		assert.Contains(t, spec, path, "route %s is missing from openapi.yaml", path)
	}
	for path, methods := range spec {
		if specOnlyPaths[path] {
			continue
		}
		for m := range methods {
			assert.True(t, registered[path][m], "openapi.yaml documents %s %s, which is not a route", m, path)
		}
	}
	assert.True(t, spec["/api/v1/folders"][http.MethodPost])
}
