package argparse

import (
	"strings"
	"testing"
)

// newTestParser builds a parser that never touches os.Exit.
func newTestParser(t *testing.T) *Parser {
	t.Helper()
	p := NewParser("prog", "test program")
	p.ExitOnHelp = false
	return p
}

func TestStoreAndTypes(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*Parser)
		args   []string
		verify func(*testing.T, *Namespace)
	}{
		{
			name: "long value separate",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"-d", "--music-dir"}, &Options{})
			},
			args: []string{"--music-dir", "/tmp/music"},
			verify: func(t *testing.T, ns *Namespace) {
				if got := ns.String("music_dir"); got != "/tmp/music" {
					t.Errorf("music_dir = %q", got)
				}
			},
		},
		{
			name: "long value with equals",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"--id"}, &Options{})
			},
			args: []string{"--id=603"},
			verify: func(t *testing.T, ns *Namespace) {
				if got := ns.String("id"); got != "603" {
					t.Errorf("id = %q", got)
				}
			},
		},
		{
			name: "short attached value",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"-b", "--bitrate"}, &Options{Type: Int})
			},
			args: []string{"-b192"},
			verify: func(t *testing.T, ns *Namespace) {
				if got := ns.Int("bitrate"); got != 192 {
					t.Errorf("bitrate = %d", got)
				}
			},
		},
		{
			name: "bundled short flags",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"-a"}, &Options{Action: StoreTrue})
				p.MustAddArgument([]string{"-b"}, &Options{Action: StoreTrue})
				p.MustAddArgument([]string{"-c"}, &Options{Action: StoreTrue})
			},
			args: []string{"-abc"},
			verify: func(t *testing.T, ns *Namespace) {
				for _, d := range []string{"a", "b", "c"} {
					if !ns.Bool(d) {
						t.Errorf("%s not set", d)
					}
				}
			},
		},
		{
			name: "bundled short flags with trailing value",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"-v"}, &Options{Action: StoreTrue})
				p.MustAddArgument([]string{"-f"}, &Options{})
			},
			args: []string{"-vfname.txt"},
			verify: func(t *testing.T, ns *Namespace) {
				if !ns.Bool("v") || ns.String("f") != "name.txt" {
					t.Errorf("v=%v f=%q", ns.Bool("v"), ns.String("f"))
				}
			},
		},
		{
			name: "defaults fill unseen arguments",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"-b", "--bitrate"}, &Options{Type: Int, Default: 160})
				p.MustAddArgument([]string{"-v", "--verbose"}, &Options{Action: StoreTrue})
			},
			args: nil,
			verify: func(t *testing.T, ns *Namespace) {
				if ns.Int("bitrate") != 160 || ns.Bool("verbose") {
					t.Errorf("bitrate=%d verbose=%v", ns.Int("bitrate"), ns.Bool("verbose"))
				}
				if ns.IsSet("bitrate") {
					t.Error("bitrate reported as set")
				}
			},
		},
		{
			name: "string default is converted",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"-n"}, &Options{Type: Int, Default: "42"})
			},
			args: nil,
			verify: func(t *testing.T, ns *Namespace) {
				if ns.Int("n") != 42 {
					t.Errorf("n = %v", ns.Int("n"))
				}
			},
		},
		{
			name: "store false and store const",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"--no-color"}, &Options{Action: StoreFalse, Dest: "color"})
				p.MustAddArgument([]string{"--mode"}, &Options{Action: StoreConst, Const: "fast"})
			},
			args: []string{"--no-color", "--mode"},
			verify: func(t *testing.T, ns *Namespace) {
				if ns.Bool("color") {
					t.Error("color should be false")
				}
				if ns.String("mode") != "fast" {
					t.Errorf("mode = %v", ns.String("mode"))
				}
			},
		},
		{
			name: "count action",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"-v"}, &Options{Action: Count})
			},
			args: []string{"-vvv", "-v"},
			verify: func(t *testing.T, ns *Namespace) {
				if ns.Count("v") != 4 {
					t.Errorf("v = %d", ns.Count("v"))
				}
			},
		},
		{
			name: "append action",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"-I", "--include"}, &Options{Action: Append})
			},
			args: []string{"-I", "a", "--include", "b"},
			verify: func(t *testing.T, ns *Namespace) {
				got := ns.Strings("include")
				if len(got) != 2 || got[0] != "a" || got[1] != "b" {
					t.Errorf("include = %v", got)
				}
			},
		},
		{
			name: "negative number is a positional",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"offset"}, &Options{Type: Int})
			},
			args: []string{"-5"},
			verify: func(t *testing.T, ns *Namespace) {
				if ns.Int("offset") != -5 {
					t.Errorf("offset = %d", ns.Int("offset"))
				}
			},
		},
		{
			name: "double dash ends options",
			setup: func(p *Parser) {
				p.MustAddArgument([]string{"-v"}, &Options{Action: StoreTrue})
				p.MustAddArgument([]string{"name"}, &Options{})
			},
			args: []string{"-v", "--", "-weird-name"},
			verify: func(t *testing.T, ns *Namespace) {
				if ns.String("name") != "-weird-name" {
					t.Errorf("name = %q", ns.String("name"))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t)
			tt.setup(p)
			ns, err := p.ParseArgs(tt.args)
			if err != nil {
				t.Fatalf("ParseArgs(%v) = %v", tt.args, err)
			}
			tt.verify(t, ns)
		})
	}
}

