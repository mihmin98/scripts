// chdcompress compresses disc images to .chd with maximum compression.
//
// Examples:
//
//	chdcompress cd  game.cue
//	chdcompress cd  /roms/psx --delete
//	chdcompress dvd '/roms/ps2/*.iso' -j 4 --outdir /roms/chd
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/mihmin98/scripts/pkg/argparse"
	"github.com/mihmin98/scripts/pkg/chd"
	"github.com/mihmin98/scripts/pkg/progress"
)

// modeCfg describes everything that differs between CD and DVD images.
type modeCfg struct {
	command  string
	unit     int
	unitName string
	// defaultUnits is chosen at 16 rather than chdman's own 8 because doubling
	// the hunk gives the compressor more context and is still widely
	// compatible.
	defaultUnits int
	// chdman picks whichever codec compresses each hunk best, so listing more
	// of them never hurts the ratio -- it only costs encoding time.
	codecs []string
	exts   []string
}

var modeCfgs = map[string]modeCfg{
	"cd": {
		command: "createcd",
		// A raw CD frame is 2352 bytes of sector data + 96 bytes of subcode.
		unit:         2448,
		unitName:     "frames",
		defaultUnits: 16, // 39168 bytes
		codecs:       []string{"cdlz", "cdzl", "cdfl"},
		exts:         []string{".cue", ".gdi", ".toc", ".iso", ".nrg"},
	},
	"dvd": {
		command:      "createdvd",
		unit:         2048, // DVD sector
		unitName:     "sectors",
		defaultUnits: 16, // 32768 bytes
		codecs:       []string{"lzma", "zlib", "huff", "flac"},
		exts:         []string{".iso"},
	},
}

// options holds the parsed command line, already validated and with the
// derived values (hunk size, codec list, extensions) filled in.
type options struct {
	mode      string
	inputs    []string
	chdman    string
	outdir    string
	recursive bool
	hunkBytes int
	cores     int
	jobs      int
	codecList []string
	exts      []string
	delete    bool
	verify    bool
	force     bool
	dryRun    bool
	quiet     bool
	extra     []string
}

var (
	cueFileQuotedRe = regexp.MustCompile(`(?i)^\s*FILE\s+"([^"]+)"`)
	cueFileBareRe   = regexp.MustCompile(`(?i)^\s*FILE\s+(\S+)`)
	gdiQuotedRe     = regexp.MustCompile(`"([^"]+)"`)
)

