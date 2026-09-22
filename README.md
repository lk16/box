# box

box runs Claude Code inside a disposable Docker sandbox ([sbx](https://docs.docker.com/ai/sandboxes/)).

- the agent works on a clone inside the container, so your own working tree is untouched
- CPU, memory and disk limits on the agent, and a network allowlist you choose
- several sandboxes for the same project side by side
- on exit, committed work comes back on a named branch and the sandbox is removed
- a sandbox holding uncommitted changes is kept instead of removed

## Install

With Go 1.24 or newer:

```sh
go install github.com/lk16/box/cmd/box@latest
```

That puts `box` in `$(go env GOBIN)`, or `$(go env GOPATH)/bin` when `GOBIN` is unset. Add that
directory to `PATH` if it is not there yet:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

You also need `sbx` 0.38.0 or newer, `git` and the `claude` CLI on `PATH`, on Linux or macOS. box
checks all of that before it does anything.

To update, run `box self-update`. Every box command checks whether a newer release is published,
at most once an hour, and says so on stderr when there is one.

## Get started

Everything below runs from the root of the repository you want an agent to work on.

**Once per machine** — point box at a Claude token:

```sh
claude setup-token                                            # prints a token
mkdir -p ~/.secrets && $EDITOR ~/.secrets/claude-oauth.token  # save it there
export CLAUDE_OAUTH_TOKEN_FILE=~/.secrets/claude-oauth.token  # direnv is a good home for this
```

**Once per project** — write the setup and commit it:

```sh
box gen                    # writes .box/ and the .gitignore lines box needs
$EDITOR .box/config.json   # fill in "model"; gen already wrote the kit it points at
```

**Then, any time:**

```sh
box run
```

That drops you into an interactive Claude session inside the sandbox: type the task at its prompt.
When the session ends, committed work comes back on a branch named after it, and the sandbox is
removed.

`box config` prints the settings in effect and runs every check a run makes. Run it after setup, or
whenever a run refuses and you want the full picture.

## Commands

| Command | What it does |
| --- | --- |
| `box run` | creates the sandbox and starts Claude in it |
| `box config` | prints the settings in effect, then runs every check a run makes |
| `box gen` | writes a starter `.box/` directory, a starter kit and the `.gitignore` lines box needs |
| `box mount-prompt` | prints a prompt that has an agent fill in this machine's [mount paths](docs/mounts.md) |
| `box self-update` | replaces this copy of box with the published release |

`gen`, `mount-prompt` and `self-update` read no settings, so they reject every flag.

## Documentation

- [Settings](docs/settings.md) — every flag and `.box/config.json` key, and what wins over what
- [Mounts](docs/mounts.md) — giving the sandbox a toolchain or a cache from your machine
- [Secrets](docs/secrets.md) — letting the agent reach an API without handing it the token
- [Groups](docs/groups.md) — one sandbox working on several repositories at once
- [Read-only tools](docs/mcp.md) — letting the agent query a database or a cluster over MCP
- [What you get back](docs/results.md) — how committed work returns, and when a sandbox is kept
- [Troubleshooting](docs/troubleshooting.md) — the refusals you are most likely to meet

Why box is built the way it is: [goal](docs/goal.md), [updates](docs/updates.md),
[terminals](docs/terminals.md), [Ctrl-C](docs/signals.md), [fetching](docs/fetching.md).

## Development

Only needed to work on box itself. box depends on nothing outside the standard library, so a
checkout needs no more than Go:

```sh
git clone https://github.com/lk16/box && cd box
./check.sh
```

`check.sh` runs everything `pre-commit` does — `go fmt`, `go vet`, `go mod tidy`, `golangci-lint` —
plus the tests. Run it before starting a change and again once it is finished. To have git run the
checks for you:

```sh
pre-commit install
```

[docs/style.md](docs/style.md) has the rules the code follows, and
[docs/sandbox.md](docs/sandbox.md) what is true of this repository inside a box sandbox.
