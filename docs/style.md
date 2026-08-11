# Style

Write code a reader understands on the first pass.

## Rules

- No optional arguments. A function takes exactly what it needs, always.
- No long argument lists. Pass the `Config` object instead of its fields.
- No ternaries. Use a plain `if` with an early return.
- No deep nesting. Return early; keep function bodies close to the left margin.
- Keep functions testable: separate the pure parts (parsing, merging, building command lists)
  from the parts that touch the system (`subprocess`, the filesystem).
- Command builders return a `list[str]` and run nothing, so tests can assert on them.

## Comments

Comments are one liners. Never longer. They say why something exists, not what the next line
does. Every function in `box.py` has a one-line docstring. In the tests, the helpers and the fakes
have one and the `test_*` functions do not: their names are the documentation, and a docstring
would only say the name again.

ruff enforces that: `D` is selected, with `D103` and `D107` ignored under `tests/`, so a missing
docstring is a failed check rather than something a reader has to notice. A fake's class docstring
covers its `__init__`, which is why `D107` is off there too.

## Naming

Full words, no abbreviations: `sandbox_name`, not `sbx_nm`.

Config settings carry one name in three places, and it is snake_case everywhere: the
`.box/config.json` key (`root_size`), the `Config` field (`root_size`) and the flag, which is the
same name with hyphens (`--root-size`). No translation layer, and argparse derives every `dest`
on its own. When you add a setting, pick a snake_case name and use it verbatim in `DEFAULTS`,
`Config` and the flag.

`--mount` is the one exception, since it is repeatable and collects a list: its `dest` is
`mounts`, and `MOUNT_FLAG` and `MOUNT_DEST` hold the two names so a message can name the flag the
user typed rather than the dest argparse stored it under.

## Checks

Run the checks locally **before starting any new change**, not only before committing. A clean
run first tells you that anything that breaks afterwards is yours:

```sh
uv run pre-commit run -a
uv run pytest -q
```

Both must pass, and both run again once the change is finished. `pre-commit` covers ruff,
`mypy --strict` and a set of whitespace and syntax hooks. CI runs the first three on Linux and
macOS, against both ends of the Python range `pyproject.toml` allows, so the whitespace hooks are
caught by a local run and nowhere else. Inside a box sandbox `pre-commit` cannot run at all --
[sandbox.md](sandbox.md) gives the commands that replace it.
