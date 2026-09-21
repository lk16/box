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
		Ask:      &system.Terminal{},
		Download: system.Web{Timeout: update.Timeout},
		Now:      time.Now,
	}
	return cli.CLI{Deps: deps, Version: release()}.Main(os.Args[1:], workingDirectory)
}

// release is what this box was installed as, which is nothing at all when it came from a checkout.
func release() string {
	info, _ := debug.ReadBuildInfo()
	return update.Release(info)
}

// watchInterrupts turns a Ctrl-C into an exit code rather than a half-finished run.
func watchInterrupts(console system.Console) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	system.Interrupts{Console: console, Attached: system.ChildAttached, Exit: os.Exit}.Watch(signals)
}
