// Package chd holds the pieces the chdcompress and chdrecompress commands
// share: locating the chdman binary, expanding command line inputs into a list
// of images, running chdman while following its progress output, and
// formatting byte counts the way both scripts report them.
package chd

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mihmin98/scripts/pkg/globx"
)

// ErrCancelled is returned by RunChdman when the stop channel closes before
// chdman finishes. It is not a failure: the caller is expected to clean up the
// partial output and record the file as skipped.
var ErrCancelled = errors.New("cancelled")

// progressRe matches chdman's single rewritten status line, e.g.
// "Compressing, 42.3% complete... (ratio=61.2%)".
var progressRe = regexp.MustCompile(
	`(?i)(\w+),\s*(\d+(?:\.\d+)?)%\s*complete(?:.*?ratio\s*=\s*(\d+(?:\.\d+)?)%)?`)

// tailLines is how much of chdman's non-progress output we keep to show after
// a failure.
const tailLines = 40

// Human formats a byte count the way both scripts print sizes: whole bytes
// below 1 KiB, one decimal and a thousands separator above it.
func Human(nbytes float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	for _, unit := range units {
		if math.Abs(nbytes) < 1024 || unit == "TiB" {
			if unit == "B" {
				return formatComma(nbytes, 0) + " B"
			}
			return formatComma(nbytes, 1) + " " + unit
		}
		nbytes /= 1024.0
	}
	return fmt.Sprintf("%.1f TiB", nbytes)
}

// formatComma renders a float with the given number of decimals and a comma
// every three digits of the integer part, matching Python's ",.Nf" format.
func formatComma(v float64, prec int) string {
	s := strconv.FormatFloat(v, 'f', prec, 64)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	intPart, frac, hasFrac := strings.Cut(s, ".")

	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := sign + b.String()
	if hasFrac {
		out += "." + frac
	}
	return out
}

// ResolveChdman finds the chdman binary: an explicit override first, then
// ./chdman, then PATH.
func ResolveChdman(override string) (string, error) {
	if override != "" {
		p := globx.ExpandUser(override)
		if isExecutableFile(p) {
			return resolve(p), nil
		}
		if found, err := exec.LookPath(override); err == nil {
			return found, nil
		}
		return "", fmt.Errorf("chdman not found or not executable: %s", override)
	}

	cwd, err := os.Getwd()
	if err == nil {
		for _, name := range []string{"chdman", "chdman.exe"} {
			local := filepath.Join(cwd, name)
			if isExecutableFile(local) {
				return resolve(local), nil
			}
		}
	}

	if found, err := exec.LookPath("chdman"); err == nil {
		return found, nil
	}
	return "", errors.New("chdman not found in the current directory or on PATH (use --chdman).")
}

// resolve makes a path absolute and follows any symlinks, the equivalent of
// pathlib.Path.resolve. A path we cannot resolve is returned as it came in.
func resolve(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode().Perm()&0o111 != 0
}

// ExpandInputs turns files, directories and glob patterns into a de-duplicated
// list of absolute paths. Directories contribute the files whose extension is
// in exts (recursing when recursive is set); a file named explicitly on the
// command line is always taken, whatever its extension, because the user asked
// for it by name.
func ExpandInputs(patterns []string, exts []string, recursive bool) []string {
	var out []string
	seen := map[string]bool{}

	extSet := map[string]bool{}
	for _, e := range exts {
		extSet[strings.ToLower(e)] = true
	}

	add := func(p string) {
		rp, err := filepath.Abs(p)
		if err != nil {
			return
		}
		if resolved, err := filepath.EvalSymlinks(rp); err == nil {
			rp = resolved
		}
		if !seen[rp] {
			seen[rp] = true
			out = append(out, rp)
		}
	}

	for _, pat := range patterns {
		expanded := globx.Glob(globx.ExpandUser(pat))
		// The shell usually expands globs itself; if it did not (quoted
		// pattern) or matched nothing, fall back to treating the argument as a
		// literal path.
		if len(expanded) == 0 {
			lit := globx.ExpandUser(pat)
			if _, err := os.Lstat(lit); err == nil {
				expanded = []string{lit}
			} else {
				fmt.Fprintf(os.Stderr, "warning: no match for '%s'\n", pat)
				continue
			}
		}

		for _, path := range expanded {
			st, err := os.Stat(path)
			if err != nil {
				continue
			}
			if st.IsDir() {
				for _, child := range scanDir(path, recursive) {
					if extSet[strings.ToLower(filepath.Ext(child))] {
						add(child)
					}
				}
			} else {
				add(path)
			}
		}
	}
	return out
}

