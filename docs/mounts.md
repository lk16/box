# Mounts

Paths differ per machine, so the project declares what it needs and each machine says where it is.

`required_mounts` in the committed `.box/config.json` holds a name and a description of what
belongs there:

```json
{
  "required_mounts": {
    "go_toolchain": "the Go install, what `go env GOROOT` prints",
    "go_mod_cache": "the Go module cache, what `go env GOMODCACHE` prints"
  }
}
```

The gitignored `.box/mounts.json` gives each of those names a path:

```json
{
  "go_toolchain": "/usr/local/go",
  "go_mod_cache": "~/go/pkg/mod"
}
```

`box run` refuses to start unless the two match exactly. A declared name with no path, a name still
holding the placeholder `box gen` writes, or a name the project never declared is an error that says
which mount is at fault. It also refuses while `.box/mounts.json` is not ignored by git, since it
holds paths that exist only on your machine. `box gen` writes that `.gitignore` entry for you.
Ignore that entry only, not the whole `.box/` directory, so `config.json` stays committed.

Every mount is read-only unless you say otherwise, since the agent should not be able to write to
your machine. `~/scratch:rw` opts one path out, and `:ro` is an error, not a synonym for the
default. The `:rw` goes on the path in `.box/mounts.json`, never in the declaration, so a shared
file cannot widen access to your disk. A leading `~` expands as it does in the shell. `--mount`
adds an unnamed workspace for one run. `box config` shows the resulting `sbx` specs.

Mounts reach `sbx` sorted by name, so the arguments do not depend on how anyone ordered a file. The same path asked for twice is passed once, and a path asked for read-only in one place
and `:rw` in another is an error naming both.

## Filling them in with an agent

```sh
box mount-prompt
```

The prompt lists every declared mount that has no path yet, with its description and the platform
and architecture a build has to match. Paste it into an **interactive** `claude` session: this agent
runs on your machine, not in a sandbox, so you should see its commands and its diff before approving
them. It is told to probe for each path and check that it exists instead of guessing, and to say
which ones it could not find. Where nothing on this machine fits — the sandbox runs Linux whatever
you run — it downloads a suitable build into `~/.box/deps` (or `$XDG_DATA_HOME/box/deps` when that
is set) and points the mount there. That directory sits outside the project on purpose: a mount
nested inside the workspace deadlocks the sandbox's clone, so never point a mount at a path inside
the repository.

Once every mount has a path, nothing is printed on stdout, so running it again gives an agent no
work to do; box says as much on stderr.

Its output is only as good as your descriptions: `"go cache"` gives an agent nothing, while
``"the Go module cache, what `go env GOMODCACHE` prints"`` gives it a command to run.
