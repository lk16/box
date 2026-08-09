# box

box runs Claude Code inside a disposable Docker sandbox (`sbx`).

- the agent works on a clone in the container, never your working tree
- CPU, memory and disk limits on the agent
- several sandboxes for the same project side by side
- on exit, committed work comes back on a named branch and the sandbox is removed
- a sandbox holding uncommitted changes is kept, not removed

Needs Python 3.11+, plus `sbx` ([Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)),
`git` and the `claude` CLI on `PATH`. Works on Linux and macOS.

## Install

```sh
mkdir -p ~/.local/bin
curl -fsSL -o ~/.local/bin/box.py https://raw.githubusercontent.com/lk16/box/main/box.py
chmod +x ~/.local/bin/box.py
```

Then add the alias to `~/.bashrc` or `~/.zshrc`, and reload your shell config (or open a new
terminal):

```sh
alias box='~/.local/bin/box.py'
```

## Use

Every command runs from the root of the repository you want an agent to work on.

### Adding box to a project, once for everyone

- **`box gen`** — adds files in the `.box` folder and updates `.gitignore`
- edit `.box/config.json` — fill in `kit`, `model` and any [`required_mounts`](#mounts) by hand

### Setting up this machine, once per clone

- point `CLAUDE_OAUTH_TOKEN_FILE` at a file holding a token from `claude setup-token`, e.g.
  `export CLAUDE_OAUTH_TOKEN_FILE=~/.secrets/claude-oauth.token` in your shell profile, or per
  project with `direnv`. box refuses to start without it
- **`box mount-prompt | claude`** — have an agent fill in the mounts this project needs on this
  machine. See [Filling them in with an agent](#filling-them-in-with-an-agent)
- **`box config`** — confirm box's config files parse, and show the settings in effect without
  running
- **`box run`** — start the sandbox. See [Settings](#settings) for flags

## Settings

Settings you use every run belong in `.box/config.json`, which is committed with the project:

```json
{
  "kit": ".sbx/kit",
  "model": "claude-opus-5",
  "prompt_file": "docs/agent.md",
  "memory": "8g"
}
```

A command line flag wins over that file, which wins over the built-in default. `box config`
prints what a run would end up with.

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
| — | `required_mounts` | `{}` | Mounts the project needs, as name to description (see [Mounts](#mounts)). |
| `--mount PATH` | `.box/mounts.json` | `{}` | Extra workspace, repeatable. Read-only; append `:rw` for read-write. |

Anything unknown in `.box/config.json` is an error, so typos surface immediately.

`kit` and `model` have no defaults: box refuses to start without them, rather than falling back to
something you did not choose — see [Conservative by default](#conservative-by-default).

The kit carries the sandbox's network allowlist, and running without one would quietly give the
agent whatever network access `sbx` grants by default. Point `kit` at the directory holding a
`spec.yaml`, by convention `.sbx/kit` — the directory, not `.sbx/kit/spec.yaml`.

The sandbox's Claude CLI is a different install from yours, so an unset model leaves what runs to
*its* default rather than to you.

## The system prompt

box always sends a built-in prompt describing what is true of every sandbox: that the agent runs
unattended with nobody to answer follow-ups, that only committed work survives removal, that
pre-commit's hook is not installed, that the sandbox runs as a different user so `PATH` and caches
do not point at the host's, and that a 403 on a dependency download is the network allowlist
rather than a bug. It lives in `box.py`, as `BASE_PROMPT`, so every project gets it without
copying it around.

`prompt_file` adds what is true of one project — its test command, its quirks — and is appended
after the built-in prompt, so it can qualify anything above it.

## Mounts

Mounts are the one part of a project's box setup that names paths on the machine box runs on: a
toolchain sits somewhere else on another machine, and somewhere else again on another OS. So the
project declares *what* it needs and each machine says *where*.

`required_mounts` in the committed `.box/config.json` is a name and a description of what belongs
there:

```json
{
  "required_mounts": {
    "go_toolchain": "the Go install, what `go env GOROOT` prints",
    "go_mod_cache": "the Go module cache, what `go env GOMODCACHE` prints"
  }
}
```

The gitignored `.box/mounts.json` gives each of those names a path:

```json
{
  "go_toolchain": "/usr/local/go",
  "go_mod_cache": "~/.local/go/pkg/mod"
}
```

`box run` refuses to start unless the two match exactly: a declared name with no path, a name
still holding the placeholder `box gen` writes, or a name the project never declared is an error
naming the mount. It also refuses while `.box/mounts.json` or `.box/deps/` is not ignored by git — one
carries paths that exist only on your machine, the other binaries that belong in nobody's history.
`box gen` writes both `.gitignore` entries for you. Ignore those two rather than the whole `.box/`
directory, so `config.json` stays committed.

Every mount is read-only unless you say otherwise, since the point of a sandbox is that the agent
cannot write to your machine. `~/scratch:rw` opts one path out, and writing `:ro` is an error
rather than a synonym for the default. The `:rw` goes on the path in `.box/mounts.json`, never in
the declaration, so a shared file can never widen access to your disk — see [Conservative by
default](#conservative-by-default). A leading `~` expands as it does in the shell. `--mount` adds
an unnamed workspace for one run, for something genuinely one-off. `box config` shows the
resulting `sbx` specs.

### Filling them in with an agent

```sh
box mount-prompt | claude
```

This prints a prompt naming every declared mount that has no path yet, each with its description
and the platform it is running on. Pipe it into an **interactive** session: this agent runs on
your machine rather than in a sandbox, so it is deliberately the one place box puts a human in
front of every command it runs and the diff it writes — see [Conservative by
default](#conservative-by-default). It assumes a shell, so the agent can probe system specific
paths and check they exist rather than guess, and is told to say which it could not find instead
of inventing one. Where nothing on the machine fits — the sandbox runs Linux whatever you run —
it downloads a suitable build into `.box/deps/` and points the mount there.

It works straight after someone declares a new mount, and prints nothing once they all have
paths, so you can re-run it after a partial fill.

Its output is only as good as your descriptions: `"go cache"` gives an agent nothing, while
`"the Go module cache, what \`go env GOMODCACHE\` prints"` gives it a command to run.

## What you get back

Only committed work survives. On exit box fetches from the `sandbox-<name>` git remote, which
lands the sandbox's commits on a `refs/sandboxes/<name>/<branch>` ref, then checks whether the
sandbox has a dirty tree.

A clean sandbox is settled and removed. Each of its refs is one of:

- **commits you do not have** — `claude -p` is given their subjects and asked for a kebab-case
  name of at most five words, the commits are put on a branch under that name, and the ref is
  dropped. `git switch <branch>` and the work is in front of you. A name the repository already
  has takes a `-2`, `-3` suffix, so nothing is overwritten
- **nothing new** — the agent committed nothing, so the ref is dropped and no branch is made,
  rather than leaving an empty name behind

Naming is best effort. If `claude` fails, answers with nothing, or takes longer than five seconds,
the commits stay on their ref and box says so — the work is already fetched, and
`git log refs/sandboxes/<name>/<branch>` still reaches it.

If the tree is dirty the sandbox is kept, its refs are left alone, and the recovery commands are
printed — inspect it with `sbx exec`, copy files out with `sbx cp`, and remove it yourself with
`sbx rm --force <name>`.

## Conservative by default

box would rather refuse to start, or hand the agent less, than let a run go wrong quietly. A
missing setting is an error instead of a guess, network access is whatever the kit allows and
nothing more, mounts are read-only until you say otherwise, the one agent that runs on your
machine is interactive, and a sandbox with uncommitted work is never removed. Losing work, or an
agent doing something you did not expect, is the thing box exists to prevent.

## Development

Only needed to work on box itself — clone this repository rather than installing the script. box
uses itself for its own development: this repository has a `.box/` directory, so `box run` here
puts an agent to work on box in a box.

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
