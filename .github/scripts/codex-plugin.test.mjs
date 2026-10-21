// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import {
  contrast, frontMatter, rasterSize, report, svgSize, validateMarketplace, validatePlugin,
} from './codex-plugin.mjs'

/** A PNG header claiming `w`x`h` pixels: all the validator reads. */
function png(w, h) {
  const buf = Buffer.alloc(33)
  Buffer.from('\x89PNG\r\n\x1a\n', 'latin1').copy(buf)
  buf.writeUInt32BE(w, 16)
  buf.writeUInt32BE(h, 20)
  return buf
}

const positive = (i) => ({ description: `case ${i}`, prompt: `prompt ${i}`, tools_triggered: 'file', expected_behavior: `result ${i}` })
const negative = (i) => ({ description: `no ${i}`, prompt: `refuse ${i}` })

/** A package that passes every check; tests break one thing at a time. */
function manifest() {
  return {
    $schema: 'https://agent-plugins.org/schemas/1.0.0/plugin.schema.json',
    name: 'tmphost',
    version: '1.0.0',
    description: 'Host files.',
    author: { name: 'tmphost', email: 'dev@tmphost.io', url: 'https://tmphost.io' },
    homepage: 'https://tmphost.io',
    repository: 'https://github.com/tmphost/tmphost',
    license: 'AGPL-3.0-only',
    keywords: ['share'],
    extensions: {
      'com.openai': {
        interface: {
          displayName: 'tmphost',
          shortDescription: 'Share files',
          longDescription: 'Line one.\nLine two.',
          developerName: 'tmphost',
          category: 'Productivity',
          capabilities: ['Upload files'],
          websiteURL: 'https://tmphost.io',
          supportURL: 'https://tmphost.io/support',
          privacyPolicyURL: 'https://tmphost.io/privacy',
          termsOfServiceURL: 'https://tmphost.io/tos',
          defaultPrompt: ['Host this file.', 'Share this site.'],
          brandColor: '#000000',
          brandColorDark: '#FFFFFF',
          composerIcon: './assets/icon.png',
          logo: './assets/icon.png',
        },
        review: {
          test_cases: { positive: [1, 2, 3, 4, 5].map(positive), negative: [1, 2, 3].map(negative) },
          demo_recording_url: 'https://tmphost.io/demo',
          commerce: false,
          commerce_description: 'No commerce.',
        },
        publication: { release_notes: 'First.' },
      },
    },
  }
}

const SKILL = '---\nname: tmphost\ndescription: "Share files."\n---\n\n# tmphost\n'
const MCP = {
  $schema: 'https://agent-plugins.org/schemas/1.0.0/mcp.schema.json',
  mcpServers: { tmphost: { type: 'streamable-http', url: 'https://mcp.tmphost.io/mcp' } },
}

/** Builds a package, letting `edit` change the manifest or mcp.json first. */
function pkg({ edit = () => {}, mcp = (m) => m, files = {} } = {}) {
  const m = manifest()
  edit(m)
  return {
    files: {
      'plugin.json': Buffer.from(JSON.stringify(m)),
      'mcp.json': Buffer.from(JSON.stringify(mcp(structuredClone(MCP)))),
      'skills/tmphost/SKILL.md': Buffer.from(SKILL),
      'assets/icon.png': png(512, 512),
      ...files,
    },
  }
}

const ui = (m) => m.extensions['com.openai'].interface
const openai = (m) => m.extensions['com.openai']
const codes = (result) => result.errors.map((e) => e.code)

/** Asserts the package fails with exactly the given codes. */
function fails(p, ...expected) {
  assert.deepEqual(codes(validatePlugin(p)).sort(), expected.sort())
}

