# Goal

`box.py` runs Claude Code inside a disposable Docker sandbox (`sbx`) so an agent can work on a
repository under CPU, memory and disk limits, without touching the host working tree.

One run does this:

1. Load settings from the command line, `.box/config.json` and `.box/mounts.json`, with the
   command line taking priority.
2. Pick the first free `<base>-<n>` sandbox name, checking both running sandboxes and leftover
   `refs/sandboxes/*` git refs.
3. Create the sandbox with `--clone`, so the agent commits into an in-container clone. The disk
   limits reach `sbx` as `DOCKER_SANDBOXES_ROOT_SIZE` and `DOCKER_SANDBOXES_DOCKER_SIZE` on its
   environment rather than as flags, so box overrides either one the user already exported.
4. Store the Claude OAuth token as an `sbx` secret scoped to that sandbox name.
5. Run the agent with `BASE_PROMPT` appended to its system prompt, followed by the project's own
   `prompt_file` when one is configured, plus the model when one is set.
6. On exit, fetch committed work back to the host, put it on a branch a headless Claude names,
   and remove the sandbox — unless the sandbox still has uncommitted changes, in which case it is
   kept and recovery steps are printed.

A group session does the same for several repositories at once: `repos` names the members, each one
is fetched and bundled into the sandbox as a clone of its own before the agent starts, and each one
gets its committed work back at the end.

`box run` does that. Every command starts with a word, so nothing happens by accident when a flag
is mistyped. The others all exit without creating a sandbox: `box config` prints the settings in
effect, `box gen` writes a starter `.box/` directory, `box mount-prompt` prints the prompt
that has an agent on this host fill in the mounts `.box/mounts.json` still leaves as placeholders,
and `box self-update` writes the published `box.py` over the running one.

## Constraints

[README.md](../README.md) is where what box does is written down, for the people who use it. This
file says why, and is the authority when the two disagree. So a rule the README spells out — the
settings table, the mount contract, what a run hands back — is here as the reason for it rather than
as a second copy of the mechanics, which is how the two came to disagree about a timeout once.

- All code lives in `box.py`. It runs standalone, with a shebang, on Linux and macOS. The shebang
  takes whatever `python3` comes first, which on a stock macOS is Xcode's 3.9, so the floor sits at
  3.9 and box runs there unmodified; on anything older it names the version it needs and stops
  rather than breaking somewhere obscure. Nothing in the file needs newer syntax than 3.9 has,
  which is what lets that message reach the reader at all.
- One installed copy serves every project, so everything resolves relative to the current
  working directory and nothing relative to the script's own location. The update check is the
  one exception: it hashes the script's own file, which is the only thing it can compare.
- box.py carries no version, so being current means hashing the same as the published copy. A
  timestamp under `XDG_CACHE_HOME` keeps the hour after a check quiet, so the notice appears as
  often as the check does and no more: an alert on every command is noise, not news. It prints on
  stderr so a piped stdout stays clean, in red only when stderr is a terminal and `NO_COLOR` is
  unset, since a pipe or a log file would otherwise be handed the escape codes as characters. box
  follows XDG on macOS as well as on Linux, the way uv, ruff, pip and gh do, rather than splitting
  the cache path per platform. `BOX_UPDATE_URL` names the copy to compare with, so a fork or a
  vendored copy is not nagged about box's own `main`, and an empty value costs no round trip at all.
  Every failure is swallowed: an unreachable GitHub, a broken cache or a missing home directory must
  never stop a command that would otherwise work.
- `box self-update` takes the update the check found, since the alternative is a `curl` line to
  copy. It writes beside the running script and moves the new copy over it, so a failed download or
  a directory it cannot write leaves the working box exactly as it was, and it refuses a `box.py`
  git tracks: that copy is someone's work in progress, and git is how it is updated.
- Standard library only. The tooling in `pyproject.toml` is for development, never for running.
  Its `version` is inert -- uv refuses a `[project]` table without one -- and says nothing about
  box, which has no version at all.
- Settings come from flags or `.box/config.json` in the current directory, flags first.
- `config` takes the same flags as `run` and resolves the same settings, so it answers "what
  would run do" and validates the project's files without creating anything. `gen` and
  `mount-prompt` work on those files instead of reading settings, so they reject every flag.
- Everything box writes to a project lives under `.box/`, apart from the `.gitignore` lines it
  needs, so a project has one box footprint rather than a scatter of dotfiles at its root. It
  reads two things from outside it: the `prompt_file` at whatever path names it, and the `kit`.
