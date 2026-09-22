# What you get back

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
refusal exits 1, a command line box cannot read exits 2, and a Ctrl-C exits 130 — see
[signals.md](signals.md) for what a Ctrl-C does during the session itself.
