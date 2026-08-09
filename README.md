# box

box runs Claude Code inside a disposable Docker sandbox (`sbx`).

- the agent works on a clone in the container, never your working tree
- CPU, memory and disk limits on the agent
- several sandboxes for the same project side by side
- on exit, committed work comes back on a named branch and the sandbox is removed
- a sandbox holding uncommitted changes is kept, not removed

## Requirements

- Python 3.11 or newer
- `sbx` ([Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)), `git` and the `claude` CLI on
  `PATH`
- Linux or macOS

## Install

```sh
mkdir -p ~/.local/bin
curl -fsSL -o ~/.local/bin/box https://raw.githubusercontent.com/lk16/box/main/box.py
chmod +x ~/.local/bin/box
```

Debian and Ubuntu put `~/.local/bin` on `PATH` for you; macOS does not. If `box` is not found, add
it in your shell's startup file — `~/.zshrc` on macOS, `~/.bashrc` on Linux, `~/.bash_profile` for
login bash:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Re-run the same `curl` to update. box prints it in red on stderr, at most once an hour, when the
published copy differs from yours. A copy git tracks is left alone, so working on box itself never
nags you to overwrite your own changes.

## Quick start

Run everything from the root of the repository you want an agent to work on:

```sh
box gen                    # writes .box/ and the .gitignore lines box needs
$EDITOR .box/config.json   # fill in kit and model
export CLAUDE_OAUTH_TOKEN_FILE=~/.secrets/claude-oauth.token
box mount-prompt | claude  # only if the project declares required_mounts
box config                 # the settings, plus every check a run makes
box run                    # start the sandbox
```

`box run` drops you into an interactive Claude session inside the sandbox: type the task at its
prompt. The built-in prompt tells the agent to make assumptions and keep going rather than stop on
a question, since you may well walk away — but the session is interactive, so you can watch what
it does and step in.

`box gen` and the config file are committed once for everyone. The token and the mount paths are
per machine, so each clone does those two again.

## Commands

| Command | What it does |
| --- | --- |
| `box gen` | writes a starter `.box/` directory and the `.gitignore` lines box needs, changing nothing already filled in |
| `box config` | prints the settings in effect, the `CLAUDE_OAUTH_TOKEN_FILE` path included, then runs every check a run makes |
| `box mount-prompt` | prints a prompt that has an agent fill in this machine's [mount paths](#mounts) |
| `box run` | creates the sandbox and starts Claude in it |

`box run` hands the agent a clone of the current repository, so it refuses to start outside a git
repository, or in one with no commits yet. `gen` and `mount-prompt` work on the project's files
rather than on settings, so they reject every flag.

`CLAUDE_OAUTH_TOKEN_FILE` names a file holding a token from `claude setup-token`, and is the one
setting with no flag and no config key: a shared project file can never point at someone else's
credentials. box refuses to start without it, or if the file it names is missing or empty.

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
what a run would end up with.

