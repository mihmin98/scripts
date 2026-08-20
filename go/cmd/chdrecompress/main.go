// chdrecompress re-runs existing .chd files through chdman with maximum
// compression settings and keeps the result only if it actually came out
// smaller.
//
// Examples:
//
//	chdrecompress game.chd
//	chdrecompress /roms/chd -r -j 4 --min-gain 1
//	chdrecompress '/roms/*.chd' --no-replace --outdir /roms/recompressed
//	chdrecompress /roms/chd --skip-optimal --dry-run
package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mihmin98/scripts/pkg/argparse"
	"github.com/mihmin98/scripts/pkg/chd"
	"github.com/mihmin98/scripts/pkg/progress"
)

// modeCfg is the per-image-type default the target settings are derived from.
type modeCfg struct {
	unit         int
	unitName     string
	defaultUnits int
	codecs       []string
}

var modeCfgs = map[string]modeCfg{
	"cd":  {unit: 2448, unitName: "frames", defaultUnits: 16, codecs: []string{"cdlz", "cdzl", "cdfl"}},
	"dvd": {unit: 2048, unitName: "sectors", defaultUnits: 16, codecs: []string{"lzma", "zlib", "huff", "flac"}},
}

var (
	hunkRe = regexp.MustCompile(`(?i)Hunk\s+Size:\s*([\d,]+)`)
	compRe = regexp.MustCompile(`(?i)Compression:\s*(.+)`)
)

// cdTags are the CD/GD-ROM track metadata tags written by createcd and its
// CD cousins; seeing one is a reliable sign that a .chd holds a CD image.
var cdTags = []string{"CHT2", "CHTR", "CHCD", "CHGT", "CHGD"}

// options holds the parsed command line.
type options struct {
	inputs      []string
	chdman      string
	mode        string
	outdir      string
	suffix      string
	recursive   bool
	hunkSize    int
	hasHunk     bool
	units       int
	hasUnits    bool
	cores       int
	jobs        int
	codecList   []string
	minGain     float64
	keepLarger  bool
	skipOptimal bool
	noReplace   bool
	backup      bool
	verify      bool
	force       bool
	dryRun      bool
	report      string
	quiet       bool
	extra       []string
}

// chdInfo is what "chdman info" tells us about an existing file.
type chdInfo struct {
	hunkBytes int
	hasHunk   bool
	codecs    []string
	isCD      bool
	raw       string
}

// result is the outcome of recompressing one file.
type result struct {
	path       string
	ok         bool
	skipped    bool
	replaced   bool
	reason     string
	mode       string
	oldBytes   int64
	newBytes   int64
	oldHunk    int
	hasOldHunk bool
	oldCodecs  []string
	log        []string
}

func main() {
	os.Exit(run())
}

