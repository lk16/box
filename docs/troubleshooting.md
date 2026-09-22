# Troubleshooting

The refusals you are most likely to meet, and what to do about each:

| Message | Fix |
| --- | --- |
| `this project has no .box/config.json` | run `box gen` |
| `kit is not set` | point `kit` at a kit directory in `.box/config.json` |
| `model is not set` | fill in a model in `.box/config.json` |
| `CLAUDE_OAUTH_TOKEN_FILE is not set` | export it, pointing at a file holding a `claude setup-token` token |
| `BOX_SECRETS_FILE is not set` | export it, pointing at a file of `NAME=value` lines for the declared [secrets](secrets.md) |
| `which is inside` | move the secrets file out of the repository or the mount it names; the sandbox can read both |
| `has no value for` | add that name to the file `BOX_SECRETS_FILE` points at |
| `quotes its value` | drop the quotes around the value: docker would keep them, so box refuses the line |
| `has no path on this machine for` | give the mount it lists a path in `.box/mounts.json`, or have an agent do it with `box mount-prompt` |
| `is missing a path for` | clone each member it lists, then put where it sits in `.box/repos.json`; `box gen` adds every declared name, and `null` runs without one this machine does not have |
| `is no directory on this machine` | fix that member's path in `.box/repos.json`; it is relative to the folder box runs in |
| `whose origin is` | point that member in `.box/repos.json` at a clone of the `git_origin` in `.box/config.json` |
| `is not a git repository` | a member has to be a git repository with an `origin` remote |
| `has no branch <name> on origin` | set the member's `branch` to one `origin` really has |
| `not ignored by git` | add that file to `.gitignore`; it holds paths that exist only on your machine, and `box gen` writes the lines |
| `these commands are not on PATH` | install `sbx`, `git` and the `claude` CLI |
| `this sbx is v… and box needs` | upgrade `sbx`; the kits box writes use the spec layout 0.38.0 introduced |
| `the sbx client and its daemon are different versions` | run `sbx daemon restart`; the daemon keeps running across an upgrade |
| `has uncommitted changes -- not removing it` | the sandbox was kept on purpose: recover with the `sbx exec` and `sbx cp` lines box printed, then `sbx rm --force <name>` |
| `this is a checkout rather than an install` | `box self-update` only replaces a `go install` copy; update a checkout with git |
| `go is not on PATH` | box installs itself with `go install`, so updating needs Go |
