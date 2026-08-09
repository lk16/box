# Repository review

A full review of box: `box.py`, its tests, its documentation, its CI and how it is published.
Findings are numbered once, continuously, and grouped by area. Each one says what is wrong, where,
and what to do about it, and carries a **Done** line once it has been dealt with.

Verified against the working tree at commit `ba1b932`, with `ruff`, `ruff format --check`,
`mypy --strict` and `pytest` (159 tests) all passing beforehand. Line numbers therefore point at
that commit, not at the file as it stands now.

**State.** Everything is done except section 5, and the suite now numbers 300 tests. Done since this
review was written: findings [1](#1)–[44](#44), [55](#55)–[69](#69) and [70](#70)–[102](#102) — every
bug and simplification in `box.py`, every contradiction between the docs and the code, every gap and
smell in the tests, the README rewrite, everything that assumed one machine or one project, every
place a reader or an agent was sent to the wrong answer, and every suggested improvement bar one.
[103](#103) is skipped on purpose, [68](#68) asked for no change, and section 10's decision is
recorded there: box stays on A.

**Still open:** [45](#45)–[54](#54), all of section 5. CI runs four steps that prove almost nothing
on macOS, pins no Python, verifies no lockfile, enforces seven pre-commit hooks nowhere, and is not a
required check on `main` — which is what section 10's recommendation asks for next.

---

## 1. Bugs and unintended side effects in `box.py`

### 1
**`cleanup` reads "the command failed" as "the tree is clean", and can then delete committed
work.** `box.py:572-581`. `capture` returns `""` both when a command succeeds with no output and
when it fails, and `cleanup` uses that for two decisions in a row: `git fetch sandbox-<name>`
(line 574, exit status discarded) and the dirty check `sbx exec <name> git -C <cwd> status
--porcelain` (line 575). If the sandbox's git daemon is down, `sbx exec` errors, or the container
path does not match `Path.cwd()`, the fetch brings nothing back and the status reads as clean —
and `sbx rm --force` on line 581 then destroys the sandbox with every commit in it. This is the
one outcome `docs/goal.md:81` says must never happen.
**Fix:** run both with `subprocess.run(..., check=False)` and inspect `returncode`. Anything
non-zero must take the `warn_dirty` path — keep the sandbox, print recovery steps — rather than
falling through to removal. "I could not tell" and "it is clean" must not be the same value.

**Done.**

### 2
**Non-string values in `.box/config.json` are coerced into garbage that passes validation.**
`box.py:198-208` type-checks nothing beyond "is a JSON object", and `build_config`
(`box.py:283-298`) wraps every value in `str()`. Verified:

```
$ cat .box/config.json
{"model": null, "kit": null, "memory": [1,2], "name": null}
$ python3 box.py config
Config(name='None', memory='[1, 2]', cpus='4', ..., model='None', kit='None')
```

`require_settings` **passes**, because `"None"` is truthy and `resolve_path("None").is_file()` is
false. box then runs `sbx create --memory '[1, 2]' --kit None` and `claude --model None`. A
`null` name also becomes the literal sandbox base name `None` instead of falling back to the
directory name.
**Fix:** validate types in `read_config_file` — every key but `required_mounts` must be a `str`,
`required_mounts` a dict — and raise a `ConfigError` naming the key. That also lets `build_config`
drop all nine `str()` calls, so the fix shortens the file.

**Done** — with one deviation: a JSON number is still taken, since `"cpus": 4` is a natural thing
to write and a number spells itself. `null`, a boolean and a container are the errors.

### 3
**A missing `sbx` exits with a traceback.** `box.py:344-349`. `capture` catches a non-zero exit
but not `OSError`, so on a machine without `sbx` on `PATH` the first thing `box run` does is die:

```
File "box.py", line 366, in taken_names
    running = set(capture(["sbx", "ls", "-q"]).split())
FileNotFoundError: [Errno 2] No such file or directory: 'sbx'
```

`store_secret` (`box.py:445-448`) has the same problem from the other side: `check=True` raises
`CalledProcessError`, and `run_session` is called *outside* `main`'s `try` (`box.py:897`), so
nothing catches it. `suggest_branch_name` (`box.py:484`) already catches `OSError` for a missing
`claude`, so the asymmetry looks unintended. This is the first thing a new user without `sbx`
sees, and it contradicts `docs/goal.md:97` ("no traceback in front of the reason").
**Fix:** `except OSError: return ""` in `capture`, and turn a failed `store_secret` into a
`ConfigError`. See also [99](#99) for checking the three required binaries up front.

**Done.**

### 4
**`to_workspace` turns a degenerate mount into a read-write mount of the current directory.**
`box.py:269-276`. Verified: `to_workspace(":rw")` returns `"."` — the project bind-mounted
**writable** into the sandbox, which is the exact accident the read-only default exists to
prevent — and `to_workspace("")` returns `".:ro"`. The `endswith(":rw")` test also runs before the
colon guard, so `/data/a:b:rw` passes a colon-containing path straight to `sbx`.
**Fix:** reject an empty mount, and an empty remainder after stripping `:rw`, with a
`ConfigError`; apply the `":" in path` check to the stripped remainder as well.

**Done.**

### 5
**A failed update check is never recorded, so the quiet hour only covers successes.**
`box.py:864-877`: `store_check_time` (line 872) runs *after* `fetch_remote_hash` (line 871)
succeeds. Verified — with GitHub unreachable, `~/.cache/box/` is never created, so every single
`box config`, `box gen` and `box run` re-attempts the network. Where the failure is a timeout
rather than an immediate refusal, that is up to `UPDATE_TIMEOUT_SECONDS` per command, and
`urlopen`'s timeout does not cover DNS resolution, so it can be longer. `docs/goal.md:31-33` says
the notice should appear "as often as the check does and no more"; a failed check is still a
check.
**Fix:** move `store_check_time(path, now)` above `fetch_remote_hash()`.

**Done.**

### 6
**`read_token` strips only `\n`.** `box.py:378-382`. A token file written on Windows, or saved
with a trailing space, yields `"sk-ant-…\r"` or `"…  "`, which is stored verbatim as the sbx
secret; the agent then starts with an invalid `CLAUDE_CODE_OAUTH_TOKEN` and fails with an opaque
auth error a long way from the cause. A whitespace-only file also passes the `st_size == 0` guard
and stores a blank token.
**Fix:** `path.read_text().strip()`, and raise `ConfigError` when the result is empty. The
existing test at `tests/test_box.py:552` still passes.

**Done.**

### 7
**`require_no_flags` names a flag that does not exist.** `box.py:668-684`. Verified:

```
$ box --mount /cache gen
box: gen takes no flags, but got --mounts; edit .box/config.json instead
```

There is no `--mounts`. `to_flag` renders the argparse *dest*, and `--mount` is the one flag whose
dest differs from its option string (`box.py:316-318`). The test at `tests/test_box.py:837-840`
asserts the wrong string, so it locks the bug in.
**Fix:** map dest to option string for `mounts`, and update the test to assert `--mount`. This
also removes the contradiction with `docs/style.md:24-26` — see [30](#30).

**Done.**

### 8
**Ctrl-C ends a run with a traceback.** `box.py:648-665`, `box.py:880-897`. The `finally: cleanup`
is correct and does run, but nothing catches `KeyboardInterrupt`, so the user sees a traceback
after it. A second Ctrl-C during cleanup aborts it half-way, leaving both the secret and the
sandbox behind.
**Fix:** `except KeyboardInterrupt: return 130` around the `main()` call, with a one-line message.

**Done.**

### 9
**An unborn HEAD makes every fetched ref unsettleable.** `box.py:508-510`, `box.py:546-548`. In a
repository with no commits, `git rev-list --count HEAD..<commit>` exits 128 (verified), so
`count_new_commits` returns `""` and `settle_ref` prints "git could not read … so it was kept" for
work that is perfectly branchable. No data is lost, but no branch is ever made.
**Fix:** fall back to `git rev-list --count <commit> --not --all`, or check
`git rev-parse --verify HEAD` first.

**Done** — box now refuses to run outside a git repository, or in one with no commits, rather than
falling back to another rev-list.

### 10
**`read_mounts_file` has the same coercion hole as [2](#2).** `box.py:211-218`. `{"cache": null}`
becomes the path string `"None"`, which is neither `""` nor `MOUNT_PLACEHOLDER`, so
`unfilled_mounts` accepts it and box mounts a relative path named `None`.
**Fix:** require string values, raise `ConfigError` otherwise.

**Done.**

### 11
**`box gen` outside a git repository appends duplicate `.gitignore` lines, and `box run` then
gives a misleading error.** `box.py:620-623`: `git check-ignore` exits 128 in a non-repository,
which `is_git_ignored` reads as "not ignored". So `ignore_local_paths` re-appends both lines on
every `gen`, and `require_ignored_local_paths` refuses to run with "Add a `.box/mounts.json` line
to .gitignore" for a line that is already there. Verified.
**Fix:** distinguish exit 1 (not ignored) from 128 (not a repository or an error), and say "this
is not a git repository" in the second case — box cannot work outside one anyway, since
`sbx create --clone` needs one.

**Done.**

### 12
**`pick_name` is check-then-create, and the loser deletes the winner's secret.** `box.py:370-375`
against `box.py:652-657`. Two `box run`s started at once in the same project both pick `demo-1`.
The loser's `sbx create` fails, which is handled — but its `drop_secret` on line 657 deletes the
secret belonging to the sandbox that *did* get created. Harmless in practice, because sbx injects
the env var at creation time, but it is a real cross-run side effect.
**Fix:** at minimum a comment saying why it is safe; better, re-check the name after create.

**Done** — a comment says why the loser's `drop_secret` is harmless; the name is not re-checked.

### 13
**`settle_ref` prints "1 commits".** `box.py:563`. Ungrammatical on every single-commit sandbox,
which is a common case.

**Done.**

### 14
**The update instruction is unquoted and assumes `curl`.** `box.py:860`. `script_path` comes from
`Path(__file__).resolve()` and is interpolated raw, so an install path containing a space produces
a `curl -o` line that silently writes to the wrong place. `curl` is not present on every minimal
image.
**Fix:** `shlex.quote(str(script_path))`. If the file is not writable by the current user, say so
rather than printing a command that will fail.

**Done.**

### 15
**The update notice tells a developer to destroy their own work in progress.** `box.py:856-861`
plus `README.md:201-203` ("box uses itself for its own development"). Any uncommitted or unmerged
change to `box.py` makes the hash differ from `main`, so box prints, in red:

```
An update to box is available. Take it with:
  curl -fsSL -o /home/luuk/projects/box/box.py https://raw.githubusercontent.com/lk16/box/main/box.py
```

Following that overwrites the change being worked on. Verified.
**Fix:** skip the check when the script sits inside a git repository whose `origin` is box's own,
or when `git status --porcelain box.py` is non-empty; alternatively gate it behind an env var —
see [66](#66).

**Done** — the check is skipped when git tracks the running `box.py`, which no installed copy is.

### 16
**Bare `except Exception` in `checked_recently`.** `box.py:841-847`. The intent (a corrupt cache
should self-heal) is right, but it also swallows programming errors.
**Fix:** `except (OSError, ValueError, KeyError, TypeError)` says the same thing precisely.

**Done.**

---

## 2. Simplifications that change no behaviour

### 17
**`require_no_flags` does not need to re-parse a synthetic argv.** `box.py:673-679`. Every flag's
argparse default is `None`, so `value != defaults[key]` is exactly `value is not None`. The
`defaults = vars(build_parser().parse_args(["run"]))` line and the comment explaining it can both
go.

**Done.**

### 18
**One helper for "run this and tell me whether it worked".** `create_branch` (`box.py:533-536`)
and `is_git_ignored` (`box.py:620-623`) have identical bodies. A shared
`succeeds(command: list[str]) -> bool` collapses both, and gives [1](#1)'s fix a natural home.

**Done.**

### 19
**`resolve_mounts` takes a whole `Namespace` for one attribute.** `box.py:584-592` reads only
`arguments.mounts`. Taking a `list[str]` matches `docs/style.md:7` ("a function takes exactly what
it needs") and makes it testable without argparse.

**Done.**

### 20
**`read_required_mounts` round-trips through `merge_values`.** `box.py:772-775` builds the whole
merged dict to read one key. `as_descriptions(read_config_file(path).get("required_mounts", {}))`
says it directly.

**Done.**

### 21
**Fixing [2](#2) removes nine `str()` calls** from `build_config` (`box.py:283-298`). Worth doing
as one change, not two.

**Done** — the nine `str()` calls are gone; a checked accessor reads each setting instead.

---

## 3. Contradictions between the docs and the code

### 22
**The branch-naming timeout is ten seconds, not five — wrong in two docs and a test name.**
`box.py:56` is `BRANCH_NAME_TIMEOUT_SECONDS = 10`. `README.md:183` says "takes longer than five
seconds"; `docs/goal.md:87` says "five seconds of silence"; and
`tests/test_box.py:387` is named `test_suggest_branch_name_gives_claude_five_seconds` while
asserting `== 10`. `git log -p` shows the constant was 10 from the commit that introduced it, so
both docs have always been wrong.
**Fix:** say ten in both docs, rename the test, and assert against the constant rather than a
literal.

**Done.**

### 23
**`box config` does not validate what `box run` validates, though `docs/goal.md:39-40` says it
does.** `main` returns from `show_config` before `prepare_launch` (`box.py:891-893`), so neither
`require_settings` (kit and model) nor `require_ignored_local_paths` ever runs. Verified in a
repository with no kit, no model and a committable `.box/mounts.json`: `box config` printed the
table and exited **0**, `box run` in the same directory exited **1**.
**Fix:** either run both checks on the `config` path — which is what the doc promises and what
makes `config` genuinely answer "what would `run` do" — or narrow `docs/goal.md:39-40` to
"validates that the JSON parses and that the mounts match". The first is better: `box config` is
the natural place to find out that a project is not set up.

**Done** — `box config` now runs the checks, printing the settings first: the first of the two
options.

### 24
**`--name`'s default is the *kebab-cased* directory name.** `README.md:70` says "current
directory name". `box.py:175-180` runs it through `to_kebab_case` and falls back to the literal
`box` when nothing survives. `My Repo` gives `my-repo`.
**Fix:** "current directory name, kebab-cased (`box` if nothing survives)".

**Done.**

### 25
**`box mount-prompt` does print something when every mount is filled.** `README.md:163` says
"prints nothing once they all have paths" and `docs/goal.md:75` says "Printing nothing when every
mount has a path". `box.py:803` prints `every mount in .box/mounts.json already has a path` to
**stderr** — which is the right design, since stdout is what gets piped into `claude`.
**Fix:** say "prints nothing on stdout" in both places.

**Done.**

### 26
**`docs/goal.md:14` says `BASE_PROMPT` is the agent's system prompt; it is appended to one.**
`box.py:425` passes `--append-system-prompt`. `README.md:94` gets this right.
**Fix:** "appended to the agent's system prompt" in goal.md.

**Done.**

### 27
**`docs/goal.md:90` omits that only the last line of the model's answer is used.** It says "the
branch name is whatever `claude` printed, kebab-cased and cut to five words".
`to_branch_name` (`box.py:460-466`) discards everything but the last non-blank line.
**Fix:** "the last line `claude` printed, kebab-cased and cut to five words".

**Done.**

### 28
**`docs/goal.md:42-43` says everything box reads from a project lives under `.box/`.** Three
things do not: `.gitignore` (read and appended, `box.py:692-708`), the `prompt_file` at any path
(`box.py:385-392` — this repository points it at `docs/sandbox.md`), and the kit directory
(`.sbx/kit`, `box.py:614`).
**Fix:** "everything box *writes* to a project lives under `.box/`, apart from the `.gitignore`
lines it needs".

**Done.**

### 29
**`docs/style.md:41` says "CI runs the same checks"; it runs a strict subset.** See [49](#49).

**Done** — `docs/style.md` now says which subset CI runs; the CI change itself is [49](#49), still
open.

### 30
**`docs/style.md:24-26` says "argparse derives every `dest` on its own"; `box.py:307` admits in a
comment that it derives "every dest but the repeatable one",** and `box.py:316-318` sets
`dest="mounts"` explicitly. That exception is what causes [7](#7).
**Fix:** fix [7](#7) and carve the repeatable flag out of the style rule in one sentence.

**Done.**

### 31
**`docs/style.md:18-19` requires a one-line docstring on every function, and CLAUDE.md scopes that
to "`box.py` and its tests".** `box.py` is perfect: 0 of 78 functions lack one. `tests/test_box.py`
has **172** functions without one — every `test_*` function — plus a handful of nested stubs. The
helpers and fakes *do* have docstrings, so the real convention is "test names are the
documentation; helpers get docstrings".
**Fix:** write that convention down in `docs/style.md` rather than leaving a rule violated 172
times. If you would rather enforce it, add `D` to ruff's `select` with a per-file ignore for
`tests/` — see [102](#102).

**Done** — the convention is written down; enforcing it with ruff, [102](#102), is still open.

### 32
**`README.md:183` lists three ways branch naming can fail; the code has five.** The two missing
are `box.py:548` ("git could not read `<ref>`") and `box.py:560` ("git refused branch `<name>`").
`docs/goal.md:87` covers the second but not the first.
**Fix:** add "or git cannot read the ref" to both.

**Done.**

### 33
**`box config` prints a `CLAUDE_OAUTH_TOKEN_FILE` row that no doc mentions.** `box.py:336-341`.
It is useful — it is the one setting with no flag and no config key — and `README.md:48-49`
describes `box config` only as showing "the settings in effect".
**Fix:** one clause in the README.

**Done.**

### 34
**The disk limits reach `sbx` through the environment, not through flags, and no doc says so.**
`box.py:402-407` sets `DOCKER_SANDBOXES_ROOT_SIZE` and `DOCKER_SANDBOXES_DOCKER_SIZE` on the child
environment; neither appears in `build_create_command`. This matters to anyone who already exports
those variables, because box silently overrides them.
**Fix:** one line in `docs/goal.md`'s run walkthrough.

**Done.**

### 35
**box exits with the sandbox agent's exit code, undocumented.** `box.py:662-663`. Relevant to
anyone scripting box.
**Fix:** one line in README's "What you get back".

**Done.**

### 36
**`read_token` rejects an empty token file as well as a missing one, undocumented.**
`box.py:380-381`; `README.md:43-45` only says box refuses to start without the variable.
**Fix:** "…without it, or if the file it names is missing or empty".

**Done.**

### 37
**`README.md:79` puts a filename in the config-key column.** The column is headed
"`.box/config.json` key", and the cell says `.box/mounts.json`. `--mount` has **no** config key by
design (`tests/test_box.py:133-134` pins this), and the two are not equivalent: mounts-file paths
are validated against `required_mounts` (`box.py:238-249`), `--mount` paths are appended
unvalidated (`box.py:591-592`).
**Fix:** put `—` in the key column and let the prose at `README.md:144` carry the relationship.

**Done.**

### 38
**`build_agent_args` takes two `Config` fields loose** (`box.py:421`) where `docs/style.md:8` says
to pass the `Config`. Its sibling `build_create_command` does it the documented way. Arguable —
`system_prompt` is derived rather than a field — but worth a decision either way.

**Done** — `build_agent_args` takes the `Config`, like its sibling.

### 39
**`pyproject.toml:3` carries `version = "0.1.0"` while `docs/goal.md:29-30` says box has no
version.** The value is inert (uv resolves the project as `virtual`, so nothing is built or
installed) but it is a second, contradictory answer to "what version is this".
**Fix:** delete the line, or note in goal.md that it exists only to satisfy the dev tooling.

**Done** — the line stays, with a comment: uv refuses a `[project]` table without a version.
`docs/goal.md` says it is inert.

**Verified as accurate, for the record:** the `-1`/`-2` sandbox suffix, the `-2`/`-3` branch
suffix, five words, `:ro` being an error, `~` expansion, the read-only default, the hourly update
check and its red-on-stderr output, the token going over stdin, the secret being stored before
`sbx create`, a failed create being reported rather than raised, a dirty sandbox never being
removed, refs dropped when empty and kept on naming failure, mounts in declaration order, unknown
config keys rejected, kit-as-file rejected and kit-not-on-disk left to sbx, `gen` never
overwriting, stdlib only, and every default in the README's table.

---

## 4. README

The README is 217 lines and about 1,640 words for a tool with four commands and nine settings.
It is accurate and well organised; it is roughly twice as long as it needs to be, and the prose
style — long compound sentences joined by em-dashes, each stating a rule and then justifying it —
is the main reason. Fourteen sentences run to 30 words or more; the longest is 69.

### 40
**Cut the essayistic justifications out of the reference sections.** Concrete rewrites:
- `README.md:93-99` — 69 words listing everything `BASE_PROMPT` says. The list duplicates
  `box.py:93-118`, which is the actual source and will drift from it. Replace with: "box always
  prepends a built-in prompt covering what is true of every sandbox. It lives in `box.py` as
  `BASE_PROMPT` — read it there."
- `README.md:191-197` ("Conservative by default") — 86 words restating rules already stated in
  Settings and Mounts, and the target of three internal links that bounce the reader around. Drop
  the section and keep each reason inline where the rule is.
- `README.md:109-111` — "Mounts are the one part of a project's box setup that names paths on the
  machine box runs on: a toolchain sits somewhere else on another machine, and somewhere else
  again on another OS." → "Paths differ per machine, so the project declares *what* it needs and
  each machine says *where* it is."
- `README.md:90-91` — "The sandbox's Claude CLI is a different install from yours, so an unset
  model leaves what runs to *its* default rather than to you." → "The sandbox has its own Claude
  install. Without `model`, it picks the model, not you."

Roughly 500 words come out with no information lost.

**Done** — all four cuts made. The README is longer than the suggested 900 words all the same: the
sections [42](#42)–[44](#44) asked for are new, and six behaviours added since ([98](#98)'s starter
kit, [99](#99)'s binary check, [100](#100)'s `self-update`, [101](#101)'s `(unset)`, [66](#66)'s
`BOX_UPDATE_URL` and [69](#69)'s Python floor) are documented there too.

### 41
**The README never says how you give the agent a task.** This is the largest gap. `box run` starts
an interactive Claude session inside the sandbox (`build_run_command`, `box.py:431-437`, passes no
`-p`), so the user types the task at the prompt — but no section says so, and the reader has just
been told at `README.md:95` that "the agent runs unattended with nobody to answer follow-ups".
Those two facts read as a contradiction until you have run it once.
**Fix:** one sentence under Use — "`box run` drops you into a Claude session inside the sandbox;
type the task there" — and one clarifying that the built-in prompt tells the agent to make
assumptions and keep going rather than block on questions, because you may well walk away.

**Done** — in Quick start, with [94](#94)'s other half: the session is interactive so the user can
watch and step in.

### 42
**Add a quick start.** The path from nothing to a first run is spread over three sections
(`install` → `gen` → edit config → export the token → `mount-prompt` → `run`). Six lines at the
top would carry it.

**Done.**

### 43
**Add a short troubleshooting table.** Every refusal box can produce is already a good message,
but a reader hits them one at a time. A five-row table mapping message to fix ("kit is not set",
"model is not set", "`CLAUDE_OAUTH_TOKEN_FILE` is not set", "no path on this machine for", "has
uncommitted changes -- not removing it") would answer most first-week questions. Keep it to five
rows.

**Done** — six rows, since [97](#97) added a message a first-timer meets before any of the five.

### 44
**Give requirements their own heading.** `README.md:11-12` buries Python, `sbx`, `git`, `claude`
and the supported platforms in a paragraph between the feature list and Install.

Suggested shape: Features / Requirements / Install / Quick start / Commands / Settings / Mounts /
What you get back / Troubleshooting / Development. That is the same content at about 900 words.

**Done** — that shape, in that order.

---

## 5. CI, and macOS in particular

You rely on CI for macOS, so this section matters most. The short version: **the macOS leg
currently proves almost nothing.**

### 45
**The macOS runner buys close to zero signal.** `.github/workflows/ci.yml:14` runs the same four
steps on both legs, over a suite that touches the OS almost nowhere. `box.py` has exactly one
platform-sensitive call site (`sys.platform`, `box.py:805`) and the tests pass the platform in as
a literal (`tests/test_box.py:661,885,891,898`). The only real host interaction is `git init` /
`git check-ignore`, which behaves identically. So the leg costs macOS minutes and catches nothing.
**Fix:** keep `macos-latest`, but give it something to catch — [46](#46), [47](#47) and a `gen` /
`config` / `mount-prompt` smoke test run in a real temporary directory (on macOS that is under
`/var/folders`, on a case-insensitive filesystem).

### 46
**Nothing verifies box.py runs standalone with no dependencies — the project's central claim.**
`docs/goal.md:27,37`, `README.md:205` and `docs/sandbox.md:34-35` all assert it, and every CI step
runs *inside* the uv venv via `uv run`, which demonstrates the opposite. Verified locally that
`env -i PATH=/usr/bin:/bin ./box.py --help` works today; nothing stops that regressing. `--help`
exits during `parse_args`, before `warn_when_outdated`, so the step needs no network.

```yaml
- name: Runs standalone with no dependencies
  run: env -u VIRTUAL_ENV -u PYTHONPATH python3 -I box.py --help > /dev/null
```

### 47
**Nothing verifies box.py imports only the standard library.** Cheap to add as a test, so it runs
everywhere including in a sandbox. Verified it passes today — the imports are `__future__`,
`argparse`, `dataclasses`, `hashlib`, `json`, `os`, `pathlib`, `re`, `subprocess`, `sys`, `time`,
`urllib`, and `sys.stdlib_module_names` covers all of them including `__future__`:

```python
def test_box_imports_only_the_standard_library() -> None:
    """box.py must run with nothing installed, so every import is standard library."""
    tree = ast.parse(Path(box.__file__).read_text())
    modules = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            modules.update(alias.name.split(".")[0] for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
            modules.add(node.module.split(".")[0])
    assert modules <= set(sys.stdlib_module_names)
```

### 48
**The Python version is unpinned, and 3.11 — the advertised floor — is tested nowhere.** There is
no `.python-version`, no `python-version:` input to `setup-uv`, no `uv python install`, and
`uv sync` (line 23) is bare. uv resolves the *newest* interpreter satisfying
`requires-python = ">=3.11"`, downloading one if needed, so the version floats with uv releases
and runner images, and the two legs can silently land on different interpreters. Locally you are
on 3.14.4. `[tool.mypy]` also sets no `python_version`, so strict checking never sees 3.11
semantics either.
**Fix:**

```yaml
strategy:
  fail-fast: false
  matrix:
    os: [ubuntu-latest, macos-latest]
    python-version: ["3.11", "3.14"]
env:
  UV_PYTHON: ${{ matrix.python-version }}
```

plus `uv python install ${{ matrix.python-version }}`, and `python_version = "3.11"` under
`[tool.mypy]`.

### 49
**Seven pre-commit hooks are enforced only on your own machine.** CI runs `ruff check`,
`ruff format --check`, `mypy --strict` and `pytest`, which covers the three `local` hooks
correctly and in non-mutating form. Never enforced anywhere else: **`check-merge-conflict`,
`debug-statements`, `check-yaml`, `check-json`, `trailing-whitespace`, `end-of-file-fixer`,
`mixed-line-ending`**. A PR with CRLF endings, a stray `breakpoint()`, a conflict marker or a
malformed `.box/config.json` passes CI green. This matters more than usual because
`docs/sandbox.md:22-27` tells the in-sandbox agent that `pre-commit run -a` cannot run there and
that "the host's own run before merging is what catches those" — so whitespace hygiene in
agent-authored commits currently depends on you remembering. (The tree is clean today; I checked.)
**Fix:** one step, plus a cache on `~/.cache/pre-commit` keyed by the config hash:

```yaml
- run: uv run pre-commit run --all-files --show-diff-on-failure
```

### 50
**`.pre-commit-config.yaml:3` pins `pre-commit-hooks` at `rev: v4.0.1`,** several majors behind.
Run `pre-commit autoupdate`.

### 51
**`uv sync` does not verify the lockfile.** `.github/workflows/ci.yml:23`. A drifting `uv.lock`
never fails the build — uv silently re-resolves.
**Fix:** `uv sync --locked`.

### 52
**Missing workflow hardening.** `.github/workflows/ci.yml:1-15` has no `permissions:` block (add
`permissions: {contents: read}` at the top — the job needs nothing but the checkout), no
`concurrency:` group (superseded PR pushes keep burning macOS minutes, which bill at 10×), no
`timeout-minutes:` (default is 360), and no `workflow_dispatch:` trigger for manual re-runs.
`fail-fast: false` is already right and should stay, especially once [48](#48) adds an axis.

### 53
**Action versions are behind and nothing updates them.** `actions/checkout@v4` and
`astral-sh/setup-uv@v5` are both behind current majors. I could not reach the network from here to
confirm the exact latest, so verify before bumping.
**Fix:** bump, and add `.github/dependabot.yml` with `package-ecosystem: github-actions` so this
does not recur.

### 54
**There is no release trigger and no release automation; `main` *is* the release channel.**
Triggers are `push: branches: [main]` and `pull_request`; `git tag` returns nothing. Because
`UPDATE_URL` (`box.py:36`) points at `raw.githubusercontent.com/lk16/box/main/box.py`, every merge
to main instantly becomes an "update available" nag for every user — including comment-only
commits that change the file's bytes and nothing else — and a broken `main` is what new installs
`curl`.
**Fix (minimum):** make the CI job a required status check on `main`, so a failing `box.py` can
never reach the URL users install from. **Fix (better):** see section 10.

---

## 6. Things specific to one machine, one OS or one project

### 55
**`BASE_PROMPT` hard-codes a Linux host's home layout, and ships in every project's prompt.**
`box.py:107-110`:

> Host home directories are mounted under `/home/*/` and more than one exists, so join the glob
> matches rather than assuming a single path:
>
>     export PATH="$PATH:$(echo /home/*/.local/bin | tr ' ' ':')"

On a macOS host, homes are `/Users/<name>`. If sbx mounts them under `/Users/` inside the Linux
sandbox, the glob matches nothing, expands to the literal string, and gets appended to `PATH` as a
no-op — while the agent has been told a falsehood about where to look. This is the clearest
"author's machine" leak in the codebase, and it is exactly the class of thing you cannot test
without a Mac. (In this sandbox `/home` holds `agent`, `luuk` and `ubuntu`, which is why the glob
joins matches.)
**Fix:** glob both roots — `echo /home/*/.local/bin /Users/*/.local/bin 2>/dev/null` — or, better,
stop asserting a layout: "the host's home directory is mounted somewhere under `/home/` or
`/Users/`; find it rather than assuming". **Worth verifying on a macOS host if you ever get
access to one.**

**Done** — the second way: `BASE_PROMPT` asserts no layout at all now, and the `PATH` line is gone
with it. Nothing left there needs a Mac to verify.

### 56
**`BASE_PROMPT` leaks this repository's tooling into every project.** `box.py:102-104`:
"pre-commit is not installed here, so its git hook will not run." A project that does not use
pre-commit gets a non-sequitur, and `docs/goal.md:79-80` explicitly forbids this ("`BASE_PROMPT`
… holds only what is true of every sandbox"). The generic truth is the following sentence.
**Fix:** reduce to "Git hooks installed by the project's tooling are not set up here, so run the
project's own checks by hand before committing", and leave the pre-commit specifics in
`docs/sandbox.md`, which already covers them. `README.md:97` documents the sentence, so it changes
too. (`uv` is correctly absent from `BASE_PROMPT` — well done.)

**Done.**

### 57
**The mount prompt is told the platform but not the architecture.** `box.py:780`, `box.py:805`.
The prompt says "this machine, which runs {platform}" — `linux` or `darwin` — yet the problem it
exists to solve is architecture-specific: `.box/config.json:11` demands "a Linux build for this
machine's architecture" and `box.py:790` mentions "the wrong platform or architecture". An agent
told only `darwin` cannot tell arm64 from x86_64 and will fetch the wrong toolchain half the time.
**Fix:** pass `f"{sys.platform} {platform.machine()}"` — `platform` is stdlib, so the
zero-dependency rule holds. Update `tests/test_box.py:885-894`.

**Done** — through a `host_description()` helper, so the prompt builder still takes a string.

### 58
**ANSI colour is emitted with no TTY or `NO_COLOR` check.** `box.py:41-42`, `box.py:861`,
`box.py:877`. There is no `isatty` anywhere in the repository, so `2>&1 | tee`, a log file or a CI
capture gets literal `\033[31m`. `docs/goal.md:34` justifies stderr "so a piped stdout stays
clean" but never considered piped stderr. `tests/test_box.py:777-782` asserts the codes are always
present, so it changes with the fix.
**Fix:** a `colour()` helper returning plain text when `not sys.stderr.isatty()` or `NO_COLOR` is
set; assert both branches.

**Done** — as `in_red`, with both branches asserted.

### 59
**`~/.cache` as the fallback is an XDG (Linux) convention applied to macOS.** `box.py:822-827`.
Defensible — uv, ruff, pip and gh all do the same — but it should be a stated decision rather than
an accident; `docs/goal.md:32` mentions `XDG_CACHE_HOME` without acknowledging macOS at all.
**Fix:** one sentence in goal.md saying box follows XDG everywhere on purpose. (Do not switch to
`~/Library/Caches` without also changing `tests/test_box.py:712-715`; I would not switch.)

**Done** — the sentence is there and the path is unchanged.

### 60
**`~/.local/bin` is not on `PATH` by default on macOS.** `README.md:16-20`. Debian and Ubuntu add
it from the stock `~/.profile` when the directory exists; macOS's default `PATH` comes from
`/etc/paths` and never includes it. The README sidesteps this with the alias at `README.md:26`
pointing at the full path — but that means `box` exists only in interactive shells, and is
invisible to scripts, `Makefile`s, `direnv` hooks, cron and other agents.
**Fix:** see section 10 — install the file as `box`, with no extension, and recommend putting
`~/.local/bin` on `PATH`, keeping the alias as the fallback.

**Done** — with one deviation: the alias is gone rather than kept as a fallback. A file named `box`
needs none, and the README says which startup file puts `~/.local/bin` on `PATH`.

### 61
**The shell-config instruction is Linux-bash-shaped.** `README.md:22` says "`~/.bashrc` or
`~/.zshrc`". macOS bash reads `~/.bash_profile` for login shells, so a Homebrew-bash user
following this literally gets nothing.
**Fix:** "your shell's startup file (`~/.zshrc` on macOS, `~/.bashrc` on Linux,
`~/.bash_profile` for login bash)".

**Done.**

### 62
**`docs/agent.md` does not exist.** `README.md:60,76` and `tests/test_box.py:23` both use
`"prompt_file": "docs/agent.md"`, while this repository's real prompt file is `docs/sandbox.md`
(`.box/config.json:8`). The README example reads as "this is box's own config" and names a file
that is not there.
**Fix:** use `docs/sandbox.md`, or rename the example to something unmistakably generic.

**Done** — `docs/sandbox.md` in the README, which is labelled as box's own config, and a plainly
generic `docs/project-prompt.md` in the tests.

### 63
**The README's config example is this repository's config without the label.** `README.md:56-63`
copies `"kit": ".sbx/kit"` and `"model": "claude-opus-5"` straight from `.box/config.json`. A
reader will paste a model id that may not be valid for their account.
**Fix:** label it ("box's own `.box/config.json` looks like this") or genericise it.

**Done** — labelled.

### 64
**The Go mounts example uses a customised path.** `README.md:126-129` gives
`"go_mod_cache": "~/.local/go/pkg/mod"`. Go's default `GOMODCACHE` is `~/go/pkg/mod`. Presenting a
personal layout as the obvious answer is unfortunate in the very section teaching that mounts are
machine-specific. (`"go_toolchain": "/usr/local/go"` is Linux-shaped too, but there that is
arguably the point.)
**Fix:** `~/go/pkg/mod`.

**Done.**

### 65
**`uv sync --python 3.14` is baked into a committed, shared file.** `.box/config.json:12` and
`docs/sandbox.md:29`. sandbox.md explains the failure mode and the recovery well; the mount
*description* — which is what the mount-prompt agent actually reads — does not, and hard-codes the
image's Python.
**Fix:** "the Python the sandbox image ships — `python3 -V` inside the sandbox names it".

**Done.**

### 66
**`UPDATE_URL` hard-codes your repository.** `box.py:36`. Any fork or vendored copy nags "An
update to box is available" on every run, forever, with no opt-out.
**Fix:** `os.environ.get("BOX_UPDATE_URL", …)`, treating an empty value as "never check". This
also gives [15](#15) an escape hatch.

**Done.**

### 67
**`.sbx/kit/spec.yaml` is box's own arrangement and is copy-hazardous.** `README.md:88` points
readers at `.sbx/kit` by name, but the allowlist is `api.anthropic.com:443` and nothing else
(lines 7-18), the clipboard-bridge removal (22-23) and the virtiofs uv-cache discovery block
(26-46) are entirely specific to box. A reader who copies it gets a sandbox where every dependency
download 403s.
**Fix:** a one-line header comment saying this kit is box's own, not a starting template.
(The `grep " virtiofs " /proc/mounts` is Linux-only but runs *inside* the sandbox, which is always
Linux — correct, not a bug.)

**Done** — and it now points a reader at the starter kit [98](#98) writes instead.

### 68
**`.box/config.json:11` is exemplary and should be the model for [57](#57).** "it must be a Linux
build for this machine's architecture, so on macOS put one in `.box/deps/` and point here instead"
is exactly the right way to encode a per-machine, per-architecture requirement — which is what
makes `BASE_PROMPT`'s `/home/*/` assumption ([55](#55)) look like an oversight rather than a
decision.

**Nothing to change** — [57](#57) took it as the model, as suggested.

### 69
**`box.py` most likely runs on Python 3.9 despite claiming 3.11+, and there is no guard.**
`README.md:11` and `pyproject.toml:5` say 3.11+. The shebang is `#!/usr/bin/env python3`, which on
a stock macOS resolves to Xcode's 3.9.6. I checked: `ast.parse(..., feature_version=(3, 8))`
succeeds, `from __future__ import annotations` defers every annotation, and there is no `match`,
no runtime `X | Y` and no 3.10+ stdlib call — so box would run on 3.9 and only break somewhere
obscure, which is worse than failing loudly.
**Fix:** either add `if sys.version_info < (3, 11): sys.exit("box needs Python 3.11 or newer")`
near the top, or — better, given the macOS system Python — lower `requires-python` to what box
actually needs and prove it in CI per [48](#48). The dev tooling can keep a higher floor
independently.

**Done** — the guard, not the lowered floor: `run_box` names both versions and exits 1 before doing
anything. `requires-python` stays at 3.11, and a test pins that `box.py` still parses on 3.9, which
is what lets that message reach the reader at all. Proving the floor in CI is still [48](#48).

---

## 7. Tests

159 tests, 0.4s, and the pure helpers are well covered. Two structural gaps stand out: the
functions that actually touch `sbx` and `git` are almost all monkeypatched away and never
executed, and `main()` is untested end to end.

### Clarity

### 70
**Two pairs of tests are duplicates.** `test_gen_leaves_a_project_whose_deps_dir_box_accepts`
(`tests/test_box.py:654`) and `test_gen_leaves_a_project_box_will_run_in` (line 995) are
byte-identical. `test_mount_prompt_needs_no_mounts_file_at_all` (908) and
`test_mount_prompt_asks_about_a_name_the_mounts_file_lacks` (943) are the same scenario with the
same assertion.
**Fix:** delete one of each.

**Done.**

### 71
**`test_build_create_command_omits_empty_kit` (492) tests two things,** the first of which
`test_build_create_command_includes_mounts_and_kit` already pins exactly. Drop lines 493-494.

**Done.**

### 72
**`test_every_config_key_is_snake_case` (1092) does not test snake_case** — `key == key.lower()`
also passes for `root-size` and `rootsize`.
**Fix:** `re.fullmatch(r"[a-z][a-z0-9_]*", key)`.

**Done.**

### 73
**`test_config_takes_the_same_flags_as_run` (689) does not test what it claims** — it parses one
flag on `config`.
**Fix:** compare the full `vars()` key sets of both parses, or rename the test.

**Done.**

### 74
**Names that mislead.** `test_suggest_branch_name_gives_claude_five_seconds` (387) asserts 10
(see [22](#22)). `test_the_prompt_offers_the_deps_dir_when_nothing_here_fits` (660) also asserts
"The sandbox runs Linux". `test_merge_values_prefers_cli_over_file` (80) also asserts the subtler
rule that a `None` CLI value leaves the file value alone, which deserves its own test.

**Done.**

### 75
**The test helper `build_config` (42) shadows `box.build_config` with a different signature** —
`(values, directory)` versus `(values, mounts, working_directory)`. A reader of
`build_config({}, tmp_path)` is misled.
**Fix:** rename to `config_from_values`.

**Done.**

### 76
**`make_config()` makes `require_settings` depend on the process working directory.** Its
`kit=".sbx/kit"` is resolved relative to `os.getcwd()`, so
`test_require_settings_accepts_a_complete_config` (1074) and
`test_prepare_launch_rejects_a_committable_mounts_file` (671) pass only because the
repository root happens to contain a `.sbx/kit` *directory*. Run pytest from elsewhere and they
change meaning.
**Fix:** use an absolute `tmp_path` kit, or a name that cannot exist on disk.

**Done** — `make_config` names a kit that is not on disk, so nothing resolves against the process
working directory.

### 77
**The module docstring is stale.** `tests/test_box.py:1` says "Tests for the pure configuration
and command-building helpers"; the file now runs `git init`, writes files and captures stderr.

### Missing coverage

**Done.**

### 78
**`main()` is completely untested.** Nothing covers the `ConfigError` → `box: <error>` → exit 1
path, `config` returning before `prepare_launch`, `SETUP_COMMANDS` routing through
`require_no_flags`, or `warn_when_outdated` running before the try. This is the whole control flow
of the tool.
**Fix:** drive `main()` with `monkeypatch.setattr(sys, "argv", …)` plus `chdir(tmp_path)`, stubbing
`run_session` and `warn_when_outdated`.

**Done.**

### 79
**`store_secret` is untested, including the security constraint `docs/goal.md:103` calls out** —
"never put the token on a command line". This is the highest-value missing test in the suite:
assert `keywords["input"] == token` and `token not in " ".join(command)`.

**Done.**

### 80
**The `store_secret`-before-`sbx create` ordering is untested,** though `docs/goal.md:104` says a
secret stored after create leaves the agent logged out. A reordering regression is invisible
today.
**Fix:** an order-recording fake in the style of `FakeSandbox`.

**Done.**

### 81
**The `sbx create` failure path is untested** (`docs/goal.md:97`): that a non-zero create returns
1, drops the secret again, prints the "never started" line, and never runs `sbx run` or `cleanup`.
Likewise `run_session`'s `finally: cleanup(…)` — nothing asserts cleanup still runs when the agent
exits non-zero, nor that the agent's exit code is propagated ([35](#35)).

**Done.**

### 82
**Every real `git` and `sbx` invocation is monkeypatched away and never executed.**
`count_new_commits`, `new_commit_subjects`, `local_branch_names`, `create_branch`, `delete_ref`,
`sandbox_refs`, `settle_sandbox_refs`, `taken_names`, `drop_secret`, `capture` — the actual
command strings (`rev-list --count HEAD..<c>`, `log --format=%s`, `for-each-ref refs/heads`,
`git branch`, `update-ref -d`, `for-each-ref --format=%(refname) %(objectname) refs/sandboxes/…`)
are asserted nowhere. Only their parsers are tested.
**Fix:** these are command builders in all but name. Either split the builder from the runner and
assert on the `list[str]`, or exercise them against a real `tmp_path` repository — you already do
that for `git check-ignore` and it costs milliseconds.

**Done.**

### 83
**Other untested functions,** in rough order of value: `capture`'s "empty string on non-zero exit"
contract (relied on by eight call sites), `prepare_launch`'s happy path, `build_environment`
inheriting `os.environ` (a regression to `{}` + limits would strip `PATH` from the sbx call and
nothing would catch it), `warn_dirty`'s three recovery lines (the entire point of the function),
`setup_command`'s dispatch, `is_git_ignored` in isolation, `fetch_remote_hash` using `UPDATE_URL`
and the timeout, `format_config`'s column alignment, `to_flag`, `resolve_path`, and
`mount_prompt` passing the real `sys.platform` through.

**Done** — every function listed is covered.

### 84
**Two small gaps in otherwise-tested functions:** `read_config_file` has no test for a JSON array
though `read_mounts_file` does, and `read_token` has no test for a missing file, only an empty
one.

**Done.**

### 85
**`settle_ref`'s success message is never asserted.** The failure branches check stderr; the line
the user reads on a normal run — "`<n>` commits from `<ref>` are on branch `<branch>`" — has no
test. Fixing [13](#13) would be caught by it.

### Smells

**Done.**

### 86
**Thirteen tests shell out to real `git init` without isolating git configuration.** This is fine
in principle — `is_git_ignored`'s contract *is* `git check-ignore`'s semantics, and a fake would
only re-implement it — but a developer with a global `core.excludesFile` matching `.box/` or
`*.json`, or a global `init.templateDir`, changes the outcome of
`test_a_committable_mounts_file_is_rejected` (613), `test_a_committable_deps_dir_is_rejected`
(623) and `test_gen_creates_a_gitignore_holding_every_local_path` (1001). CI runners have no
global gitignore, so the asymmetry stays invisible until someone else clones.
**Fix:** there is no `conftest.py` at all — add one with an autouse fixture setting
`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_SYSTEM=/dev/null` and `GIT_CONFIG_NOSYSTEM=1`.

**Done.**

### 87
**`test_warn_when_outdated_stays_silent_when_the_check_fails` (785) passes for the wrong reason.**
`XDG_CACHE_HOME=/nonexistent/unwritable` is never written to, because the stubbed
`fetch_remote_hash` raises *before* `store_check_time` is reached. It asserts nothing about
unwritability, and would create `/nonexistent` if the ordering changed and the suite ran as root.
**Fix:** point it at `tmp_path`, and add a separate test that a genuinely unwritable cache
directory is survived.

**Done.**

### 88
**`FakeSandbox.capture` dispatches on `if "status" in command`** (441) — a token match over the
whole argv. Any future cleanup command containing a `status` argument silently takes the
dirty-check branch. Match the command prefix instead.

**Done.**

### 89
**Five tests monkeypatch `subprocess.run` globally** (383, 394, 403, 411, 419) plus
`FakeSandbox.install` (438). It does not leak across tests, but within a test it swallows every
subprocess call anywhere, and the fakes take `**keywords`, so a wrong call site is absorbed rather
than failing. Prefer patching the narrow seam (`box.capture`, `box.suggest_branch_name`).

**Done.**

### 90
**`FakeRepository.__init__` takes three unlabelled positionals plus a post-construction flag**
(274). `FakeRepository("", "add-retry-logic", set())` requires reading the class to learn that
`""` means "git could not count". Keyword-only arguments, and move `refuse_branch` into the
constructor.

**Done.**

---

## 8. Where a human or an AI gets confused

### 91
**`CLAUDE.md` contains no commands at all,** so an agent that reads it and stops has nothing to
run. It points at `docs/style.md`, whose command (`uv run pre-commit run -a`) is the one that
*cannot* work inside a box sandbox — which `docs/sandbox.md:22-27` then has to correct. Three
files, three answers, and the first one an agent reads is the wrong one.
**Fix:** put the two-line check block directly in `CLAUDE.md`, with a one-clause note that inside
a box sandbox the commands in `docs/sandbox.md` apply instead.

**Done.**

### 92
**A generic `CLAUDE.md` one directory up contradicts this project's tooling.**
`/home/luuk/projects/CLAUDE.md` (outside the repository, so not fixable here) tells agents to use
`pip install -r requirements.txt`, `black`/`autopep8` and `pylint`/`flake8`. None apply — this
project uses `uv`, `ruff` and `mypy`. Because the repository's own `CLAUDE.md` states no commands
([91](#91)), an agent has nothing closer to fall back on.
**Fix:** [91](#91) fixes this from inside the repository, which is the right place.

**Done** — by [91](#91), as suggested. The file one directory up is still there and still wrong;
`CLAUDE.md` now answers first.

### 93
**Nothing says that `docs/sandbox.md` *is* this repository's `prompt_file`.** `.box/config.json:8`
points at it, but `CLAUDE.md:10` describes it only as "what is true of this repository inside a
box sandbox". So editing that file silently edits the system prompt of every box run on this
repository.
**Fix:** add "(this repository's own `prompt_file`)" to `CLAUDE.md:10`.

**Done** — with the consequence spelled out: editing it edits the system prompt of every run here.

### 94
**"Unattended" versus an interactive session.** See [41](#41) — the built-in prompt tells the
agent nobody is available to answer follow-ups, while `box run` opens an interactive Claude
session. Both are intentional, and neither doc reconciles them.

**Done** — reconciled in the README and in `goal.md`, as a decision rather than an oversight: the
session is interactive so the user can see what the agent does and step in, and the prompt tells it
to assume and keep going because the user may well walk away, where an agent waiting on a question
they never see wastes the whole run.

### 95
**The docs restate the README rather than linking to it, and have already drifted.**
`README.md:105-166` and `docs/goal.md:44-58` cover the mount contract almost line for line;
`README.md:191-197` and `docs/goal.md:92-102` likewise. [22](#22) is that drift materialising —
the same wrong "five seconds" in both.
**Fix:** have `goal.md` carry the *why* and link to the README for the *what*.

**Done** — `goal.md` says so at the top of its constraints, and the mount contract it restated is now
reasons plus a pointer.

### 96
**`.box/config.json` in this repository is both a working config and the de-facto example,** and
nothing labels it as either. Same for `.sbx/kit` ([67](#67)) and the README's example ([63](#63)).

**Done** — `CLAUDE.md` says the config, the kit and the prompt file are box's own working setup, not
templates, and names `box gen` as where a new project starts.

---

## 9. Suggested improvements

Scoped deliberately: each of these removes friction that exists today, and none adds a subsystem.

### 97
**`box run` should say "run `box gen`" when a project has no `.box/` at all.** Verified: in a
fresh repository, `box run` says "kit is not set", which tells a first-timer nothing about what to
do first.
**Fix:** if `.box/config.json` does not exist, say so and name `box gen`. Two lines.

**Done** — checked before any setting is mentioned, so "kit is not set" no longer greets a
first-timer.

### 98
**`box gen` should scaffold a starter `.sbx/kit/spec.yaml`.** `kit` is required and has no
default, and hand-writing a kit is the single biggest setup hurdle — the one step `box gen`
currently leaves entirely to the user. A minimal "allow `api.anthropic.com`, nothing else" spec is
what most projects want as a starting point, and it is a template constant next to `BASE_PROMPT`,
which `docs/goal.md:60-61` already establishes as the right home for such text.
**Caveat:** it couples box to sbx's kit schema, which can drift. Write only the two or three
fields you are confident in, and have the file say it is a starting point.

**Done**, with one deviation: it lands at `.box/kit/spec.yaml`, not `.sbx/kit`, so everything box
writes still lives under `.box/` — and the starter config points `kit` at it, leaving `model` as the
only value to fill in. The file says it is a starting point and holds only the fields box is sure of.

### 99
**Check the required binaries up front.** `README.md:11-12` lists `sbx`, `git` and `claude` as
requirements and nothing verifies any of them; the result is [3](#3).
**Fix:** a `shutil.which` check in `require_settings` producing the usual `box: …` message. This
is most of a `box doctor` without adding a command.

**Done** — in `require_project` rather than `require_settings`, since it is a fact about the machine
and not about a setting, and first, since nothing else box does works without them.

### 100
**Add `box self-update`.** The update check already knows the URL and both hashes; the user
currently copy-pastes a `curl` line. Roughly ten lines — fetch, write to a temporary file
alongside the script, `chmod +x`, `os.replace` — and it removes the last manual step from the
install story ([15](#15) and [14](#14) both stop mattering).

**Done** — and the notice names the command, so [14](#14)'s quoted `curl` line is gone with it. It
refuses a `box.py` git tracks, which is [15](#15)'s hazard from the other side.

### 101
**Show `(unset)` rather than an empty column in `box config`.** Verified: unset settings print as
trailing whitespace, so "is `model` empty or is the row missing?" is a real question a reader has.

**Done.**

### 102
**Enforce the docstring rule with ruff instead of by hand.** Add `"D"` to
`[tool.ruff.lint] select` with `[tool.ruff.lint.per-file-ignores] "tests/*" = ["D103"]`. That
turns `docs/style.md:18-19` into a check rather than a promise, and resolves [31](#31) in the
direction of keeping the rule.

**Done** — `D107` is ignored under `tests/` as well, since a fake's class docstring covers its
`__init__`. `docs/style.md` says so.

### 103
**Not recommended, listed so it is a decision rather than an oversight:** `box ls` for leftover
sandboxes and refs (`sbx ls` and `git for-each-ref` already do it), shell completion, `box config
--json`, and passing an initial task on the command line. The last is the only tempting one — it
would make `box run "do the thing"` fire-and-forget and would reconcile [94](#94) — but it changes
box from "start a session" to "start a session and drive it", which is a different tool. Decide
deliberately.

**Decided: none of them.** [94](#94) is reconciled in the docs instead, which is what it needed.

---

## 10. Publishing box

Today: users `curl` `box.py` from `main` into `~/.local/bin`, `chmod +x` it, and add a shell
alias. Should that change?

### A. Status quo — a single file from `main`, plus an alias

**For:** no packaging, no release process, no version to bump. The whole tool is one readable
file, which is a genuine selling point for something that runs an agent against your repository.
The hash-based update check works *because* there is one file and one canonical URL. It runs on
any `python3`. Uninstalling is deleting one file and one alias.

**Against:** the install is four manual steps. The alias exists only in interactive shells, so
`box` is invisible to scripts, `Makefile`s, `direnv`, cron and other agents. Every installer gets
whatever is on `main` at that moment, so a broken `main` breaks new installs ([54](#54)) and
byte-level churn nags every user ([15](#15), [66](#66)). No pinning, no rollback, no changelog, no
`--version`.

### B. PyPI (`uv tool install` / `pipx install`)

**For:** a real `box` on `PATH` with no alias; `uv tool upgrade box`; versions, pinning, rollback,
a changelog; `--python 3.13` pins the interpreter, which would settle [69](#69) outright. Still
zero runtime dependencies.

**Against:** needs a free name — `box` on PyPI is very likely taken (`python-box` exists); verify
before committing. Adds a build backend, a version number and a trusted-publishing release
workflow. A version number directly contradicts `docs/goal.md:29-30` and makes the hash-based
update check dead code. The "one file you can read end to end" property is diluted.

### C. Homebrew tap

**For:** `brew install lk16/box/box`, `brew upgrade`, native on macOS — the platform you cannot
test on.

**Against:** a second repository, a formula bump per release, Linuxbrew for Linux users. Heavy
machinery for a 900-line script, and it does not solve anything A cannot.

### D. GitHub Releases plus an `install.sh`

**For:** versioned artifacts, a real `box` on `PATH`, an honest changelog, `box --version`.

**Against:** `curl … | sh` is a pattern many people refuse on principle. Needs a release process.
Buys little over A once A's rough edges are filed off.

### E. Keep A, and add a PEP 723 header to the file

**For:** a `# /// script` block naming `requires-python` lets `uv run box` pick and download a
matching interpreter, which removes the "the Mac's `python3` is 3.9" problem ([69](#69))
completely. It is a comment block, so the file still runs under a bare `python3`. No release
process.

**Against:** only helps people who have `uv`. Running from the URL each time costs a round trip
and does not work offline.

### Recommendation

**Stay on A, and spend the effort on its rough edges instead.** A is the right shape for this
project: the single readable file is a feature, not a limitation, and `docs/goal.md`'s
no-version constraint is a deliberate choice that B and D both overturn. Four changes get most of
B's benefit for a fraction of the cost:

1. **Publish the file as `box`, with no extension.** The install becomes one `curl` and one
   `chmod`, the alias disappears, and `box` starts working in scripts, `Makefile`s and other
   agents' shells ([60](#60)). This is the single biggest friction win available.
2. **Add `box self-update`** ([100](#100)), so updating is a command rather than a copy-paste.
3. **Make CI a required check on `main`** ([54](#54)), since `main` is the release channel and
   nothing currently stops a broken `box.py` reaching the URL users install from.
4. **Settle the Python floor** ([48](#48), [69](#69)) — verified in CI — and optionally add the
   PEP 723 header from E for `uv` users.

Revisit B when box has users outside this machine. The thing that will force the change is
needing to say "upgrade to 1.4.2, it fixes X" — and nothing needs that today.

**Decided: A, with three of the four rough edges filed off.**

1. **Done.** The README installs the file as `~/.local/bin/box` and drops the alias rather than
   keeping it as a fallback ([60](#60)) — a file named `box` needs none.
2. **Done** ([100](#100)). The notice names the command instead of a `curl` line.
3. **Open.** Making CI a required check on `main` is a repository setting, and the CI job it would
   gate is still section 5's work ([54](#54)).
4. **Half done.** `box.py` names the Python it needs and stops on an older one ([69](#69)); proving
   that floor in CI is [48](#48), still open. No PEP 723 header: it helps only `uv` users, and E's
   round trip is a worse trade than the guard.

Nothing here moves box off A, and no version number was introduced, so `docs/goal.md`'s no-version
constraint stands.