- Mounts are declared and supplied separately, because what a project needs is the same everywhere
  and where it sits is not. The two must match exactly, rather than a missing path being skipped,
  since a mount that is quietly absent becomes a failure inside the sandbox with nothing pointing
  back here, and a name nobody declared is a typo. The contract itself is in the README.
- Mounts reach `sbx` in declaration order, so the arguments do not depend on how one machine
  ordered its file.
- `:rw` belongs on the path in `.box/mounts.json` and never in the declaration. Whoever owns the
  machine decides what the agent may write to; a description may ask for write access, but
  nothing enforces it. Read-only is the default everywhere, so write access to the host is always
  something that was asked for.
- `~/.box/deps` — `$XDG_DATA_HOME/box/deps` when that is set, resolved by `deps_path()` — holds
  what this machine cannot supply, such as a Linux toolchain on macOS, since the sandbox runs
  Linux whatever the host runs and a build that exists only to be mounted belongs to the machine,
  not to any project. It sits outside every repository deliberately: `sbx create --clone` wipes
  the in-container workspace before cloning into it, and a mount nested inside the workspace
  turns that wipe into a deadlock, EROFS on every file with a mountpoint that cannot be removed.
  The agent answering `mount-prompt` creates it; box never downloads into it. A dependency
  reaches the sandbox by being mounted, not by sitting in the project, because the container gets
  a clone and a clone holds no ignored files. The host repository is another matter: sbx mounts it
  read-only at `/run/sandbox/source`, ignored files included, so a project directory is no place
  for a secret even when git never sees it.
- Refuse to run while `.box/mounts.json` exists and `git check-ignore` says it is not ignored,
  since it would carry a machine's paths into every clone.
- `box gen` writes a starter kit at `.box/kit/spec.yaml` and points `kit` at it, since a hand-written
  network policy is the biggest step in setting box up and "the agent's own API calls and nothing
  else" is where most projects start. It says in the file that it is a starting point, and holds only
  the few fields box is sure of, because the schema is sbx's and can drift. It lives under `.box/`
  like everything else box writes; box's own kit predates it and stays in `.sbx/kit`.
- `box gen` never changes a value that is already there, so re-running it cannot lose a config or
  a path someone filled in. It adds declared mount names the file is missing, as placeholders it
  warns about, which is how a machine picks up a mount declared after it was set up. It takes no
  flags, since it writes defaults to edit rather than settings that were chosen, and it appends
  the mounts file to `.gitignore`, so what it writes is a project box will run in.
- Prompt text lives in `box.py`, next to `BASE_PROMPT`, so one installed script stays the whole
  of box. `mount-prompt` writes for an agent with a shell on this host: it may run commands to
  find a path and must check it exists, never guess one, and never add `:rw` unless a description
  asked for it.
- `mount-prompt` prints to stdout, and the user hands the prompt to an interactive agent session
  themselves. The agent runs commands on the host and edits a file that points at the user's own
  directories, so the tool calls and the diff belong in front of them to approve. It offers the
  deps directory as the third outcome, after finding a path and giving up, and tells the agent
  never to point a mount inside the project, since a mount nested in the workspace deadlocks the
  clone's wipe. It asks about every declared mount without a path, whether the key is absent or
  holds the placeholder, so it follows a declaration directly. Printing nothing on stdout when
  every mount has a path keeps a second run from asking for done work; it says so on stderr, so
  stdout stays nothing but a prompt to hand over.
- The OAuth token path is the one exception: it comes from `CLAUDE_OAUTH_TOKEN_FILE` and from
  nowhere else, so a shared project config can never point at someone else's credentials.
  `BOX_SECRETS_FILE` names the values behind `secret_hosts` for the same reason: the config says
  which variable may reach which host, which is the same everywhere, and the file holding the
  values is this machine's alone.
- Neither of those files may sit inside the repository box runs in or inside any mount, symlinks
  resolved. The sandbox reads the whole repository at `/run/sandbox/source` and every mount at its
  own path, so a secret kept there is a secret the agent can read for itself, and no scoping by
  host would help.
- The values file follows docker's `--env-file` rules, since the same file is what a `docker run`
  on this host would read. That means the value is everything after the first `=`, verbatim, so box
  refuses a quoted value rather than handing the quotes to sbx as part of it, and refuses a line
  docker would reject outright. A rejected line is named by its number and never by its content.
  Names the config does not declare are ignored, so one file can serve several projects.
