package argparse

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// ParseArgs parses a command line, which should not include the program name
// (pass os.Args[1:]). A returned error is always an *ArgumentError describing
// bad user input; use MustParseArgs for Python's print-usage-and-exit
// behaviour.
//
// Positional values are collected across the whole command line and then
// distributed over the positional arguments left to right, each taking its
// minimum first and variadic ones absorbing the rest.
func (p *Parser) ParseArgs(args []string) (*Namespace, error) {
	ns := newNamespace()
	p.helpRequested = false
	for _, a := range p.allArguments() {
		a.seen = false
	}

	var positionalValues []string
	optionsDone := false
	// A positional with nargs=REMAINDER swallows everything from the first
	// positional token onwards, options included, as Python's REMAINDER does.
	remainderPositional := false
	for _, a := range p.positionals {
		if a.nargs.kind == nargsRemainder {
			remainderPositional = true
		}
	}

	for i := 0; i < len(args); i++ {
		tok := args[i]

		if !optionsDone && tok == "--" {
			optionsDone = true
			continue
		}
		if optionsDone || !p.looksLikeOption(tok) {
			positionalValues = append(positionalValues, tok)
			if remainderPositional {
				optionsDone = true
			}
			continue
		}

		if err := p.consumeOption(ns, args, &i); err != nil {
			return nil, err
		}
		if p.helpRequested {
			return ns, nil
		}
	}

	if err := p.consumePositionals(ns, positionalValues); err != nil {
		return nil, err
	}
	if err := p.finalize(ns); err != nil {
		return nil, err
	}
	return ns, nil
}

func (p *Parser) allArguments() []*Argument {
	return append(append([]*Argument{}, p.optionals...), p.positionals...)
}

// looksLikeOption mirrors Python's _parse_optional: a bare "-" is a
// positional, and a negative-number-looking token is a positional unless the
// parser has options that could be negative numbers.
func (p *Parser) looksLikeOption(tok string) bool {
	if !strings.HasPrefix(tok, "-") || tok == "-" || tok == "--" {
		return false
	}
	if _, err := strconv.ParseFloat(tok, 64); err == nil && !p.hasNegativeNumberOption() {
		return false
	}
	return true
}

func (p *Parser) hasNegativeNumberOption() bool {
	for f := range p.byFlag {
		if _, err := strconv.ParseFloat(f, 64); err == nil {
			return true
		}
	}
	return false
}

// consumeOption handles the token at args[*i], advancing *i past any values it
// consumes. It handles "--long value", "--long=value", "-x value", "-xvalue"
// and bundled short flags such as "-abc".
func (p *Parser) consumeOption(ns *Namespace, args []string, i *int) error {
	tok := args[*i]

	if strings.HasPrefix(tok, "--") {
		name, explicit, hasExplicit := strings.Cut(tok, "=")
		arg, ok := p.byFlag[name]
		if !ok {
			return argErrorf("unrecognized arguments: %s", tok)
		}
		return p.applyOption(ns, arg, name, explicit, hasExplicit, args, i)
	}

	// Short flags, possibly bundled: every character is a flag, and the first
	// one that takes a value claims the rest of the token.
	for pos := 1; pos < len(tok); pos++ {
		name := "-" + string(tok[pos])
		arg, ok := p.byFlag[name]
		if !ok {
			return argErrorf("unrecognized arguments: %s", tok)
		}
		if arg.action.takesValues() && pos+1 < len(tok) {
			return p.applyOption(ns, arg, name, tok[pos+1:], true, args, i)
		}
		if err := p.applyOption(ns, arg, name, "", false, args, i); err != nil {
			return err
		}
		if p.helpRequested {
			return nil
		}
	}
	return nil
}

// applyOption gathers the values an option needs and stores them.
func (p *Parser) applyOption(ns *Namespace, arg *Argument, name, explicit string, hasExplicit bool, args []string, i *int) error {
	arg.seen = true

	if !arg.action.takesValues() {
		if hasExplicit {
			return argErrorf("argument %s: ignored explicit argument %s", p.argName(arg, name), quote(explicit))
		}
		return p.store(ns, arg, nil)
	}

	var values []string
	switch {
	case hasExplicit:
		values = []string{explicit}
	case arg.nargs.kind == nargsRemainder:
		values = append(values, args[*i+1:]...)
		*i = len(args) - 1
	default:
		max := arg.nargs.max()
		for *i+1 < len(args) && (max < 0 || len(values) < max) {
			next := args[*i+1]
			if p.looksLikeOption(next) || next == "--" {
				break
			}
			values = append(values, next)
			*i++
		}
	}

	if min := arg.nargs.min(); len(values) < min {
		return argErrorf("argument %s: expected %s", p.argName(arg, name), expectedPhrase(arg.nargs))
	}
	if max := arg.nargs.max(); max >= 0 && len(values) > max {
		return argErrorf("argument %s: expected %s", p.argName(arg, name), expectedPhrase(arg.nargs))
	}
	return p.store(ns, arg, values)
}

// argName is how an argument is named in error messages: all of its option
// strings joined by "/", as Python does.
func (p *Parser) argName(arg *Argument, _ string) string {
	if arg.positional {
		return arg.dest
	}
	return strings.Join(arg.flagStrings(), "/")
}

