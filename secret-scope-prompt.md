# Pass the sandbox to `sbx secret` as `--sandbox`

One small change to `box.py`: stop passing the sandbox name to `sbx secret` as a positional
argument. Do this and nothing else.

## The problem

`box.py` stores and drops the Claude OAuth token with the sandbox name as a positional argument:

- `store_secret` (around `box.py:608`) runs
  `sbx secret set-custom <sandbox> --host api.anthropic.com --env CLAUDE_CODE_OAUTH_TOKEN`, with the
  token on stdin.
- `drop_secret` (around `box.py:603`) runs `sbx secret rm <sandbox> --host api.anthropic.com -f`
  through `capture`, which discards both the output and a failure.

sbx 0.38.0 still accepts that form, but it prints a warning on stderr for each call:

```
Warning: positional sandbox scope is deprecated; use:
  sbx secret set-custom --sandbox <name>
```

`rm` prints the same warning, naming `sbx secret rm --sandbox <name>`. The user sees the
`set-custom` warning on every `box run`. The `rm` warning is hidden by `capture`, and that is the
real risk: once a later sbx drops the positional form, `drop_secret` fails without a word. The
token then outlives its sandbox, scoped to a name that `pick_name` hands out again.

## What was verified on the host

These were checked against the real sbx v0.38.0 with dummy secrets. Do not run `sbx` to recheck
them. Inside a box sandbox it is not available, and the tests fake it anyway.

- `sbx secret set-custom --sandbox <name> --host <host> --env <VAR>`, with the value on stdin, stores
  the secret in scope `<name>`. It prints no warning.
- `sbx secret rm --sandbox <name> --host <host> -f` deletes only the custom secret for that host in
  that scope. A secret for another host in the same scope stays. It prints no warning and exits 0.
- The same `rm` with nothing to delete prints `No custom secrets found ...` and exits 0.
- `sbx secret rm --help` does not list `--host`, but the flag works, and it is what limits the
  removal to one host. Keep it.

Both forms exist in 0.38.0, which is already `SBX_MINIMUM`, so the version floor stays where it is.

## The change

1. **`box.py`**: these two are the only `sbx secret` calls. Confirm that with a grep, then change
   them:
   - `store_secret`:
     `["sbx", "secret", "set-custom", "--sandbox", sandbox_name, "--host", SECRET_HOST, "--env", SECRET_ENV]`
   - `drop_secret`:
     `["sbx", "secret", "rm", "--sandbox", sandbox_name, "--host", SECRET_HOST, "-f"]`

   Nothing else in either function changes. The token still goes over stdin and never onto a
   command line. A failed drop is still ignored. The error messages stay as they are.
2. **`tests/test_box.py`**: update `test_store_secret_names_the_sandbox_the_host_and_the_variable`
   and `test_drop_secret_removes_the_secret_for_one_sandbox` to expect the new lists. The stdin
   test and the missing-sbx and refusing-sbx tests should pass unchanged. If one of them does not,
   find out why before you edit it.
3. **`docs/goal.md`**: add one constraint in the file's own voice, next to the existing secret
   constraints (around line 163). Give the reason, not the mechanics: the sandbox scope reaches
   `sbx secret` as `--sandbox`, because sbx 0.38 deprecates the positional scope and `drop_secret`
   swallows its output, so once sbx drops that form, a token would outlive its sandbox without a
   word.
4. **`README.md`**: no change. No flag, config key or user-facing message changes.

## Out of scope

Everything else about secrets. Add no new secrets. Do not change when a secret is stored or
dropped. Do not move the two commands into builder functions or otherwise restructure the code
around them.

## How to work

Read `CLAUDE.md`, `docs/goal.md` and `docs/style.md` before changing anything. Run the checks
before you start and again once you are done.

- On the host: `uv run pre-commit run -a` and `uv run pytest -q`.
- Inside a box sandbox: the four commands in `docs/sandbox.md` instead, since `pre-commit` cannot
  run there.

## Done when

- Every check passes.
- The change is one commit. Write its subject in this repository's style: imperative, sentence
  case, no prefix. For example: `Pass the sandbox to sbx secret as --sandbox`.
- This prompt file is not part of the commit.