// result is the outcome of converting one image.
type result struct {
	image    string
	output   string
	ok       bool
	skipped  bool
	reason   string
	srcBytes int64
	dstBytes int64
	log      []string
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

	images := chd.ExpandInputs(opts.inputs, opts.exts, opts.recursive)
	if len(images) == 0 {
		fmt.Fprintln(os.Stderr, "Nothing to do: no matching images found.")
		return 1
	}
	images = dropCollisions(images, opts.outdir)

	cfg := modeCfgs[opts.mode]
	fmt.Printf("chdman:  %s\n", chdman)
	fmt.Printf("mode:    %s (%s)\n", opts.mode, cfg.command)
	fmt.Printf("codecs:  %s\n", strings.Join(opts.codecList, ","))
	fmt.Printf("hunk:    %d bytes (%d %s)\n", opts.hunkBytes, opts.hunkBytes/cfg.unit, cfg.unitName)
	fmt.Printf("jobs:    %d x %d core(s)\n", opts.jobs, opts.cores)
	fmt.Printf("files:   %d\n\n", len(images))

	stop := make(chan struct{})
	interrupted := chd.WatchInterrupts(stop)

	showBars := !opts.quiet && !opts.dryRun && chd.IsTerminal(os.Stderr)
	nbars := 0
	if showBars {
		nbars = opts.jobs
	}

	lines := 0
	if !opts.dryRun {
		lines = 1 + nbars
	}
	display := progress.NewDisplay(os.Stderr, os.Stdout, lines)

	var overall *progress.Bar
	if !opts.dryRun {
		overall = progress.NewBar(float64(len(images)), "file")
		display.Set(0, overall.Meter(display.Width()))
	}

	var mu sync.Mutex
	results := make([]result, 0, len(images))

	chd.RunJobs(len(images), opts.jobs, stop, func(index, slot int) {
		r := processOne(images[index], opts, chdman, display, slot, nbars, stop)

		mu.Lock()
		defer mu.Unlock()
		results = append(results, r)
		if overall != nil {
			overall.N++
			display.Set(0, overall.Meter(display.Width()))
		}
		name := filepath.Base(r.image)
		switch {
		case r.ok:
			pct := 0.0
			if r.srcBytes > 0 {
				pct = float64(r.dstBytes) / float64(r.srcBytes) * 100
			}
			display.Write(fmt.Sprintf("  ok   %s -> %s (%.1f%% of source)",
				name, chd.Human(float64(r.dstBytes)), pct))
		case r.skipped:
			display.Write(fmt.Sprintf("  skip %s: %s", name, r.reason))
		default:
			display.Write(fmt.Sprintf("  FAIL %s: %s", name, r.reason))
			for _, line := range chd.LastLines(r.log, 5) {
				display.Write("       " + line)
			}
		}
	})

	display.Close()
	signal.Reset(os.Interrupt)

	var okCount, failCount, skipCount int
	var src, dst int64
	for _, r := range results {
		switch {
		case r.ok:
			okCount++
			src += r.srcBytes
			dst += r.dstBytes
		case r.skipped:
			skipCount++
		default:
			failCount++
		}
	}

	fmt.Printf("\nConverted %d, skipped %d, failed %d.\n", okCount, skipCount, failCount)
	if okCount > 0 {
		saved := src - dst
		pct := 0.0
		if src > 0 {
			pct = float64(saved) / float64(src) * 100
		}
		fmt.Printf("%s -> %s  (saved %s, %.1f%%)\n",
			chd.Human(float64(src)), chd.Human(float64(dst)), chd.Human(float64(saved)), pct)
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

// dropCollisions guards against two sources mapping onto the same .chd, e.g.
// game.cue and game.iso sitting side by side.
func dropCollisions(images []string, outdir string) []string {
	claimed := map[string]string{}
	unique := make([]string, 0, len(images))
	for _, img := range images {
		dir := outdir
		if dir == "" {
			dir = filepath.Dir(img)
		}
		dest, err := filepath.Abs(filepath.Join(dir, chd.Stem(img)+".chd"))
		if err != nil {
			dest = filepath.Join(dir, chd.Stem(img)+".chd")
		}
		if prev, taken := claimed[dest]; taken {
			fmt.Fprintf(os.Stderr, "warning: skipping %s, would collide with %s\n",
				filepath.Base(img), filepath.Base(prev))
			continue
		}
		claimed[dest] = img
		unique = append(unique, img)
	}
	return unique
}

// processOne converts a single image, optionally verifies it, and optionally
// deletes the sources it came from.
func processOne(image string, opts options, chdman string, display *progress.Display, slot, nbars int, stop <-chan struct{}) result {
	res := result{image: image}

	sources := sourceSet(image)
	for _, p := range sources {
		if st, err := os.Stat(p); err == nil {
			res.srcBytes += st.Size()
		}
	}

	outdir := opts.outdir
	if outdir == "" {
		outdir = filepath.Dir(image)
	}
	output := filepath.Join(outdir, chd.Stem(image)+".chd")
	res.output = output

	if _, err := os.Stat(output); err == nil && !opts.force {
		res.skipped = true
		res.reason = "output exists (use --force to overwrite)"
		return res
	}

	cmd := buildCommand(chdman, opts, image, output)
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
	}
	defer func() {
		if bar != nil {
			// The per-file bars are tqdm leave=False bars: they vanish once
			// the file is done.
			display.Set(line, "")
		}
	}()

	label := filepath.Base(image)
	onProgress := chd.BarPainter(display, bar, line, label)

	rc, tail, err := chd.RunChdman(stop, cmd, onProgress)
	if err != nil {
		return cancelledResult(res, output)
	}
	if rc != 0 {
		res.reason = fmt.Sprintf("chdman exited %d", rc)
		res.log = tail
		os.Remove(output)
		return res
	}

	// Verification is cheap insurance and is forced on when we are about to
	// delete the only other copy of the data.
	if opts.verify || opts.delete {
		if bar != nil {
			bar.Reset(100)
		}
		rc, tail, err = chd.RunChdman(stop, []string{chdman, "verify", "-i", output}, onProgress)
		if err != nil {
			return cancelledResult(res, output)
		}
		if rc != 0 {
			res.reason = fmt.Sprintf("verification failed (chdman exited %d)", rc)
			res.log = tail
			os.Remove(output)
			return res
		}
	}

	res.ok = true
	if st, err := os.Stat(output); err == nil {
		res.dstBytes = st.Size()
	}

	if opts.delete {
		for _, p := range sources {
			if err := os.Remove(p); err != nil {
				res.log = append(res.log, fmt.Sprintf("could not delete %s: %v", p, err))
			}
		}
	}
	return res
}

// cancelledResult removes the half-written output and records the file as
// skipped: cancelling is not a failure, so it stays out of the failed count
// and the non-zero exit status that would otherwise imply something broke.
func cancelledResult(res result, output string) result {
	os.Remove(output)
	res.skipped = true
	res.reason = "cancelled"
	return res
}

func buildCommand(chdman string, opts options, image, output string) []string {
	cfg := modeCfgs[opts.mode]
	cmd := []string{
		chdman, cfg.command,
		"-i", image,
		"-o", output,
		"-c", strings.Join(opts.codecList, ","),
		"-hs", fmt.Sprint(opts.hunkBytes),
	}
	if opts.cores != 0 {
		cmd = append(cmd, "--numprocessors", fmt.Sprint(opts.cores))
	}
	if opts.force {
		cmd = append(cmd, "-f")
	}
	return append(cmd, opts.extra...)
}

// sourceSet is the image plus any files it references, which is what --delete
// removes and what the size accounting counts.
func sourceSet(image string) []string {
	switch strings.ToLower(filepath.Ext(image)) {
	case ".cue":
		return append([]string{image}, cueSidecars(image)...)
	case ".gdi":
		return append([]string{image}, gdiSidecars(image)...)
	default:
		return []string{image}
	}
}

// cueSidecars lists the files a .cue sheet references, i.e. its .bin tracks.
func cueSidecars(cue string) []string {
	var files []string
	for _, line := range readLines(cue) {
		m := cueFileQuotedRe.FindStringSubmatch(line)
		if m == nil {
			m = cueFileBareRe.FindStringSubmatch(line)
		}
		if m == nil {
			continue
		}
		if ref, ok := existingSibling(cue, m[1]); ok {
			files = append(files, ref)
		}
	}
	return files
}

// gdiSidecars lists the track files a .gdi references. The first line is the
// track count, not a track.
func gdiSidecars(gdi string) []string {
	var files []string
	lines := readLines(gdi)
	if len(lines) > 0 {
		lines = lines[1:]
	}
	for _, line := range lines {
		name := ""
		if m := gdiQuotedRe.FindStringSubmatch(line); m != nil {
			name = m[1]
		} else if parts := strings.Fields(line); len(parts) >= 5 {
			name = parts[4]
		}
		if name == "" {
			continue
		}
		if ref, ok := existingSibling(gdi, name); ok {
			files = append(files, ref)
		}
	}
	return files
}

// existingSibling resolves a track name relative to the sheet that named it
// and reports whether it is a file we can actually see.
func existingSibling(sheet, name string) (string, bool) {
	ref := filepath.Join(filepath.Dir(sheet), name)
	if resolved, err := filepath.EvalSymlinks(ref); err == nil {
		ref = resolved
	}
	st, err := os.Stat(ref)
	if err != nil || st.IsDir() {
		return "", false
	}
	return ref, true
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// parseArgs mirrors the Python script's argument parser, including the checks
// it performs after parsing.
func parseArgs(argv []string) options {
	p := argparse.NewParser("chdcompress", "Compress disc images to .chd with maximum compression.")
	// The Python original uses RawDescriptionHelpFormatter, so the notes below
	// are laid out by hand rather than re-wrapped.
	p.RawText = true
	p.Epilog = `Notes:
  * For CD mode, always point at the .cue / .gdi / .toc, never the raw .bin.
  * --hunk-size and --frames are two ways of saying the same thing; the unit is
    2448 bytes per frame for CD and 2048 bytes per sector for DVD.
  * Larger hunks compress better but cost more work per random read, and some
    emulators are conservative about non-default values.  Pass --frames 8 (CD)
    to fall back to chdman's stock hunk size.
  * --delete also removes the .bin/track files referenced by a .cue or .gdi.`

	p.MustAddArgument([]string{"mode"}, &argparse.Options{Choices: []string{"cd", "dvd"}, Help: "use createcd or createdvd"})
	p.MustAddArgument([]string{"inputs"}, &argparse.Options{Nargs: argparse.OneOrMore, Metavar: "INPUT",
		Help: "file, directory, or glob pattern (quote globs to let the script expand them)"})

	p.MustAddArgument([]string{"--chdman"}, &argparse.Options{Metavar: "PATH", Help: "path to the chdman binary"})
	p.MustAddArgument([]string{"-o", "--outdir"}, &argparse.Options{Metavar: "DIR", Help: "write .chd files here (default: next to the source)"})
	p.MustAddArgument([]string{"-r", "--recursive"}, &argparse.Options{Action: argparse.StoreTrue, Help: "recurse into subdirectories"})

	p.MustAddArgument([]string{"--hunk-size"}, &argparse.Options{Type: argparse.Int, Metavar: "BYTES", Help: "hunk size in bytes"})
	p.MustAddArgument([]string{"--frames", "--sectors"}, &argparse.Options{Dest: "units", Type: argparse.Int, Metavar: "N",
		Help: "hunk size in CD frames (2448 B) or DVD sectors (2048 B)"})

	p.MustAddArgument([]string{"-c", "--cores"}, &argparse.Options{Type: argparse.Int, Metavar: "N", Help: "cores per chdman process (default: all, split across --jobs)"})
	p.MustAddArgument([]string{"-j", "--jobs"}, &argparse.Options{Type: argparse.Int, Default: 1, Metavar: "N", Help: "files to convert in parallel (default: 1)"})
	p.MustAddArgument([]string{"--codecs"}, &argparse.Options{Metavar: "LIST", Help: "override the codec list, e.g. cdlz,cdzl,cdfl"})
	p.MustAddArgument([]string{"--ext"}, &argparse.Options{Metavar: "LIST", Help: "extensions to pick up when scanning directories"})

	p.MustAddArgument([]string{"--delete"}, &argparse.Options{Action: argparse.StoreTrue, Help: "delete source images after a successful, verified conversion"})
	p.MustAddArgument([]string{"--verify"}, &argparse.Options{Action: argparse.StoreTrue, Help: "run 'chdman verify' on each result (implied by --delete)"})
	p.MustAddArgument([]string{"-f", "--force"}, &argparse.Options{Action: argparse.StoreTrue, Help: "overwrite existing .chd files"})
	p.MustAddArgument([]string{"-n", "--dry-run"}, &argparse.Options{Action: argparse.StoreTrue, Help: "print the commands without running them"})
	p.MustAddArgument([]string{"-q", "--quiet"}, &argparse.Options{Action: argparse.StoreTrue, Help: "suppress per-file progress bars"})
	p.MustAddArgument([]string{"--extra"}, &argparse.Options{Nargs: argparse.Remainder, Help: "everything after this is passed straight to chdman"})

	ns := p.MustParseArgs(argv)

	opts := options{
		mode:      ns.String("mode"),
		inputs:    ns.Strings("inputs"),
		chdman:    ns.String("chdman"),
		outdir:    ns.String("outdir"),
		recursive: ns.Bool("recursive"),
		cores:     ns.Int("cores"),
		jobs:      ns.Int("jobs"),
		delete:    ns.Bool("delete"),
		verify:    ns.Bool("verify"),
		force:     ns.Bool("force"),
		dryRun:    ns.Bool("dry_run"),
		quiet:     ns.Bool("quiet"),
		extra:     ns.Strings("extra"),
	}
	cfg := modeCfgs[opts.mode]

	// --hunk-size and --frames are the two halves of a mutually exclusive
	// group, which pkg/argparse does not model, so enforce it here.
	if ns.IsSet("hunk_size") && ns.IsSet("units") {
		p.Fail("argument --frames/--sectors: not allowed with argument --hunk-size")
	}

	if ns.IsSet("hunk_size") {
		hs := ns.Int("hunk_size")
		if hs <= 0 || hs%cfg.unit != 0 {
			p.Fail(fmt.Sprintf("--hunk-size must be a positive multiple of %d for %s images", cfg.unit, opts.mode))
		}
		opts.hunkBytes = hs
	} else {
		units := cfg.defaultUnits
		if ns.IsSet("units") {
			units = ns.Int("units")
		}
		if units <= 0 {
			p.Fail("--frames must be positive")
		}
		opts.hunkBytes = units * cfg.unit
	}

	if opts.jobs < 1 {
		p.Fail("--jobs must be >= 1")
	}
	if !ns.IsSet("cores") {
		opts.cores = max(1, runtime.NumCPU()/opts.jobs)
	} else if opts.cores < 1 {
		p.Fail("--cores must be >= 1")
	}

	opts.codecList = cfg.codecs
	if s := ns.String("codecs"); s != "" {
		opts.codecList = chd.SplitList(s)
	}

	opts.exts = cfg.exts
	if s := ns.String("ext"); s != "" {
		opts.exts = nil
		for _, e := range strings.Split(s, ",") {
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			opts.exts = append(opts.exts, e)
		}
	}
	return opts
}
