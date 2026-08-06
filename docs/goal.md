# Goal

`box.py` runs Claude Code inside a disposable Docker sandbox (`sbx`) so an agent can work on a
repository under CPU, memory and disk limits, without touching the host working tree.

One run does this:

1. Load settings from the command line and `.box.json`, with the command line taking priority.
2. Pick the first free `<base>-<n>` sandbox name, checking both running sandboxes and leftover
   `refs/sandboxes/*` git refs.
3. Create the sandbox with `--clone`, so the agent commits into an in-container clone.
4. Store the Claude OAuth token as an `sbx` secret scoped to that sandbox name.
5. Run the agent, passing through the system prompt file and model when configured.
6. On exit, fetch committed work back to the host and remove the sandbox — unless the sandbox
   still has uncommitted changes, in which case it is kept and recovery steps are printed.

## Constraints

- All code lives in `box.py`. It runs standalone, with a shebang, on Linux and macOS.
- Standard library only. The tooling in `pyproject.toml` is for development, never for running.
- Settings come from flags or `.box.json` in the current directory, flags first.
- The OAuth token path is the one exception: it comes from `CLAUDE_OAUTH_TOKEN_FILE` and from
  nowhere else, so a shared project config can never point at someone else's credentials.
- Unknown keys in `.box.json` are an error, so typos surface immediately.
- Never remove a sandbox that holds uncommitted work.
- Never put the token on a command line; it goes to `sbx secret set-custom` over stdin.
- This is a small project. Add a setting or an abstraction only when something needs it.
