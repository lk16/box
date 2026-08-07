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
everything relative to the directory you run it in, never relative to itself: the
`.box/config.json` it reads, the workspace it hands to `sbx`, and the git remote it fetches
committed work from.

To update, run the same `curl` again.

## Usage

Run it from the root of the repository the agent should work on:

```sh
cd ~/projects/my-project
box
box -v --memory 8g --cpus 8
```

To set a repository up, run `box gen` in it. It creates `.box/`, writes a `config.json` holding
every setting at its default and an empty `mounts.json`, and adds the mounts file to
`.gitignore` unless git already ignores it. It never overwrites a file that already exists, so
it is safe to re-run — and it takes no flags, since it writes defaults for you to edit rather
than settings you chose.

Settings that you use every time belong in a `.box/config.json` in that repository:

```json
{
  "kit": ".sbx/kit",
  "model": "claude-opus-5",
  "prompt_file": "docs/agent.md",
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

There is no flag and no `.box/config.json` key for it. `.box/config.json` is committed with the
project and shared, while the token is yours and machine-specific — keeping it out of both means
a project config can never carry a path to someone else's credentials. Set it once per machine,
next to the alias in your shell profile, or per project with `direnv`.

`box.py` refuses to start when the variable is unset or points at a missing or empty file. The
token itself is never passed on a command line: it goes to `sbx` over stdin, and the sandbox
only ever sees a placeholder that the proxy swaps for the real value.

## The system prompt

`box.py` always sends a built-in prompt describing what is true of every sandbox: that the agent
runs unattended with nobody to answer follow-ups, that only committed work survives removal, that
pre-commit's hook is not installed, that the sandbox runs as a different user so `PATH` and caches
do not point at the host's, and that a 403 on a dependency download is the network allowlist
rather than a bug. It lives in `box.py`, as `BASE_PROMPT`, so every project gets it without
copying it around.

`prompt_file` adds what is true of one project — its test command, its quirks — and is appended
after the built-in prompt, so it can qualify anything above it.

## Settings

| Flag | `.box/config.json` key | Default | Meaning |
| --- | --- | --- | --- |
| `--name NAME` | `name` | current directory name | Sandbox base name; a `-1`, `-2`, … suffix is added per run. |
| `--memory SIZE` | `memory` | `4g` | Memory limit for the sandbox. |
| `--cpus N` | `cpus` | `4` | CPUs allocated to the sandbox. |
| `--root-size SIZE` | `root_size` | `10g` | Sandbox root filesystem size. |
| `--docker-size SIZE` | `docker_size` | `10g` | Sandbox Docker storage size. |
| `--model MODEL` | `model` | — (required) | Model passed to the Claude CLI. |
| `--prompt-file PATH` | `prompt_file` | unset | File added after the built-in prompt (see [The system prompt](#the-system-prompt)). |
| `--kit REF` | `kit` | — (required) | `sbx` kit holding the sandbox's network policy. |
| `--mount PATH` | `.box/mounts.json` | `[]` | Extra workspace, repeatable. Read-only; append `:rw` for read-write. |
| `-v`, `--verbose` | — | off | Print the settings in effect, then run. |

Anything unknown in `.box/config.json` is an error, so typos surface immediately.

`kit` and `model` have no defaults: `box.py` refuses to start without them, rather than falling
back to something you did not choose.

The kit carries the sandbox's network allowlist, so running without one would quietly give the
agent whatever network access `sbx` grants by default. Point it at a directory with a
`spec.yaml`, by convention `.sbx/kit` in the project.

The model must be named because the Claude CLI inside the sandbox is a different install from the
one on your machine, possibly a different version, and an unset model means *its* default decides
what runs — which can differ from what you expect and from run to run. Naming it makes the
sandbox's choice yours.

Extra mounts are read-only unless you say otherwise, since the point of a sandbox is that the
agent cannot write to your machine. `--mount ~/.cache/go-build` mounts read-only, and
`--mount ~/scratch:rw` opts that one path out. Writing `:ro` is an error rather than a synonym
for the default, so nothing looks like it grants access it does not. `-v` shows the resulting
`sbx` specs.

A leading `~` expands to your home directory, the same as in the shell, so `~/.cargo` is
preferred over spelling out `/home/you/.cargo`.

Mounts live in their own file, `.box/mounts.json`, holding a JSON array of paths:

```json
[
  "~/.cargo",
  "/usr/local/go",
  "~/projects/some-dependency"
]
```

They are the one part of a project's box setup that names paths on the machine box runs on: a
toolchain sits somewhere else on a colleague's laptop, and somewhere else again on macOS. So
they are kept out of `.box/config.json`, which holds only what every checkout of the project
shares. `--mount` adds to the file's list for one run rather than replacing it.

Because of that, `box` refuses to start while `.box/mounts.json` exists and is not ignored by
git. Committing it would put paths that exist only on your machine into everyone else's clone.
`box gen` writes that entry for you; by hand it is one line in `.gitignore`:

```gitignore
.box/mounts.json
```

Ignore the file rather than the whole `.box/` directory, so `config.json` stays committed. A
project with no mounts file needs no entry.

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
