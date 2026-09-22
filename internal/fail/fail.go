// Package fail carries the one kind of error box reports: a setup it will not run with.
package fail

import "fmt"

// Error is a refusal the user sees as one line rather than as a stack trace.
type Error struct {
	Message string
}

// Error returns the message printed after "box: ".
func (e *Error) Error() string {
	return e.Message
}

// Errorf builds a refusal from a format string.
func Errorf(format string, arguments ...any) error {
	return &Error{Message: fmt.Sprintf(format, arguments...)}
}
