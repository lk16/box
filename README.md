# box

`box.py` runs Claude Code inside a disposable Docker sandbox (`sbx`), so an agent can work on
your repository without touching your machine.

What it gives you:

- CPU, memory and disk limits on the agent
- an in-container git clone instead of a bind mount, so the working tree stays untouched
- several sandboxes for the same project side by side, without name collisions
- committed work fetched back to the host, and the sandbox removed, when the agent exits
- a refusal to remove a sandbox that still holds uncommitted changes

## Requirements

- Python 3.11+ (standard library only)
- `sbx` ([Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)) and `git` on `PATH`
- a Claude OAuth token in a file — run `claude setup-token`, save the printed token, and point
  `CLAUDE_OAUTH_TOKEN_FILE` at it (see [The OAuth token](#the-oauth-token))

Works on Linux and macOS.

## Install

Download the script once, to a single place on your machine:

```sh
mkdir -p ~/.local/bin
curl -fsSL -o ~/.local/bin/box.py https://raw.githubusercontent.com/lk16/box/main/box.py
chmod +x ~/.local/bin/box.py
```

Then alias it, in `~/.bashrc` or `~/.zshrc`:

```sh
alias box='~/.local/bin/box.py'
```

The alias points at an absolute path, so `~/.local/bin` does not have to be on your `PATH`.

That one copy serves every project — do not put `box.py` in your repositories. It resolves
everything relative to the directory you run it in, never relative to itself: the `.box.json` it
reads, the workspace it hands to `sbx`, and the git remote it fetches committed work from.

To update, run the same `curl` again.

## Usage

Run it from the root of the repository the agent should work on:

```sh
cd ~/projects/my-project
box
box -v --memory 8g --cpus 8
```

Settings that you use every time belong in a `.box.json` in that repository:

```json
{
  "prompt_file": "docs/agent.md",
  "kit": ".sbx/kit",
  "memory": "8g"
}
```

A command line flag always wins over the JSON file, which wins over the built-in default.
`-v` prints the settings in effect before the sandbox starts.

## The OAuth token

The token path is the one setting that comes from the environment, and only from there:

```sh
export CLAUDE_OAUTH_TOKEN_FILE=~/.secrets/claude-oauth.token
```

There is no flag and no `.box.json` key for it. `.box.json` is committed with the project and
shared, while the token is yours and machine-specific — keeping it out of both means a project
config can never carry a path to someone else's credentials. Set it once per machine, next to
the alias in your shell profile, or per project with `direnv`.

`box.py` refuses to start when the variable is unset or points at a missing or empty file. The
token itself is never passed on a command line: it goes to `sbx` over stdin, and the sandbox
only ever sees a placeholder that the proxy swaps for the real value.

## Settings

| Flag | `.box.json` key | Default | Meaning |
| --- | --- | --- | --- |
| `--name NAME` | `name` | current directory name | Sandbox base name; a `-1`, `-2`, … suffix is added per run. |
| `--memory SIZE` | `memory` | `4g` | Memory limit for the sandbox. |
| `--cpus N` | `cpus` | `4` | CPUs allocated to the sandbox. |
| `--root-size SIZE` | `root_size` | `10g` | Sandbox root filesystem size. |
| `--docker-size SIZE` | `docker_size` | `10g` | Sandbox Docker storage size. |
| `--model MODEL` | `model` | unset | Model passed to the Claude CLI. |
| `--prompt-file PATH` | `prompt_file` | unset | File appended to the agent's system prompt. |
| `--kit REF` | `kit` | unset | `sbx` kit reference, e.g. a network policy directory. |
| `--mount SPEC` | `mounts` | `[]` | Extra workspace, repeatable. Append `:ro` for read-only. |
| `-v`, `--verbose` | — | off | Print the settings in effect, then run. |

Anything unknown in `.box.json` is an error, so typos surface immediately.

## When the agent leaves work behind

Only committed work survives. On exit `box.py` fetches from the `sandbox-<name>` git remote,
then checks whether the sandbox has a dirty tree. If it does, the sandbox is kept and the
recovery commands are printed — inspect it with `sbx exec`, copy files out with `sbx cp`, and
remove it yourself with `sbx rm --force <name>`.

## Development

Only needed to work on `box.py` itself — clone this repository rather than installing the script.
`box.py` depends on nothing at runtime, but the repository is set up for linting and tests:

```sh
uv sync
uv run pre-commit install
```

Run the checks before starting any change, and again once it is finished:

```sh
uv run pre-commit run -a
uv run pytest -q
```
