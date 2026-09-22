# Groups

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

A machine that does not have a member at all answers it with `null` rather than a path:

```json
{
  "billing-api": "../../billing-api",
  "kubernetes": null
}
```

That member is left out of the whole run: it is never checked, fetched, cloned into the sandbox or
fetched back from it, and nothing it declares of its own is read. box says on stderr which members
it is running without, and the agent is told they are declared but not there, so it can say what it
could not do. `box gen` never writes a `null` itself — it writes the empty path that is an error
until someone fills it in — and it keeps a `null` that is already there.

## What box checks and does

Before anything is created, box checks that each path is a clone of the `git_origin` it is declared
with. `git@host:path`, `ssh://user@host:port/path` and `https://host/path` all name the same
repository, with or without a trailing `.git`; a different host or path is an error naming both URLs
and the full path. Two members with one `git_origin` are an error too, since two clones of one
repository would bring their work back over each other.

box then fetches every member at once and packs each one's commits into a bundle. The fetches share
no terminal, so none of them can ask for a passphrase and none of them prints over another: each is
reported under its own name once they are all in, in the order `repos` names them. A fetch that
failed is asked again on its own with the terminal, which is where `ssh` gets to ask. See
[fetching](fetching.md). Each member then becomes a clone inside the sandbox at the same path
it has on your machine, starting on the branch you named, with every `origin/*` branch present and
`origin` set to its `git_origin`. Nothing can be pushed from inside.

A member is never mounted, so only committed work reaches the sandbox; the fetch itself only writes
`origin/*`, so your checkout, your index and your own branches are untouched. box refuses a member
that is not a git repository, has no `origin`, sits inside the repository box runs in, or is covered
by a mount.

## A member's own settings

A member may carry a `.box/config.json` of its own, and box reads three things from it: its
`required_mounts`, answered by its own gitignored `.box/mounts.json`; its `kit`, passed alongside
the group's, since two kits add up to one allowlist; and its `prompt_file`, appended to the prompt
under the path its clone sits at. Paths in it are relative to the member. Everything else is
ignored — a member's own `repos`, `secret_hosts` and `mcp` included, so groups never nest — except
`template`, which is an error: one sandbox runs one image, so the group's template has to cover
every member.

Mounts reach the sandbox in that order: the group's, then each member's, then any `--mount` flags.

## What comes back

On exit each member's committed work comes back the way the main repository's does: onto a branch a
headless `claude` names, in that member's own repository. A member counts as new whatever none of
its `origin/*` branches hold, so commits you had not pushed yourself do not end up on a sandbox
branch. Every clone has to be committed before the sandbox is removed — a dirty member keeps it,
and the warning names which one.
