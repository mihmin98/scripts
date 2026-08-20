package argparse

import "fmt"

// Namespace holds the parsed values, keyed by dest. It is the equivalent of
// the object returned by Python's parse_args.
type Namespace struct {
	values map[string]any
	set    map[string]bool
}

func newNamespace() *Namespace {
	return &Namespace{values: map[string]any{}, set: map[string]bool{}}
}

// Get returns the raw value stored for dest and whether it was present.
func (n *Namespace) Get(dest string) (any, bool) {
	v, ok := n.values[dest]
	return v, ok
}

// Set stores a value for dest, overwriting any previous one.
func (n *Namespace) Set(dest string, value any) {
	n.values[dest] = value
}

// mustGet panics for an unknown dest: asking for one is a bug in the program,
// not bad user input (Python raises AttributeError here).
func (n *Namespace) mustGet(dest string) any {
	v, ok := n.values[dest]
	if !ok {
		panic(fmt.Sprintf("argparse: no argument with dest %q", dest))
	}
	return v
}

func typed[T any](n *Namespace, dest string) T {
	v := n.mustGet(dest)
	if v == nil {
		var zero T
		return zero
	}
	t, ok := v.(T)
	if !ok {
		var zero T
		panic(fmt.Sprintf("argparse: dest %q holds %T, not %T", dest, v, zero))
	}
	return t
}

// String returns a string-valued argument. A nil default yields "".
func (n *Namespace) String(dest string) string { return typed[string](n, dest) }

// Int returns an int-valued argument. A nil default yields 0.
func (n *Namespace) Int(dest string) int { return typed[int](n, dest) }

// Float returns a float-valued argument. A nil default yields 0.
func (n *Namespace) Float(dest string) float64 { return typed[float64](n, dest) }

// Bool returns a bool-valued argument, e.g. one using StoreTrue.
func (n *Namespace) Bool(dest string) bool { return typed[bool](n, dest) }

// Count returns the occurrence count of a Count argument.
func (n *Namespace) Count(dest string) int { return typed[int](n, dest) }

// Strings returns a list-valued argument (nargs or Append) as strings.
func (n *Namespace) Strings(dest string) []string { return list[string](n, dest) }

// Ints returns a list-valued argument as ints.
func (n *Namespace) Ints(dest string) []int { return list[int](n, dest) }

// Floats returns a list-valued argument as float64s.
func (n *Namespace) Floats(dest string) []float64 { return list[float64](n, dest) }

// IsSet reports whether the argument was present on the command line, as
// opposed to having fallen back to its default.
func (n *Namespace) IsSet(dest string) bool {
	return n.set[dest]
}

func list[T any](n *Namespace, dest string) []T {
	v := n.mustGet(dest)
	if v == nil {
		return nil
	}
	if l, ok := v.([]T); ok {
		return l
	}
	raw, ok := v.([]any)
	if !ok {
		var zero T
		panic(fmt.Sprintf("argparse: dest %q holds %T, not []%T", dest, v, zero))
	}
	out := make([]T, 0, len(raw))
	for _, e := range raw {
		t, ok := e.(T)
		if !ok {
			var zero T
			panic(fmt.Sprintf("argparse: dest %q contains %T, not %T", dest, e, zero))
		}
		out = append(out, t)
	}
	return out
}
