# Staying current

box is installed with `go install`, so it is a compiled binary rather than a script, and the two
halves of staying current follow from that.

## What "current" means

A binary is built for one platform, so box cannot compare its own bytes with a published copy the
way a single script could. It compares **releases** instead: the version the binary was stamped
with at install time, against the newest version the module proxy knows.

- The running version comes from `runtime/debug.ReadBuildInfo`. `go install <module>@<tag>` stamps
  the tag and embeds no VCS data; a build from a checkout embeds `vcs.revision`, and stamps either
  `(devel)` or a pseudo-version derived from the commit.
- So a binary carrying `vcs.revision` is a copy someone is working on, whatever version it claims,
  and git is how that one is updated: the check says nothing about it and `box self-update` refuses
  it. This is the binary's equivalent of the script refusing a `box.py` that git tracks. Going by
  the version string alone is not enough — `go install ./cmd/box` in a clone stamps a
  pseudo-version that looks exactly like a release.
- The newest version comes from `https://proxy.golang.org/<module>/@latest`, whose JSON answer
  holds a `Version` field. `BOX_UPDATE_URL` points the check at something else — a fork, a private
  proxy — and an empty value switches it off, costing no round trip at all.

Everything else about the check is unchanged: a timestamp under `XDG_CACHE_HOME` keeps the hour
after a check quiet, the notice prints on stderr so a piped stdout stays clean, it is red only when
stderr is a terminal and `NO_COLOR` is unset, and every failure is swallowed — an unreachable
proxy, a broken cache or a missing home directory must never stop a command that would otherwise
work.

## What `self-update` does

It runs `go install <module>/cmd/box@latest`, which is the same line the README gives for
installing box in the first place. The old script wrote the new copy beside the running one and
moved it over; `go install` does that job, writing into `GOBIN` (or `$GOPATH/bin`), and a failed
download or a directory it cannot write leaves the working box exactly as it was.

So box needs `go` on `PATH` to update itself. It says so rather than failing inside the toolchain,
which is the binary's equivalent of the script reporting a copy it could not write.