func TestNargs(t *testing.T) {
	tests := []struct {
		name   string
		nargs  Nargs
		args   []string
		want   []string
		single string
		errStr string
	}{
		{name: "exact count", nargs: NArgs(2), args: []string{"--f", "a", "b"}, want: []string{"a", "b"}},
		{name: "exact count too few", nargs: NArgs(2), args: []string{"--f", "a"}, errStr: "expected 2 arguments"},
		{name: "one or more", nargs: OneOrMore, args: []string{"--f", "a", "b", "c"}, want: []string{"a", "b", "c"}},
		{name: "one or more needs one", nargs: OneOrMore, args: []string{"--f"}, errStr: "at least one argument"},
		{name: "zero or more", nargs: ZeroOrMore, args: []string{"--f"}, want: nil},
		{name: "optional present", nargs: Optional, args: []string{"--f", "a"}, single: "a"},
		{name: "remainder", nargs: Remainder, args: []string{"--f", "a", "-x", "--y"}, want: []string{"a", "-x", "--y"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t)
			p.MustAddArgument([]string{"--f"}, &Options{Nargs: tt.nargs})
			ns, err := p.ParseArgs(tt.args)
			if tt.errStr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errStr) {
					t.Fatalf("err = %v, want containing %q", err, tt.errStr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseArgs(%v) = %v", tt.args, err)
			}
			if tt.single != "" {
				if got := ns.String("f"); got != tt.single {
					t.Fatalf("f = %q, want %q", got, tt.single)
				}
				return
			}
			got := ns.Strings("f")
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("f = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPositionals(t *testing.T) {
	p := newTestParser(t)
	p.MustAddArgument([]string{"src"}, &Options{})
	p.MustAddArgument([]string{"rest"}, &Options{Nargs: ZeroOrMore})

	ns, err := p.ParseArgs([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if ns.String("src") != "a" {
		t.Errorf("src = %q", ns.String("src"))
	}
	if got := ns.Strings("rest"); strings.Join(got, ",") != "b,c" {
		t.Errorf("rest = %v", got)
	}

	if _, err := p.ParseArgs(nil); err == nil || !strings.Contains(err.Error(), "required: src") {
		t.Errorf("missing positional err = %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*Parser)
		args   []string
		errStr string
	}{
		{
			name:   "missing required optional",
			setup:  func(p *Parser) { p.MustAddArgument([]string{"-n", "--name"}, &Options{Required: true}) },
			args:   nil,
			errStr: "the following arguments are required: -n/--name",
		},
		{
			name:   "unrecognized option",
			setup:  func(p *Parser) {},
			args:   []string{"--nope"},
			errStr: "unrecognized arguments: --nope",
		},
		{
			name:   "invalid choice",
			setup:  func(p *Parser) { p.MustAddArgument([]string{"-m"}, &Options{Choices: []string{"tmdb", "tvdb"}}) },
			args:   []string{"-m", "imdb"},
			errStr: "invalid choice: 'imdb' (choose from 'tmdb', 'tvdb')",
		},
		{
			name:   "invalid int",
			setup:  func(p *Parser) { p.MustAddArgument([]string{"-y"}, &Options{Type: Int}) },
			args:   []string{"-y", "nineteen"},
			errStr: "invalid int value: 'nineteen'",
		},
		{
			name:   "missing value",
			setup:  func(p *Parser) { p.MustAddArgument([]string{"-y"}, &Options{}) },
			args:   []string{"-y"},
			errStr: "expected one argument",
		},
		{
			name:   "extra positional",
			setup:  func(p *Parser) {},
			args:   []string{"stray"},
			errStr: "unrecognized arguments: stray",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t)
			tt.setup(p)
			_, err := p.ParseArgs(tt.args)
			if err == nil {
				t.Fatalf("ParseArgs(%v) succeeded, want error", tt.args)
			}
			if !strings.Contains(err.Error(), tt.errStr) {
				t.Fatalf("err = %q, want containing %q", err, tt.errStr)
			}
			var argErr *ArgumentError
			if !asArgumentError(err, &argErr) {
				t.Fatalf("err type = %T, want *ArgumentError", err)
			}
		})
	}
}

func asArgumentError(err error, target **ArgumentError) bool {
	e, ok := err.(*ArgumentError)
	if ok {
		*target = e
	}
	return ok
}

func TestDefinitionErrors(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
		opts  *Options
	}{
		{name: "mixed positional and flag", flags: []string{"name", "--name"}},
		{name: "long short flag", flags: []string{"-vv"}},
		{name: "nargs with store true", flags: []string{"-v"}, opts: &Options{Action: StoreTrue, Nargs: OneOrMore}},
		{name: "required positional", flags: []string{"name"}, opts: &Options{Required: true}},
		{name: "store const without const", flags: []string{"--x"}, opts: &Options{Action: StoreConst}},
		{name: "duplicate flag", flags: []string{"-h"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t)
			if _, err := p.AddArgument(tt.flags, tt.opts); err == nil {
				t.Fatalf("AddArgument(%v) succeeded, want error", tt.flags)
			}
		})
	}
}

func TestHelpOutput(t *testing.T) {
	p := newTestParser(t)
	p.Prog = "generate-jellyfin-name"
	p.Description = "Script for generating a movie/show directory name for Jellyfin"
	p.MustAddArgument([]string{"-n", "--name"}, &Options{Required: true, Help: "Name of the movie/show"})
	p.MustAddArgument([]string{"-y", "--year"}, &Options{Type: Int, Help: "Year of the movie/show"})
	p.MustAddArgument([]string{"-r", "--replace-spaces"}, &Options{Action: StoreTrue, Help: "Replace spaces with dots"})
	p.MustAddArgument([]string{"files"}, &Options{Nargs: ZeroOrMore, Help: "Files to rename"})

	want := `usage: generate-jellyfin-name [-h] -n NAME [-y YEAR] [-r] [files ...]

Script for generating a movie/show directory name for Jellyfin

positional arguments:
  files                 Files to rename

options:
  -h, --help            show this help message and exit
  -n NAME, --name NAME  Name of the movie/show
  -y YEAR, --year YEAR  Year of the movie/show
  -r, --replace-spaces  Replace spaces with dots
`
	if got := p.FormatHelp(); got != want {
		t.Errorf("FormatHelp() =\n%s\nwant\n%s", got, want)
	}
}

func TestHelpFlagStopsParsing(t *testing.T) {
	p := newTestParser(t)
	p.MustAddArgument([]string{"-n"}, &Options{Required: true})

	if _, err := p.ParseArgs([]string{"--help"}); err != nil {
		t.Fatalf("--help returned error %v", err)
	}
	if !p.HelpRequested() {
		t.Error("HelpRequested() = false")
	}
}
