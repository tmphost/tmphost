# tmphost plugin for Claude Code

It connects Claude Code to tmphost's MCP server, so Claude can host files at
temporary public URLs: upload, download, list and link files in folders that
expire after a TTL. It also adds a `tmphost` skill on when and how to share
files with it.

```sh
/plugin install tmphost --marketplace tmphost/tmphost
```

It connects to `https://mcp.tmphost.io/mcp` and signs in through the browser
on the first tool call. To use a self-hosted tmphost, add your own MCP server
pointing at its MCP domain or app URL instead:
`claude mcp add --transport http tmphost https://mcp.example.com/mcp`.

[Privacy Policy](https://tmphost.io/privacy) ·
[Terms of Service](https://tmphost.io/tos)

## License

This plugin is licensed under the [Apache License 2.0](LICENSE). The tmphost
server it connects to is AGPL-3.0-only.
