// Package globx implements the subset of Python's glob module that the shell
// does not already provide: shell-style patterns with a recursive "**"
// component, matched the way glob.glob(pattern, recursive=True) matches them.
//
// The differences from filepath.Glob are the ones that matter for quoted
// patterns such as "/roms/**/*.iso":
//
//   - "**" as a whole path component matches zero or more directories, and as
//     the final component it also matches every file underneath.
//   - entries whose name starts with a dot are only matched by a pattern
//     component that itself starts with a dot.
//   - a pattern with no wildcards is returned as-is if the path exists.
package globx

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

// ExpandUser expands a leading "~" or "~/..." to the current user's home
// directory, mirroring os.path.expanduser. Other users' homes ("~bob") are
// left untouched, as is a path we cannot resolve a home for.
func ExpandUser(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	if len(path) > 1 && path[1] != '/' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	return home + path[1:]
}

// HasMagic reports whether the pattern contains any wildcard character.
func HasMagic(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// Glob expands a pattern into the list of matching paths, sorted. Unlike
// Python's glob it never returns an error: an unreadable directory simply
// contributes no matches.
func Glob(pattern string) []string {
	if pattern == "" {
		return nil
	}

	root := ""
	rest := pattern
	if strings.HasPrefix(pattern, "/") {
		root = "/"
		rest = strings.TrimLeft(pattern, "/")
	}

	comps := splitComponents(rest)
	if len(comps) == 0 {
		if _, err := os.Lstat(root); err == nil {
			return []string{root}
		}
		return nil
	}

	// Each round maps the current candidate directories to the paths produced
	// by the next pattern component.
	current := []string{root}
	for i, comp := range comps {
		last := i == len(comps)-1
		var next []string
		for _, dir := range current {
			next = append(next, expandComponent(dir, comp, last)...)
		}
		current = next
		if len(current) == 0 {
			break
		}
	}

	sort.Strings(current)
	return current
}

// splitComponents drops the empty components that repeated or trailing
// slashes produce, except for a trailing one, which Python keeps as a
// "directories only" marker. We do not need that marker, so a trailing slash
// simply restricts the result to directories via the pattern itself.
func splitComponents(s string) []string {
	var out []string
	for _, c := range strings.Split(s, "/") {
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// join glues a directory prefix and a name, keeping "" as "relative to the
// working directory" and "/" as the filesystem root.
func join(dir, name string) string {
	switch dir {
	case "":
		return name
	case "/":
		return "/" + name
	default:
		return dir + "/" + name
	}
}

// expandComponent produces every path that the single pattern component comp
// matches inside dir. last tells us whether files are acceptable results:
// intermediate components can only ever continue through directories.
func expandComponent(dir, comp string, last bool) []string {
	if comp == "**" {
		return expandRecursive(dir, last)
	}

	if !HasMagic(comp) {
		p := join(dir, comp)
		if _, err := os.Lstat(p); err != nil {
			return nil
		}
		if !last && !isDir(p) {
			return nil
		}
		return []string{p}
	}

	re := translate(comp)
	entries, err := os.ReadDir(dirForRead(dir))
	if err != nil {
		return nil
	}
	hidden := strings.HasPrefix(comp, ".")

	var out []string
	for _, e := range entries {
		name := e.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !re.MatchString(name) {
			continue
		}
		p := join(dir, name)
		if !last && !isDir(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// expandRecursive implements "**": zero or more directory levels. As the final
// component it also yields every file underneath, which is what makes
// "dir/**" list a whole tree.
func expandRecursive(dir string, last bool) []string {
	// The zero-directory case is dir itself. A leading "**" starts from the
	// working directory, which has no name to contribute, so dir stays "" and
	// the paths built on top of it stay relative.
	out := []string{dir}
	return append(out, walk(dir, last)...)
}

// walk collects every descendant of dir, following the same hidden-file rule
// as a wildcard component. includeFiles is false while we are still looking
// for a directory to continue the pattern in.
func walk(dir string, includeFiles bool) []string {
	entries, err := os.ReadDir(dirForRead(dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		p := join(dir, name)
		isD := isDir(p)
		if isD || includeFiles {
			out = append(out, p)
		}
		if isD {
			out = append(out, walk(p, includeFiles)...)
		}
	}
	return out
}

// dirForRead turns the empty prefix into "." so ReadDir has something to open.
func dirForRead(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// translate converts an fnmatch pattern for a single path component into a
// regexp, following fnmatch.translate: "*" and "?" never cross a separator,
// and "[...]" is a character class with "!" as the negation marker.
func translate(pat string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString(`\A`)
	for i := 0; i < len(pat); i++ {
		c := pat[i]
		switch c {
		case '*':
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '[':
			if cls, next, ok := charClass(pat, i); ok {
				b.WriteString(cls)
				i = next
				continue
			}
			b.WriteString(regexp.QuoteMeta("["))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`\z`)
	re, err := regexp.Compile(b.String())
	if err != nil {
		// A class we mis-translated should never make the whole pattern
		// unusable; fall back to matching the component literally.
		return regexp.MustCompile(`\A` + regexp.QuoteMeta(pat) + `\z`)
	}
	return re
}

// charClass parses "[...]" starting at pat[i]. It returns the regexp source,
// the index of the closing bracket, and whether a class was found at all.
func charClass(pat string, i int) (string, int, bool) {
	j := i + 1
	if j < len(pat) && (pat[j] == '!' || pat[j] == '^') {
		j++
	}
	if j < len(pat) && pat[j] == ']' {
		j++
	}
	for j < len(pat) && pat[j] != ']' {
		j++
	}
	if j >= len(pat) {
		return "", i, false
	}

	body := pat[i+1 : j]
	negate := false
	if strings.HasPrefix(body, "!") || strings.HasPrefix(body, "^") {
		negate = true
		body = body[1:]
	}
	// "^" inside a Go class means negation, so it has to be escaped; the rest
	// of the body is already regexp class syntax.
	body = strings.ReplaceAll(body, `\`, `\\`)
	body = strings.ReplaceAll(body, "^", `\^`)
	if negate {
		return "[^" + body + "]", j, true
	}
	return "[" + body + "]", j, true
}
