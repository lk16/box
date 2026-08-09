# box

`box.py` runs Claude Code inside a disposable Docker sandbox (`sbx`), so an agent can work on
your repository without touching your machine.

What it gives you:

- CPU, memory and disk limits on the agent
- an in-container git clone instead of a bind mount, so the working tree stays untouched
- several sandboxes for the same project side by side, without name collisions
- committed work fetched back to the host onto a named branch, and the sandbox removed, when the
  agent exits
- a refusal to remove a sandbox that still holds uncommitted changes

Needs Python 3.11+, plus `sbx` ([Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)) and
`git` on `PATH`. The `claude` CLI is used to name the branch, and its absence costs you the name
and nothing else. Works on Linux and macOS.

## Install

```sh
mkdir -p ~/.local/bin
curl -fsSL -o ~/.local/bin/box.py https://raw.githubusercontent.com/lk16/box/main/box.py
chmod +x ~/.local/bin/box.py
echo "alias box='~/.local/bin/box.py'" >> ~/.bashrc      # or ~/.zshrc
```

Re-run the same `curl` to update. One copy serves every project, so do not commit `box.py` into
your repositories. Every command checks whether the published copy differs from yours, at most
once an hour, and prints the `curl` to take it in red on stderr when it does.

## Use

Every command runs from the root of the repository you want an agent to work on.

### Adding box to a project

- **`box gen`** — adds files in the `.box` folder and updates `.gitignore`
- edit `.box/config.json` — fill in `kit`, `model` and any [`required_mounts`](#mounts) by hand

### Configuring box for this repo on this machine

- point `CLAUDE_OAUTH_TOKEN_FILE` at a file holding a token from `claude setup-token`, e.g.
  `export CLAUDE_OAUTH_TOKEN_FILE=~/.secrets/claude-oauth.token` in your shell profile, or per
  project with `direnv`. box refuses to start without it
- **`box mount-prompt | claude`** — have an agent fill in the mounts this project needs on this
  machine. See [Filling them in with an agent](#filling-them-in-with-an-agent)
- **`box config`** — confirm box's config files parse, and show the settings in effect without
  running
- **`box run`** — start the sandbox. See [Settings](#settings) for flags

A new machine on an existing project needs only the second group.

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

`kit` and `model` have no defaults: `box.py` refuses to start without them, rather than falling
back to something you did not choose.

The kit carries the sandbox's network allowlist, so running without one would quietly give the
agent whatever network access `sbx` grants by default. Point it at the directory holding a
`spec.yaml`, by convention `.sbx/kit` — the directory, not `.sbx/kit/spec.yaml`, since `sbx`
reads anything that is not a directory as a zip artifact. `box run` rejects a `kit` that names a
file on disk; anything not on disk is left alone, since `sbx` resolves those itself.

The model must be named because the Claude CLI inside the sandbox is a different install from the
one on your machine, possibly a different version, and an unset model means *its* default decides
what runs — which can differ from what you expect and from run to run. Naming it makes the
sandbox's choice yours.

## Mounts

Mounts are the one part of a project's box setup that names paths on the machine box runs on: a
toolchain sits somewhere else on a colleague's laptop, and somewhere else again on macOS. They
are split in two, so each half is stated where it is true.

The project declares *what* it needs, in `required_mounts` in the committed `.box/config.json` —
a name and a description of what belongs there:

```json
{
  "required_mounts": {
    "go_toolchain": "the Go install, what `go env GOROOT` prints",
    "go_mod_cache": "the Go module cache, what `go env GOMODCACHE` prints"
  }
}
```

Each machine says *where*, under the same names, in the gitignored `.box/mounts.json`:

```json
{
  "go_toolchain": "/usr/local/go",
  "go_mod_cache": "~/.local/go/pkg/mod"
}
```

`box run` refuses to start unless the two match exactly. A declared name with no path — or still
holding the `/placeholder/for/real/path` that `box gen` writes — is an error listing the name
and its description, so a forgotten module cache surfaces before the sandbox starts rather than
as a 403 halfway through. A name in `.box/mounts.json` that the project does not declare is an
error too, the same way an unknown config key is: it is almost always a typo.

Mounts are passed to `sbx` in declaration order, so the arguments do not depend on how one
machine happened to order its file. `--mount` adds an unnamed workspace for one run, on top of
the declared ones — the escape hatch for something genuinely one-off.

Every mount is read-only unless you say otherwise, since the point of a sandbox is that the agent
cannot write to your machine. `~/scratch:rw` opts one path out, and writing `:ro` is an error
rather than a synonym for the default. The `:rw` goes on the path in `.box/mounts.json`, never in
the declaration, so a shared file can never widen access to your disk. A leading `~` expands as
it does in the shell. `box config` shows the resulting `sbx` specs.

`box run` also refuses to start while `.box/mounts.json` or `.box/deps/` exists and is not
ignored by git — one carries paths that exist only on your machine, the other binaries that
belong in nobody's history. `box gen` writes both entries for you; by hand they are two lines in
`.gitignore`:

```gitignore
.box/mounts.json
.box/deps/
```

Ignore those two rather than the whole `.box/` directory, so `config.json` stays committed.

### Filling them in with an agent

```sh
box mount-prompt | claude
```

This prints a prompt naming every declared mount that has no path yet, each with its description
and the platform it is running on. Pipe it into an **interactive** session, so you approve each
command the agent runs and the diff it writes. It assumes a shell here: the agent can run
`go env GOMODCACHE` or `brew --prefix` and check a path exists rather than guess, and is told to
say which it could not find instead of inventing one. Where nothing on the machine fits — the
sandbox runs Linux whatever you run — it downloads a suitable build into `.box/deps/` and points
the mount there.

It works straight after someone declares a new mount, and prints nothing once they all have
paths, so you can re-run it after a partial fill.

Its output is only as good as your descriptions: `"go cache"` gives an agent nothing, while
`"the Go module cache, what \`go env GOMODCACHE\` prints"` gives it a command to run.

## What you get back

Only committed work survives. On exit `box.py` fetches from the `sandbox-<name>` git remote,
which lands the sandbox's commits on a `refs/sandboxes/<name>/<branch>` ref, then checks whether
the sandbox has a dirty tree.

A clean sandbox is settled and removed. Each of its refs is one of:

- **commits you do not have** — `claude -p` is given their subjects and asked for a kebab-case
  name of at most five words, the commits are put on a branch under that name, and the ref is
  dropped. `git switch <branch>` and the work is in front of you. A name the repository already
  has takes a `-2`, `-3` suffix, so nothing is overwritten
- **nothing new** — the agent committed nothing, so the ref is dropped and no branch is made,
  rather than leaving an empty name behind

Naming is best effort. If `claude` is not on `PATH`, fails, answers with nothing, or takes longer
than five seconds, the commits stay on their ref and box says so — the work is already fetched,
and `git log refs/sandboxes/<name>/<branch>` still reaches it.

If the tree is dirty the sandbox is kept, its refs are left alone, and the recovery commands are
printed — inspect it with `sbx exec`, copy files out with `sbx cp`, and remove it yourself with
`sbx rm --force <name>`.

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
