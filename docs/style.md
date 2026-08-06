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
does. Every function has a one-line docstring.

## Naming

Full words, no abbreviations: `sandbox_name`, not `sbx_nm`. Config keys are camelCase in JSON
(`rootSize`) and snake_case in Python (`root_size`); `Config` is where the two meet.

## Checks

Run the checks locally **before starting any new change**, not only before committing. A clean
run first tells you that anything that breaks afterwards is yours:

```sh
uv run pre-commit run -a
uv run pytest -q
```

Both must pass, and both run again once the change is finished. `pre-commit` covers ruff and
`mypy --strict`; CI runs the same checks on Linux and macOS.