- A secret is scoped to the sandbox and dropped with it, the way the token is, and
  `CLAUDE_CODE_OAUTH_TOKEN` and `api.anthropic.com` are refused as declarations: secrets are
  dropped by host, so a project naming either would drop box's own token along with its own.
- A value reaches sbx on stdin and lives in the sandbox as a placeholder sbx swaps in at its own
  proxy, so the real value never enters the sandbox and never appears on a command line.
- Unknown keys in `.box/config.json` are an error, so typos surface immediately.
- `BASE_PROMPT` stays in `box.py` and holds only what is true of every sandbox. Anything about
  one project belongs in that project's `prompt_file`, and anything about one machine or one host
  operating system belongs nowhere: the sandbox is Linux, but where a host directory is mounted is
  not something the prompt can assert.
- `BASE_PROMPT` tells the agent it is unattended while `box run` opens an *interactive* session, and
  both are deliberate. The session is interactive so the user can watch what the agent does and step
  in; the prompt tells it to assume and keep going because the user may well walk away, and an agent
  waiting on a question they never see wastes the whole run.
- Never remove a sandbox that holds uncommitted work. Its refs are left as they are too, since
  the run is not over: the work is fetched again once the sandbox is clean.
- A `refs/sandboxes/*` ref is where the fetch lands, not where work is left. A ref holding
  commits becomes a branch named by `claude -p`, and is dropped once that branch exists; a ref
  holding nothing new is dropped without one, since there is nothing to name.
- Naming a branch is a courtesy, so every way it can fail -- no `claude` on `PATH`, a non-zero
  exit, ten seconds of silence, an empty answer, a name git refuses, a ref git cannot read --
  keeps the ref instead. The work is already fetched, and a kept ref is what the user had before.
- The branch name is the last line `claude` printed, kebab-cased and cut to five words, so a model
  that answers with a sentence still produces a name rather than an error. A name the repository
  already has takes a `-2`, `-3` suffix, since a fetched commit must never overwrite a branch.
- A directory git does not read as a repository, or a repository with no commits, is an error
  naming what to do about it. `sbx create --clone` clones the working directory, so neither gives
  the agent anything to work from, and the second leaves every fetched ref with no HEAD to settle
  against.
- `sbx`, `git` and `claude` are checked on `PATH` before anything else, since nothing box does works
  without them and a missing one would otherwise surface as whichever call reached it first.
- The `sbx` on `PATH` must be 0.38.0 or newer, read from `sbx version` before a command creates or
  writes anything, since the kits box writes use the spec layout that release introduced. A version
  line box cannot read is left alone: it is no evidence of an old sbx, and stopping on it would
  break a working machine over a change in how `sbx` spells its release.
- A project with no `.box/config.json` at all is an error naming `box gen`, before any setting is
  mentioned. "kit is not set" answers the wrong question for someone who has not set box up yet.
- `kit` and `model` have no defaults and are errors when missing. A missing network policy or an
  unnamed model would otherwise be decided silently by `sbx` or by the sandbox's own Claude
  install, which is not this host's.
- `template` is optional where those two are not, because what `sbx` picks unasked is an image for
  the agent it is launching, which is neither a policy nobody chose nor a foreign model. So an
  unset template passes no flag at all and the create command stays what it was. It is set when the
  project needs a toolchain the agent image has no way to install under its own kit, and the answer
  is a derived image rather than a mounted sysroot: one `apt-get` on sbx's own base matches its
  glibc exactly and leaves `LD_LIBRARY_PATH` alone, where a mounted toolchain's library directory
  shadows the image's OpenSSL and curl and breaks TLS for everything. box names the image and
  nothing more -- it never builds or loads one, and never checks that one exists, since `sbx` owns
  that and fails clearly at create time.