func expectedPhrase(n Nargs) string {
	switch n.kind {
	case nargsCount:
		return fmt.Sprintf("%d arguments", n.count)
	case nargsOneOrMore:
		return "at least one argument"
	default:
		return "one argument"
	}
}

// consumePositionals distributes the collected positional values over the
// positional arguments.
func (p *Parser) consumePositionals(ns *Namespace, values []string) error {
	counts := make([]int, len(p.positionals))
	remaining := len(values)

	var missing []string
	for i, a := range p.positionals {
		counts[i] = a.nargs.min()
		remaining -= counts[i]
		if remaining < 0 {
			missing = append(missing, a.metavarOrDefault())
			remaining = 0
			counts[i] = 0
		}
	}
	if len(missing) > 0 {
		return argErrorf("the following arguments are required: %s", strings.Join(missing, ", "))
	}

	for i, a := range p.positionals {
		if remaining == 0 {
			break
		}
		max := a.nargs.max()
		if max < 0 {
			counts[i] += remaining
			remaining = 0
			continue
		}
		if extra := max - counts[i]; extra > 0 {
			take := min(extra, remaining)
			counts[i] += take
			remaining -= take
		}
	}
	if remaining > 0 {
		return argErrorf("unrecognized arguments: %s", strings.Join(values[len(values)-remaining:], " "))
	}

	offset := 0
	for i, a := range p.positionals {
		if counts[i] == 0 && !a.nargs.producesList() {
			continue // an unmatched "?" positional falls back to its default
		}
		a.seen = true
		if err := p.store(ns, a, values[offset:offset+counts[i]]); err != nil {
			return err
		}
		offset += counts[i]
	}
	return nil
}

// store converts raw values and applies the argument's action.
func (p *Parser) store(ns *Namespace, arg *Argument, raw []string) error {
	converted := make([]any, 0, len(raw))
	for _, s := range raw {
		if len(arg.choices) > 0 && !slices.Contains(arg.choices, s) {
			return argErrorf("argument %s: invalid choice: %s (choose from %s)",
				p.argName(arg, ""), quote(s), quotedList(arg.choices))
		}
		v, ok := arg.typ.apply(s)
		if !ok {
			return argErrorf("argument %s: invalid %s value: %s", p.argName(arg, ""), arg.typ.name, quote(s))
		}
		converted = append(converted, v)
	}

	ns.set[arg.dest] = true
	switch arg.action {
	case StoreTrue, StoreFalse, StoreConst:
		ns.Set(arg.dest, arg.constVal)
	case AppendConst:
		ns.Set(arg.dest, appendAny(ns, arg.dest, arg.constVal))
	case Count:
		prev, _ := ns.Get(arg.dest)
		n, _ := prev.(int)
		ns.Set(arg.dest, n+1)
	case Append:
		if arg.nargs.producesList() {
			ns.Set(arg.dest, appendAny(ns, arg.dest, converted))
		} else {
			ns.Set(arg.dest, appendAny(ns, arg.dest, single(converted, arg)))
		}
	case Help:
		p.helpRequested = true
		if p.ExitOnHelp {
			os.Stdout.WriteString(p.FormatHelp())
			os.Exit(0)
		}
	default: // Store
		if arg.nargs.producesList() {
			ns.Set(arg.dest, converted)
		} else {
			ns.Set(arg.dest, single(converted, arg))
		}
	}
	return nil
}

// single returns the lone value of a non-list argument. An "?" argument with
// no value falls back to Const, as in Python.
func single(converted []any, arg *Argument) any {
	if len(converted) == 0 {
		return arg.constVal
	}
	return converted[0]
}

func appendAny(ns *Namespace, dest string, value any) []any {
	prev, _ := ns.Get(dest)
	list, _ := prev.([]any)
	return append(list, value)
}

// finalize fills in defaults for everything that was not seen and reports
// missing required arguments.
func (p *Parser) finalize(ns *Namespace) error {
	var missing []string
	for _, a := range p.allArguments() {
		if a.seen {
			continue
		}
		if a.required {
			missing = append(missing, p.argName(a, ""))
			continue
		}
		v, err := p.defaultValue(a)
		if err != nil {
			return err
		}
		ns.Set(a.dest, v)
	}
	if len(missing) > 0 {
		return argErrorf("the following arguments are required: %s", strings.Join(missing, ", "))
	}
	return nil
}

// defaultValue resolves an argument's default, running string defaults through
// the type converter the way Python does.
func (p *Parser) defaultValue(arg *Argument) (any, error) {
	if s, ok := arg.defVal.(string); ok && arg.typ.name != String.name {
		v, ok := arg.typ.apply(s)
		if !ok {
			return nil, argErrorf("argument %s: invalid %s value: %s", p.argName(arg, ""), arg.typ.name, quote(s))
		}
		return v, nil
	}
	return arg.defVal, nil
}

func quotedList(list []string) string {
	parts := make([]string, len(list))
	for i, s := range list {
		parts[i] = quote(s)
	}
	return strings.Join(parts, ", ")
}
