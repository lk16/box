# Group sessions and read-only tools

Implement the box side of group sessions. A group session is one sandbox that works on several
related repositories and can use read-only tools. This file is the whole brief, and nothing else
describes the design.

Read `CLAUDE.md`, `docs/goal.md`, `docs/style.md` and `docs/sandbox.md` before changing anything.

## What a user will do with this

A team keeps one repository of box setups, with a subfolder per group of related repositories:

```
~/work/
  boxes/                    one git repo, shared by the team
    billing/.box/config.json
    search/.box/config.json
  billing-api/              the member repositories sit next to boxes
  billing-worker/
  kubernetes/
```

`cd ~/work/boxes/billing && box run` starts one sandbox:
- **Members:** the agent gets a fresh clone of every member repository.
- **Coming back:** it can commit on branches in any of them. Every branch with new work comes back
  to the host as a branch in that repository.
- **Overlap:** groups may share members.

The same session can use read-only tools:
- **HTTP APIs** (GitLab, Sentry, SigNoz) get a token through the sbx proxy, so the real value never
  enters the sandbox.
- **Other tools** (Postgres, Kubernetes) run on the host as MCP servers. The user registers them
  with `sbx mcp add`, and box only names them.

The `boxes` repository, the tokens, the MCP servers and any image are the user's to set up. They
are not part of this work.

## Rules for the whole change

- **Stay backwards compatible.** A config without `repos`, `secret_hosts` and `mcp` must produce the
  same sbx commands, system prompt and cleanup as today. There are exactly two accepted
  differences: `box config` prints the new settings, and a `CLAUDE_OAUTH_TOKEN_FILE` inside a
  mounted path is now refused (step 3).
- **Keep `box gen`'s single-repo output the same.** At every step, it writes exactly what it writes
  today. New keys only appear in the group starter (step 6), so a fresh single-repo config still
  works with an older box.
- **Toolchain:** standard library only, Python 3.9, and everything in `box.py`.
- **Commits:** one per step, in order. Each commit leaves the checks passing and box working, and
  carries its own README and `docs/goal.md` changes.
- **Don't run `sbx`.** The tests fake sbx and git, and the facts below were verified on a host.
- **Secrets never appear on a command line or in a message.** Values go to sbx over stdin.
- **Paths go to `sbx exec` as arguments.** Never paste them into a shell string.
- **Wording is yours.** Messages and the generated prompt text should match the style of the
  existing ones: short and plain.

The code to start from in `box.py`: `build_create_command`, `build_system_prompt`,
`store_secret` and `drop_secret`, `prepare_launch`, `run_session`, `cleanup`, `settle_ref`,
`taken_names`, `order_mounts`, `require_ignored_local_paths`, `format_config` and `generate`.

## Verified facts (sbx v0.38.0, on the host)

1. **Subfolders:** `sbx create --clone` from a subfolder of a repository fails with
   `ERROR: --clone requires a Git repository, but <dir> is not in a Git repository`. From the
   repository root it works.
2. **The primary path:** in clone mode, the agent's working directory is the path given to
   `sbx create`. That's `WORKSPACE_DIR`, and also where `sbx exec` starts. The path is mounted
   read-only at `/run/sandbox/source`, ignored files included. The clone itself sits at the path's
   host location and holds no ignored files.
3. **Users:** `sbx exec` runs as the user `agent` by default. `sbx exec -u root <sandbox> <command>`
   runs as root; the flags go before the sandbox name. `sbx exec` starts a stopped sandbox first.
4. **Copying files:** right after `sbx create`, `sbx cp <host file> <sandbox>:/tmp/<file>` works,
   and so does `sbx cp <sandbox>:<path> <host path>`.
5. **Member paths:** a member's host path does not exist in the sandbox. When the member sits next
   to the main repository, its parent exists and belongs to `agent`. Anywhere else, the parent may
   belong to root. Then `git init` as `agent` fails with `Permission denied`. It works after
   `mkdir -p <path>` and `chown agent:agent <path>` as root.
6. **Clone sequence:** this gives a clone on the fresh base, with every `origin/*` branch present:
   ```
   git init -q P
   git -C P remote add origin URL
   git -C P fetch -q BUNDLE 'refs/remotes/origin/*:refs/remotes/origin/*'
   git -C P switch -q -c develop --track origin/develop
   ```
