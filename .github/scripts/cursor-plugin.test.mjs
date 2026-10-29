// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import { checkOAuth, inside, validateMarketplace, validatePlugin } from './cursor-plugin.mjs'

const URL_ = 'https://mcp.tmphost.io/mcp'

function manifest() {
  return {
    name: 'tmphost',
    displayName: 'tmphost',
    version: '1.0.0',
    description: 'Host files at temporary public URLs.',
    author: { name: 'tmphost' },
    homepage: 'https://tmphost.io',
    repository: 'https://github.com/tmphost/tmphost',
    license: 'AGPL-3.0-only',
    logo: 'assets/logo.svg',
    keywords: ['a', 'b', 'c'],
    rules: './rules/',
    skills: './skills/',
    commands: './commands/',
    mcpServers: './mcp.json',
  }
}

/** A plugin that passes every check; tests break one thing at a time. */
function pkg({ manifest: m = manifest(), files = {} } = {}) {
  const all = {
    '.cursor-plugin/plugin.json': JSON.stringify(m),
    'mcp.json': JSON.stringify({ mcpServers: { tmphost: { url: URL_ } } }),
    'assets/logo.svg': '<svg/>',
    'rules/use.mdc': '---\ndescription: Use it\nalwaysApply: false\n---\n\nUse tmphost.\n',
    'skills/tmphost/SKILL.md': '---\nname: tmphost\ndescription: Share files\n---\n\n# tmphost\n',
    'skills/tmphost/notes.md': 'not a skill',
    'commands/share-file.md': '---\nname: share-file\ndescription: Share a file\n---\n\nUpload it.\n',
    ...files,
  }
  return { files: Object.fromEntries(Object.entries(all).filter(([, v]) => v !== null).map(([k, v]) => [k, Buffer.from(v)])) }
}

const codes = (result) => (Array.isArray(result) ? result : result.errors).map((e) => e.code)

describe('inside', () => {
  it('normalises relative paths and rejects escaping ones', () => {
    assert.equal(inside('./rules/'), 'rules')
    assert.equal(inside('./a/./b'), 'a/b')
    assert.equal(inside('./'), '')
    for (const bad of [undefined, '', '/etc', '../x', 'a/../../x', 'https://x']) assert.equal(inside(bad), null, String(bad))
  })
})

describe('validatePlugin', () => {
  it('passes a valid plugin and returns its manifest and MCP URL', () => {
    const r = validatePlugin(pkg())
    assert.deepEqual(r.errors, [])
    assert.equal(r.manifest.name, 'tmphost')
    assert.equal(r.url, URL_)
  })

  it('rejects symbolic links', () => {
    assert.deepEqual(codes(validatePlugin({ ...pkg(), links: ['skills/x'] })), ['symlink'])
  })

  it('needs a JSON object manifest', () => {
    assert.deepEqual(codes(validatePlugin(pkg({ files: { '.cursor-plugin/plugin.json': null } }))), ['manifest_missing'])
    assert.deepEqual(codes(validatePlugin(pkg({ files: { '.cursor-plugin/plugin.json': '{' } }))), ['manifest_invalid_json'])
    assert.deepEqual(codes(validatePlugin(pkg({ files: { '.cursor-plugin/plugin.json': '[]' } }))), ['manifest_wrong_type'])
  })

  it('checks every manifest field', () => {
    const m = {
      ...manifest(), extra: 1, name: 'Tmp_host', displayName: ' ', version: '1', description: 'short',
      author: {}, homepage: 'http://tmphost.io', repository: 'nope', license: '', keywords: ['a'],
    }
    assert.deepEqual(codes(validatePlugin(pkg({ manifest: m }))), [
      'manifest_unknown_field', 'name_format', 'display_name_missing', 'version_format', 'description_missing',
      'author_missing', 'homepage_not_https', 'repository_not_https', 'license_missing', 'keywords_missing',
    ])
  })

  it('checks manifest paths', () => {
    const m = { ...manifest(), logo: '../logo.svg', rules: 'nowhere/' }
    assert.deepEqual(codes(validatePlugin(pkg({ manifest: m }))), ['path_unsafe', 'path_missing'])
    // The plugin root itself exists.
    assert.deepEqual(codes(validatePlugin(pkg({ manifest: { ...manifest(), skills: './' } }))), [])
  })

  it('needs a logo and refuses unchecked components', () => {
    const { logo, ...m } = manifest()
    assert.ok(logo)
    assert.deepEqual(codes(validatePlugin(pkg({ manifest: m }))), ['logo_missing'])
    assert.deepEqual(codes(validatePlugin(pkg({ manifest: { ...manifest(), hooks: {} } }))), ['component_unchecked'])
  })

  it('reads mcp.json by default and checks it', () => {
    const { mcpServers, ...m } = manifest()
    assert.ok(mcpServers)
    assert.equal(validatePlugin(pkg({ manifest: m })).url, URL_)
    assert.deepEqual(codes(validatePlugin(pkg({ files: { 'mcp.json': null } }))), ['path_missing', 'mcp_missing'])
    assert.deepEqual(codes(validatePlugin(pkg({ files: { 'mcp.json': '{' } }))), ['mcp_invalid_json'])
    assert.deepEqual(codes(validatePlugin(pkg({ files: { 'mcp.json': '[]' } }))), ['mcp_servers'])
    assert.deepEqual(codes(validatePlugin(pkg({ files: { 'mcp.json': '{"mcpServers":{"a":{}}}' } }))), ['mcp_servers'])
  })

  it('needs an OAuth-only HTTPS server', () => {
    const server = (s) => JSON.stringify({ mcpServers: { tmphost: s } })
    assert.deepEqual(codes(validatePlugin(pkg({ files: { 'mcp.json': server({ url: URL_, headers: {} }) } }))), ['mcp_not_oauth'])
    assert.deepEqual(codes(validatePlugin(pkg({ files: { 'mcp.json': server({ url: 'http://x' }) } }))), ['mcp_url_not_https'])
    assert.deepEqual(codes(validatePlugin(pkg({ files: { 'mcp.json': server(null) } }))), ['mcp_not_oauth', 'mcp_url_not_https'])
  })

  it('needs each component', () => {
    const r = validatePlugin(pkg({ files: { 'rules/use.mdc': null, 'skills/tmphost/SKILL.md': null, 'commands/share-file.md': null } }))
    // The manifest's paths go missing too.
    assert.deepEqual(codes(r), ['path_missing', 'path_missing', 'rules_missing', 'skills_missing', 'commands_missing'])
  })

  it('checks front matter', () => {
    const r = validatePlugin(pkg({ files: {
      'rules/use.mdc': 'no front matter',
      'rules/other.md': '---\ndescription: x\nalwaysApply: yes\n---\n\nBody\n',
      'commands/share-file.md': '---\nname: share-file\n---\n\n',
    } }))
    assert.deepEqual(codes(r), ['frontmatter_invalid', 'rule_always_apply', 'description_missing', 'body_empty'])
  })

  it('names skills after their directory and commands after their file, uniquely', () => {
    const r = validatePlugin(pkg({ files: {
      'skills/tmphost/SKILL.md': '---\nname: other\ndescription: x\n---\n\nBody\n',
      'commands/tmphost.txt': '---\ndescription: x\n---\n\nBody\n',
      'commands/x.md': '---\nname: x\ndescription: x\n---\n\nBody\n',
      'skills/x/SKILL.md': '---\nname: x\ndescription: x\n---\n\nBody\n',
    } }))
    assert.deepEqual(codes(r), ['name_mismatch', 'name_mismatch', 'name_duplicate'])
  })
})

