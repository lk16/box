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

## Leaving a server out

A server often needs access not everyone on the project has, such as credentials to a cluster.
`sbx create` fails on a name this machine has not registered, so name the servers you cannot
register and box leaves them out of the run:

```sh
box run --without-mcp kubernetes --without-mcp postgres
```

For every run on this machine, name them in the environment instead, comma-separated, which is
what a shell profile or an `.envrc` is for:

```sh
export BOX_WITHOUT_MCP=kubernetes,postgres
```

The two add up: what box passes to `--static-mcp` is the configured list minus both, in the order
the config lists them. A run that really leaves a configured server out says so before it creates
the sandbox, and `box config` shows the list that is passed on next to what was left out and which
of the two asked for it. A [group](groups.md) filters its own `mcp` list, which is the only one a
session has.

Nothing else changes. A name that is not in `mcp` is no error, since one variable serves every
project on this machine and will often name another one's servers. A name you meant to leave out
but misspelled stays in and fails the create, which is what catches the typo, and with neither set
every configured name is passed on exactly as before.
