# box

box runs Claude Code inside a disposable Docker sandbox (`sbx`).

- the agent works on a clone inside the container, so your own working tree is untouched
- CPU, memory and disk limits on the agent
- several sandboxes for the same project side by side
- on exit, committed work comes back on a named branch and the sandbox is removed
- a sandbox holding uncommitted changes is kept instead of removed

## Requirements

- Python 3.11 or newer. box checks the version at startup and stops with a clear message on an
  older one. macOS ships 3.9 as its `python3`, so put a newer one earlier on `PATH`
- `sbx` ([Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)) 0.38.0 or newer, `git` and the
  `claude` CLI on `PATH`. `box run` and `box config` list any that are missing before doing
  anything else. The kits box writes use the spec layout sbx 0.38.0 introduced, which older
  releases reject, so every box command reads `sbx version` first and stops on an older one. When
  the `sbx` CLI and its daemon are different versions — the daemon keeps running across an
  upgrade — every box command stops and says to run `sbx daemon restart`
- Linux or macOS

## Install

```sh
mkdir -p ~/.local/bin
curl -fsSL -o ~/.local/bin/box https://raw.githubusercontent.com/lk16/box/main/box.py
chmod +x ~/.local/bin/box
```

Debian and Ubuntu put `~/.local/bin` on `PATH` for you; macOS does not. If `box` is not found, add
it in your shell's startup file (`~/.zshrc` on macOS, `~/.bashrc` on Linux, `~/.bash_profile` for
login bash):

```sh
export PATH="$HOME/.local/bin:$PATH"
```

To update, run `box self-update`. Every box command checks whether an update is available, at most
once an hour, and says so on stderr when there is one.

## Quick start

Everything below runs from the root of the repository you want an agent to work on.

Once per machine:

