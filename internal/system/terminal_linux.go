//go:build linux

package system

// readTerminalSettings is TCGETS, the ioctl that reads a terminal's settings on Linux.
const readTerminalSettings = 0x5401