func run() int {
	opts := parseArgs(os.Args[1:])

	chdman, err := chd.ResolveChdman(opts.chdman)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	files := chd.ExpandInputs(opts.inputs, []string{".chd"}, opts.recursive)
	files = slices.DeleteFunc(files, func(f string) bool {
		return strings.ToLower(filepath.Ext(f)) != ".chd"
	})
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "Nothing to do: no .chd files found.")
		return 1
	}
	if opts.outdir != "" {
		files = dropCollisions(files, opts.outdir, opts.suffix)
	}

	fmt.Printf("chdman:  %s\n", chdman)
	fmt.Printf("mode:    %s\n", opts.mode)
	if len(opts.codecList) > 0 {
		fmt.Printf("codecs:  %s\n", strings.Join(opts.codecList, ","))
	} else {
		fmt.Printf("codecs:  cd=%s  dvd=%s\n",
			strings.Join(modeCfgs["cd"].codecs, ","), strings.Join(modeCfgs["dvd"].codecs, ","))
	}
	if opts.hasHunk {
		fmt.Printf("hunk:    %d bytes\n", opts.hunkSize)
	} else {
		cdUnits, dvdUnits := modeCfgs["cd"].defaultUnits, modeCfgs["dvd"].defaultUnits
		if opts.hasUnits {
			cdUnits, dvdUnits = opts.units, opts.units
		}
		fmt.Printf("hunk:    cd=%d  dvd=%d\n", cdUnits*2448, dvdUnits*2048)
	}
	fmt.Printf("jobs:    %d x %d core(s)\n", opts.jobs, opts.cores)
	fmt.Printf("files:   %d\n\n", len(files))

	stop := make(chan struct{})
	interrupted := chd.WatchInterrupts(stop)

	nbars := 0
	if !opts.quiet && !opts.dryRun && chd.IsTerminal(os.Stderr) {
		nbars = opts.jobs
	}
	lines := 0
	if !opts.dryRun {
		lines = 1 + nbars
	}
	display := progress.NewDisplay(os.Stderr, os.Stdout, lines)

	var overall *progress.Bar
	if !opts.dryRun {
		overall = progress.NewBar(float64(len(files)), "file")
		display.Set(0, overall.Meter(display.Width()))
	}

	var mu sync.Mutex
	results := make([]result, 0, len(files))

	chd.RunJobs(len(files), opts.jobs, stop, func(index, slot int) {
		r := processOne(files[index], opts, chdman, display, slot, nbars, stop)

		mu.Lock()
		defer mu.Unlock()
		results = append(results, r)
		if overall != nil {
			overall.N++
			display.Set(0, overall.Meter(display.Width()))
		}
		name := filepath.Base(r.path)
		switch {
		case r.skipped:
			display.Write(fmt.Sprintf("  skip %s: %s", name, r.reason))
		case r.ok && r.newBytes < r.oldBytes:
			display.Write(fmt.Sprintf("  ok   %s [%s] %s -> %s (%s)",
				name, r.mode, chd.Human(float64(r.oldBytes)), chd.Human(float64(r.newBytes)), r.reason))
		case r.ok:
			display.Write(fmt.Sprintf("  keep %s [%s]: %s", name, r.mode, r.reason))
		default:
			display.Write(fmt.Sprintf("  FAIL %s: %s", name, r.reason))
			for _, line := range chd.LastLines(r.log, 5) {
				display.Write("       " + line)
			}
		}
	})

	display.Close()
	signal.Reset(os.Interrupt)

	var okCount, shrunk, failCount, skipCount int
	var oldTotal, newTotal int64
	for _, r := range results {
		switch {
		case r.ok:
			okCount++
			oldTotal += r.oldBytes
			newTotal += r.newBytes
			if r.newBytes < r.oldBytes {
				shrunk++
			}
		case r.skipped:
			skipCount++
		default:
			failCount++
		}
	}

	fmt.Printf("\nProcessed %d (%d improved), skipped %d, failed %d.\n",
		okCount, shrunk, skipCount, failCount)
	if okCount > 0 {
		saved := oldTotal - newTotal
		pct := 0.0
		if oldTotal > 0 {
			pct = float64(saved) / float64(oldTotal) * 100
		}
		fmt.Printf("%s -> %s  (saved %s, %.2f%%)\n",
			chd.Human(float64(oldTotal)), chd.Human(float64(newTotal)), chd.Human(float64(saved)), pct)
	}

	if opts.report != "" && len(results) > 0 {
		if err := writeReport(opts.report, results); err != nil {
			fmt.Fprintf(os.Stderr, "could not write report: %v\n", err)
		} else {
			fmt.Printf("Report written to %s\n", opts.report)
		}
	}

	if interrupted() {
		fmt.Println("Interrupted.")
		return 130
	}
	if failCount > 0 {
		return 1
	}
	return 0
}

// dropCollisions handles the case where results go to a shared --outdir and
// two same-named inputs from different directories would land on the same
// destination.
func dropCollisions(files []string, outdir, suffix string) []string {
	claimed := map[string]string{}
	unique := make([]string, 0, len(files))
	for _, f := range files {
		dest, err := filepath.Abs(filepath.Join(outdir, chd.Stem(f)+suffix+".chd"))
		if err != nil {
			dest = filepath.Join(outdir, chd.Stem(f)+suffix+".chd")
		}
		if prev, taken := claimed[dest]; taken {
			fmt.Fprintf(os.Stderr, "warning: skipping %s, would collide with %s\n", f, prev)
			continue
		}
		claimed[dest] = f
		unique = append(unique, f)
	}
	return unique
}

