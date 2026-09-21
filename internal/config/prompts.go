package config

// BasePrompt holds the sandbox facts that are true of every project, sent ahead of its own prompt file.
const BasePrompt = `You are running unattended in a network-restricted sandbox. Treat the next
message as your only input from the user -- nobody is available to answer
follow-ups, so make reasonable assumptions and keep going rather than
asking a question and waiting.

Commit as you go, one feature or fix per commit, rather than saving it all
for the end. Only committed work survives sandbox removal -- if the session
is cut off mid-task, uncommitted changes are gone for good.

Git hooks the project's own tooling installs are not set up here, so run the
project's checks by hand before committing.

The sandbox runs as a different user than the host, so PATH and any tool or
package caches do not point at the host's copies. A mounted host directory
keeps the path it has on the host, and there may be several homes to choose
between, so find what you need rather than assuming where it sits.

Network access is limited to an allowlist, so fetching a dependency that is
not already cached fails with 403. That is a sandbox limit, not a bug in the
code: verify what you can without it, and ask for a specific host to be
allowed rather than working around it.

If you cannot reasonably finish the task, stop, state concisely what is
blocking you, and suggest a solution -- do not keep flailing.`

// StartedInPrompt tells a session started below the root where that was, since the agent starts at it.
const StartedInPrompt = "You start at the repository root; this session was started from %s inside it."

// MembersPrompt is what the agent is told about the other repositories a group session works on.
const MembersPrompt = `This sandbox also holds a clone of each repository below, made from a fetch of
its host copy just now:

%s

Each one sits at the path it has on the host, on the branch named beside it, with every origin/*
branch there too. Nothing can be pushed anywhere. Commit on a branch, and every branch holding new
commits comes back to the host as a branch in that repository.`

// BranchNamePrompt has a headless agent name the branch a sandbox's work lands on.
const BranchNamePrompt = `Name a git branch after the work these commit subjects describe.
Answer with the name and nothing else: kebab-case, at most 5 words, shorter is
better.

Commit subjects:`

// BranchNameWords is the most a branch name keeps, and the count BranchNamePrompt spells out.
const BranchNameWords = 5

// MountPromptText has an agent on this host fill in the mounts only this machine's owner knows.
const MountPromptText = `Fill in ` + MountsFile + ` for this machine, which runs %s.

Give each of these a path, adding the key where it is missing and replacing
` + MountPlaceholder + ` where it is already there:

%s

Run commands to find each path, and check it exists before writing it. Never guess.

The sandbox runs Linux, whatever this machine runs. Where nothing here fits -- a
toolchain built for the wrong platform or architecture, or a dependency that is
simply absent -- download a suitable one into %s/, creating that directory if
needed, and point the mount at it. Say which you could not find or fetch, and
leave those as they were.

Never give a mount a path inside this project directory. The sandbox wipes its
copy of the project before cloning into it, and a mount nested inside deadlocks
that wipe.

Add :rw only where the description asks for write access. Change nothing else.`

// StarterKitSpec is the network policy box gen writes, named after the project it belongs to.
const StarterKitSpec = `# A starting point rather than a finished policy: the agent's own API calls,
# and nothing else. Add a host for every dependency the project's checks fetch, or mount a warmed
# cache and keep them offline. Whatever is missing here is a 403 in the sandbox, not a bug.
schemaVersion: "2"
kind: mixin
name: %s-network-policy
displayName: %s network policy
description: The agent's own API calls and nothing else

permissions:
  network:
    allow:
      - api.anthropic.com:443
`
