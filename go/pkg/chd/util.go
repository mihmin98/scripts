package chd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/mihmin98/scripts/pkg/progress"
	"golang.org/x/term"
)

// Stem is the file name without its final extension, the equivalent of
// pathlib.Path.stem.
func Stem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// SplitList parses a comma separated option value, dropping empty entries and
// trimming whitespace, as the Python scripts do for --codecs.
func SplitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Truncate cuts a string to at most n characters, counting runes rather than
// bytes so that a multi-byte file name is not sliced in half.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Capitalize upper-cases the first character and lower-cases the rest, like
// Python's str.capitalize.
func Capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(strings.ToLower(s))
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// LastLines returns at most the final n elements of a slice.
func LastLines(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// IsTerminal reports whether f is attached to a terminal.
func IsTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// WatchInterrupts closes stop on the first SIGINT and returns a predicate
// telling whether one was seen. As in the Python scripts, an interrupt only
// asks the run to wind down: in-flight chdman processes are terminated by
// their own workers, which then clean up after themselves.
func WatchInterrupts(stop chan struct{}) func() bool {
	var seen atomic.Bool
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		<-ch
		seen.Store(true)
		close(stop)
	}()
	return seen.Load
}

// BarPainter turns chdman status updates into redraws of the bar on the given
// display line. It returns nil when there is no bar, which tells RunChdman not
// to bother parsing progress at all.
func BarPainter(display *progress.Display, bar *progress.Bar, line int, label string) func(Progress) {
	if bar == nil {
		return nil
	}
	return func(p Progress) {
		bar.N = p.Pct
		suffix := ""
		if p.Ratio != "" {
			suffix = " ratio=" + p.Ratio + "%"
		}
		bar.Desc = Truncate(fmt.Sprintf("  %s %s%s", Capitalize(p.Verb), label, suffix), 70)
		display.Set(line, bar.Percent(display.Width()))
	}
}
