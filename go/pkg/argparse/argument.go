package argparse

import (
	"strings"
)

// Options mirrors the keyword arguments of Python's add_argument. The zero
// value means action="store", nargs=None, type=str.
type Options struct {
	Action   Action
	Nargs    Nargs
	Const    any
	Default  any
	Type     TypeFunc
	Choices  []string
	Required bool
	Help     string
	Metavar  string
	Dest     string
}

// Argument is a single registered command line argument.
type Argument struct {
	shortFlags []string // e.g. "-v"
	longFlags  []string // e.g. "--verbose"
	positional bool
	name       string // positional name, for help output

	action   Action
	nargs    Nargs
	constVal any
	defVal   any
	typ      TypeFunc
	choices  []string
	required bool
	help     string
	metavar  string
	dest     string

	seen bool
}

// flagStrings returns every flag of an optional in declaration order.
func (a *Argument) flagStrings() []string {
	return append(append([]string{}, a.shortFlags...), a.longFlags...)
}

// displayName is how the argument is referred to in error messages, matching
// Python: the first flag string for optionals, the dest for positionals.
func (a *Argument) displayName() string {
	if a.positional {
		return a.dest
	}
	return a.flagStrings()[0]
}

// metavarOrDefault mirrors Python's _get_formatter_metavar: upper-cased dest
// for optionals, the dest itself for positionals.
func (a *Argument) metavarOrDefault() string {
	if a.metavar != "" {
		return a.metavar
	}
	if len(a.choices) > 0 {
		// Python's default metavar for choices is "{a,b,c}".
		return "{" + strings.Join(a.choices, ",") + "}"
	}
	if a.positional {
		return a.dest
	}
	return strings.ToUpper(a.dest)
}

// newArgument validates a definition and builds the Argument.
func newArgument(flags []string, opts *Options) (*Argument, error) {
	if opts == nil {
		opts = &Options{}
	}
	if len(flags) == 0 {
		return nil, defErrorf("argparse: at least one flag or name is required")
	}

	arg := &Argument{
		action:   opts.Action,
		nargs:    opts.Nargs,
		constVal: opts.Const,
		defVal:   opts.Default,
		typ:      opts.Type,
		choices:  opts.Choices,
		required: opts.Required,
		help:     opts.Help,
		metavar:  opts.Metavar,
		dest:     opts.Dest,
	}
	if !arg.typ.isSet() {
		arg.typ = String
	}

	if err := arg.classifyFlags(flags); err != nil {
		return nil, err
	}
	if err := arg.applyActionRules(); err != nil {
		return nil, err
	}
	return arg, nil
}

// classifyFlags splits flags into short/long forms (or recognises a single
// positional name) and derives dest.
func (a *Argument) classifyFlags(flags []string) error {
	optional := strings.HasPrefix(flags[0], "-")
	for _, f := range flags {
		if f == "" {
			return defErrorf("argparse: empty flag string")
		}
		if strings.HasPrefix(f, "-") != optional {
			return defErrorf("argparse: cannot mix positional name and flags in %v", flags)
		}
	}

	if !optional {
		if len(flags) != 1 {
			return defErrorf("argparse: a positional takes exactly one name, got %v", flags)
		}
		a.positional = true
		a.name = flags[0]
		if a.dest == "" {
			a.dest = destFromName(flags[0])
		}
		return nil
	}

	for _, f := range flags {
		switch {
		case strings.HasPrefix(f, "--"):
			if len(f) <= 2 {
				return defErrorf("argparse: invalid flag %q", f)
			}
			a.longFlags = append(a.longFlags, f)
		default:
			if len(f) != 2 {
				return defErrorf("argparse: short flag %q must be a single character", f)
			}
			a.shortFlags = append(a.shortFlags, f)
		}
	}

	if a.dest == "" {
		// Python prefers the first long option, else the first short one.
		if len(a.longFlags) > 0 {
			a.dest = destFromName(strings.TrimPrefix(a.longFlags[0], "--"))
		} else {
			a.dest = destFromName(strings.TrimPrefix(a.shortFlags[0], "-"))
		}
	}
	return nil
}

// applyActionRules rejects nonsensical option combinations and fills in the
// implicit defaults each action carries.
func (a *Argument) applyActionRules() error {
	switch a.action {
	case StoreTrue, StoreFalse, StoreConst, AppendConst, Count, Help:
		if a.positional {
			return defErrorf("argparse: action %s is not valid for positional %q", a.action, a.dest)
		}
		if a.nargs.isSet() {
			return defErrorf("argparse: nargs is not allowed with action %s (%s)", a.action, a.displayName())
		}
		if len(a.choices) > 0 {
			return defErrorf("argparse: choices are not allowed with action %s (%s)", a.action, a.displayName())
		}
	}

	switch a.action {
	case StoreTrue:
		a.constVal = true
		if a.defVal == nil {
			a.defVal = false
		}
	case StoreFalse:
		a.constVal = false
		if a.defVal == nil {
			a.defVal = true
		}
	case StoreConst, AppendConst:
		if a.constVal == nil {
			return defErrorf("argparse: action %s requires Const (%s)", a.action, a.displayName())
		}
	case Count:
		if a.defVal == nil {
			a.defVal = 0
		}
	}

	if a.positional && a.required {
		return defErrorf("argparse: positional %q cannot be Required", a.dest)
	}
	if a.nargs.kind == nargsRemainder && !a.positional && a.action != Store {
		return defErrorf("argparse: nargs=... requires action store (%s)", a.displayName())
	}
	if a.nargs.kind == nargsCount && a.nargs.count < 1 {
		return defErrorf("argparse: nargs must be >= 1 (%s)", a.displayName())
	}
	return nil
}

// destFromName turns a flag or positional name into a dest, replacing dashes
// with underscores the way Python does.
func destFromName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}