7. **Bundles and counting:**
   - On the host, `git bundle create F --remotes=origin` holds `refs/remotes/origin/*` only.
   - In the sandbox, `git bundle create F --branches` holds the local branches.
   - On the host, fetching that bundle with `+refs/heads/*:refs/sandboxes/<sandbox>/*` works.
   - With the host checked out on an older local branch, an untouched `develop` counted 0 new
     commits with `git rev-list --count <ref> --not --remotes=origin`, but 2 with `HEAD..<ref>`.
     A branch with one agent commit counted 1 and 3.
8. **Fetch leaves the checkout alone:** `git fetch origin` ran in a member on another branch, with a
   dirty file and an untracked file. It moved only `origin/*`. `HEAD`, the index, the working tree
   and the local branches were unchanged.
9. **Two kits:** with two `--kit` flags, both kits' startup steps ran. `sbx policy ls <sandbox>`
   listed both kits' hosts in one allowlist.
10. **Custom secrets:**
    - `sbx secret set-custom --sandbox S --host H --env E` reads the value from stdin, and the
      sandbox sees a placeholder in `E`.
    - `sbx secret rm --sandbox S --host H -f` removes only the custom secrets for `H` in `S`. It
      exits 0 when there is none.
    - Neither `sbx create` nor `sbx run` has a flag that sets an environment variable.
11. **Docker's env-file format:** `docker run --env-file` keeps everything after the first `=`
    verbatim, quotes included: `A='x'` gives `'x'`. `export A=x` or `A = x` makes Docker reject the
    whole file. `#` lines are comments.
12. **MCP names:** `sbx create --static-mcp` takes a comma-separated list of servers registered with
    `sbx mcp add`.
13. **Extra paths:** `--clone` clones only the first path given to `sbx create`. Extra paths are
    mounted as they are.

## Step 1: `mcp`

- **The setting:** a text setting like `template`. Key `mcp`, flag `--mcp NAMES`, default empty,
  shown by `box config`.
- **What it holds:** the names of MCP servers the user registered with `sbx mcp add`,
  comma-separated.
- **What box passes:** when set, `sbx create` gets `--static-mcp <value>` (fact 12). When empty, no
  flag at all.
- **What box doesn't do:** register, check or start a server. sbx fails at create time on an unknown
  name, and box already reports a failed create.
- **goal.md:** say why box only passes the names, as it does for `template`. Registering a server
  starts a process on the host with the user's own access, which is the machine owner's call.
- **README:** a settings table row and a few lines.

## Step 2: running from a subfolder

Today `box run` in a subfolder of a repository fails: box's own checks pass, then `sbx create
--clone` refuses (fact 1). Making it work breaks nobody.

- **The path sbx gets:** when the working directory is below the repository root, pass the root to
  `sbx create` instead of `.`. `git rev-parse --show-toplevel` finds it.
- **What stays with the subfolder:** settings still come from the working directory's `.box/`. The
  sandbox base name still comes from the working directory's name, e.g. `billing`.
- **The prompt:** the agent starts at the repository root (fact 2). When the root and the working
  directory differ, append one line to the system prompt naming the folder the session was started
  from, relative to the root.
- **Cleanup:** the `git status` check and the recovery lines use the root.
- **At the root itself:** nothing changes.
- **Docs:** goal.md and README.

## Step 3: `secret_hosts` and `BOX_SECRETS_FILE`

### The config

`secret_hosts` is a config key only, like `required_mounts`. It's an object from an environment
variable name to the one host the value may be sent to:

```json
"secret_hosts": {
  "GITLAB_TOKEN": "gitlab.com",
  "SIGNOZ_API_KEY": "signoz.example.com"
}
```

- **Names:** must be valid environment variable names.
- **Hosts:** one exact host or sbx wildcard pattern, never empty.
- **Reserved:** `CLAUDE_CODE_OAUTH_TOKEN` and the host `api.anthropic.com` are refused. box's own
  token uses them, and dropping secrets by host would drop the token too.

### The values

`BOX_SECRETS_FILE` names the file that holds the values. It comes from the environment only, for
the same reason as `CLAUDE_OAUTH_TOKEN_FILE`. box reads it only when `secret_hosts` has entries,
and ignores it otherwise.

The file follows Docker's `--env-file` rules (fact 11), since the same file is meant for
`docker run --env-file` on the host:
- **Skipped:** blank lines, and lines starting with `#`.
- **Every other line** is `NAME=value`. The value is everything after the first `=`, verbatim.
- **Refused**, naming the file and the line number but never the value:
  - a line without `=`
  - a name that isn't a valid variable name, which catches `export NAME=` and spaces around `=`
  - a value wrapped in matching quotes, since Docker would keep the quotes as part of it

