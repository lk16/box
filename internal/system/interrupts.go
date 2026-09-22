package system

import "os"

// Interrupted is what a Ctrl-C exits with, which is what a shell reports for the same thing.
const Interrupted = 130

// Interrupts turns a Ctrl-C into an exit code rather than a half-finished run.
type Interrupts struct {
	Console Console
	// Attached says whether a child box started is holding the terminal at this moment.
	Attached func() bool
	Exit     func(code int)
}

// Watch answers every Ctrl-C that arrives until the channel closes.
func (i Interrupts) Watch(signals <-chan os.Signal) {
	for range signals {
		// A child on the terminal answers its own Ctrl-C, so box waits. See docs/signals.md.
		if i.Attached() {
			continue
		}
		i.Console.Warn("box: interrupted.")
		i.Exit(Interrupted)
	}
}