describe('validateMarketplace', () => {
  const opts = { name: 'tmphost', path: 'plugin/cursor', isPlugin: () => true }
  const market = (plugins) => ({ name: 'tmphost', owner: { name: 'tmphost' }, plugins })

  it('passes an entry pointing at the plugin', () => {
    assert.deepEqual(validateMarketplace(market([{ name: 'tmphost', source: './plugin/cursor' }]), opts), [])
    assert.deepEqual(validateMarketplace(market([{ name: 'tmphost', source: { path: 'plugin/cursor' } }]), opts), [])
  })

  it('checks its own fields', () => {
    assert.deepEqual(codes(validateMarketplace(null, opts)), ['marketplace_wrong_type'])
    assert.deepEqual(codes(validateMarketplace({ name: 'Bad Name', plugins: {} }, opts)), ['marketplace_name', 'marketplace_owner', 'marketplace_plugins'])
  })

  it('checks the entries', () => {
    assert.deepEqual(codes(validateMarketplace(market([null, null]), opts)), ['marketplace_duplicate', 'marketplace_entry_missing'])
    assert.deepEqual(codes(validateMarketplace(market([{ name: 'tmphost', source: './plugin/codex' }]), opts)), ['marketplace_source'])
    assert.deepEqual(codes(validateMarketplace(market([{ name: 'tmphost', source: './plugin/cursor' }]), { ...opts, isPlugin: () => false })), ['marketplace_source_not_plugin'])
  })
})

describe('checkOAuth', () => {
  const PRM = 'https://mcp.tmphost.io/.well-known/oauth-protected-resource/mcp'
  const json = (body) => ({ json: async () => body })
  /** A server answering like production; `override` replaces responses by URL. */
  function server(override = {}) {
    const responses = {
      [URL_]: { status: 401, headers: new Headers({ 'www-authenticate': `Bearer resource_metadata="${PRM}"` }) },
      [PRM]: json({ resource: URL_, authorization_servers: ['https://mcp.tmphost.io'] }),
      'https://mcp.tmphost.io/.well-known/oauth-authorization-server': json({
        issuer: 'https://mcp.tmphost.io',
        registration_endpoint: 'https://mcp.tmphost.io/oauth2/register',
        code_challenge_methods_supported: ['S256'],
      }),
      ...override,
    }
    return async (url) => responses[url]
  }

  it('passes a server that asks for OAuth sign-in', async () => {
    assert.deepEqual(await checkOAuth(URL_, server()), [])
  })

  it('needs a 401 with resource metadata', async () => {
    assert.deepEqual(codes(await checkOAuth(URL_, server({ [URL_]: { status: 200 } }))), ['oauth_no_challenge'])
    assert.deepEqual(codes(await checkOAuth(URL_, server({ [URL_]: { status: 401, headers: new Headers() } }))), ['oauth_no_metadata'])
  })

  it('checks the protected resource and authorization server', async () => {
    assert.deepEqual(codes(await checkOAuth(URL_, server({ [PRM]: json({ resource: 'x' }) }))), ['oauth_resource', 'oauth_no_issuer'])
    const as = 'https://mcp.tmphost.io/.well-known/oauth-authorization-server'
    assert.deepEqual(codes(await checkOAuth(URL_, server({ [as]: json({}) }))), ['oauth_issuer', 'oauth_no_registration', 'oauth_no_pkce'])
  })
})