describe('validatePlugin', () => {
  it('passes a complete package and returns its manifest and server', () => {
    const result = validatePlugin(pkg())
    assert.deepEqual(result.errors, [])
    assert.deepEqual(result.warnings, [])
    assert.equal(result.manifest.name, 'tmphost')
    assert.equal(result.url, 'https://mcp.tmphost.io/mcp')
  })

  it('accepts optional fields left out and a single string prompt', () => {
    const result = validatePlugin(pkg({
      edit: (m) => {
        for (const k of ['homepage', 'repository', 'license', 'keywords']) delete m[k]
        delete m.author.email
        delete m.author.url
        ui(m).defaultPrompt = 'Host this file.'
        for (const k of ['brandColor', 'brandColorDark']) delete ui(m)[k]
        const r = openai(m).review
        for (const k of ['commerce', 'commerce_description']) delete r[k]
      },
    }))
    assert.deepEqual(result.errors, [])
    const bare = validatePlugin(pkg({ edit: (m) => delete ui(m).defaultPrompt }))
    assert.deepEqual(bare.errors, [])
  })

  it('warns, without failing, when the demo recording is missing', () => {
    const result = validatePlugin(pkg({ edit: (m) => delete openai(m).review.demo_recording_url }))
    assert.deepEqual(result.errors, [])
    assert.deepEqual(result.warnings.map((w) => w.code), ['demo_recording_url_missing'])
  })

  describe('plugin.json', () => {
    it('must exist and be a JSON object', () => {
      const missing = pkg()
      delete missing.files['plugin.json']
      fails(missing, 'plugin.json_missing')
      fails(pkg({ files: { 'plugin.json': Buffer.from('{') } }), 'plugin.json_json_malformed')
      fails(pkg({ files: { 'plugin.json': Buffer.from('[]') } }), 'plugin.json_root_not_object')
    })

    it('rejects unknown fields and a wrong $schema', () => {
      fails(pkg({ edit: (m) => { m.icon = 'x' } }), 'unknown_field')
      fails(pkg({ edit: (m) => { m.$schema = 'x' } }), 'plugin_schema')
    })

    it('checks the name', () => {
      fails(pkg({ edit: (m) => { m.name = 'Tmp--host' } }), 'plugin_name_format')
      fails(pkg({ edit: (m) => { m.name = 7 } }), 'plugin_name_wrong_type')
      fails(pkg({ edit: (m) => { m.name = ' ' } }), 'plugin_name_empty')
      fails(pkg({ edit: (m) => { m.name = 'a'.repeat(65) } }), 'plugin_name_too_long', 'skill_identity_too_long')
      fails(pkg({ edit: (m) => { m.name = 'a'.repeat(60) } }), 'skill_identity_too_long')
    })

    it('checks the version, description and author', () => {
      fails(pkg({ edit: (m) => { m.version = '1.0' } }), 'plugin_version_not_semver')
      fails(pkg({ edit: (m) => { delete m.version } }), 'plugin_version_wrong_type')
      fails(pkg({ edit: (m) => { m.description = 'a\tb' } }), 'plugin_description_character_unsupported')
      fails(pkg({ edit: (m) => { m.description = 'a b' } }), 'plugin_description_character_unsupported')
      fails(pkg({ edit: (m) => { delete m.author } }), 'plugin_developer_missing')
      fails(pkg({ edit: (m) => { m.author.twitter = 'x' } }), 'unknown_field')
      fails(pkg({ edit: (m) => { m.author.name = 'a\nb' } }), 'plugin_author_name_character_unsupported')
      fails(pkg({ edit: (m) => { m.author.email = '' } }), 'plugin_author_email_empty')
      fails(pkg({ edit: (m) => { m.author.url = 'http://tmphost.io' } }), 'plugin_author_url_not_https')
      fails(pkg({ edit: (m) => { m.author.url = 'https://u:p@tmphost.io' } }), 'plugin_author_url_has_credentials')
      fails(pkg({ edit: (m) => { m.author.url = 'not a url' } }), 'plugin_author_url_not_https')
    })

    it('checks the optional package fields', () => {
      fails(pkg({ edit: (m) => { m.homepage = 'ftp://x' } }), 'plugin_homepage_not_https')
      fails(pkg({ edit: (m) => { m.repository = '' } }), 'plugin_repository_empty')
      fails(pkg({ edit: (m) => { m.license = 5 } }), 'plugin_license_wrong_type')
      fails(pkg({ edit: (m) => { m.keywords = ['ok', ''] } }), 'plugin_keywords_wrong_type')
      fails(pkg({ edit: (m) => { m.keywords = 'share' } }), 'plugin_keywords_wrong_type')
    })

    it('rejects credentials anywhere in the package', () => {
      fails(pkg({ edit: (m) => { openai(m).review.test_cases.positive[0].test_credentials = 'x' } }), 'package_secret_field', 'unknown_field')
      fails(pkg({ edit: (m) => { openai(m).reviewer_instructions = 'x' } }), 'package_secret_field', 'unknown_field')
    })
  })

  describe('extensions["com.openai"]', () => {
    it('is required and holds only OpenAI settings', () => {
      fails(pkg({ edit: (m) => { delete m.extensions } }), 'openai_extension_missing')
      fails(pkg({ edit: (m) => { m.extensions['com.example'] = {} } }), 'unknown_field')
    })

    it('rejects apps and hooks, which the directory refuses', () => {
      fails(pkg({ edit: (m) => { openai(m).apps = './.app.json' } }), 'app_configuration_excluded')
      fails(pkg({ edit: (m) => { openai(m).hooks = './hooks/hooks.json' } }), 'hooks_excluded')
    })

    it('checks id and the onboarding skill', () => {
      fails(pkg({ edit: (m) => { openai(m).id = '' } }), 'plugin_id_empty')
      assert.deepEqual(validatePlugin(pkg({ edit: (m) => { openai(m).id = 'plugin_1' } })).errors, [])
      assert.deepEqual(validatePlugin(pkg({ edit: (m) => { openai(m).onboardingSkill = './skills/tmphost/SKILL.md' } })).errors, [])
      fails(pkg({ edit: (m) => { openai(m).onboardingSkill = './skills/setup/SKILL.md' } }), 'onboarding_skill_missing')
      fails(pkg({ edit: (m) => { openai(m).onboardingSkill = 'skills/tmphost/SKILL.md' } }), 'onboarding_skill_missing')
      fails(pkg({ edit: (m) => { openai(m).onboardingSkill = '' } }), 'onboarding_skill_empty')
    })
  })

  describe('interface', () => {
    it('is required', () => {
      fails(pkg({ edit: (m) => { delete openai(m).interface } }), 'plugin_interface_wrong_type')
      fails(pkg({ edit: (m) => { ui(m).tagline = 'x' } }), 'unknown_field')
    })

    it('holds the listing text within the directory limits', () => {
      fails(pkg({ edit: (m) => { ui(m).displayName = 'a'.repeat(31) } }), 'submission_display_name_too_long')
      fails(pkg({ edit: (m) => { ui(m).shortDescription = 'a\nb' } }), 'submission_subtitle_character_unsupported')
      fails(pkg({ edit: (m) => { ui(m).longDescription = 'a\r\nb' } }), 'submission_description_character_unsupported')
      fails(pkg({ edit: (m) => { ui(m).developerName = '' } }), 'submission_developer_name_empty')
      fails(pkg({ edit: (m) => { ui(m).category = 'Tools' } }), 'plugin_category_unknown')
    })

    it('checks capabilities', () => {
      fails(pkg({ edit: (m) => { delete ui(m).capabilities } }), 'plugin_capabilities_wrong_type')
      fails(pkg({ edit: (m) => { ui(m).capabilities = Array(21).fill('x') } }), 'plugin_capabilities_too_many')
      fails(pkg({ edit: (m) => { ui(m).capabilities = ['a'.repeat(121)] } }), 'plugin_capability[0]_too_long')
    })

    it('needs all four listing URLs over HTTPS', () => {
      fails(pkg({ edit: (m) => { delete ui(m).supportURL } }), 'interface.supportURL_wrong_type')
      fails(pkg({ edit: (m) => { ui(m).websiteURL = 'http://tmphost.io' } }), 'interface.websiteURL_not_https')
      fails(pkg({ edit: (m) => { ui(m).termsOfServiceURL = `https://tmphost.io/${'a'.repeat(1024)}` } }), 'interface.termsOfServiceURL_too_long')
    })

    it('checks the starter prompts', () => {
      fails(pkg({ edit: (m) => { ui(m).defaultPrompt = 5 } }), 'plugin_default_prompt_wrong_type')
      fails(pkg({ edit: (m) => { ui(m).defaultPrompt = ['a', 'b', 'c', 'd'] } }), 'plugin_default_prompt_too_many')
      fails(pkg({ edit: (m) => { ui(m).defaultPrompt = ['Ask @tmphost to host it'] } }), 'plugin_default_prompt_mention')
      fails(pkg({ edit: (m) => { ui(m).defaultPrompt = ['Host  it', 'host it'] } }), 'plugin_default_prompt_duplicate')
      fails(pkg({ edit: (m) => { ui(m).defaultPrompt = ['a'.repeat(129)] } }), 'plugin_default_prompt[0]_too_long')
    })

    it('checks the brand colours and their contrast', () => {
      fails(pkg({ edit: (m) => { ui(m).brandColor = 'black' } }), 'plugin_brandColor_format')
      fails(pkg({ edit: (m) => { ui(m).brandColor = '#FFFF00' } }), 'plugin_brandColor_contrast')
      fails(pkg({ edit: (m) => { ui(m).brandColorDark = '#222222' } }), 'plugin_brandColorDark_contrast')
    })

    it('needs a logo and composer icon', () => {
      fails(pkg({ edit: (m) => { delete ui(m).logo } }), 'plugin_logo_path_missing')
      fails(pkg({ edit: (m) => { delete ui(m).composerIcon } }), 'plugin_composer_icon_path_missing')
    })

    it('rejects screenshots without a UI template', () => {
      fails(pkg({ edit: (m) => { ui(m).screenshots = ['./assets/icon.png'] } }), 'screenshots_not_allowed')
    })
  })

  describe('images', () => {
    const logo = (path, files = {}) => pkg({ edit: (m) => { ui(m).logo = path }, files })

    it('must be ./ paths to files inside the plugin', () => {
      fails(logo('assets/icon.png'), 'branding_asset_path_missing_root_prefix')
      fails(logo('./../icon.png'), 'declared_asset_path_unsafe')
      fails(logo('.//etc/icon.png'), 'declared_asset_path_unsafe')
      fails(logo('./assets/logo.png'), 'declared_asset_file_missing')
      fails(logo(''), 'interface.logo_empty')
    })

    it('must be a supported format that decodes', () => {
      fails(logo('./assets/logo.gif', { 'assets/logo.gif': png(64, 64) }), 'image_file_format_unsupported')
      fails(logo('./assets/logo.png', { 'assets/logo.png': Buffer.from('not a png') }), 'raster_image_decode_failed')
      fails(logo('./assets/logo.png', { 'assets/logo.png': Buffer.concat([png(64, 64), Buffer.alloc(5 * 1024 * 1024)]) }), 'image_file_too_large')
    })

    it('must be square and between 48 and 4096 px', () => {
      fails(logo('./assets/logo.png', { 'assets/logo.png': png(64, 48) }), 'image_not_square')
      fails(logo('./assets/logo.png', { 'assets/logo.png': png(32, 32) }), 'image_dimensions_too_small')
      fails(logo('./assets/logo.png', { 'assets/logo.png': png(5000, 5000) }), 'raster_image_dimensions_too_large')
    })

    it('reads JPEG and SVG logos', () => {
      const jpeg = Buffer.from([0xff, 0xd8, 0xff, 0xc0, 0, 17, 8, 0, 64, 0, 64, 3, 0, 0, 0, 0])
      assert.deepEqual(validatePlugin(logo('./assets/logo.jpg', { 'assets/logo.jpg': jpeg })).errors, [])
      const svg = Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"></svg>')
      assert.deepEqual(validatePlugin(logo('./assets/logo.svg', { 'assets/logo.svg': svg })).errors, [])
      fails(logo('./assets/logo.svg', { 'assets/logo.svg': Buffer.from('<svg width="64px" height="64px"/>') }), 'svg_dimensions_not_numeric')
    })
  })

  describe('review', () => {
    const r = (m) => openai(m).review

    it('is required with five positive and three negative cases', () => {
      fails(pkg({ edit: (m) => { delete openai(m).review } }), 'review_missing')
      fails(pkg({ edit: (m) => { r(m).reviewer = 'x' } }), 'unknown_field')
      fails(pkg({ edit: (m) => { delete r(m).test_cases } }), 'test_cases_missing')
      fails(pkg({ edit: (m) => { r(m).test_cases.extra = [] } }), 'unknown_field')
      fails(pkg({ edit: (m) => { r(m).test_cases.positive.pop() } }), 'test_cases_positive_count')
      fails(pkg({ edit: (m) => { r(m).test_cases.negative = 'x' } }), 'test_cases_negative_count')
    })

    it('checks each case', () => {
      fails(pkg({ edit: (m) => { r(m).test_cases.positive[0] = 'x' } }), 'test_case_wrong_type')
      fails(pkg({ edit: (m) => { delete r(m).test_cases.positive[1].tools_triggered } }), 'review.test_cases.positive[1].tools_triggered_wrong_type')
      fails(pkg({ edit: (m) => { r(m).test_cases.negative[0].prompt = '' } }), 'review.test_cases.negative[0].prompt_empty')
      fails(pkg({ edit: (m) => { r(m).test_cases.negative[0].why = 'x' } }), 'unknown_field')
      fails(pkg({ edit: (m) => { r(m).test_cases.positive[0].expected_output_url = 'http://x' } }), 'review.test_cases.positive[0].expected_output_url_not_https')
      fails(pkg({ edit: (m) => { r(m).test_cases.positive[0].file_attachment_urls = 'x' } }), 'review.test_cases.positive[0].file_attachment_urls_wrong_type')
      fails(pkg({ edit: (m) => { r(m).test_cases.positive[0].file_attachment_urls = ['http://x'] } }), 'review.test_cases.positive[0].file_attachment_urls[0]_not_https')
      const ok = pkg({
        edit: (m) => Object.assign(r(m).test_cases.positive[0], { expected_output_url: 'https://x.io', file_attachment_urls: ['https://x.io/a'] }),
      })
      assert.deepEqual(validatePlugin(ok).errors, [])
    })

    it('checks the demo URL and commerce declaration', () => {
      fails(pkg({ edit: (m) => { r(m).demo_recording_url = 'http://x' } }), 'review.demo_recording_url_not_https')
      fails(pkg({ edit: (m) => { r(m).commerce = 'no' } }), 'review_commerce_wrong_type')
      fails(pkg({ edit: (m) => { r(m).commerce_description = '' } }), 'review.commerce_description_empty')
    })
  })

  describe('publication', () => {
    const p = (m) => openai(m).publication

    it('needs release notes', () => {
      fails(pkg({ edit: (m) => { delete openai(m).publication } }), 'publication_missing')
      fails(pkg({ edit: (m) => { p(m).release_notes = ' ' } }), 'publication.release_notes_empty')
      fails(pkg({ edit: (m) => { p(m).notes = 'x' } }), 'unknown_field')
    })

    it('checks countries', () => {
      assert.deepEqual(validatePlugin(pkg({ edit: (m) => { p(m).countries = ['US', 'GB'] } })).errors, [])
      fails(pkg({ edit: (m) => { p(m).countries = ['us'] } }), 'publication_countries_format')
      fails(pkg({ edit: (m) => { p(m).countries = 'US' } }), 'publication_countries_format')
    })

    it('checks translations', () => {
      const ok = pkg({ edit: (m) => { p(m).translations = { 'fr-FR': { subtitle: 'Partager', description: 'Un.\nDeux.' }, 'de-DE': { subtitle: null, description: null }, 'ja-JP': {} } } })
      assert.deepEqual(validatePlugin(ok).errors, [])
      assert.deepEqual(validatePlugin(pkg({ edit: (m) => { p(m).translations = null } })).errors, [])
      fails(pkg({ edit: (m) => { p(m).translations = [] } }), 'publication_translations_wrong_type')
      fails(pkg({ edit: (m) => { p(m).translations = { ' ': {} } } }), 'publication_translation_wrong_type')
      fails(pkg({ edit: (m) => { p(m).translations = { 'fr-FR': { subtitle: 'a'.repeat(31) } } } }), 'publication.translations.fr-FR.subtitle_too_long')
      fails(pkg({ edit: (m) => { p(m).translations = { 'fr-FR': { title: 'x' } } } }), 'unknown_field')
    })
  })

  describe('mcp.json', () => {
    it('must exist and be a JSON object', () => {
      const missing = pkg()
      delete missing.files['mcp.json']
      fails(missing, 'mcp.json_missing')
    })

    it('declares exactly one public streamable-http server', () => {
      fails(pkg({ mcp: (m) => ({ ...m, extra: 1 }) }), 'unknown_field')
      fails(pkg({ mcp: (m) => ({ ...m, $schema: 'x' }) }), 'mcp_schema')
      fails(pkg({ mcp: (m) => ({ ...m, mcpServers: [] }) }), 'mcp_servers_wrong_type')
      fails(pkg({ mcp: (m) => ({ ...m, mcpServers: {} }) }), 'mcp_server_count')
      fails(pkg({ mcp: (m) => ({ ...m, mcpServers: { a: MCP.mcpServers.tmphost, b: MCP.mcpServers.tmphost } }) }), 'mcp_server_count')
      fails(pkg({ mcp: (m) => ({ ...m, mcpServers: { ' ': MCP.mcpServers.tmphost } }) }), 'mcp_server_wrong_type')
      fails(pkg({ mcp: (m) => ({ ...m, mcpServers: { tmphost: 'x' } }) }), 'mcp_server_wrong_type')
    })

    it('checks the server', () => {
      const server = (s) => pkg({ mcp: (m) => { Object.assign(m.mcpServers.tmphost, s); return m } })
      fails(server({ type: 'http' }), 'mcp_server_type')
      fails(server({ url: 'http://mcp.tmphost.io/mcp' }), 'mcpServers.tmphost.url_not_https')
      fails(server({ url: 'https://localhost:2027/mcp' }), 'mcp_server_not_public')
      fails(server({ headers: { Authorization: 'Bearer x' } }), 'mcp_server_headers')
      fails(server({ extensions: { 'com.openai': { auth: { clientSecret: 'x' } } } }), 'unknown_field', 'package_secret_field')
      assert.deepEqual(validatePlugin(server({ headers: {} })).errors, [])
    })
  })

  describe('skills', () => {
    const skill = (source, dir = 'tmphost') => {
      const p = pkg({ files: { [`skills/${dir}/SKILL.md`]: Buffer.from(source) } })
      if (dir !== 'tmphost') delete p.files['skills/tmphost/SKILL.md']
      return p
    }

    it('needs at least one skill directory with a SKILL.md', () => {
      const none = pkg()
      delete none.files['skills/tmphost/SKILL.md']
      fails(none, 'plugin_runtime_surface_missing')
      fails(pkg({ files: { 'skills/README.md': Buffer.from('x') } }), 'skill_file_ignored')
      fails(pkg({ files: { 'skills/other/notes.md': Buffer.from('x') } }), 'skill_manifest_missing')
      fails(pkg({ files: { 'skills/tmphost/agents/openai.yaml': Buffer.from('x') } }), 'skill_agent_unsupported')
    })

    it('checks the front matter', () => {
      fails(skill('# no front matter'), 'skill_frontmatter_missing')
      fails(skill('---\nname: tmphost\n'), 'skill_frontmatter_unclosed')
      fails(skill('---\nname: tmphost\ndescription: >\n  folded\n---\nbody'), 'skill_frontmatter_yaml_malformed')
      fails(skill('---\nname: tmphost\ndescription: x\nmetadata: x\n---\nbody'), 'unknown_field')
      fails(skill('---\nname: tmphost\n---\nbody'), 'skill_description_wrong_type')
      fails(skill(`---\nname: tmphost\ndescription: ${'a'.repeat(1025)}\n---\nbody`), 'skill_description_too_long')
      fails(skill('---\nname: tmphost\ndescription: x\n---\n \n'), 'skill_body_empty')
    })

    it('rejects a SKILL.md that is not UTF-8', () => {
      const p = pkg({ files: { 'skills/tmphost/SKILL.md': Buffer.concat([Buffer.from(SKILL), Buffer.from([0xff])]) } })
      fails(p, 'skill_manifest_invalid_utf8')
    })

    it('checks the skill name', () => {
      fails(skill('---\nname: other\ndescription: x\n---\nbody'), 'skill_name_format')
      fails(skill('---\nname: .hidden\ndescription: x\n---\nbody', '.hidden'), 'skill_directory_hidden', 'skill_name_format')
      fails(skill('---\nname: ""\ndescription: x\n---\nbody'), 'skill_name_empty')
      const twice = pkg({ files: { 'skills/copy/SKILL.md': Buffer.from(SKILL) } })
      fails(twice, 'skill_name_format', 'skill_identity_duplicate')
    })
  })

  describe('archive', () => {
    it('applies the ZIP limits', () => {
      fails(pkg({ files: { 'a\\b.txt': Buffer.from('x') } }), 'archive_member_path_has_backslash')
      fails(pkg({ files: { 'a//b.txt': Buffer.from('x') } }), 'archive_member_path_has_empty_segment')
      fails(pkg({ files: { 'a /b.txt': Buffer.from('x') } }), 'archive_member_path_has_empty_segment')
      fails(pkg({ files: { 'a/../b.txt': Buffer.from('x') } }), 'archive_member_path_has_parent_segment')
      fails(pkg({ files: { [`${'d/'.repeat(20)}f`]: Buffer.from('x') } }), 'archive_member_path_too_deep')
      fails(pkg({ files: { 'README.md': Buffer.from('x'), 'readme.md': Buffer.from('x') } }), 'archive_member_path_normalization_collision')
      fails({ ...pkg(), links: ['assets/link.png'] }, 'archive_member_type_unsupported')
    })

    it('applies the size and count limits', () => {
      const big = Buffer.alloc(101 * 1024 * 1024)
      fails(pkg({ files: { 'big.bin': big } }), 'archive_member_too_large')
      const many = Object.fromEntries(Array.from({ length: 5000 }, (_, i) => [`docs/${i}.txt`, Buffer.from('x')]))
      fails(pkg({ files: many }), 'archive_too_many_entries')
      const huge = Object.fromEntries(Array.from({ length: 6 }, (_, i) => [`big${i}.bin`, big]))
      fails(pkg({ files: huge }), 'archive_uncompressed_too_large', ...Array(6).fill('archive_member_too_large'))
    })

    it('rejects files the directory excludes', () => {
      fails(pkg({ files: { '.app.json': Buffer.from('{}') } }), 'app_configuration_excluded')
      fails(pkg({ files: { '.mcp.json': Buffer.from('{}') } }), 'undeclared_mcp_manifest_ignored')
      fails(pkg({ files: { 'hooks/hooks.json': Buffer.from('{}') } }), 'hooks_excluded')
      fails(pkg({ files: { '.codex-plugin/plugin.json': Buffer.from('{}') } }), 'overlay_manifest_ignored')
    })
  })
})