// probe reads the hunk size, codec list and CD-ness out of "chdman info".
func probe(chdman, path string) chdInfo {
	var info chdInfo

	cmd := exec.Command(chdman, "info", "-i", path)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Start(); err != nil {
		return info
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(120 * time.Second):
		cmd.Process.Kill()
		<-done
		return info
	}

	info.raw = out.String() + errOut.String()

	if m := hunkRe.FindStringSubmatch(info.raw); m != nil {
		if v, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", "")); err == nil {
			info.hunkBytes, info.hasHunk = v, true
		}
	}

	if m := compRe.FindStringSubmatch(info.raw); m != nil {
		for _, chunk := range strings.Split(m[1], ",") {
			fields := strings.Fields(chunk)
			if len(fields) == 0 {
				continue
			}
			// chdman writes codecs as "cdlz (CD LZMA)"; the tag is the part
			// before the parenthesised description.
			tag := strings.TrimSpace(strings.Split(fields[0], "(")[0])
			if tag != "" && !strings.EqualFold(tag, "none") {
				info.codecs = append(info.codecs, tag)
			}
		}
	}

	switch {
	case containsAny(info.raw, cdTags),
		strings.Contains(strings.ToLower(info.raw), "cdrom"),
		hasCDCodec(info.codecs):
		info.isCD = true
	case info.hasHunk && info.hunkBytes%2448 == 0 && info.hunkBytes%2048 != 0:
		// Divisible by the CD frame size but not the DVD sector size.
		info.isCD = true
	}
	return info
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func hasCDCodec(codecs []string) bool {
	for _, c := range codecs {
		if strings.HasPrefix(c, "cd") {
			return true
		}
	}
	return false
}

// targetSettings resolves the codec list and hunk size to aim for, given the
// mode a file turned out to be.
func targetSettings(mode string, opts options) ([]string, int) {
	cfg := modeCfgs[mode]
	codecs := opts.codecList
	if len(codecs) == 0 {
		codecs = cfg.codecs
	}
	if opts.hasHunk {
		return codecs, opts.hunkSize
	}
	units := cfg.defaultUnits
	if opts.hasUnits {
		units = opts.units
	}
	return codecs, units * cfg.unit
}

