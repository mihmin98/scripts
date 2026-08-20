package argparse

import "fmt"

// ArgumentError is returned by ParseArgs when the command line supplied by the
// user is invalid. It corresponds to Python's argparse error path, which exits
// with status 2.
type ArgumentError struct {
	Message string
}

func (e *ArgumentError) Error() string {
	return e.Message
}

func argErrorf(format string, a ...any) *ArgumentError {
	return &ArgumentError{Message: fmt.Sprintf(format, a...)}
}

// DefinitionError describes an invalid argument definition. Unlike
// ArgumentError it signals a bug in the program, not bad user input, so
// MustAddArgument panics with it.
type DefinitionError struct {
	Message string
}

func (e *DefinitionError) Error() string {
	return e.Message
}

func defErrorf(format string, a ...any) *DefinitionError {
	return &DefinitionError{Message: fmt.Sprintf(format, a...)}
}
