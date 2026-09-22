# Style

Write code a reader understands on the first pass.

## Rules

- Short functions. One job each, and a name that says which.
- No long argument lists. Pass the `Config`, the `Project` or the `Deps` rather than their fields.
- No deep nesting. Return early; keep function bodies close to the left margin.
- Keep functions testable: separate the pure parts (parsing, merging, building command lists) from
  the parts that touch the system.
- Command builders return a `[]string` and run nothing, so tests can assert on them.
- Everything that touches the outside world goes through an interface in `internal/system`, so a
  test stands a fake in front of it rather than in front of the real `sbx`.

## Comments

Comments are one liners. Never longer. They say why something exists, not what the next line does.
Every declaration has a one-line doc comment, starting with its own name, as `go doc` expects.

A decision that needs a paragraph is not a comment: write it in `docs/` and point at the file from
a one-line comment where the code implements it. [terminals.md](terminals.md),
[signals.md](signals.md) and [updates.md](updates.md) exist for exactly that reason.

In the tests, the helpers and the fakes have a doc comment and the `Test*` functions do not: their
names are the documentation, and a comment would only say the name again. Name a test after the
behaviour it pins down, not after the function it calls.

## Naming

Full words, no abbreviations: `sandboxName`, not `sbxNm`.

A config setting carries one name in three places: the `.box/config.json` key (`root_size`), the
`Config` field (`RootSize`) and the flag, which is the key with hyphens (`--root-size`). The one
translation is `cli.ConfigKey`, which turns a flag name into its config key by swapping hyphens for
underscores; nothing else maps between the three.

## Checks

Run the checks **before starting any new change**, not only before committing. A clean run first
tells you that anything that breaks afterwards is yours:

```sh
./check.sh
```

That is `go fmt`, `go vet`, `go mod tidy`, `golangci-lint` and `go test`, which is everything
`pre-commit run -a` does plus the tests. Both the hooks and CI run the same set, so a local run
catches what CI would.

## Dependencies

box depends on nothing outside the standard library, and must keep doing so. `go.mod` has no
`require` block, so a `go install` fetches one module and nothing else. Where the standard library
genuinely has no answer — telling a terminal from a pipe — box writes the few lines itself behind a
build tag and says why in `docs/`.