// processOne recompresses a single .chd, keeping the result only if it is
// worth keeping.
func processOne(path string, opts options, chdman string, display *progress.Display, slot, nbars int, stop <-chan struct{}) result {
	res := result{path: path}
	if st, err := os.Stat(path); err == nil {
		res.oldBytes = st.Size()
	}

	info := probe(chdman, path)
	res.oldHunk, res.hasOldHunk = info.hunkBytes, info.hasHunk
	res.oldCodecs = info.codecs

	mode := opts.mode
	if mode == "auto" {
		if strings.TrimSpace(info.raw) == "" {
			// "chdman info" produced nothing (missing/corrupt file, timeout).
			// Guessing "dvd" here would silently apply DVD codecs and a DVD
			// hunk size to what may well be a CD image, so refuse instead.
			res.reason = "could not read chdman info (use --mode cd/dvd to override)"
			return res
		}
		if info.isCD {
			mode = "cd"
		} else {
			mode = "dvd"
		}
	}
	res.mode = mode

	codecs, hunk := targetSettings(mode, opts)
	unit := modeCfgs[mode].unit
	if hunk%unit != 0 {
		res.reason = fmt.Sprintf("hunk size %d is not a multiple of %d for %s images", hunk, unit, mode)
		return res
	}

	if opts.skipOptimal && info.hasHunk && info.hunkBytes == hunk && slices.Equal(info.codecs, codecs) {
		res.skipped = true
		res.reason = fmt.Sprintf("already %s @ %d", strings.Join(codecs, ","), hunk)
		return res
	}

	outdir := opts.outdir
	if outdir == "" {
		outdir = filepath.Dir(path)
	}
	replace := opts.outdir == "" && !opts.noReplace
	// Two inputs with the same stem living in different directories land on
	// the same temp name once --outdir is in play, and parallel jobs would
	// then scribble over each other, so make the temp name unique per run and
	// slot.
	tmpName := fmt.Sprintf("%s.recomp.%d.%d.tmp.chd", chd.Stem(path), os.Getpid(), slot)

	var final, tmp string
	if replace {
		final = path
		tmp = filepath.Join(filepath.Dir(path), tmpName)
	} else {
		final = filepath.Join(outdir, chd.Stem(path)+opts.suffix+".chd")
		tmp = filepath.Join(outdir, tmpName)
		if _, err := os.Stat(final); err == nil && !opts.force {
			res.skipped = true
			res.reason = "output exists (use --force)"
			return res
		}
		if samePath(final, path) {
			res.skipped = true
			res.reason = "output would overwrite the input (set --suffix or --outdir)"
			return res
		}
	}

	cmd := []string{
		chdman, "copy",
		"-i", path,
		"-o", tmp,
		"-c", strings.Join(codecs, ","),
		"-hs", strconv.Itoa(hunk),
		"-f",
	}
	if opts.cores != 0 {
		cmd = append(cmd, "--numprocessors", strconv.Itoa(opts.cores))
	}
	cmd = append(cmd, opts.extra...)

	if opts.dryRun {
		res.skipped = true
		res.reason = "dry run: " + strings.Join(cmd, " ")
		return res
	}

	if err := os.MkdirAll(outdir, 0o777); err != nil {
		res.reason = err.Error()
		return res
	}

	var bar *progress.Bar
	line := slot + 1
	if nbars > 0 {
		bar = progress.NewBar(100, "")
		// The per-file bars are tqdm leave=False bars: they vanish once the
		// file is done.
		defer display.Set(line, "")
	}
	onProgress := chd.BarPainter(display, bar, line, filepath.Base(path))

	rc, tail, err := chd.RunChdman(stop, cmd, onProgress)
	if err != nil {
		return cancelledResult(res, tmp)
	}
	if rc != 0 {
		res.reason = fmt.Sprintf("chdman copy exited %d", rc)
		res.log = tail
		os.Remove(tmp)
		return res
	}

	if st, err := os.Stat(tmp); err == nil {
		res.newBytes = st.Size()
	}
	gain := res.oldBytes - res.newBytes
	gainPct := 0.0
	if res.oldBytes > 0 {
		gainPct = float64(gain) / float64(res.oldBytes) * 100
	}

	// With the default --min-gain of 0 a byte-for-byte identical result would
	// still pass "gainPct < 0", so require a real saving as well.
	if (gain <= 0 || gainPct < opts.minGain) && !opts.keepLarger {
		os.Remove(tmp)
		res.ok = true
		if gain > 0 {
			res.reason = fmt.Sprintf("no worthwhile gain (%+.2f%%)", gainPct)
		} else {
			res.reason = fmt.Sprintf("already better (%+.2f%%)", gainPct)
		}
		res.newBytes = res.oldBytes
		return res
	}

	if opts.verify {
		if bar != nil {
			bar.Reset(100)
		}
		rc, tail, err = chd.RunChdman(stop, []string{chdman, "verify", "-i", tmp}, onProgress)
		if err != nil {
			return cancelledResult(res, tmp)
		}
		if rc != 0 {
			res.reason = fmt.Sprintf("verification failed (chdman exited %d)", rc)
			res.log = tail
			os.Remove(tmp)
			return res
		}
	}

	if replace && opts.backup {
		backup := path + ".bak"
		if _, err := os.Stat(backup); err == nil && !opts.force {
			os.Remove(tmp)
			res.reason = fmt.Sprintf("backup %s already exists", filepath.Base(backup))
			return res
		}
		if err := os.Rename(path, backup); err != nil {
			os.Remove(tmp)
			res.reason = fmt.Sprintf("could not create backup: %v", err)
			return res
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		res.reason = err.Error()
		return res
	}
	res.replaced = replace

	res.ok = true
	res.reason = fmt.Sprintf("%+.2f%%", gainPct)
	return res
}

// cancelledResult drops the temporary file and records the input as skipped:
// an interrupted run is not a failure.
func cancelledResult(res result, tmp string) result {
	os.Remove(tmp)
	res.skipped = true
	res.reason = "cancelled"
	return res
}

// samePath reports whether two paths refer to the same file once symlinks are
// resolved, which is how the script notices an output that would clobber its
// own input.
func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra, _ = filepath.Abs(a)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb, _ = filepath.Abs(b)
	}
	return ra == rb
}

