// Command box runs Claude Code inside a disposable Docker sandbox (sbx).
package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"time"

	"github.com/lk16/box/internal/cli"
	"github.com/lk16/box/internal/system"
	"github.com/lk16/box/internal/update"
)

// Interrupted is what a Ctrl-C exits with, which is what a shell reports for the same thing.
const Interrupted = 130

func main() {
	os.Exit(run())
}

// run wires box to the real system and hands the command line to it.
func run() int {
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "box: %s\n", err)
		return 1
	}
	console := system.Console{Out: os.Stdout, Err: os.Stderr, Terminal: system.IsTerminal(os.Stderr)}
	go watchInterrupts(console)
	deps := system.Deps{
		Run:      system.Commands{},
		Console:  console,
		Ask:      system.Terminal{},
		Download: system.Web{Timeout: update.Timeout},
		Now:      time.Now,
	}
	return cli.CLI{Deps: deps, Version: version()}.Main(os.Args[1:], workingDirectory)
}

// version is the release go install stamped this binary with, or "(devel)" for a checkout.
func version() string {
	info, read := debug.ReadBuildInfo()
	if !read {
		return ""
	}
	return info.Main.Version
}

// watchInterrupts turns a Ctrl-C into an exit code rather than a half-finished run.
func watchInterrupts(console system.Console) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	for range signals {
		// A child on the terminal was sent the same Ctrl-C and answers it itself, so box waits
		// for it to finish and cleans up after it. See docs/signals.md.
		if system.ChildAttached() {
			continue
		}
		console.Warn("box: interrupted.")
		os.Exit(Interrupted)
	}
}
