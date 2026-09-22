//go:build !linux && !darwin

package system

import "os"

// IsTerminal says nothing is a terminal, since box supports Linux and macOS and nothing else.
func IsTerminal(*os.File) bool {
	return false
}
