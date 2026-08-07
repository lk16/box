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
box run
box run -v --memory 8g --cpus 8
```

To set a repository up, run `box gen` in it. It creates `.box/`, writes a `config.json` holding
every setting at its default, writes a `mounts.json` answering every mount the project declares,
and adds the mounts file to `.gitignore` unless git already ignores it. It never changes a value
that is already there, so it is safe to re-run — that is how you pick up a mount a colleague
declared after you set up. It takes no flags, since it writes defaults for you to edit rather
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

`box config` prints the settings in effect and stops there, so it doubles as a check that the
project is set up: it reads both JSON files and resolves the mounts, so a bad key, broken JSON or
an unfilled mount is an error without a sandbox being created. It takes the same flags as
`box run`, so `box config --memory 8g` shows what that run would use. `box run -v` prints the
same block and then goes ahead.

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
| — | `required_mounts` | `{}` | Mounts the project needs, as name to description (see [Mounts](#mounts)). |
| `--mount PATH` | `.box/mounts.json` | `{}` | Extra workspace, repeatable. Read-only; append `:rw` for read-write. |
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

### Filling them in with an agent

`box mount-prompt` prints a prompt asking an agent to fill in the paths this machine still owes.
Run `box gen` first, then paste the output into an **interactive** agent session on this host:

```sh
box gen
box mount-prompt
```

Paste it into a running `claude`, rather than piping it to `claude -p`. The agent is going to
run commands on your machine and edit a file, and an interactive session is where you see each
tool call it wants to make and the diff it produces. Piped into headless mode, all of that
happens where you cannot watch it — for a file whose whole job is to point at directories on
your disk.

The prompt names each unfilled mount with its description, says which platform it is running on,
and tells the agent to edit `.box/mounts.json` in place, replacing only the placeholders. It
assumes a shell on this host — the agent can run `go env GOMODCACHE` or `brew --prefix` and
check a path exists before writing it, which a plain chat window could only guess at. The agent
is told to leave a placeholder alone and say so rather than invent a path, and to add `:rw` only
where a description asks for write access.

When every mount already has a path it prints nothing and exits 0, so running it again after a
partial fill asks only about what is left.

Its output is only as good as your descriptions. `"go cache"` gives an agent nothing to work
with; `"the Go module cache, what \`go env GOMODCACHE\` prints"` gives it a command to run, and
`"a checkout of github.com/abulmo/edax-reversi"` gives it something to search for. Write them for
that reader as well as the human one.

Whether a mount is writable is the machine's call, not the project's: `:rw` goes on the path in
`.box/mounts.json`, never in the declaration. A description may ask for it, but nothing enforces
it, so a shared file can never widen access to your disk.

Every mount is read-only unless you say otherwise, since the point of a sandbox is that the agent
cannot write to your machine. `~/.cache/go-build` mounts read-only, and `~/scratch:rw` opts that
one path out. Writing `:ro` is an error rather than a synonym for the default, so nothing looks
like it grants access it does not. `-v` shows the resulting `sbx` specs.

A leading `~` expands to your home directory, the same as in the shell, so `~/.cargo` is
preferred over spelling out `/home/you/.cargo`.

Because of that, `box run` refuses to start while `.box/mounts.json` exists and is not ignored by
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
