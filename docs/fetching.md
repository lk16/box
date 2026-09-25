# Fetching a group's members

A group session brings every member's `origin/*` refs up to date before it bundles one, because
current data is the whole point of the session. Each fetch is a round trip to a git host, so ten
members used to mean ten waits one after another, with the slowest deciding when the agent started.

box runs them all at once instead: `internal/session/fetch.go` starts one goroutine per member and
waits for the lot. The wait is now the slowest single fetch rather than the sum of them.

## Why they are captured rather than shown

Fetches that run together cannot share a terminal. Two of them writing progress to the same stderr
interleave into something no one can read, and two of them *asking* something — an ssh passphrase,
a password — would be worse: the prompts say nothing about which member they belong to, and
whatever is typed reaches whichever process happens to read it first.

So a parallel fetch owns no terminal. Its output is captured, and nothing is printed until every
fetch is done; each member is then reported in order of its name, its own output under its
own name. The order does not depend on which host answered first, so two runs of the same group
read the same way.

Not owning a terminal is not enough on its own, because git and ssh both reach past their streams
to `/dev/tty` when they want to ask something. `FetchEnvironment` turns both off:

- `GIT_TERMINAL_PROMPT=0`, so git reports a credential it does not have rather than asking for one.
- `GIT_SSH_COMMAND=… -o BatchMode=yes`, so ssh fails rather than asking for a passphrase. Whatever
  the user already set `GIT_SSH_COMMAND` to is kept, with the option appended to it.

A command reads the last spelling of a variable, so both override what the user exported.

## Why a failed fetch is asked again

A fetch that may not ask cannot get a passphrase, so one that needed one fails. box does not treat
that as the answer: every member whose parallel fetch failed is fetched again, alone, with box's
own terminal and box's own environment — which is what a fetch used to get. The prompt appears with
only that member's name above it, and whatever is typed can only reach the one process that is
running.

That means a member whose fetch really is broken is fetched twice before the run stops. It costs a
second round trip on the way to an error that was going to be reported anyway, and it is what keeps
the parallel pass from turning a passphrase into a refusal.

One case gets slower rather than faster: a `core.sshCommand` in git's own config is overridden by
the `GIT_SSH_COMMAND` the parallel pass sets, so a fetch that needs it fails and is asked again
with the config that does apply. The second fetch works, which is why the setting is an environment
variable rather than a `git -c` flag — the retry has to be able to drop it.
