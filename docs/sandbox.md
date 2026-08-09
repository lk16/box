# Sandbox environment

What is true of this repository inside a box sandbox. The constraints the code must keep are in
[goal.md](goal.md), the rules for writing it in [style.md](style.md); read both before changing
`box.py`.

The allowlist holds api.anthropic.com and nothing else, so no dependency can be downloaded here.
Everything comes off the host's uv cache, mounted read-only and copied into your home at startup,
with the host's own uv wired in over the image's older one. Both are already in place when you
start, so the whole setup is:

    export UV_OFFLINE=1
    uv sync            # the clone has no .venv/

Run the checks before starting a change and again once it is finished:

    uv run ruff check --fix
    uv run ruff format
    uv run mypy --strict
    uv run pytest -q

`uv run pre-commit run -a`, which [style.md](style.md) asks for, does not work here: its upstream
hook repo is cloned from github and given an environment built from PyPI, and neither is
reachable. The four commands above are that run minus its whitespace hooks, so leave no trailing
whitespace and end every file with a newline; the host's own run before merging is what catches
those.

A cache holds wheels built for the Python that filled it, and the image's Python is newer than
what a host normally runs, so the host warms the cache for it once with `uv sync --python 3.14`.
If `uv sync` fails here on a missing wheel while the host is fine, that is what has gone stale:
say so, and ask for that command to be run on the host against the Python this image now ships —
`python3 -V` here names it.

`box.py` imports the standard library only, and must keep doing so — everything in
`pyproject.toml` is for the checks, never for a run. The tests fake `sbx` and `git` rather than
calling them, so nothing here creates a sandbox from inside one; do not try to run `sbx`.

`README.md` documents box for its users. A change to a flag, a config key or a message belongs
there in the same commit.
