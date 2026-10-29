# tmphost plugin for Cursor

It connects Cursor to tmphost's MCP server, so the agent can host files at
temporary public URLs: upload, download, list and link files in folders that
expire after a TTL.

| Component | What it adds |
| --- | --- |
| `mcp.json` | The `tmphost` MCP server, `https://mcp.tmphost.io/mcp`, signed in with OAuth |
| `skills/tmphost/SKILL.md` | When and how to use the `uploadFile`, `downloadFile`, `listFiles`, `getUrl`, `extendFolder` and `addPinToFolder` tools |
| `rules/prefer-tmphost-mcp.mdc` | Routes "share this" requests to the right tool and keeps secrets out of public links |
| `commands/share-file.md` | `/share-file`: upload one file and reply with its link |
| `commands/share-site.md` | `/share-site`: upload a static HTML site and reply with its URL |

## Install and connect

1. Install **tmphost** from the Cursor Marketplace, or import this repository
   ([`.cursor-plugin/marketplace.json`](../../.cursor-plugin/marketplace.json)
   lists the plugin) as a team marketplace.
2. Open **Cursor Settings → Tools & MCP** and enable the `tmphost` server.
3. Select **Connect**, sign in with your tmphost account and approve access.

The server answers the first request with a 401; Cursor discovers its OAuth
endpoints and registers itself (Dynamic Client Registration), so there is no
API key to paste. To use a self-hosted tmphost, add your own MCP server
pointing at its MCP domain or app URL (`https://mcp.example.com/mcp`) instead.

## Try it

```text
Host this HTML report with tmphost and give me a link that works for an hour.
```

```text
/share-site dist
```

Anyone with a link can open the file until its folder expires. Uploads and
extensions spend credits from your tmphost account.

## Local development

The plugin follows Cursor's [plugin template](https://github.com/cursor/plugin-template):
a plugin directory with `.cursor-plugin/plugin.json`, listed by the
repository's `.cursor-plugin/marketplace.json`. CI
(`.github/workflows/plugin-cursor.yml`) runs the template's validator, checks
the manifest, the MCP config, every rule, skill and command, and the
repository marketplace, then probes the live server's OAuth discovery:

```sh
node .github/scripts/validate-cursor-plugin.mjs
node .github/scripts/validate-cursor-plugin.mjs --live
```

To load it from a checkout, copy it into Cursor's local plugin folder (Cursor
skips symlinks that point outside it), then run **Developer: Reload Window**:

```sh
mkdir -p ~/.cursor/plugins/local
rsync -a --delete plugin/cursor/ ~/.cursor/plugins/local/tmphost/
```

Bump `version` in `.cursor-plugin/plugin.json` whenever the plugin changes.

[Privacy Policy](https://tmphost.io/privacy) ·
[Terms of Service](https://tmphost.io/tos)

## License

This plugin is licensed under the [Apache License 2.0](LICENSE). The tmphost
server it connects to is AGPL-3.0-only.