- A group session is one sandbox working on several repositories: the one box runs in, plus the
  members `repos` names. The constraints that shape it:
  - Members reach the sandbox as bundles. `sbx create --clone` clones the first path it is given
    and mounts every other one as it is, and a mount would hand over everything a member holds,
    ignored files included. A bundle carries committed history and nothing else, which is exactly
    what a clone is made from, so a member is never mounted and a mount holding one is refused.
  - box fetches every member before it bundles one, with the terminal attached so ssh can ask for
    a passphrase, and refuses a base the fetch did not produce. Current data is the whole point of
    the session, and a fetch writes the `origin/*` refs and nothing else, so the user's own
    checkout, index and branches are left exactly as they were.
  - Each member names the branch its clone starts from, because `origin/HEAD` is written when a
    clone is made and goes stale, and guessing `main` for a repository living on `develop` would
    start every session in the wrong place.
  - A member counts new commits against every `origin/*` branch, where the repository box runs in
    keeps counting against `HEAD`. The host checkout of a member is whatever the user left it on,
    so `HEAD..` there would name commits the sandbox never made; the repository box runs in is
    cloned from that same checkout, so comparing with `HEAD` is what keeps unpushed local commits
    from getting a branch of their own.
  - A member's clone sits at the member's own host path inside the sandbox, so a tool that prints
    a path names something the user recognises, and so two members can never collide.
  - A member's own `.box/config.json` is read for the three things a sandbox has to hold for it:
    what it needs mounted, the hosts its work reaches, and what an agent has to know about it. Its
    mounts follow the same declare-and-supply contract, checked in the member itself, and every
    message names the member so a group of ten says which one is at fault. Its kit is passed
    alongside the group's, since two kits are one allowlist, and its prompt comes after the section
    naming the members, headed with the path its clone sits at.
  - Everything else in a member's config is ignored, its own `repos`, `secret_hosts` and `mcp`
    included, so a group never nests and reading one is never recursive. A `template` is the one
    exception and an error: a sandbox runs one image, so the group's has to cover every member.
  - Mounts reach sbx in one order: the group's own, then each member's in the order `repos` names
    them, then the `--mount` flags, which are this run's rather than anyone's settings. The same
    spec twice is passed once, since two groups sharing a member would otherwise ask for one path
    twice, and one path asked for read-only in one place and writable in another is an error: only
    one of the two can hold, and picking either silently would surprise whoever asked for the other.
  - Every repository a run can leave work in is read when a sandbox name is picked, and every one
    of them is fetched, checked for uncommitted work and settled before anything is removed. A
    sandbox is only ever removed when all of them came back clean.
- `mcp` names servers the user registered with `sbx mcp add`, and box passes the names on as
  `--static-mcp` and nothing more, the way it names a template. Registering one starts a process on
  this host holding the user's own access to a database or a cluster, which is the machine owner's
  call rather than a project's, and box neither registers, checks nor starts one: an unknown name is
  a failed create, which box already reports. The names are a JSON list, and one holding a comma is
  refused, since sbx takes them all as one comma-separated argument and would split it in two.
- `box gen` writes a single project's settings and never the keys only a group needs, so a config it
  writes today still runs on a box from before groups existed. It asks one question first -- one
  project or a group -- and only where a config is missing and stdin is a terminal, so it still
  takes no flags and a script still gets today's defaults without answering anything. The question
  picks which defaults to write and nothing else; a group's members are still typed into the file
  afterwards, which is what the line gen prints says. An input that ends without an answer writes
  nothing at all, since guessing at that point is how a folder ends up set up as the wrong thing.
- Settings come from the working directory's `.box/`, but `sbx create --clone` is given the
  repository root, since it refuses a path that is not a repository of its own. So a folder below
  the root is a place to keep settings, and the sandbox still gets the whole repository. The
  sandbox base name follows the working directory too, so sibling folders name their own sandboxes.
  The agent starts at the root, which is the path `sbx create` was given, so one generated line
  names the folder the session was started from; the dirty check and the recovery lines address
  that same root.
- A failed `sbx create` is reported, not raised. `sbx` has already said why, there is no sandbox
  to clean up or keep, and the stored secret is dropped again, so a failure leaves nothing
  behind and no traceback in front of the reason.
- `kit` must name a directory when it names anything on disk, since `sbx` reads a non-directory
  as a zip artifact and fails with a message about zip files. A `kit` that is not on disk is a
  reference `sbx` resolves itself and is left alone.
- Never put the token on a command line; it goes to `sbx secret set-custom` over stdin.
- The sandbox scope reaches `sbx secret` as `--sandbox`, never as a positional argument. sbx 0.38
  deprecates the positional form, and `drop_secret` throws its output away, so the day sbx removes
  it a token would outlive its sandbox without a word, scoped to a name `pick_name` hands out again.
- Store the secret *before* `sbx create`. `sbx` injects the placeholder env var into the sandbox
  at creation time, so a secret stored afterwards leaves `CLAUDE_CODE_OAUTH_TOKEN` unset and the
  agent starts logged out.
- This is a small project. Add a setting or an abstraction only when something needs it.
