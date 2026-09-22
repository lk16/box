# CLAUDE.md

Index of the documentation in `docs/`. Read the relevant file before changing code.

Run the checks before starting any change, and again once it is finished:

```sh
./check.sh
```

That is `go fmt`, `go vet`, `go mod tidy`, `golangci-lint` and `go test` — everything `pre-commit`
runs plus the tests. Inside a box sandbox `pre-commit` itself cannot run at all;
[docs/sandbox.md](docs/sandbox.md) says why and what replaces it.

The toolchain is Go and nothing else. box imports only the standard library, and `go.mod` has no
`require` block, so a `go install` fetches one module. Keep it that way.

- [docs/goal.md](docs/goal.md) — what box is for and the constraints it must keep
- [docs/style.md](docs/style.md) — code style rules, including where a decision belongs
- [docs/sandbox.md](docs/sandbox.md) — what is true of this repository inside a box sandbox, and
  this repository's own `prompt_file`, so editing it edits the system prompt of every box run here

Decisions that needed more than a comment: [docs/updates.md](docs/updates.md),
[docs/terminals.md](docs/terminals.md), [docs/signals.md](docs/signals.md),
[docs/fetching.md](docs/fetching.md).

User-facing documentation lives in [README.md](README.md) and the pages it links to.

## Layout

- `cmd/box/main.go` — wires box to the real system: processes, the terminal, the network, the clock
- `internal/system` — those as interfaces, so every test stands a fake in front of them
- `internal/config` — the settings a project's `.box/` files and the flags resolve to
- `internal/project` — what must be true of the repository before anything is created
- `internal/session` — creating the sandbox, running the agent, bringing committed work back
- `internal/setup` — `box gen` and `box mount-prompt`, which write files rather than read settings
- `internal/update` — the update check and `box self-update`
- `internal/cli` — the command line, and dispatch to one of the above
- `internal/boxtest` — the fakes and the git helpers the tests share

This repository's `.box/config.json`, `.sbx/kit/spec.yaml` and `docs/sandbox.md` are box's own
working setup, not templates to copy. `box gen` writes the starting points a new project wants.
