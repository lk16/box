# Settings

Settings you use every run belong in `.box/config.json`, which is committed with the project.
box's own looks like this:

```json
{
  "kit": ".sbx/kit",
  "model": "claude-opus-5",
  "prompt_file": "docs/sandbox.md",
  "memory": "8g"
}
```

A command line flag wins over that file, which wins over the built-in default. `box config` prints
what a run would end up with, showing `(unset)` where nothing was given.

| Flag | `.box/config.json` key | Default | Meaning |
| --- | --- | --- | --- |
| `--name NAME` | `name` | directory name, kebab-cased (`box` when it holds no letters or digits) | Sandbox base name; a `-1`, `-2`, … suffix is added per run. |
| `--memory SIZE` | `memory` | `4g` | Memory limit for the sandbox. |
| `--cpus N` | `cpus` | `4` | CPUs allocated to the sandbox. |
| `--root-size SIZE` | `root_size` | `10g` | Sandbox root filesystem size. |
| `--docker-size SIZE` | `docker_size` | `10g` | Sandbox Docker storage size. |
| `--model MODEL` | `model` | — (required) | Model passed to the Claude CLI. |
| `--prompt-file PATH` | `prompt_file` | unset | File added after the built-in prompt. |
| `--kit REF` | `kit` | — (required) | `sbx` kit holding the sandbox's network policy. |
| `--template REF` | `template` | unset | `sbx` template the sandbox's container image comes from. |
| — | `required_mounts` | `{}` | Mounts the project needs, as name to description (see [Mounts](mounts.md)). |
| — | `secret_hosts` | `{}` | Tokens the agent may use, as variable name to host (see [Secrets](secrets.md)). |
| — | `mcp` | `[]` | MCP servers the sandbox may use, as a list of names (see [Read-only tools](mcp.md)). |
| — | `repos` | `{}` | Other repositories this session works on (see [Groups](groups.md)). |
| `--mount PATH` | — | none | Extra workspace, repeatable. Read-only; append `:rw` for read-write. |

Anything unknown in `.box/config.json` is an error, so typos surface immediately. Every setting is
text, though `"cpus": 4` works as well as `"cpus": "4"`. A `null`, a `true` or a list is an error
that says which key holds it. `required_mounts`, `secret_hosts` and `repos` are the keys holding an
object, and `mcp` is the one holding a list.

## kit and model have no default

An unset kit would leave the sandbox's network access to whatever `sbx` grants, and an unset model
would leave the choice to the sandbox's own Claude install, which is not this host's. Both are
errors rather than guesses.

`kit` points at the directory holding a `spec.yaml`, not at the file inside it. `box gen` writes a
starter policy at `.box/kit/spec.yaml`, allowing the agent's own API calls and nothing else, and
points `kit` at it, so `model` is the only setting left to fill in. Widen the allowlist for whatever
the project's checks fetch, or point `kit` at a policy you keep elsewhere. box's own kit lives in
`.sbx/kit`, which is sbx's convention.

## template

`template` names the image the sandbox runs on. Unset, which is the default, leaves that to `sbx`,
and its own agent image is what almost every project wants. Set it when the project needs something
that image does not have — a compiler, say, which a kit allowing only `api.anthropic.com` gives the
agent no way to install from inside. Producing that image is yours to do: `docker build` on the host
from sbx's base plus whatever you need, then `docker save` and `sbx template load`, and put the tag
`sbx template ls` shows here. box only passes the value on. An image the sandbox runtime does not
hold is a pull error at create time that says nothing useful, so load it before you run.

## Where settings are read from

Settings come from the working directory's `.box/`, and the whole repository around it is what the
sandbox gets. So a folder below the repository root can hold a `.box/` of its own: the sandbox is
named after that folder, the agent still starts at the root, and the prompt tells it which folder
you ran box in.

`CLAUDE_OAUTH_TOKEN_FILE` points at a file holding a token from `claude setup-token`. It is the one
setting with no flag and no config key, so that a shared project file can never point at someone
else's credentials. box refuses to start without it, or if the file it points at is missing or
empty.

## The system prompt

box always prepends a built-in prompt covering what is true of every sandbox — that the session is
unattended, that the network is an allowlist, that only committed work survives. It lives in
`internal/config/prompts.go` as `BasePrompt`.

`prompt_file` adds what is true of one project, such as its test command and its quirks. It is
appended after the built-in prompt, so it can qualify anything above it.
