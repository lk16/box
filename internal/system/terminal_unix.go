//go:build linux || darwin

package system

import (
	"os"
	"syscall"
	"unsafe"
)

// IsTerminal says whether a file is a terminal, which is what decides whether a notice is coloured.
func IsTerminal(file *os.File) bool {
	// Reading the terminal settings is the only question a terminal answers and a pipe does not;
	// a stat says /dev/null is a character device too. See docs/terminals.md.
	var settings [128]byte
	_, _, failure := syscall.Syscall(
		syscall.SYS_IOCTL, file.Fd(), readTerminalSettings, uintptr(unsafe.Pointer(&settings[0])),
	)
	return failure == 0
}