describe('validateMarketplace', () => {
  const plugin = { name: 'tmphost', path: 'plugin/codex', isPlugin: (p) => p === './plugin/codex' }
  const market = () => ({
    name: 'tmphost',
    interface: { displayName: 'tmphost' },
    plugins: [{ name: 'tmphost', source: { source: 'local', path: './plugin/codex' }, policy: { installation: 'AVAILABLE', authentication: 'ON_USE' }, category: 'Productivity' }],
  })
  const errs = (edit) => {
    const m = market()
    edit(m)
    return validateMarketplace(m, plugin).map((e) => e.code)
  }

  it('passes a local entry for the plugin, also as a plain path', () => {
    assert.deepEqual(errs(() => {}), [])
    assert.deepEqual(errs((m) => { m.plugins[0].source = './plugin/codex' }), [])
  })

  it('checks the marketplace itself', () => {
    assert.deepEqual(validateMarketplace([], plugin).map((e) => e.code), ['marketplace_wrong_type'])
    assert.deepEqual(errs((m) => { m.owner = 'x' }), ['unknown_field'])
    assert.deepEqual(errs((m) => { m.name = '' }), ['marketplace_name_empty'])
    assert.deepEqual(errs((m) => { delete m.interface }), ['marketplace_display_name'])
    assert.deepEqual(errs((m) => { m.plugins = {} }), ['marketplace_plugins_wrong_type'])
    assert.deepEqual(errs((m) => { m.plugins[0].name = 'other' }), ['marketplace_plugin_missing'])
  })

  it('checks the entry', () => {
    assert.deepEqual(errs((m) => { m.plugins[0].version = '1' }), ['unknown_field'])
    assert.deepEqual(errs((m) => { m.plugins[0].source = './plugin/claude' }), ['marketplace_source'])
    assert.deepEqual(errs((m) => { m.plugins[0].source = { source: 'url', url: 'https://x' } }), ['marketplace_source'])
    assert.deepEqual(errs((m) => { m.plugins[0].policy.installation = 'YES' }), ['marketplace_policy_installation'])
    assert.deepEqual(errs((m) => { delete m.plugins[0].policy }), ['marketplace_policy_installation', 'marketplace_policy_authentication'])
    assert.deepEqual(errs((m) => { m.plugins[0].category = 'Files' }), ['marketplace_category'])
  })
})

