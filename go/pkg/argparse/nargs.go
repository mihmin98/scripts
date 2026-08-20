package argparse

import "strconv"

// Nargs mirrors Python's nargs= parameter. The zero value means "a single
// value, stored directly" (Python's nargs=None).
type Nargs struct {
	kind  nargsKind
	count int
}

type nargsKind int

const (
	nargsNone  nargsKind = iota // nargs=None: exactly one value, not a list
	nargsCount                  // nargs=N: exactly N values, as a list
	nargsOptional
	nargsZeroOrMore
	nargsOneOrMore
	nargsRemainder
)

var (
	// Optional is Python's nargs="?".
	Optional = Nargs{kind: nargsOptional}
	// ZeroOrMore is Python's nargs="*".
	ZeroOrMore = Nargs{kind: nargsZeroOrMore}
	// OneOrMore is Python's nargs="+".
	OneOrMore = Nargs{kind: nargsOneOrMore}
	// Remainder is Python's nargs=argparse.REMAINDER.
	Remainder = Nargs{kind: nargsRemainder}
)

// NArgs is Python's nargs=N: exactly n values, always collected into a list.
func NArgs(n int) Nargs {
	return Nargs{kind: nargsCount, count: n}
}

// isSet reports whether nargs was explicitly given.
func (n Nargs) isSet() bool {
	return n.kind != nargsNone
}

// min is the smallest number of values this nargs accepts.
func (n Nargs) min() int {
	switch n.kind {
	case nargsCount:
		return n.count
	case nargsOneOrMore:
		return 1
	case nargsOptional, nargsZeroOrMore, nargsRemainder:
		return 0
	default:
		return 1
	}
}

// max is the largest number of values this nargs accepts, or -1 for unbounded.
func (n Nargs) max() int {
	switch n.kind {
	case nargsCount:
		return n.count
	case nargsOptional:
		return 1
	case nargsZeroOrMore, nargsOneOrMore, nargsRemainder:
		return -1
	default:
		return 1
	}
}

// producesList reports whether the parsed values are stored as a slice.
func (n Nargs) producesList() bool {
	return n.kind != nargsNone && n.kind != nargsOptional
}

// String renders nargs the way Python does in error messages.
func (n Nargs) String() string {
	switch n.kind {
	case nargsCount:
		return strconv.Itoa(n.count)
	case nargsOptional:
		return "?"
	case nargsZeroOrMore:
		return "*"
	case nargsOneOrMore:
		return "+"
	case nargsRemainder:
		return "..."
	default:
		return "None"
	}
}
