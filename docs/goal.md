# Goal

`box.py` runs Claude Code inside a disposable Docker sandbox (`sbx`) so an agent can work on a
repository under CPU, memory and disk limits, without touching the host working tree.

One run does this:

1. Load settings from the command line, `.box/config.json` and `.box/mounts.json`, with the
   command line taking priority.
2. Pick the first free `<base>-<n>` sandbox name, checking both running sandboxes and leftover
   `refs/sandboxes/*` git refs.
3. Create the sandbox with `--clone`, so the agent commits into an in-container clone.
4. Store the Claude OAuth token as an `sbx` secret scoped to that sandbox name.
5. Run the agent with `BASE_PROMPT` as its system prompt, followed by the project's own
   `prompt_file` when one is configured, plus the model when one is set.
6. On exit, fetch committed work back to the host and remove the sandbox — unless the sandbox
   still has uncommitted changes, in which case it is kept and recovery steps are printed.

`box gen` instead writes a starter `.box/` directory and exits, without creating a sandbox.

## Constraints

- All code lives in `box.py`. It runs standalone, with a shebang, on Linux and macOS.
- One installed copy serves every project, so everything resolves relative to the current
  working directory and nothing relative to the script's own location.
- Standard library only. The tooling in `pyproject.toml` is for development, never for running.
- Settings come from flags or `.box/config.json` in the current directory, flags first.
- Everything box reads from a project lives under `.box/`, so a project has one box footprint
  rather than a scatter of dotfiles at its root.
- Mounts are declared and supplied separately. `required_mounts` in `.box/config.json` names what
  the project needs and describes what belongs there; `.box/mounts.json` gives each of those names
  a path on this machine. The two must match exactly: a declared name with no path, or a path
  still holding `MOUNT_PLACEHOLDER`, is an error naming the mount and its description, and a name
  the project does not declare is an error the way an unknown config key is.
- Mounts reach `sbx` in declaration order, so the arguments do not depend on how one machine
  ordered its file.
- `:rw` belongs on the path in `.box/mounts.json` and never in the declaration. Whoever owns the
  machine decides what the agent may write to; a description may ask for write access, but
  nothing enforces it.
- Refuse to run while `.box/mounts.json` exists and `git check-ignore` says it is not ignored.
  A committed mounts file carries one machine's paths into every clone of the project.
- `box gen` never changes a value that is already there, so re-running it cannot lose a config or
  a path someone filled in. It adds declared mount names the file is missing, as placeholders it
  warns about, which is how a machine picks up a mount declared after it was set up. It takes no
  flags, since it writes defaults to edit rather than settings that were chosen, and it appends
  the mounts file to `.gitignore`, so what it writes is a project box will run in.
- The OAuth token path is the one exception: it comes from `CLAUDE_OAUTH_TOKEN_FILE` and from
  nowhere else, so a shared project config can never point at someone else's credentials.
- Unknown keys in `.box/config.json` are an error, so typos surface immediately.
- `BASE_PROMPT` stays in `box.py` and holds only what is true of every sandbox. Anything about
  one project belongs in that project's `prompt_file`.
- Never remove a sandbox that holds uncommitted work.
- Extra mounts are read-only unless the user appends `:rw`, so write access to the host is
  always something that was asked for.
- `kit` and `model` have no defaults and are errors when missing. A missing network policy or an
  unnamed model would otherwise be decided silently by `sbx` or by the sandbox's own Claude
  install, which is not this host's.
- Never put the token on a command line; it goes to `sbx secret set-custom` over stdin.
- Store the secret *before* `sbx create`. `sbx` injects the placeholder env var into the sandbox
  at creation time, so a secret stored afterwards leaves `CLAUDE_CODE_OAUTH_TOKEN` unset and the
  agent starts logged out.
- This is a small project. Add a setting or an abstraction only when something needs it.
