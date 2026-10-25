# tmphost extension for Gemini CLI

It connects [Gemini CLI](https://github.com/google-gemini/gemini-cli) to
tmphost's MCP server, so Gemini can host files at temporary public URLs:
upload, download, list and link files in folders that expire after a TTL.

```bash
gemini extensions install https://github.com/tmphost/tmphost
```

The server is `https://mcp.tmphost.io/mcp`. It answers an unauthenticated
request with a 401, Gemini CLI discovers its OAuth endpoints from that, and you
sign in with your tmphost account; `/mcp auth tmphost` starts the sign-in by
hand. To use a self-hosted tmphost, define a server named `tmphost` in
`~/.gemini/settings.json` pointing at its MCP domain or app URL; a server
defined there takes precedence over the extension's:

```json
{
  "mcpServers": {
    "tmphost": { "type": "http", "url": "https://mcp.example.com/mcp" }
  }
}
```

## Where the files are

Gemini CLI installs an extension from a git URL and reads its manifest from
the repository root. So the manifest is
[`gemini-extension.json`](../../gemini-extension.json) at the root of this
repository, and its `contextFileName` points back here, at
[`GEMINI.md`](GEMINI.md), the instructions loaded into every session.

Gemini CLI also loads `skills/`, `agents/`, `commands/`, `hooks/` and
`policies/` from an extension's root, which is this repository's root. Adding a
folder with one of those names there adds it to the extension, so CI fails if
one appears.

## Permissions

Gemini CLI asks before each tool call. To allow the tmphost tools without
asking, add a policy file such as `~/.gemini/policies/tmphost.toml`:

```toml
[[rule]]
mcpName = "tmphost"
decision = "allow"
priority = 100
```

Uploads and extends spend credits, so allow them only if you are happy for
Gemini to spend them unprompted.

## Tools

| Tool | What it does |
| --- | --- |
| `uploadFile` | Uploads a file into a new folder that expires after a TTL, or with `folderId` into an existing one; text as `textContent`, anything else as `base64Content` |
| `downloadFile` | Reads a file back from a folder |
| `listFiles` | Lists a folder's files and their URLs |
| `getUrl` | Builds the public URL of a folder, directory or file; with `pin`, a URL that carries the folder's PIN |
| `extendFolder` | Adds time to a folder |
| `addPinToFolder` | Gives a folder a PIN visitors must enter |

Each file costs 1 credit per started MB of its size per started 30 minutes of
its folder's remaining lifetime; extending costs the same for the added time.

This table is checked against the server in CI (see
`backend/internal/handler/mcp/plugin_docs_test.go`): a tool added to the
server without being documented here fails the build.