// scanDir lists the files under dir, sorted by path so that runs are
// reproducible, mirroring the sorted() around Python's glob/rglob.
func scanDir(dir string, recursive bool) []string {
	var files []string
	if recursive {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				files = append(files, path)
			}
			return nil
		})
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				files = append(files, p)
			}
		}
	}
	sort.Strings(files)
	return files
}

// RunJobs calls fn for each index in [0, count), at most jobs at a time. Each
// call gets an exclusive slot in [0, jobs), which is the screen line its
// progress bar owns. With jobs == 1 the work runs on the calling goroutine, so
// a single-job run behaves exactly like a plain loop.
//
// Once stop closes, no further work is dispatched; whatever is already running
// is left to notice the cancellation itself.
func RunJobs(count, jobs int, stop <-chan struct{}, fn func(index, slot int)) {
	slots := make(chan int, jobs)
	for i := range jobs {
		slots <- i
	}

	var wg sync.WaitGroup
	for i := range count {
		select {
		case <-stop:
			wg.Wait()
			return
		default:
		}

		slot := <-slots
		if jobs == 1 {
			fn(i, slot)
			slots <- slot
			continue
		}
		wg.Add(1)
		go func(index, slot int) {
			defer wg.Done()
			defer func() { slots <- slot }()
			fn(index, slot)
		}(i, slot)
	}
	wg.Wait()
}

// Progress is one parsed chdman status update.
type Progress struct {
	Verb  string // "compressing", "verifying", ...
	Pct   float64
	Ratio string // compression ratio as chdman printed it, or "" if absent
}

// RunChdman runs chdman with stdout discarded and stderr streamed. chdman
// rewrites a single progress line using carriage returns, so the stream is
// split on \r as well as \n and every status update is handed to onProgress
// (which may be nil). Lines that are not progress updates are collected and
// the last few returned, to be shown if the run fails.
//
// Closing stop terminates chdman and returns ErrCancelled.
func RunChdman(stop <-chan struct{}, argv []string, onProgress func(Progress)) (int, []string, error) {
	var tail []string

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = nil
	// chdman runs in its own process group so that a Ctrl-C on the terminal
	// does not reach it: we want to shut it down ourselves, after deciding
	// what to do with the partial output.
	detachProcess(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, nil, err
	}
	if err := cmd.Start(); err != nil {
		return -1, nil, err
	}

	handle := func(line string) {
		if m := progressRe.FindStringSubmatch(line); m != nil {
			if onProgress != nil {
				pct, _ := strconv.ParseFloat(m[2], 64)
				onProgress(Progress{Verb: m[1], Pct: math.Min(pct, 100.0), Ratio: m[3]})
			}
			return
		}
		if s := strings.TrimSpace(line); s != "" {
			tail = append(tail, s)
			if len(tail) > tailLines {
				tail = tail[len(tail)-tailLines:]
			}
		}
	}

	// A reader goroutine keeps the blocking read off the path that has to
	// notice a cancellation.
	type chunk struct {
		data []byte
		err  error
	}
	chunks := make(chan chunk)
	go func() {
		defer close(chunks)
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				chunks <- chunk{data: append([]byte(nil), buf[:n]...)}
			}
			if err != nil {
				return
			}
		}
	}()

	var pending []byte
	cancelled := false
loop:
	for {
		select {
		case <-stop:
			cancelled = true
			break loop
		case c, ok := <-chunks:
			if !ok {
				break loop
			}
			pending = append(pending, c.data...)
			parts := splitLines(string(pending))
			// The last fragment has no terminator yet, so it stays buffered
			// until the rest of the line arrives.
			pending = []byte(parts[len(parts)-1])
			for _, part := range parts[:len(parts)-1] {
				handle(part)
			}
		}
	}

	if cancelled {
		waitOrKill(cmd)
		return -1, tail, ErrCancelled
	}

	if len(pending) > 0 {
		handle(string(pending))
	}
	err = cmd.Wait()
	return exitCode(cmd, err), tail, nil
}

// waitOrKill asks chdman to stop and gives it ten seconds to do so before
// killing it, so a half-written .chd is closed cleanly where possible.
func waitOrKill(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := signalTerminate(cmd); err != nil {
		cmd.Process.Kill()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		cmd.Wait()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		<-done
	}
}

// splitLines splits on either carriage return or newline, keeping empty
// fields and the trailing (possibly empty) fragment as the last element.
func splitLines(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' || s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func exitCode(cmd *exec.Cmd, err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	return -1
}
