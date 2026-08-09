# CLAUDE.md

Index of the documentation in `docs/`. Read the relevant file before changing code.

Run the checks before starting any change, and again once it is finished:

```sh
uv run pre-commit run -a
uv run pytest -q
```

Inside a box sandbox `pre-commit` cannot run at all — [docs/sandbox.md](docs/sandbox.md) gives the
commands that replace it there.

- [docs/goal.md](docs/goal.md) — what box is for and the constraints it must keep
- [docs/style.md](docs/style.md) — code style rules for `box.py` and its tests
- [docs/sandbox.md](docs/sandbox.md) — what is true of this repository inside a box sandbox, and
  this repository's own `prompt_file`, so editing it edits the system prompt of every box run here

User-facing documentation lives in [README.md](README.md).

This repository's `.box/config.json`, `.sbx/kit/spec.yaml` and `docs/sandbox.md` are box's own
working setup, not templates to copy. `box gen` writes the starting points a new project wants.