| Flag | `.box/config.json` key | Default | Meaning |
| --- | --- | --- | --- |
| `--name NAME` | `name` | directory name, kebab-cased (`box` if nothing survives) | Sandbox base name; a `-1`, `-2`, … suffix is added per run. |
| `--memory SIZE` | `memory` | `4g` | Memory limit for the sandbox. |
| `--cpus N` | `cpus` | `4` | CPUs allocated to the sandbox. |
| `--root-size SIZE` | `root_size` | `10g` | Sandbox root filesystem size. |
| `--docker-size SIZE` | `docker_size` | `10g` | Sandbox Docker storage size. |
| `--model MODEL` | `model` | — (required) | Model passed to the Claude CLI. |
| `--prompt-file PATH` | `prompt_file` | unset | File added after the built-in prompt. |
| `--kit REF` | `kit` | — (required) | `sbx` kit holding the sandbox's network policy. |
| — | `required_mounts` | `{}` | Mounts the project needs, as name to description (see [Mounts](#mounts)). |
| `--mount PATH` | — | `{}` | Extra workspace, repeatable. Read-only; append `:rw` for read-write. |

Anything unknown in `.box/config.json` is an error, so typos surface immediately. Every setting is
text: `"cpus": 4` and `"cpus": "4"` both work, since a number spells itself, but `null`, `true` and
a list are errors naming the key. `required_mounts` is the one key holding an object.

`kit` and `model` are required rather than defaulted: an unset kit leaves the sandbox's network
access to whatever `sbx` grants, and an unset model leaves the choice to the sandbox's own Claude
install, which is not this host's. Point `kit` at the directory holding a `spec.yaml`, by
convention `.sbx/kit` — the directory, not `.sbx/kit/spec.yaml`.

## The system prompt

box always prepends a built-in prompt covering what is true of every sandbox. It lives in `box.py`
as `BASE_PROMPT` — read it there.

`prompt_file` adds what is true of one project — its test command, its quirks — and is appended
after the built-in prompt, so it can qualify anything above it.

## Mounts

Paths differ per machine, so the project declares *what* it needs and each machine says *where*.

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
  "go_mod_cache": "~/go/pkg/mod"
}
```

`box run` refuses to start unless the two match exactly: a declared name with no path, a name still
holding the placeholder `box gen` writes, or a name the project never declared is an error naming
the mount. It also refuses while `.box/mounts.json` or `.box/deps/` is not ignored by git — one
carries paths that exist only on your machine, the other binaries that belong in nobody's history.
`box gen` writes both `.gitignore` entries for you. Ignore those two rather than the whole `.box/`
directory, so `config.json` stays committed.

Every mount is read-only unless you say otherwise: the point of a sandbox is that the agent cannot
write to your machine. `~/scratch:rw` opts one path out, and `:ro` is an error rather than a synonym
for the default. The `:rw` goes on the path in `.box/mounts.json`, never in the declaration, so a
shared file can never widen access to your disk. A leading `~` expands as it does in the shell.
`--mount` adds an unnamed workspace for one run. `box config` shows the resulting `sbx` specs.

### Filling them in with an agent

```sh
box mount-prompt | claude
```

The prompt names every declared mount that has no path yet, with its description and the platform
and architecture a build has to match. Pipe it into an **interactive** session: this agent runs on
your machine rather than in a sandbox, so its commands and its diff belong in front of you to approve.
It is told to probe for each path and check it exists rather than guess, to say which it could not
find, and — where nothing here fits, since the sandbox runs Linux whatever you run — to download a
suitable build into `.box/deps/` and point the mount there.

Nothing is printed on stdout once every mount has a path, so a second run cannot ask for done work.
It says so on stderr, which no pipe into `claude` carries.

Its output is only as good as your descriptions: `"go cache"` gives an agent nothing, while
`"the Go module cache, what \`go env GOMODCACHE\` prints"` gives it a command to run.

## What you get back

Only committed work survives. On exit box fetches from the `sandbox-<name>` git remote, landing the
sandbox's commits on a `refs/sandboxes/<name>/<branch>` ref, then checks the sandbox for a dirty
tree.

A clean sandbox is settled and removed. Each of its refs is one of:

- **commits you do not have** — `claude -p` names a branch after their subjects, the commits go on
  it, and the ref is dropped. `git switch <branch>` and the work is in front of you. A name the
  repository already has takes a `-2`, `-3` suffix, so nothing is overwritten
- **nothing new** — the ref is dropped without a branch, rather than leaving an empty name behind

Naming is best effort. If `claude` fails, answers with nothing, takes longer than ten seconds, or
git cannot read the ref or refuses the name, the commits stay on their ref and box says so:
`git log refs/sandboxes/<name>/<branch>` still reaches them.

A dirty sandbox is kept, its refs are left alone, and the recovery commands are printed. The same
happens when box cannot tell — a fetch or a status check that fails is never read as "there is
nothing here to lose".

`box run` exits with the sandbox agent's own exit code, so a script can act on it. Every other
refusal exits 1, and a Ctrl-C exits 130.

## Troubleshooting

box would rather refuse to start than let a run go wrong quietly, so a refusal names what to do.
The five you are most likely to meet:

| Message | Fix |
| --- | --- |
| `kit is not set` | point `kit` at a kit directory in `.box/config.json` |
| `model is not set` | name a model in `.box/config.json` |
| `CLAUDE_OAUTH_TOKEN_FILE is not set` | export it, pointing at a file holding a `claude setup-token` token |
| `has no path on this machine for` | give the named mount a path in `.box/mounts.json`, or have an agent do it with `box mount-prompt` |
| `has uncommitted changes -- not removing it` | the sandbox was kept on purpose: recover with the `sbx exec` and `sbx cp` lines box printed, then `sbx rm --force <name>` |

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
