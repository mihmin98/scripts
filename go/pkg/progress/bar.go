// Package progress renders tqdm-compatible progress bars, including tqdm's
// stacked multi-bar layout: one bar per screen line, with ordinary messages
// scrolling above the block the way tqdm.write does.
//
// The rendering deliberately matches Python's tqdm character for character --
// the same eighth-block glyphs, the same "  0%|          | 0/2 [00:00<?, ?file/s]"
// meter, the same rate and interval formatting -- so a Go port of a tqdm
// script looks identical on screen.
package progress

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// blocks is tqdm's Bar.UTF charset: a space for "empty" followed by the eighth
// blocks U+258F..U+2588, so a bar cell can show any eighth of a character.
var blocks = []string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}

// defaultBarWidth is what tqdm falls back to when it cannot determine the
// terminal width (ncols=None).
const defaultBarWidth = 10

// Bar is a single progress bar. It only formats itself; putting the text on
// screen is the Display's job.
type Bar struct {
	Total   float64
	N       float64
	Desc    string
	Unit    string
	started time.Time
}

// NewBar creates a bar with the given total and unit, starting its clock now.
func NewBar(total float64, unit string) *Bar {
	return &Bar{Total: total, Unit: unit, started: time.Now()}
}

// Reset returns the bar to zero and restarts its clock, like tqdm's reset().
func (b *Bar) Reset(total float64) {
	b.Total = total
	b.N = 0
	b.started = time.Now()
}

// Elapsed is the time since the bar was created or last reset.
func (b *Bar) Elapsed() time.Duration {
	return time.Since(b.started)
}

// Meter renders tqdm's default bar_format:
//
//	{desc}: {percentage:3.0f}%|{bar}| {n}/{total} [{elapsed}<{remaining}, {rate_fmt}]
//
// width is the terminal width, or 0 when it is unknown.
func (b *Bar) Meter(width int) string {
	frac := 0.0
	if b.Total > 0 {
		frac = b.N / b.Total
	}

	left := ""
	if b.Desc != "" {
		left = b.Desc + ": "
	}
	left += fmt.Sprintf("%3.0f%%|", frac*100)

	elapsed := b.Elapsed().Seconds()
	var rate float64
	if elapsed > 0 {
		rate = b.N / elapsed
	}
	remaining := "?"
	if rate > 0 && b.Total > 0 {
		remaining = FormatInterval((b.Total - b.N) / rate)
	}
	right := fmt.Sprintf("| %s/%s [%s<%s, %s]",
		formatCount(b.N), formatCount(b.Total),
		FormatInterval(elapsed), remaining, formatRate(rate, b.Unit))

	return left + renderBar(frac, barWidth(width, len(left)+len(right))) + right
}

// Percent renders the per-file bar_format used by the chd scripts:
//
//	{desc} |{bar}| {n:.1f}%
func (b *Bar) Percent(width int) string {
	frac := 0.0
	if b.Total > 0 {
		frac = b.N / b.Total
	}
	left := b.Desc + " |"
	right := fmt.Sprintf("| %.1f%%", b.N)
	// The description may hold non-ASCII characters, and tqdm sizes the bar in
	// display cells, not bytes.
	return left + renderBar(frac, barWidth(width, runeLen(left)+runeLen(right))) + right
}

// barWidth is how many cells are left for the bar itself once the surrounding
// text is accounted for.
func barWidth(width, used int) int {
	if width <= 0 {
		return defaultBarWidth
	}
	if n := width - used; n > 0 {
		return n
	}
	return 0
}

// renderBar draws frac of a bar n cells wide, using a partial block for the
// leftover eighths exactly as tqdm's Bar.__format__ does.
func renderBar(frac float64, n int) string {
	if n <= 0 {
		return ""
	}
	frac = math.Max(0, math.Min(1, frac))

	nsyms := len(blocks) - 1
	total := int(frac * float64(n) * float64(nsyms))
	full, part := total/nsyms, total%nsyms

	var b strings.Builder
	b.WriteString(strings.Repeat(blocks[nsyms], full))
	if full < n {
		b.WriteString(blocks[part])
		b.WriteString(strings.Repeat(blocks[0], n-full-1))
	}
	return b.String()
}

// FormatInterval renders seconds as tqdm's format_interval: MM:SS, or
// H:MM:SS once there is at least one hour.
func FormatInterval(seconds float64) string {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		seconds = 0
	}
	t := int(seconds)
	h, rem := t/3600, t%3600
	m, s := rem/60, rem%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// formatRate renders tqdm's rate_fmt, which flips to seconds-per-item once the
// rate drops below one item per second.
func formatRate(rate float64, unit string) string {
	if rate <= 0 {
		return "?" + unit + "/s"
	}
	if rate < 1 {
		return fmt.Sprintf("%5.2fs/%s", 1/rate, unit)
	}
	return fmt.Sprintf("%5.2f%s/s", rate, unit)
}

// formatCount prints whole numbers without a decimal point, matching tqdm's
// integer n_fmt/total_fmt.
func formatCount(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

func runeLen(s string) int {
	return len([]rune(s))
}
