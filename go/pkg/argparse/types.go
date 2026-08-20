package argparse

import (
	"fmt"
	"strconv"
)

// TypeFunc converts a command line string into a value, mirroring Python's
// type= parameter. Returning an error produces a Python-style
// "invalid <name> value: '<arg>'" message.
type TypeFunc struct {
	name    string
	convert func(string) (any, error)
}

// NewType builds a custom converter. name is used in error messages, the way
// Python uses the callable's __name__.
func NewType(name string, convert func(string) (any, error)) TypeFunc {
	return TypeFunc{name: name, convert: convert}
}

var (
	// String is the default converter: the argument is used as-is.
	String = NewType("str", func(s string) (any, error) { return s, nil })

	// Int parses a base-10 integer.
	Int = NewType("int", func(s string) (any, error) {
		v, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		return v, nil
	})

	// Float parses a floating point number.
	Float = NewType("float", func(s string) (any, error) {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		return v, nil
	})

	// Bool parses Go's boolean literals ("true", "1", "f", ...).
	Bool = NewType("bool", func(s string) (any, error) {
		v, err := strconv.ParseBool(s)
		if err != nil {
			return nil, err
		}
		return v, nil
	})
)

func (t TypeFunc) isSet() bool {
	return t.convert != nil
}

// apply converts s. On failure the caller turns the result into a
// Python-shaped "argument X: invalid <type> value: 'v'" error.
func (t TypeFunc) apply(s string) (any, bool) {
	v, err := t.convert(s)
	if err != nil {
		return nil, false
	}
	return v, true
}

func quote(s string) string {
	return fmt.Sprintf("'%s'", s)
}
