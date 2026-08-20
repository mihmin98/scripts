// Package argparse is a command line argument parser modelled on Python's
// argparse module: the same vocabulary (actions, nargs, choices, dest,
// metavar), the same parsing behaviour and the same help output, expressed
// with Go types and explicit errors instead of keyword arguments and
// exceptions.
//
// A typical use:
//
//	parser := argparse.NewParser("", "Convert flac files to opus")
//	parser.MustAddArgument([]string{"-d", "--music-dir"}, &argparse.Options{
//		Required: true, Help: "Directory containing the .flac files"})
//	parser.MustAddArgument([]string{"-b", "--bitrate"}, &argparse.Options{
//		Type: argparse.Int, Default: 160, Help: "Output bitrate in kbps"})
//	parser.MustAddArgument([]string{"-v", "--verbose"}, &argparse.Options{
//		Action: argparse.StoreTrue, Help: "Enable verbose output"})
//
//	ns := parser.MustParseArgs(os.Args[1:])
//	dir, bitrate, verbose := ns.String("music_dir"), ns.Int("bitrate"), ns.Bool("verbose")
//
// Sub-parsers, argument groups and mutually exclusive groups are not
// implemented.
package argparse

import (
	"os"
	"path/filepath"
)

// Parser holds the argument definitions for a program.
type Parser struct {
	Prog        string
	Description string
	Epilog      string

	// ExitOnHelp controls whether the automatic -h/--help exits the process
	// (the Python behaviour, and the default). Tests set it to false.
	ExitOnHelp bool

	optionals   []*Argument
	positionals []*Argument
	byFlag      map[string]*Argument
	byDest      map[string]*Argument

	helpRequested bool
}

// NewParser creates a parser. An empty prog defaults to the executable name,
// as Python defaults to sys.argv[0].
func NewParser(prog, description string) *Parser {
	if prog == "" {
		prog = filepath.Base(os.Args[0])
	}
	p := &Parser{
		Prog:        prog,
		Description: description,
		ExitOnHelp:  true,
		byFlag:      map[string]*Argument{},
		byDest:      map[string]*Argument{},
	}
	p.MustAddArgument([]string{"-h", "--help"}, &Options{
		Action: Help,
		Help:   "show this help message and exit",
	})
	return p
}

// AddArgument registers an argument. flags is either a list of option strings
// ("-v", "--verbose") or a single positional name ("files").
func (p *Parser) AddArgument(flags []string, opts *Options) (*Argument, error) {
	arg, err := newArgument(flags, opts)
	if err != nil {
		return nil, err
	}
	if _, dup := p.byDest[arg.dest]; dup {
		return nil, defErrorf("argparse: duplicate dest %q", arg.dest)
	}
	for _, f := range arg.flagStrings() {
		if _, dup := p.byFlag[f]; dup {
			return nil, defErrorf("argparse: duplicate option string %q", f)
		}
	}

	p.byDest[arg.dest] = arg
	if arg.positional {
		p.positionals = append(p.positionals, arg)
	} else {
		for _, f := range arg.flagStrings() {
			p.byFlag[f] = arg
		}
		p.optionals = append(p.optionals, arg)
	}
	return arg, nil
}

// MustAddArgument is AddArgument for definitions that cannot fail at runtime;
// it panics on an invalid definition, which is always a programming error.
func (p *Parser) MustAddArgument(flags []string, opts *Options) *Argument {
	arg, err := p.AddArgument(flags, opts)
	if err != nil {
		panic(err)
	}
	return arg
}

// MustParseArgs parses args and, on a bad command line, prints the usage and
// error to stderr and exits with status 2, exactly like Python's parse_args.
func (p *Parser) MustParseArgs(args []string) *Namespace {
	ns, err := p.ParseArgs(args)
	if err != nil {
		p.Fail(err.Error())
	}
	return ns
}

// Fail prints "usage: ..." plus "<prog>: error: <message>" to stderr and exits
// with status 2, mirroring Python's ArgumentParser.error.
func (p *Parser) Fail(message string) {
	os.Stderr.WriteString(p.FormatUsage())
	os.Stderr.WriteString(p.Prog + ": error: " + message + "\n")
	os.Exit(2)
}

// HelpRequested reports whether -h/--help was seen during the last parse. It
// is only useful when ExitOnHelp is false.
func (p *Parser) HelpRequested() bool {
	return p.helpRequested
}