// writeReport writes the per-file summary CSV requested with --report.
func writeReport(path string, results []result) error {
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fh.Close()

	w := csv.NewWriter(fh)
	// Python's csv module writes CRLF line endings by default, and the report
	// is meant to be interchangeable with the one the original script wrote.
	w.UseCRLF = true
	defer w.Flush()

	if err := w.Write([]string{"file", "mode", "status", "old_bytes", "new_bytes",
		"saved_bytes", "saved_pct", "old_hunk", "old_codecs", "note"}); err != nil {
		return err
	}

	sorted := append([]result(nil), results...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path < sorted[j].path })

	for _, r := range sorted {
		var saved int64
		pct := 0.0
		if r.ok {
			saved = r.oldBytes - r.newBytes
			if r.oldBytes > 0 {
				pct = float64(saved) / float64(r.oldBytes) * 100
			}
		}
		status := "failed"
		switch {
		case r.skipped:
			status = "skipped"
		case r.ok:
			status = "ok"
		}
		newBytes := ""
		if r.newBytes != 0 {
			newBytes = strconv.FormatInt(r.newBytes, 10)
		}
		oldHunk := ""
		if r.hasOldHunk && r.oldHunk != 0 {
			oldHunk = strconv.Itoa(r.oldHunk)
		}
		if err := w.Write([]string{
			r.path, r.mode, status,
			strconv.FormatInt(r.oldBytes, 10), newBytes,
			strconv.FormatInt(saved, 10), fmt.Sprintf("%.2f", pct),
			oldHunk, strings.Join(r.oldCodecs, ","), r.reason,
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

// parseArgs mirrors the Python script's argument parser, including the checks
// it performs after parsing.
func parseArgs(argv []string) options {
	p := argparse.NewParser("chdrecompress",
		"Recompress existing .chd files with maximum compression, keeping the result only if it is smaller.")
	// The Python original uses RawDescriptionHelpFormatter, so the notes below
	// are laid out by hand rather than re-wrapped.
	p.RawText = true
	p.Epilog = `By default each file is re-encoded to a temporary file next to the original,
verified, and then moved over the original only if it came out smaller.  Use
--no-replace (optionally with --outdir / --suffix) to keep both copies, or
--backup to leave the previous version as <name>.chd.bak.

--skip-optimal uses 'chdman info' to skip files whose codec list and hunk size
already match the target, which is much faster than re-encoding everything.`

	p.MustAddArgument([]string{"inputs"}, &argparse.Options{Nargs: argparse.OneOrMore, Metavar: "INPUT",
		Help: "file, directory, or glob pattern"})

	p.MustAddArgument([]string{"--chdman"}, &argparse.Options{Metavar: "PATH", Help: "path to the chdman binary"})
	p.MustAddArgument([]string{"--mode"}, &argparse.Options{Choices: []string{"auto", "cd", "dvd"}, Default: "auto",
		Help: "which defaults to apply (default: detect from chdman info)"})
	p.MustAddArgument([]string{"-o", "--outdir"}, &argparse.Options{Metavar: "DIR",
		Help: "write results here instead of replacing the originals"})
	p.MustAddArgument([]string{"--suffix"}, &argparse.Options{Default: ".max", Metavar: "STR",
		Help: "filename suffix when not replacing (default: .max)"})
	p.MustAddArgument([]string{"-r", "--recursive"}, &argparse.Options{Action: argparse.StoreTrue, Help: "recurse into subdirectories"})

	p.MustAddArgument([]string{"--hunk-size"}, &argparse.Options{Type: argparse.Int, Metavar: "BYTES", Help: "target hunk size in bytes"})
	p.MustAddArgument([]string{"--frames", "--sectors"}, &argparse.Options{Dest: "units", Type: argparse.Int, Metavar: "N",
		Help: "target hunk size in CD frames (2448 B) or DVD sectors (2048 B)"})

	p.MustAddArgument([]string{"-c", "--cores"}, &argparse.Options{Type: argparse.Int, Metavar: "N",
		Help: "cores per chdman process (default: all, split across --jobs)"})
	p.MustAddArgument([]string{"-j", "--jobs"}, &argparse.Options{Type: argparse.Int, Default: 1, Metavar: "N",
		Help: "files to process in parallel (default: 1)"})
	p.MustAddArgument([]string{"--codecs"}, &argparse.Options{Metavar: "LIST", Help: "override the codec list"})

	p.MustAddArgument([]string{"--min-gain"}, &argparse.Options{Type: argparse.Float, Default: 0.0, Metavar: "PCT",
		Help: "only keep the new file if it saves at least this % (default: 0)"})
	p.MustAddArgument([]string{"--keep-larger"}, &argparse.Options{Action: argparse.StoreTrue,
		Help: "keep the re-encoded file even if it is not smaller"})
	p.MustAddArgument([]string{"--skip-optimal"}, &argparse.Options{Action: argparse.StoreTrue,
		Help: "skip files already using the target codecs and hunk size"})
	p.MustAddArgument([]string{"--no-replace"}, &argparse.Options{Action: argparse.StoreTrue,
		Help: "never overwrite the original, write a second file instead"})
	p.MustAddArgument([]string{"--backup"}, &argparse.Options{Action: argparse.StoreTrue,
		Help: "keep the original as <name>.chd.bak when replacing"})

	p.MustAddArgument([]string{"--no-verify"}, &argparse.Options{Dest: "verify", Action: argparse.StoreFalse,
		Help: "skip the verification pass before keeping a result"})
	p.MustAddArgument([]string{"-f", "--force"}, &argparse.Options{Action: argparse.StoreTrue, Help: "overwrite existing outputs/backups"})
	p.MustAddArgument([]string{"-n", "--dry-run"}, &argparse.Options{Action: argparse.StoreTrue, Help: "print what would happen"})
	p.MustAddArgument([]string{"--report"}, &argparse.Options{Metavar: "CSV", Help: "write a per-file summary to this CSV"})
	p.MustAddArgument([]string{"-q", "--quiet"}, &argparse.Options{Action: argparse.StoreTrue, Help: "suppress per-file progress bars"})
	p.MustAddArgument([]string{"--extra"}, &argparse.Options{Nargs: argparse.Remainder,
		Help: "everything after this is passed straight to chdman"})

	ns := p.MustParseArgs(argv)

	opts := options{
		inputs:      ns.Strings("inputs"),
		chdman:      ns.String("chdman"),
		mode:        ns.String("mode"),
		outdir:      ns.String("outdir"),
		suffix:      ns.String("suffix"),
		recursive:   ns.Bool("recursive"),
		hunkSize:    ns.Int("hunk_size"),
		hasHunk:     ns.IsSet("hunk_size"),
		units:       ns.Int("units"),
		hasUnits:    ns.IsSet("units"),
		cores:       ns.Int("cores"),
		jobs:        ns.Int("jobs"),
		minGain:     ns.Float("min_gain"),
		keepLarger:  ns.Bool("keep_larger"),
		skipOptimal: ns.Bool("skip_optimal"),
		noReplace:   ns.Bool("no_replace"),
		backup:      ns.Bool("backup"),
		verify:      ns.Bool("verify"),
		force:       ns.Bool("force"),
		dryRun:      ns.Bool("dry_run"),
		report:      ns.String("report"),
		quiet:       ns.Bool("quiet"),
		extra:       ns.Strings("extra"),
	}

	// --hunk-size and --frames are the two halves of a mutually exclusive
	// group, which pkg/argparse does not model, so enforce it here.
	if opts.hasHunk && opts.hasUnits {
		p.Fail("argument --frames/--sectors: not allowed with argument --hunk-size")
	}
	if opts.hasHunk && opts.hunkSize <= 0 {
		p.Fail("--hunk-size must be positive")
	}
	if opts.hasUnits && opts.units <= 0 {
		p.Fail("--frames must be positive")
	}

	if opts.jobs < 1 {
		p.Fail("--jobs must be >= 1")
	}
	if !ns.IsSet("cores") {
		opts.cores = max(1, runtime.NumCPU()/opts.jobs)
	} else if opts.cores < 1 {
		p.Fail("--cores must be >= 1")
	}

	if s := ns.String("codecs"); s != "" {
		opts.codecList = chd.SplitList(s)
	}
	return opts
}
