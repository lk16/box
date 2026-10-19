# Task: let a user leave out named MCP servers

This file is the whole brief. Delete it in the last commit (see "Commits" below).

## Why

A project's committed `.box/config.json` lists the MCP servers its sandboxes use under `mcp`, and
box passes every name to `sbx create --static-mcp`. Some servers need access not every user of the
project has, such as credentials to a cluster. A user without that access cannot register the
server, so their `sbx create` fails on the unknown name, and today the only way past that is
editing the committed config.

## What to build

A user can name MCP servers to leave out of this run, in two ways that add up:

- `--without-mcp NAME`, a repeatable flag, like `--mount`.
- `BOX_WITHOUT_MCP`, a comma-separated list of names in the environment, meant for a user's
  `.envrc` or shell profile.

The servers passed to `--static-mcp` are the configured `mcp` list minus the union of both, in the
configured order. Nothing else changes:

- With neither set, behaviour is exactly as today: every configured name is passed on, and an
  unknown name is a failed create. Keep that; it is what catches typos and missing setup.
- A name to leave out that is not in `mcp` is not an error. The environment variable is shared
  across projects, so it will often name servers another project uses. A typo in it is still
  caught, since the server it meant to leave out stays in and fails the create.
- Empty entries in `BOX_WITHOUT_MCP` (`a,,b`, a trailing comma, surrounding spaces) are tolerated:
  trim each entry and skip the empty ones. An empty `--without-mcp ""` is an error, like other
  empty flag values.
- It applies the same in a group: the group's `mcp` list is what gets filtered.
- When a run actually leaves a configured server out, say so in one line before creating the
  sandbox, naming the servers, so a user is not surprised the tools are missing.
- `box config` shows the effective `mcp` list, and which configured servers were left out and why
  (flag or `BOX_WITHOUT_MCP`). Follow how it already shows `BOX_SECRETS_FILE`.

Box itself never runs `sbx mcp ls` or checks registration; it only removes names the user asked
it to remove.

## Where things are

Read `CLAUDE.md`, then `docs/style.md` and `docs/goal.md` before changing code.

- `internal/cli/flags.go`: flag parsing; `--mount` is the model for a repeatable flag with no
  config key.
- `internal/cli/cli.go`: reads the environment variables box already uses, and `box config`.
- `internal/config/names.go`: the names of settings and environment variables
  (`SecretsEnv`, `TokenFileEnv`, ...); add `BOX_WITHOUT_MCP` there.
- `internal/config/config.go`: `ToMCPServers` and the `Config` the run uses.
- `internal/config/format.go`: what `box config` prints.
- `internal/session/commands.go`: where `--static-mcp` is built.
- Help text in `internal/cli` lists every flag; add `--without-mcp` there.

Keep the filtering a pure function with its own tests, separate from reading the environment.

## Docs

- `docs/mcp.md`: a short section on leaving servers out, with both forms and an `.envrc` example.
- `docs/settings.md`: the flag in the table, and the environment variable wherever the others are
  documented.
- `docs/goal.md`: the `mcp` bullet says box passes the names on "and nothing more". Amend it: the
  user may remove names for a run, box still never checks registration, and an unknown name that
  was not removed is still a failed create.
- `README.md` if it lists flags or environment variables.

Keep examples generic (`postgres`, `kubernetes`), as the existing docs do.

## Commits

- Run `./check.sh` before starting, and again before every commit. Commit only when it passes.
  `pre-commit` cannot run in this sandbox; `docs/sandbox.md` says why and what replaces it.
- Small commits, each one passing on its own: for example, the filtering with tests, then the
  flag and environment variable, then `box config`, then the docs.
- The last commit deletes this `prompt.md`, and nothing in the final tree may refer to it.
