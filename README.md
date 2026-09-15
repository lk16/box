# box

box runs Claude Code inside a disposable Docker sandbox (`sbx`).

- the agent works on a clone inside the container, so your own working tree is untouched
- CPU, memory and disk limits on the agent
- several sandboxes for the same project side by side
- on exit, committed work comes back on a named branch and the sandbox is removed
- a sandbox holding uncommitted changes is kept instead of removed

## Requirements

- Python 3.9 or newer, which the `python3` a stock macOS ships already satisfies. box checks the
  version at startup and stops with a clear message on an older one
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

- `box gen` — writes `.box/` and the `.gitignore` lines box needs. In a folder with no config yet it
  asks whether this is one project or a [group](#groups); pressing Enter takes one project
- `$EDITOR .box/config.json` — fill in `model`; `gen` already wrote the kit it points at

Once per machine per project, if `.box/config.json` declares `required_mounts`:

- `box mount-prompt` — prints a prompt; give it to Claude to fill in the gitignored
  `.box/mounts.json`

Once per machine per group, if `.box/config.json` declares `repos`:

- `box gen`, then fill in the gitignored `.box/repos.json` with where each [member](#groups) sits

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
| `box gen` | writes a starter `.box/` directory, a starter kit and the `.gitignore` lines box needs, leaving anything already filled in alone. Asks whether the folder is one project or a group, unless it already has a config or nobody is at the terminal |
| `box config` | prints the settings in effect, including the `CLAUDE_OAUTH_TOKEN_FILE` path, then runs every check a run makes |
| `box mount-prompt` | prints a prompt that has an agent fill in this machine's [mount paths](#mounts) |
| `box run` | creates the sandbox and starts Claude in it |
| `box self-update` | replaces this copy of box with the published one |

`box run` hands the agent a clone of the current repository, so it refuses to start outside a git
repository, or in one with no commits yet. `gen`, `mount-prompt` and `self-update` read no settings,
so they reject every flag.

Settings are read from the working directory's `.box/`, and the whole repository around it is what
the sandbox gets. So a folder below the repository root can hold a `.box/` of its own: the sandbox
is named after that folder, the agent still starts at the root, and the prompt tells it which folder
you ran box in.

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
| `--template REF` | `template` | unset | `sbx` template the sandbox's container image comes from. |
| — | `required_mounts` | `{}` | Mounts the project needs, as name to description (see [Mounts](#mounts)). |
| — | `secret_hosts` | `{}` | Tokens the agent may use, as variable name to host (see [Secrets](#secrets)). |
| — | `mcp` | `[]` | MCP servers the sandbox may use, as a list of names (see [Read-only tools](#read-only-tools)). |
| — | `repos` | `{}` | Other repositories this session works on, as name to `branch` and `git_origin` (see [Groups](#groups)). |
| `--mount PATH` | — | none | Extra workspace, repeatable. Read-only; append `:rw` for read-write. |

Anything unknown in `.box/config.json` is an error, so typos surface immediately. Every setting is
text, though `"cpus": 4` works as well as `"cpus": "4"`. A `null`, a `true` or a list is an error
that says which key holds it. `required_mounts`, `secret_hosts` and `repos` are the keys holding an
object, and `mcp` is the one holding a list.

`kit` and `model` have no default. An unset kit would leave the sandbox's network access to whatever
`sbx` grants, and an unset model would leave the choice to the sandbox's own Claude install, which
is not this host's.

`kit` points at the directory holding a `spec.yaml`, not at the file inside it. `box gen` writes a
starter policy at `.box/kit/spec.yaml`, allowing the agent's own API calls and nothing else, and
points `kit` at it, so `model` is the only setting left to fill in. Widen the allowlist for whatever
the project's checks fetch, or point `kit` at a policy you keep elsewhere. box's own kit lives in
`.sbx/kit`, which is sbx's convention.

`template` names the image the sandbox runs on. Unset, which is the default, leaves that to `sbx`,
and its own agent image is what almost every project wants. Set it when the project needs something
that image does not have — a compiler, say, which a kit allowing only `api.anthropic.com` gives the
agent no way to install from inside. Producing that image is yours to do: `docker build` on the host
from sbx's base plus whatever you need, then `docker save` and `sbx template load`, and put the tag
`sbx template ls` shows here. box only passes the value on. An image the sandbox runtime does not
hold is a pull error at create time that says nothing useful, so load it before you run.

## Read-only tools

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
takes every name in one comma-separated argument.

## Groups

One task often spans several repositories. A group is one sandbox that works on all of them: keep a
folder of box setups next to the repositories themselves, one subfolder per group.

```
~/work/
  boxes/                    one git repository, shared by the team
    billing/.box/config.json
    search/.box/config.json
  billing-api/              the members sit next to boxes
  billing-worker/
  kubernetes/
```

`box gen` in `~/work/boxes/billing` asks whether the folder is one project or a group; answering `2`
writes a config with the group keys already in it. `cd ~/work/boxes/billing && box run` then starts a
sandbox named after the folder it ran in. `repos` names the members, the branch each clone starts
from, and the repository each one is a clone of:

```json
{
  "repos": {
    "billing-api": {"branch": "develop", "git_origin": "git@example.com:team/billing-api.git"},
    "kubernetes": {"branch": "main", "git_origin": "https://example.com/ops/kubernetes.git"}
  }
}
```

Where a member sits differs from one machine to the next, so its path lives apart from the shared
config, in the gitignored `.box/repos.json` beside it, the way mounts do in `.box/mounts.json`:

```json
{
  "billing-api": "../../billing-api",
  "kubernetes": "~/src/kubernetes"
}
```

Paths are relative to the folder box runs in, and a leading `~` expands. After adding a member, run
`box gen`: it gives every declared name an empty path and lists the `git_origin` to clone each one
from. A name with no path, an empty one, or one the config does not declare is an error. Groups may
share members.

Before anything is created, box checks that each path is a clone of the `git_origin` it is declared
with. `git@host:path`, `ssh://user@host:port/path` and `https://host/path` all name the same
repository, with or without a trailing `.git`; a different host or path is an error naming both URLs
and the full path. Two members with one `git_origin` are an error too.

box then fetches each member — with the terminal attached, so `ssh` can ask for a passphrase — and
packs its commits into a bundle. Each member then becomes a clone inside the sandbox at the same path
it has on your machine, starting on the branch you named, with every `origin/*` branch present and
`origin` set to its `git_origin`. Nothing can be pushed from inside.

A member is never mounted, so only committed work reaches the sandbox; the fetch itself only writes
`origin/*`, so your checkout, your index and your own branches are untouched. box refuses a member
that is not a git repository, has no `origin`, sits inside the repository box runs in, or is covered
by a mount.

A member may carry a `.box/config.json` of its own, and box reads three things from it: its
`required_mounts`, answered by its own gitignored `.box/mounts.json`; its `kit`, passed alongside
the group's, since two kits add up to one allowlist; and its `prompt_file`, appended to the prompt
under the path its clone sits at. Paths in it are relative to the member. Everything else is
ignored — a member's own `repos`, `secret_hosts` and `mcp` included, so groups never nest — except
`template`, which is an error: one sandbox runs one image, so the group's template has to cover
every member.

Mounts reach the sandbox in that order: the group's, then each member's, then any `--mount` flags.
The same path asked for twice is passed once, and a path asked for read-only in one place and `:rw`
in another is an error naming both.

On exit each member's committed work comes back the way the main repository's does: onto a branch a
headless `claude` names, in that member's own repository. A member counts as new whatever none of
its `origin/*` branches hold, so commits you had not pushed yourself do not end up on a sandbox
branch. Every clone has to be committed before the sandbox is removed — a dirty member keeps it,
and the warning names which one.

## Secrets

An HTTP API the agent reads from — GitLab, Sentry, SigNoz — wants a token. `secret_hosts` says
which variable carries which token, and the one host it may be sent to:

```json
{
  "secret_hosts": {
    "GITLAB_TOKEN": "gitlab.com",
    "SIGNOZ_API_KEY": "signoz.example.com"
  }
}
```

The sandbox sees a placeholder in each variable; `sbx` swaps the real value in on its way to that
host and nowhere else, so the value never enters the sandbox. The kit has to allow the host as well,
or the request is a 403 before any token is needed.

The values live on your machine, in a file `BOX_SECRETS_FILE` points at:

```sh
export BOX_SECRETS_FILE=~/.secrets/box.env
```

```
# one NAME=value line per secret
GITLAB_TOKEN=glpat-xxxxxxxxxxxx
SIGNOZ_API_KEY=xxxxxxxx
```

Like `CLAUDE_OAUTH_TOKEN_FILE`, it comes from the environment and from nowhere else, and box reads
it only when the config declares `secret_hosts`. The format is docker's `--env-file`, so the same
file works with `docker run --env-file`: blank lines and `#` lines are skipped, and the value is
everything after the first `=`, quotes included. box refuses a line docker would keep quotes from,
a line that is not `NAME=value`, and a declared name the file has no value for, naming the line
number but never the value. Names the config does not declare are ignored, so one file can serve
several projects.

**Keep that file outside every repository and every mount.** The sandbox can read the whole
repository — sbx mounts it at `/run/sandbox/source`, ignored files and all — and every mount you
give it, so box refuses to start when either secrets file, symlinks resolved, sits inside one. The
same goes for `CLAUDE_OAUTH_TOKEN_FILE`.

Secrets are scoped to the sandbox and dropped when it is removed, exactly like the OAuth token. A
sandbox box keeps because it holds uncommitted work keeps its secrets too. `CLAUDE_CODE_OAUTH_TOKEN`
and `api.anthropic.com` cannot be declared: dropping secrets by host would take box's own token with
them.

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
which mount is at fault. It also refuses while `.box/mounts.json` is not ignored by git, since it
holds paths that exist only on your machine. `box gen` writes that `.gitignore` entry for you.
Ignore that entry only, not the whole `.box/` directory, so `config.json` stays committed.

Every mount is read-only unless you say otherwise, since the agent should not be able to write to
your machine. `~/scratch:rw` opts one path out, and `:ro` is an error, not a synonym for the
default. The `:rw` goes on the path in `.box/mounts.json`, never in the declaration, so a shared
file cannot widen access to your disk. A leading `~` expands as it does in the shell. `--mount`
adds an unnamed workspace for one run. `box config` shows the resulting `sbx` specs.

### Filling them in with an agent

```sh
box mount-prompt
```

The prompt lists every declared mount that has no path yet, with its description and the platform
and architecture a build has to match. Paste it into an **interactive** `claude` session: this agent
runs on your machine, not in a sandbox, so you should see its commands and its diff before approving
them. It is told to probe for each path and check that it exists instead of guessing, and to say
which ones it could not find. Where nothing on this machine fits — the sandbox runs Linux whatever
you run — it downloads a suitable build into `~/.box/deps` (or `$XDG_DATA_HOME/box/deps` when that
is set) and points the mount there. That directory sits outside the project on purpose: a mount
nested inside the workspace deadlocks the sandbox's clone, so never point a mount at a path inside
the repository.

Once every mount has a path, nothing is printed on stdout, so running it again gives an agent no
work to do; box says as much on stderr.

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
| `BOX_SECRETS_FILE is not set` | export it, pointing at a file of `NAME=value` lines for the declared [secrets](#secrets) |
| `which is inside` | move the secrets file out of the repository or the mount it names; the sandbox can read both |
| `has no value for` | add that name to the file `BOX_SECRETS_FILE` points at |
| `quotes its value` | drop the quotes around the value: docker would keep them, so box refuses the line |
| `has no path on this machine for` | give the mount it lists a path in `.box/mounts.json`, or have an agent do it with `box mount-prompt` |
| `is missing a path for` | clone each member it lists, then put where it sits in `.box/repos.json`; `box gen` adds every declared name |
| `is no directory on this machine` | fix that member's path in `.box/repos.json`; it is relative to the folder box runs in |
| `whose origin is` | point that member in `.box/repos.json` at a clone of the `git_origin` in `.box/config.json` |
| `is not a git repository` | a member has to be a git repository with an `origin` remote |
| `has no branch <name> on origin` | set the member's `branch` to one `origin` really has |
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
