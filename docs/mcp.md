# Read-only tools

An agent often needs to look something up that lives outside the repository: a database, a
Kubernetes cluster, an error tracker. `sbx mcp add` registers an MCP server on your machine, and
`mcp` names the ones this project's sandboxes may use:

```json
{
  "mcp": ["postgres", "kubernetes"]
}
```

The server runs on the host, under your own access, so registering one is yours to do and box only
passes the names on to `sbx create --static-mcp`. `sbx mcp ls` shows what this machine has. A name
`sbx` does not know is a failed create that says so. A name holding a comma is refused, since `sbx`
takes every name in one comma-separated argument and would split it in two.
