# tmphost plugin for Claude Code

It connects Claude Code to tmphost's MCP server, so Claude can host files at
temporary public URLs: upload, download, list and link files in folders that
expire after a TTL. It also adds a `tmphost` skill on when and how to share
files with it.

```sh
/plugin install tmphost --marketplace tmphost/tmphost
```

It connects to `<server>/mcp`, by default `https://mcp.tmphost.io/mcp`, and
signs in through the browser on the first tool call. For a self-hosted
server, set the plugin's `server` option to its MCP domain or app URL with
`/plugin configure tmphost@tmphost`.

[Privacy Policy](https://tmphost.io/privacy) ·
[Terms of Service](https://tmphost.io/tos)
