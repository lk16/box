# Secrets

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

A value reaches sbx on stdin, never on a command line, so it does not land in the shell history or
in the process list.
