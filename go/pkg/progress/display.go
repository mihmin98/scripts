package progress

import (
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Display owns a block of consecutive terminal lines, one per progress bar,
// and keeps them redrawn as their contents change. Messages written through
// Write scroll away above the block, reproducing tqdm.write's behaviour of
// never letting ordinary output collide with a live bar.
//
// As in tqdm, bars go to stderr and messages go to stdout.
type Display struct {
	mu       sync.Mutex
	bars     io.Writer
	msgs     io.Writer
	tty      bool
	lines    []string
	drawn    int
	lastLen  int
	lastDraw time.Time
	fd       int
}

// NewDisplay creates a display for n bar lines. bars is where the bars are
// drawn (stderr) and msgs is where Write sends text (stdout). Off a terminal
// there is no cursor to move, so a lone bar is rewritten with carriage returns
// the way tqdm does when its output is redirected.
func NewDisplay(bars, msgs io.Writer, n int) *Display {
	d := &Display{bars: bars, msgs: msgs, lines: make([]string, n), fd: -1}
	if f, ok := bars.(*os.File); ok {
		d.fd = int(f.Fd())
		d.tty = term.IsTerminal(d.fd)
	}
	return d
}

// Width is the terminal width, or 0 when the output is not a terminal. It is
// the value to pass to Bar.Meter and Bar.Percent.
func (d *Display) Width() int {
	if !d.tty {
		return 0
	}
	w, _, err := term.GetSize(d.fd)
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

// MinInterval is the shortest gap between two repaints, tqdm's mininterval.
// Updates that arrive sooner are remembered but not drawn, which keeps a fast
// series of updates from flooding the terminal; the pending text is picked up
// by the next repaint, so nothing is lost.
const MinInterval = 100 * time.Millisecond

// Set replaces the text of line pos (0 is the top line of the block) and
// repaints unless the previous repaint was too recent. An empty text blanks
// the line, which is how a "leave=False" bar disappears when it is closed.
func (d *Display) Set(pos int, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if pos < 0 || pos >= len(d.lines) {
		return
	}
	d.lines[pos] = text
	if time.Since(d.lastDraw) < MinInterval {
		return
	}
	d.redraw()
}

// Write prints a message above the bar block, then puts the bars back.
func (d *Display) Write(text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.clear()
	io.WriteString(d.msgs, text+"\n")
	d.redraw()
}

// Close leaves the cursor on a fresh line below the block so that whatever is
// printed next does not land on top of the final bar.
func (d *Display) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.drawn > 0 {
		// tqdm's close() paints the bar one last time before moving off it, so
		// the final state survives in a captured log.
		d.redraw()
		io.WriteString(d.bars, "\n")
		d.drawn = 0
	}
}

// clear moves back to the top of the block and erases everything from there to
// the bottom of the screen.
func (d *Display) clear() {
	if d.drawn == 0 {
		return
	}
	if !d.tty {
		if len(d.lines) == 1 {
			io.WriteString(d.bars, "\r"+strings.Repeat(" ", d.lastLen)+"\r")
		}
		d.drawn = 0
		return
	}
	var b strings.Builder
	b.WriteString("\r")
	if d.drawn > 1 {
		b.WriteString(cursorUp(d.drawn - 1))
	}
	b.WriteString("\x1b[0J")
	io.WriteString(d.bars, b.String())
	d.drawn = 0
}

// redraw paints the whole block. Painting all of it every time costs a few
// hundred bytes per update and removes any need to track where the cursor is
// between calls, which is what makes concurrent updates from several workers
// safe.
func (d *Display) redraw() {
	if !d.tty {
		// Redirected output gets the same carriage-return rewriting tqdm uses,
		// so a captured log is byte-for-byte what the Python script produced.
		// Only a single bar can be rewritten this way; the stacked per-file
		// bars are never enabled off a terminal.
		if len(d.lines) == 1 {
			io.WriteString(d.bars, "\r"+d.lines[0])
			d.lastLen = runeLen(d.lines[0])
			d.drawn = 1
			d.lastDraw = time.Now()
			return
		}
		for _, line := range d.lines {
			if line != "" {
				io.WriteString(d.bars, line+"\n")
			}
		}
		return
	}

	var b strings.Builder
	b.WriteString("\r")
	if d.drawn > 1 {
		b.WriteString(cursorUp(d.drawn - 1))
	}
	for i, line := range d.lines {
		b.WriteString("\x1b[2K")
		b.WriteString(line)
		if i < len(d.lines)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\x1b[0J")
	io.WriteString(d.bars, b.String())
	d.drawn = len(d.lines)
	d.lastDraw = time.Now()
}

func cursorUp(n int) string {
	if n <= 0 {
		return ""
	}
	return "\x1b[" + strconv.Itoa(n) + "A"
}
