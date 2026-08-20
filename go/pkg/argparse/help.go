package argparse

import (
	"os"
	"strconv"
	"strings"
)

const (
	defaultWidth = 80
	// Column at which help text starts, as in Python's HelpFormatter.
	helpPosition = 24
	indent       = "  "
)

// FormatUsage renders the "usage: ..." block, terminated by a newline.
func (p *Parser) FormatUsage() string {
	parts := make([]string, 0, len(p.optionals)+len(p.positionals))
	for _, a := range p.optionals {
		parts = append(parts, bracketed(a))
	}
	for _, a := range p.positionals {
		parts = append(parts, positionalMetavars(a))
	}

	prefix := "usage: " + p.Prog
	pad := strings.Repeat(" ", len(prefix)+1) // continuation lines align under the first part
	line := prefix
	out := strings.Builder{}
	for _, part := range parts {
		if len(line)+1+len(part) > p.width() && line != prefix && line != pad {
			out.WriteString(line + "\n")
			line = pad
		}
		if line == pad {
			line += part
		} else {
			line += " " + part
		}
	}
	out.WriteString(line + "\n")
	return out.String()
}

// FormatHelp renders the full help text: usage, description, the argument
// sections and the epilog.
func (p *Parser) FormatHelp() string {
	out := strings.Builder{}
	out.WriteString(p.FormatUsage())

	if p.Description != "" {
		out.WriteString("\n" + p.wrap(p.Description, "") + "\n")
	}
	if len(p.positionals) > 0 {
		out.WriteString("\npositional arguments:\n")
		for _, a := range p.positionals {
			out.WriteString(p.formatArgument(a))
		}
	}
	if len(p.optionals) > 0 {
		out.WriteString("\noptions:\n")
		for _, a := range p.optionals {
			out.WriteString(p.formatArgument(a))
		}
	}
	if p.Epilog != "" {
		out.WriteString("\n" + p.wrap(p.Epilog, "") + "\n")
	}
	return out.String()
}

// PrintHelp writes the help text to stdout.
func (p *Parser) PrintHelp() {
	os.Stdout.WriteString(p.FormatHelp())
}

// formatArgument renders one help line: the invocation, then the help text in
// the right-hand column (on its own lines if the invocation is too wide).
func (p *Parser) formatArgument(a *Argument) string {
	invocation := indent + invocationString(a)
	if a.help == "" {
		return invocation + "\n"
	}

	helpIndent := strings.Repeat(" ", helpPosition)
	if len(invocation) > helpPosition-2 {
		return invocation + "\n" + p.wrap(a.help, helpIndent) + "\n"
	}
	padded := invocation + strings.Repeat(" ", helpPosition-len(invocation))
	wrapped := p.wrap(a.help, helpIndent)
	return padded + strings.TrimPrefix(wrapped, helpIndent) + "\n"
}

// invocationString is "-n NAME, --name NAME" for optionals and the metavar for
// positionals.
func invocationString(a *Argument) string {
	if a.positional {
		// The section lists the bare metavar; brackets belong in the usage line.
		return a.metavarOrDefault()
	}
	flags := a.flagStrings()
	if !a.action.takesValues() {
		return strings.Join(flags, ", ")
	}
	parts := make([]string, len(flags))
	for i, f := range flags {
		parts[i] = f + " " + valueMetavars(a)
	}
	return strings.Join(parts, ", ")
}

// valueMetavars renders the value placeholders an option takes, honouring
// nargs: "NAME", "NAME [NAME ...]", "NAME NAME", ...
func valueMetavars(a *Argument) string {
	m := a.metavarOrDefault()
	switch a.nargs.kind {
	case nargsCount:
		return strings.TrimSpace(strings.Repeat(m+" ", a.nargs.count))
	case nargsOptional:
		return "[" + m + "]"
	case nargsZeroOrMore:
		return "[" + m + " ...]"
	case nargsOneOrMore:
		return m + " [" + m + " ...]"
	case nargsRemainder:
		return "..."
	default:
		return m
	}
}

// positionalMetavars is like valueMetavars but without the brackets that mark
// an optional value in the usage line.
func positionalMetavars(a *Argument) string {
	return valueMetavars(a)
}

// bracketed renders an optional in the usage line, in square brackets unless
// it is required.
func bracketed(a *Argument) string {
	s := a.flagStrings()[0]
	if a.action.takesValues() {
		s += " " + valueMetavars(a)
	}
	if a.required {
		return s
	}
	return "[" + s + "]"
}

// width is the wrapping width: $COLUMNS if set and sane, else 80.
func (p *Parser) width() int {
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 20 {
			return n
		}
	}
	return defaultWidth
}

// wrap word-wraps text to the parser width, prefixing every line with prefix.
func (p *Parser) wrap(text, prefix string) string {
	limit := max(p.width()-len(prefix), 20)

	var lines []string
	line := ""
	for word := range strings.FieldsSeq(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= limit:
			line += " " + word
		default:
			lines = append(lines, prefix+line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, prefix+line)
	}
	return strings.Join(lines, "\n")
}
