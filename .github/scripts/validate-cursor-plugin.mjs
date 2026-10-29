// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Fail the build when the Cursor plugin in plugin/cursor wouldn't pass the
 * Cursor Marketplace's submission checklist, its marketplace entry is off, or
 * its MCP server differs from the other plugins'. With --live, also probe
 * that server's OAuth discovery the way Cursor signs in.
 *
 * The rules live in `cursor-plugin.mjs`, where they are tested; this is the
 * part that reads the disk and the network.
 *
 *   node .github/scripts/validate-cursor-plugin.mjs [--live]
 */
import { existsSync, lstatSync, readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { report } from './codex-plugin.mjs'
import { checkOAuth, validateMarketplace, validatePlugin } from './cursor-plugin.mjs'

const ROOT = fileURLToPath(new URL('../../', import.meta.url))
const PLUGIN = 'plugin/cursor'
const MARKETPLACE = '.cursor-plugin/marketplace.json'

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
const out = [report({ errors: result.errors, warnings: [] }, `${PLUGIN}/.cursor-plugin/plugin.json`)]
let failed = result.errors.length > 0

if (result.manifest) {
  const errors = validateMarketplace(readJSON(MARKETPLACE), {
    name: result.manifest.name,
    path: PLUGIN,
    isPlugin: (path) => existsSync(join(ROOT, path, '.cursor-plugin', 'plugin.json')),
  })
  out.push(report({ errors, warnings: [] }, MARKETPLACE))
  failed ||= errors.length > 0
}

// Every plugin connects to the same server.
const others = {
  'plugin/claude/.claude-plugin/plugin.json': readJSON('plugin/claude/.claude-plugin/plugin.json').mcpServers.tmphost.url,
  'plugin/codex/mcp.json': readJSON('plugin/codex/mcp.json').mcpServers.tmphost.url,
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

if (process.argv.includes('--live') && result.url) {
  const errors = await checkOAuth(result.url, (url, init) => fetch(url, { ...init, signal: AbortSignal.timeout(20_000) }))
  out.push(report({ errors, warnings: [] }, `${PLUGIN}/mcp.json`))
  failed ||= errors.length > 0
}

const text = out.filter(Boolean).join('\n')
if (text) console.log(text)
if (failed) process.exit(1)
console.log(`✓ ${PLUGIN} passes the Cursor Marketplace checks${process.argv.includes('--live') ? ' and its MCP server asks for OAuth sign-in' : ''}`)
