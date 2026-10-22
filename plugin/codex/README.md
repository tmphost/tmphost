# tmphost plugin for Codex and ChatGPT

It connects Codex and ChatGPT to tmphost's MCP server, so they can host files
at temporary public URLs: upload, download, list and link files in folders
that expire after a TTL. It also adds a `tmphost` skill on when and how to
share files with it.

It is a portable [Agent Plugins](https://agent-plugins.org) package, laid out
as [OpenAI's plugin guide](https://developers.openai.com/plugins/build/plugins)
describes:

| File | What it holds |
| --- | --- |
| `plugin.json` | The manifest; the listing, review cases and release notes live under `extensions["com.openai"]` |
| `mcp.json` | The `tmphost` MCP server, `https://mcp.tmphost.io/mcp` |
| `skills/tmphost/SKILL.md` | When and how to use the `file` tool's `upload`, `download`, `get`, `url`, `extend` and `pin` actions |
| `assets/icon.png` | The composer icon and listing logo |

## Install in Codex

The repository's [`.agents/plugins/marketplace.json`](../../.agents/plugins/marketplace.json)
lists this plugin:

```sh
codex plugin marketplace add tmphost/tmphost
codex plugin add tmphost@tmphost
```

The first tool call asks you to sign in with your tmphost account: the server
answers with a 401 and Codex discovers its OAuth endpoints and registers
itself (Dynamic Client Registration). To use a self-hosted tmphost, add your
own MCP server pointing at its MCP domain or app URL instead.

## Submit to the plugin directory

CI (`.github/workflows/plugin-codex.yml`) checks the package against OpenAI's
submission rules and uploads `tmphost-codex-plugin.zip`, the ZIP to upload in
the [Plugins dashboard](https://platform.openai.com/plugins). Bump `version`
and `publication.release_notes` in `plugin.json` for every new upload. The
dashboard also needs a demo recording URL (`review.demo_recording_url`) and
reviewer credentials, which never go in the package.

[Privacy Policy](https://tmphost.io/privacy) ·
[Terms of Service](https://tmphost.io/tos)