Also errors:
- `secret_hosts` has entries but `BOX_SECRETS_FILE` is unset. The message says how to set it up.
- The file is missing.
- A declared name is absent from the file, or has an empty value.

Names in the file that the config doesn't declare are ignored, so one file can serve several
groups.

### Where the file may live

Refuse to run when the secrets file, with symlinks resolved, lies inside any of these:
- **the repository box runs in, from its root:** the sandbox can read all of it (fact 2)
- **any mount:** declared or given with `--mount`. Steps 4 and 5 add member repositories and member
  mounts to this list.

Apply the same check to `CLAUDE_OAUTH_TOKEN_FILE`. The message names the file and the directory,
and says the sandbox can read everything there.

### Each run

This is the same path the token takes (fact 10):
- **Before `sbx create`:** for each entry, `sbx secret set-custom --sandbox <sandbox> --host <host>
  --env <NAME>`, with the value on stdin. Drop leftovers first, as the token does.
- **At cleanup, and after a failed create:** `sbx secret rm --sandbox <sandbox> --host <host> -f`,
  once per distinct host. A kept sandbox keeps its secrets, as it keeps the token.

### `box config` and docs

- **`box config`:** prints the file path and each name with its host, never a value. It runs every
  check above.
- **goal.md:** add the constraints and their reasons: why the path comes from the environment only,
  why the file must never be under a mounted path, why Docker's format, and why secrets are dropped
  with the sandbox. Also fix the line saying a clone has no ignored files. The clone has none, but
  the host repository is readable at `/run/sandbox/source`, ignored files included (fact 2), so a
  project must never hold a secret.
- **README:** a section and troubleshooting rows. Say that the kit must also allow each host.

## Step 4: `repos`

### The config

`repos` is a config key only. It's an object from a path to the branch on `origin` that the clone
starts from:

```json
"repos": {
  "../../billing-api": "develop",
  "../../kubernetes": "main"
}
```

Paths are relative to the working directory, and `~` expands.

### Checks before anything is created

These run in `box config` too. Each refusal names the repository:
- an empty base
- a path that doesn't exist, isn't a git repository, or has no `origin` remote
- a path at or inside the repository box runs in. Its clone would sit inside the main clone and
  make it look dirty.
- two paths that are the same repository
- a mount at or above a member's path. That would expose the member's ignored files; members only
  ever come in as bundles.

### Fetch and bundle

This happens on `box run` only, not in `box config`, still before anything is created:
1. **Fetch:** run `git -C <path> fetch origin` for each member, one at a time, with the terminal
   attached so SSH can ask for a passphrase or a key touch. Print which repository is being fetched.
   A failure stops the run, with git's message and the repository's path. A fetch only writes the
   `origin/*` refs and objects (fact 8). box never runs pull, merge, checkout or reset on a member.
2. **Check the base:** refuse when `refs/remotes/origin/<base>` doesn't exist after the fetch.
3. **Bundle:** run `git -C <path> bundle create <file> --remotes=origin` (fact 7). The files go in a
   temporary directory that box removes when the run ends.

### Setting up the clones

After `sbx create` succeeds and before `sbx run`, do this for each member (facts 3 to 6):
1. `sbx cp` the bundle into the sandbox.
2. As root, create the member's host path and hand it to the default user.
3. As the default user, run the sequence from fact 6. Set `origin` to the member's origin URL on the
   host, so tools like glab can tell which project it is.

If any of this fails, the sandbox holds nothing yet. Remove it, drop the secrets, and say which
repository failed.

### The system prompt

When `repos` has entries, append a generated section after the project's prompt. It says:
- where each member is (its host path)
- which branch each started on, and that it was fetched just now
- that every `origin/*` branch is there too
- that nothing can be pushed
- that each branch with new commits comes back to the host as a branch in that repository

