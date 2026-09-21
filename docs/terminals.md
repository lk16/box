# Knowing a terminal from a pipe

box asks twice whether a stream is a terminal, and gets it wrong in two different ways if it
guesses:

- **stderr**, because the update notice is coloured only where escape codes are read as colour. A
  pipe, a log file or a CI capture would otherwise be handed them as characters.
- **stdin**, because `box gen` asks one question, and only where there is someone to answer it. A
  script must get today's defaults without answering anything.

The obvious stdlib answer — `os.Stdin.Stat()` and a check for `os.ModeCharDevice` — is wrong.
`/dev/null` is a character device, so `box gen < /dev/null` would sit there asking a question
nobody can answer. Go's standard library has no `isatty`, and `golang.org/x/term` is the usual way
to get one.

box does it without the dependency instead: `internal/system/terminal_unix.go` asks the kernel for
the stream's terminal settings, which is the one question a terminal answers and a pipe, a file
and `/dev/null` all refuse. The ioctl is spelled differently per platform (`TCGETS` on Linux,
`TIOCGETA` on macOS), so each one is a three-line file behind a build tag, and every other
platform answers "not a terminal" — box supports Linux and macOS and nothing else.
