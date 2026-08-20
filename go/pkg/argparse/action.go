package argparse

// Action mirrors Python's action= parameter. The zero value is Store.
type Action int

const (
	// Store keeps the value(s) that follow the flag. Python's "store".
	Store Action = iota
	// StoreTrue stores true when the flag is present, false otherwise.
	StoreTrue
	// StoreFalse stores false when the flag is present, true otherwise.
	StoreFalse
	// StoreConst stores Options.Const when the flag is present.
	StoreConst
	// Append appends each occurrence's value(s) to a slice.
	Append
	// AppendConst appends Options.Const on each occurrence.
	AppendConst
	// Count counts how many times the flag appears.
	Count
	// Help prints the help text and exits. Used by the automatic -h/--help.
	Help
)

func (a Action) String() string {
	switch a {
	case StoreTrue:
		return "store_true"
	case StoreFalse:
		return "store_false"
	case StoreConst:
		return "store_const"
	case Append:
		return "append"
	case AppendConst:
		return "append_const"
	case Count:
		return "count"
	case Help:
		return "help"
	default:
		return "store"
	}
}

// takesValues reports whether the action consumes command line values.
func (a Action) takesValues() bool {
	switch a {
	case Store, Append:
		return true
	default:
		return false
	}
}
