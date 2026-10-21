// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Fail the build when the Codex / ChatGPT plugin in plugin/codex wouldn't
 * pass OpenAI's directory submission, its marketplace entry is off, or its
 * MCP server differs from the other plugins'.
 *
 * The rules live in `codex-plugin.mjs`, where they are tested; this is the
 * part that reads the disk.
 *
 *   node .github/scripts/validate-codex-plugin.mjs
 */
import { existsSync, lstatSync, readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { report, validateMarketplace, validatePlugin } from './codex-plugin.mjs'

const ROOT = fileURLToPath(new URL('../../', import.meta.url))
const PLUGIN = 'plugin/codex'
const MARKETPLACE = '.agents/plugins/marketplace.json'

/** Every file under `dir`, by its path inside it, and any symbolic links. */
function snapshot(dir, prefix = '', pkg = { files: {}, links: [] }) {
  for (const name of readdirSync(join(dir, prefix))) {
    const rel = prefix ? `${prefix}/${name}` : name
    const stat = lstatSync(join(dir, rel))
    if (stat.isSymbolicLink()) pkg.links.push(rel)
    else if (stat.isDirectory()) snapshot(dir, rel, pkg)
    else pkg.files[rel] = readFileSync(join(dir, rel))
  }
  return pkg
}

const readJSON = (path) => JSON.parse(readFileSync(join(ROOT, path), 'utf8'))

const result = validatePlugin(snapshot(join(ROOT, PLUGIN)))
const out = [report(result, `${PLUGIN}/plugin.json`)]
let failed = result.errors.length > 0

if (result.manifest) {
  const errors = validateMarketplace(readJSON(MARKETPLACE), {
    name: result.manifest.name,
    path: PLUGIN,
    isPlugin: (path) => existsSync(join(ROOT, path, 'plugin.json')),
  })
  out.push(report({ errors, warnings: [] }, MARKETPLACE))
  failed ||= errors.length > 0
}

// Every plugin connects to the same server.
const others = {
  'plugin/claude/.claude-plugin/plugin.json': readJSON('plugin/claude/.claude-plugin/plugin.json').mcpServers.tmphost.url,
  'gemini-extension.json': readJSON('gemini-extension.json').mcpServers.tmphost.url,
}
for (const [file, url] of Object.entries(others)) {
  if (url !== result.url) {
    out.push(`::error file=${PLUGIN}/mcp.json::mcp_url_mismatch: the MCP URL ${result.url} differs from ${file}'s ${url}`)
    failed = true
  }
}

if (!readFileSync(join(ROOT, PLUGIN, 'README.md'), 'utf8').includes('[Privacy Policy](https://tmphost.io/privacy)')) {
  out.push(`::error file=${PLUGIN}/README.md::README.md must link [Privacy Policy](https://tmphost.io/privacy)`)
  failed = true
}

const text = out.filter(Boolean).join('\n')
if (text) console.log(text)
if (failed) process.exit(1)
console.log(`✓ ${PLUGIN} passes OpenAI's plugin submission checks (${result.warnings.length} warning(s))`)