describe('helpers', () => {
  it('contrast follows WCAG', () => {
    assert.equal(contrast('#000000', '#FFFFFF'), 21)
    assert.equal(contrast('#FFFFFF', '#000000'), 21)
  })

  it('rasterSize reads PNG, JPEG and WebP headers', () => {
    assert.deepEqual(rasterSize(png(10, 20), 'png'), { width: 10, height: 20 })
    assert.equal(rasterSize(Buffer.alloc(30), 'png'), null)

    const jpeg = Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 4, 0, 0, 0xff, 0xc2, 0, 17, 8, 0, 20, 0, 10, 3, 0, 0])
    assert.deepEqual(rasterSize(jpeg, 'jpeg'), { width: 10, height: 20 })
    assert.equal(rasterSize(Buffer.from([0xff, 0xd8, 0x00, 0, 0, 0, 0, 0, 0, 0, 0]), 'jpeg'), null)
    assert.equal(rasterSize(Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 2]), 'jpeg'), null)
    assert.equal(rasterSize(Buffer.from([0, 0, 0, 0]), 'jpeg'), null)

    const webp = (chunk, fill) => {
      const buf = Buffer.alloc(32)
      buf.write('RIFF', 0, 'latin1')
      buf.write('WEBP', 8, 'latin1')
      buf.write(chunk, 12, 'latin1')
      fill(buf)
      return buf
    }
    assert.deepEqual(rasterSize(webp('VP8X', (b) => { b.writeUIntLE(9, 24, 3); b.writeUIntLE(19, 27, 3) }), 'webp'), { width: 10, height: 20 })
    assert.deepEqual(rasterSize(webp('VP8L', (b) => b.writeUInt32LE(9 | (19 << 14), 21)), 'webp'), { width: 10, height: 20 })
    assert.deepEqual(rasterSize(webp('VP8 ', (b) => { b.writeUInt16LE(10, 26); b.writeUInt16LE(20, 28) }), 'webp'), { width: 10, height: 20 })
    assert.equal(rasterSize(webp('ALPH', () => {}), 'webp'), null)
    assert.equal(rasterSize(Buffer.alloc(32), 'webp'), null)
  })

  it('svgSize reads the viewBox, else width and height', () => {
    assert.deepEqual(svgSize('<svg viewBox="0 0 64 32">'), { width: 64, height: 32 })
    assert.deepEqual(svgSize('<svg width="48" height="48">'), { width: 48, height: 48 })
    assert.deepEqual(svgSize('<html>'), { error: 'svg_root_element_invalid' })
    assert.deepEqual(svgSize('<svg width="48">'), { error: 'svg_dimensions_missing' })
    assert.deepEqual(svgSize('<svg viewBox="0 0 64">'), { error: 'svg_dimensions_missing' })
    assert.deepEqual(svgSize('<svg viewBox="0 0 100% 100%">'), { error: 'svg_dimensions_not_numeric' })
    assert.deepEqual(svgSize('<svg viewBox="0 0 0 64">'), { error: 'svg_dimensions_not_positive' })
  })

  it('frontMatter parses plain key: value lines', () => {
    assert.deepEqual(frontMatter("---\n# comment\n\nname: 'a b'\n---"), { fields: { name: 'a b' }, body: '' })
    assert.deepEqual(frontMatter('---\nname\n---\n'), { error: 'skill_frontmatter_yaml_malformed' })
  })

  it('report formats findings as annotations', () => {
    const out = report({ errors: [{ code: 'e', message: 'bad' }], warnings: [{ code: 'w', message: 'meh' }] }, 'p.json')
    assert.equal(out, '::error file=p.json::e: bad\n::warning file=p.json::w: meh')
  })
})