- `export CLAUDE_OAUTH_TOKEN_FILE=~/.secrets/claude-oauth.token` — a file holding a token from
  `claude setup-token`. [direnv](https://direnv.net/) is a good place to set it

Once per project, committed for everyone:

- `box gen` — writes `.box/` and the `.gitignore` lines box needs
- `$EDITOR .box/config.json` — fill in `model`; `gen` already wrote the kit it points at

Once per machine per project, if `.box/config.json` declares `required_mounts`:

- `box mount-prompt | claude` — fills in the gitignored `.box/mounts.json`

Then, to start a sandbox:

- `box run`

`box config` prints the settings in effect and runs every check a run makes. Run it after setup, or
when a run refuses and you want the full picture.

`box run` drops you into an interactive Claude session inside the sandbox: type the task at its
prompt. The built-in prompt tells the agent to make assumptions and keep going instead of stopping
on a question, since you may well walk away. The session is interactive either way, so you can
watch what it does and step in.

## Commands

| Command | What it does |
| --- | --- |
| `box gen` | writes a starter `.box/` directory, a starter kit and the `.gitignore` lines box needs, leaving anything already filled in alone |
| `box config` | prints the settings in effect, including the `CLAUDE_OAUTH_TOKEN_FILE` path, then runs every check a run makes |
| `box mount-prompt` | prints a prompt that has an agent fill in this machine's [mount paths](#mounts) |
| `box run` | creates the sandbox and starts Claude in it |
| `box self-update` | replaces this copy of box with the published one |

`box run` hands the agent a clone of the current repository, so it refuses to start outside a git
repository, or in one with no commits yet. `gen`, `mount-prompt` and `self-update` read no settings,
so they reject every flag.

`CLAUDE_OAUTH_TOKEN_FILE` points at a file holding a token from `claude setup-token`. It is the one
setting with no flag and no config key, so that a shared project file can never point at someone
else's credentials. box refuses to start without it, or if the file it points at is missing or
empty.

## Settings

Settings you use every run belong in `.box/config.json`, which is committed with the project. box's
own looks like this:

```json
{
  "kit": ".sbx/kit",
  "model": "claude-opus-5",
  "prompt_file": "docs/sandbox.md",
  "memory": "8g"
}
```

A command line flag wins over that file, which wins over the built-in default. `box config` prints
what a run would end up with, showing `(unset)` where nothing was given.

| Flag | `.box/config.json` key | Default | Meaning |
| --- | --- | --- | --- |
| `--name NAME` | `name` | directory name, kebab-cased (`box` when it holds no letters or digits) | Sandbox base name; a `-1`, `-2`, … suffix is added per run. |
| `--memory SIZE` | `memory` | `4g` | Memory limit for the sandbox. |
| `--cpus N` | `cpus` | `4` | CPUs allocated to the sandbox. |
| `--root-size SIZE` | `root_size` | `10g` | Sandbox root filesystem size. |
| `--docker-size SIZE` | `docker_size` | `10g` | Sandbox Docker storage size. |
| `--model MODEL` | `model` | — (required) | Model passed to the Claude CLI. |
| `--prompt-file PATH` | `prompt_file` | unset | File added after the built-in prompt. |
| `--kit REF` | `kit` | — (required) | `sbx` kit holding the sandbox's network policy. |
| — | `required_mounts` | `{}` | Mounts the project needs, as name to description (see [Mounts](#mounts)). |
| `--mount PATH` | — | none | Extra workspace, repeatable. Read-only; append `:rw` for read-write. |

Anything unknown in `.box/config.json` is an error, so typos surface immediately. Every setting is
text, though `"cpus": 4` works as well as `"cpus": "4"`. A `null`, a `true` or a list is an error
that says which key holds it. `required_mounts` is the one key holding an object.

`kit` and `model` have no default. An unset kit would leave the sandbox's network access to whatever
`sbx` grants, and an unset model would leave the choice to the sandbox's own Claude install, which
is not this host's.

`kit` points at the directory holding a `spec.yaml`, not at the file inside it. `box gen` writes a
starter policy at `.box/kit/spec.yaml`, allowing the agent's own API calls and nothing else, and
points `kit` at it, so `model` is the only setting left to fill in. Widen the allowlist for whatever
the project's checks fetch, or point `kit` at a policy you keep elsewhere. box's own kit lives in
`.sbx/kit`, which is sbx's convention.

## The system prompt

box always prepends a built-in prompt covering what is true of every sandbox. `box.py` is a single
file, and that prompt is the `BASE_PROMPT` string near the top of it.

`prompt_file` adds what is true of one project, such as its test command and its quirks. It is
appended after the built-in prompt, so it can qualify anything above it.

## Mounts

Paths differ per machine, so the project declares what it needs and each machine says where it is.

`required_mounts` in the committed `.box/config.json` holds a name and a description of what belongs
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
  "go_mod_cache": "~/go/pkg/mod"
}
```

`box run` refuses to start unless the two match exactly. A declared name with no path, a name still
holding the placeholder `box gen` writes, or a name the project never declared is an error that says
which mount is at fault. It also refuses while `.box/mounts.json` or `.box/deps/` is not ignored by
git: one holds paths that exist only on your machine, the other binaries that should not be
committed. `box gen` writes both `.gitignore` entries for you. Ignore those two entries only, not
the whole `.box/` directory, so `config.json` stays committed.

Every mount is read-only unless you say otherwise, since the agent should not be able to write to
your machine. `~/scratch:rw` opts one path out, and `:ro` is an error, not a synonym for the
default. The `:rw` goes on the path in `.box/mounts.json`, never in the declaration, so a shared
file cannot widen access to your disk. A leading `~` expands as it does in the shell. `--mount`
adds an unnamed workspace for one run. `box config` shows the resulting `sbx` specs.

### Filling them in with an agent

```sh
box mount-prompt | claude
```

The prompt lists every declared mount that has no path yet, with its description and the platform
and architecture a build has to match. Pipe it into an **interactive** session: this agent runs on
your machine, not in a sandbox, so you should see its commands and its diff before approving them.
It is told to probe for each path and check that it exists instead of guessing, to say which ones it
could not find, and to download a suitable build into `.box/deps/` and point the mount there where
nothing on this machine fits, since the sandbox runs Linux whatever you run.

Once every mount has a path, nothing is printed on stdout, so running it again gives the agent no
work to do. box says as much on stderr, which a pipe into `claude` does not carry.

Its output is only as good as your descriptions: `"go cache"` gives an agent nothing, while
`"the Go module cache, what \`go env GOMODCACHE\` prints"` gives it a command to run.

## What you get back

Only committed work comes back. On exit box fetches from the `sandbox-<name>` git remote, landing
the sandbox's commits on a `refs/sandboxes/<name>/<branch>` ref, then checks the sandbox for a dirty
tree.

A clean sandbox is removed once its refs have been dealt with. Each ref is one of:

- **commits you do not have** — `claude -p` picks a branch name from their subjects, the commits go
  on that branch, and the ref is dropped. `git switch <branch>` and the work is in front of you. A
  name the repository already has takes a `-2`, `-3` suffix, so nothing is overwritten
- **nothing new** — the ref is dropped without creating a branch

Naming is best effort. If `claude` fails, answers with nothing, takes longer than ten seconds, or
git cannot read the ref or rejects the name, the commits stay on their ref and box says so.
`git log refs/sandboxes/<name>/<branch>` still reaches them.

A dirty sandbox is kept, its refs are left alone, and the recovery commands are printed. The same
happens when box cannot tell whether there is anything to lose, such as when a fetch or a status
check fails.

`box run` exits with the sandbox agent's own exit code, so a script can act on it. Every other
refusal exits 1, and a Ctrl-C exits 130.

## Troubleshooting

The refusals you are most likely to meet, and what to do about each:

| Message | Fix |
| --- | --- |
| `this project has no .box/config.json` | run `box gen` |
| `kit is not set` | point `kit` at a kit directory in `.box/config.json` |
| `model is not set` | fill in a model in `.box/config.json` |
| `CLAUDE_OAUTH_TOKEN_FILE is not set` | export it, pointing at a file holding a `claude setup-token` token |
| `has no path on this machine for` | give the mount it lists a path in `.box/mounts.json`, or have an agent do it with `box mount-prompt` |
| `has uncommitted changes -- not removing it` | the sandbox was kept on purpose: recover with the `sbx exec` and `sbx cp` lines box printed, then `sbx rm --force <name>` |

## Development

Only needed to work on box itself: clone this repository instead of installing the script. box uses
itself for its own development, so this repository has its own `.box/` directory and `box run` here
starts an agent working on box.

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
