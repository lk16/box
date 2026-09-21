package system

import "fmt"

// Print writes one line to stdout.
func (c Console) Print(format string, arguments ...any) {
	_, _ = fmt.Fprintf(c.Out, format+"\n", arguments...)
}

// Warn writes one line to stderr, which is where everything but a command's own answer goes.
func (c Console) Warn(format string, arguments ...any) {
	_, _ = fmt.Fprintf(c.Err, format+"\n", arguments...)
}
