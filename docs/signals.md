# Ctrl-C

`box run` exits 130 on a Ctrl-C, and the whole of the handling is in `cmd/box/main.go`.

The subtlety is that `box run` hands the terminal to an interactive Claude session, and a Ctrl-C
typed there goes to the whole foreground process group: box and the agent both get it. The agent
answers it itself, and box must not take that as a reason to exit — the run is not over, and
skipping out would leave the sandbox behind with no attempt to bring its committed work back.

So box counts the children holding the terminal (`system.ChildAttached`), and its signal handler
does nothing while that count is above zero. The child finishes, `sbx run` returns, and cleanup
runs exactly as it does after any other exit. Everywhere else — resolving settings, fetching a
member, settling refs — a Ctrl-C means "stop", so box says `box: interrupted.` and exits 130.