`BASE_PROMPT` itself doesn't change.

### Cleanup

In this order:
1. **Fetch everything first.** The main repository comes back as today. Then for each member:
   - inside the sandbox, run `git -C <path> bundle create /tmp/<file> --branches`
   - `sbx cp` the bundle out
   - on the host, run `git -C <path> fetch <bundle> '+refs/heads/*:refs/sandboxes/<sandbox>/*'`

   Any failure keeps the sandbox and prints recovery lines that name the repository.
2. **Check for uncommitted work:** `git status --porcelain` in the main clone, as today, and in
   every member clone. Any dirty clone keeps the sandbox, and the warning names it.
3. **Settle refs:**
   - The main repository settles exactly as today.
   - Members use the same code, run in the member, but count and list new commits with
     `<commit> --not --remotes=origin` instead of `HEAD..<commit>`. Fact 7 shows why.
   - The main repository keeps `HEAD..`, so local commits the user hasn't pushed never get a branch
     of their own.
4. **Finish:** drop the secrets and remove the sandbox, as today.

### Elsewhere

- **`taken_names`:** also read `refs/sandboxes/*` in every member, so a new sandbox never takes a
  name whose refs are still waiting in a member.
- **`box config`:** shows each member with its base.
- **goal.md:** add the constraints and their reasons:
  - Members come in as bundles because sbx clones only the first path (fact 13), and a bundle
    carries committed history only.
  - box fetches first and refuses stale data, because current data is the point.
  - The base is required because `origin/HEAD` is written at clone time and goes stale.
  - Members count new commits against `origin/*`; the main repository keeps comparing with `HEAD`.
  - Members are never mounted.
- **README:** a section on groups, with the layout above.

## Step 5: member settings

For each member that has a `.box/config.json`, read it with the usual rules. It comes from the
member's working tree on the host, and unknown keys are an error naming that file.

- **Mounts:** the member's `required_mounts` and its `.box/mounts.json`. They get the same
  exact-match contract and the same "must be ignored by git" check, run in the member.
  - Messages scope each name to its member, e.g. `../../billing-api: go_toolchain`.
  - Order: the group's own mounts first, then each member's in `repos` order, then `--mount` flags
    last.
  - The same spec twice is passed once.
  - The same path read-only in one place and read-write in another is an error naming both.
- **Prompt:** the member's `prompt_file`, relative to the member. It's appended after the generated
  section, headed with the member's path.
- **Kit:** the member's `kit`, relative to the member when it's on disk. It's passed as another
  `--kit` after the group's (fact 9). The same kit twice is passed once. The existing "kit must be a
  directory" check applies.
- **Template:** a member with a `template` is an error. A sandbox runs one image, so the group's
  template has to cover the member.
- **Everything else** in a member's config is ignored, including its own `repos`, `secret_hosts`
  and `mcp`. That way groups never nest.
- **No config:** a member without `.box/config.json` contributes nothing.

The secrets location check from step 3 covers member mounts.

Docs: goal.md and README.

## Step 6: `box gen` asks what the folder is for

When `.box/config.json` doesn't exist yet and stdin is a terminal, ask one question before writing
anything. Use very simple words, for example:

```
Is this for one project, or for a group of projects?
  1  one project: the code in this folder
  2  a group: several projects next to this folder
Type 1 or 2 (Enter means 1):
```

- **1, or Enter:** write exactly today's starter config.
- **2:** write today's starter plus `"repos": {}`, `"secret_hosts": {}` and `"mcp": ""`. Then print
  one simple next step, e.g.
  `Next: list each project under "repos" in .box/config.json, like "../api": "main" (the branch to start from).`
- **Anything else:** ask again.
- **End of input:** stop and write nothing.
- **No terminal, or a config already there:** behave exactly as today, without asking.

goal.md: `box gen` still takes no flags. The one question picks which defaults to write, and it's
only asked when there is someone to ask. Also update the README.

## Done when

- All six steps are committed in order, each with its tests and docs.
- The checks in `docs/sandbox.md` pass.
- A run whose config has none of the new keys behaves as before, apart from the two accepted
  differences.
- This file is unchanged. It is removed in a separate commit.
