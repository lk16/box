//go:build darwin

package system

// readTerminalSettings is TIOCGETA, the ioctl that reads a terminal's settings on macOS.
const readTerminalSettings = 0x40487413
