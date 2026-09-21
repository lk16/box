# Sandbox environment

What is true of this repository inside a box sandbox. The constraints the code must keep are in
[goal.md](goal.md), the rules for writing it in [style.md](style.md); read both before changing
anything under `internal/`.

The allowlist holds api.anthropic.com and nothing else, so no dependency can be downloaded here.
box depends on nothing outside the standard library, so nothing needs downloading: the Go toolchain
and the module cache are mounted read-only from the host, and `go build` and `go test` work offline
as they are.

`pre-commit` cannot run here: its upstream hook repository would be cloned from github and given an
environment built from PyPI, and neither is reachable. Run the checks directly instead:

```sh
./check.sh
```

That is the same set the hooks run, plus the tests. It leaves no trailing whitespace and ends every
file with a newline for you, which is what the whitespace hooks would otherwise catch; the host's
own `pre-commit run -a` before merging is what catches the rest.

`go test -race` needs a C compiler, and the image ships none, so it cannot run here. The only
concurrency box has is the signal watcher reading an atomic counter, so there is little for it to
find; run it on the host if you add more.

The tests fake `sbx` rather than calling it, and they run `git` for real in temporary directories,
so nothing here creates a sandbox from inside one. Do not try to run `sbx`.

`README.md` documents box for its users, and `docs/` for everyone else. A change to a flag, a
config key or a message belongs in the right one of those in the same commit.
