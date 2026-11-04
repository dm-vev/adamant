package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// updateLongFuncs rewrites the ratchet of TestLongFunctions to exactly the long functions that exist now.
var updateLongFuncs = flag.Bool("update-longfuncs", false, "rewrite testdata/boundary/longfuncs.txt to the current functions over 100 lines")

const (
	// longFuncRatchet is the path of the ratchet of long functions, relative to the repository root.
	longFuncRatchet = "testdata/boundary/longfuncs.txt"
	// longFuncLines is the length above which a function is long.
	longFuncLines = 100
)

// longFuncHeader starts the ratchet file.
const longFuncHeader = `# Hand-written functions over 100 lines that still exist.
#
# Each line is "<file> <func> <lines> [tag]": a file below server/, protocol/ or cmd/ that is not a test and not
# generated, relative to the repository root and written with slashes, the function or method (as Recv.Name)
# declared in it, and its length in lines from "func" to the closing brace. A name declared more than once in a
# file, as init is, takes "#2", "#3", ... after the first, in the order of the file.
#
# The tag says why a function may stay long:
#
#	port	a port of Java generation under server/world/generator, which mirrors a Java method one to one and
#		whose draw order the generation hash pins
#	tool	a command under cmd/, which the server does not run
#
# This list is a ratchet. No function over 100 lines may be added and none may grow: either fails the test.
# A function without a tag that shrank fails until its entry is lowered, and one that went or fell to 100 lines
# or fewer fails until its entry is removed. A tagged function need not shrink: its length is a ceiling.
# Adding an entry, or raising one, needs a justification in review.
#
# Regenerate it with (the tags of entries that stay are kept, and new ones are tagged by their directory):
#
#	go test -run TestLongFunctions -update-longfuncs .
#
# Blank lines and lines starting with # are ignored. Entries are sorted.
`

// longFunc is a function over longFuncLines lines, or an entry of the ratchet.
type longFunc struct {
	file, name string
	lines      int
	tag        string
}

func (f longFunc) key() string { return f.file + " " + f.name }

func (f longFunc) line() string {
	s := fmt.Sprintf("%s %s %d", f.file, f.name, f.lines)
	if f.tag != "" {
		s += " " + f.tag
	}
	return s
}

// longFuncTag returns the tag a new entry for a file takes: port for the generator, tool for commands.
func longFuncTag(rel string) string {
	switch {
	case under(rel, "server/world/generator"):
		return "port"
	case under(rel, "cmd"):
		return "tool"
	}
	return ""
}

// funcDeclName writes a function declaration as the ratchet names it: Name, or Recv.Name for a method.
func funcDeclName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	e := d.Recv.List[0].Type
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.Ident:
			return t.Name + "." + d.Name.Name
		default:
			return "?." + d.Name.Name
		}
	}
}

// longFuncs returns the hand-written functions of the module over longFuncLines lines, sorted. The module is
// loaded once for R4, R5 and this (loadModule).
func longFuncs() ([]longFunc, error) {
	m, err := loadModule()
	if err != nil {
		return nil, err
	}
	var out []longFunc
	seen := map[string]bool{}
	err = m.files(func(rel string, f *ast.File, p *packages.Package) error {
		// A file of a package loaded twice, as a test variant, is counted once.
		if seen[rel] || ast.IsGenerated(f) {
			return nil
		}
		seen[rel] = true
		out = append(out, longFuncsOf(rel, f, p.Fset)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b longFunc) int { return strings.Compare(a.key(), b.key()) })
	return out, nil
}

// longFuncsOf returns the functions over longFuncLines lines declared in the file rel, in the order of the file.
func longFuncsOf(rel string, f *ast.File, fset *token.FileSet) []longFunc {
	var out []longFunc
	count := map[string]int{}
	for _, decl := range f.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := funcDeclName(d)
		if count[name]++; count[name] > 1 {
			name += "#" + strconv.Itoa(count[name])
		}
		lines := fset.PositionFor(d.End(), false).Line - fset.PositionFor(d.Pos(), false).Line + 1
		if lines > longFuncLines {
			out = append(out, longFunc{file: rel, name: name, lines: lines, tag: longFuncTag(rel)})
		}
	}
	return out
}

// parseLongFunc parses an entry of the ratchet.
func parseLongFunc(entry string) (longFunc, error) {
	fields := strings.Fields(entry)
	if len(fields) != 3 && len(fields) != 4 {
		return longFunc{}, fmt.Errorf("malformed entry %q, want \"<file> <func> <lines> [tag]\"", entry)
	}
	n, err := strconv.Atoi(fields[2])
	if err != nil || n <= 0 {
		return longFunc{}, fmt.Errorf("malformed entry %q: %q is not a number of lines", entry, fields[2])
	}
	f := longFunc{file: fields[0], name: fields[1], lines: n}
	if len(fields) == 4 {
		f.tag = fields[3]
		if f.tag != "port" && f.tag != "tool" {
			return longFunc{}, fmt.Errorf("malformed entry %q: unknown tag %q, want port or tool", entry, f.tag)
		}
	}
	return f, nil
}

func TestLongFunctions(t *testing.T) {
	current, err := longFuncs()
	if err != nil {
		t.Fatal(err)
	}

	var listed []longFunc
	entries, err := readAllowlist(longFuncRatchet)
	if err != nil && !*updateLongFuncs {
		t.Fatalf("read the ratchet: %v (generate it with: go test -run TestLongFunctions -update-longfuncs .)", err)
	}
	for _, entry := range entries {
		f, err := parseLongFunc(entry)
		if err != nil {
			t.Errorf("%s: %v", longFuncRatchet, err)
			continue
		}
		if n := len(listed); n > 0 && listed[n-1].key() >= f.key() {
			t.Errorf("%s: entry %q is out of order or repeated, regenerate with -update-longfuncs", longFuncRatchet, entry)
		}
		listed = append(listed, f)
	}
	byKey := map[string]longFunc{}
	for _, f := range listed {
		byKey[f.key()] = f
	}

	if *updateLongFuncs {
		lines := make([]string, 0, len(current))
		for _, f := range current {
			if old, ok := byKey[f.key()]; ok {
				f.tag = old.tag
			}
			lines = append(lines, f.line())
		}
		if err := writeSnapshot(longFuncRatchet, longFuncHeader, lines); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d functions to %s", len(current), longFuncRatchet)
		return
	}

	for _, problem := range longFuncProblems(current, listed) {
		t.Error(problem)
	}
}

// longFuncProblems compares the long functions of the module, current, to the entries of the ratchet, listed,
// and describes each difference that fails the test.
func longFuncProblems(current, listed []longFunc) []string {
	byKey := map[string]longFunc{}
	for _, f := range listed {
		byKey[f.key()] = f
	}
	var out []string
	found := map[string]bool{}
	for _, f := range current {
		found[f.key()] = true
		old, ok := byKey[f.key()]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s %s has %d lines, over %d: split it into functions of its steps (a new entry in %s needs a justification in review)", f.file, f.name, f.lines, longFuncLines, longFuncRatchet))
		case f.lines > old.lines:
			out = append(out, fmt.Sprintf("%s %s grew to %d lines from %d: keep it at %d or fewer (raising its entry in %s needs a justification in review)", f.file, f.name, f.lines, old.lines, old.lines, longFuncRatchet))
		case f.lines < old.lines && old.tag == "":
			out = append(out, fmt.Sprintf("%s %s shrank to %d lines from %d: lower its entry in %s to %d", f.file, f.name, f.lines, old.lines, longFuncRatchet, f.lines))
		}
	}
	for _, old := range listed {
		if !found[old.key()] {
			out = append(out, fmt.Sprintf("stale ratchet entry %q: the function went or has %d lines or fewer, remove it from %s", old.line(), longFuncLines, longFuncRatchet))
		}
	}
	return out
}

// longFuncFixture declares functions of each kind TestLongFunctionRules names; body is replaced by n lines.
const longFuncFixture = `package x

func short() {
	body(10)
}

func init() {
	body(101)
}

func init() {
	body(200)
}

type set[T any] struct{}

func (s *set[T]) Add() {
	body(99)
}

func (s set[T]) Remove() {
	body(98)
}
`

// TestLongFunctionRules checks how functions are named and measured, and what the ratchet makes of each change.
func TestLongFunctionRules(t *testing.T) {
	var src strings.Builder
	for line := range strings.Lines(longFuncFixture) {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "body(%d)", &n); err == nil {
			src.WriteString(strings.Repeat("\t_ = 0\n", n))
			continue
		}
		src.WriteString(line)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "server/x/x.go", src.String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, fn := range longFuncsOf("server/x/x.go", f, fset) {
		got = append(got, fn.line())
	}
	// A function of n body lines has n+2 lines; set.Add has 101 and set.Remove 100, which is not over.
	want := []string{"server/x/x.go init 103", "server/x/x.go init#2 202", "server/x/x.go set.Add 101"}
	if !slices.Equal(got, want) {
		t.Errorf("long functions of the fixture: %q, want %q", got, want)
	}
	if got := longFuncTag("server/world/generator/advanced/overworld/x.go"); got != "port" {
		t.Errorf("tag of the generator: %q, want port", got)
	}
	if got := longFuncTag("cmd/loadtest/bot.go"); got != "tool" {
		t.Errorf("tag of a command: %q, want tool", got)
	}
	if got := longFuncTag("server/world/generatorx/x.go"); got != "" {
		t.Errorf("tag of server/world/generatorx: %q, want none", got)
	}

	fn := func(name string, lines int, tag string) longFunc {
		return longFunc{file: "server/x/x.go", name: name, lines: lines, tag: tag}
	}
	for _, tc := range []struct {
		name            string
		current, listed []longFunc
		fails           bool
	}{
		{"unchanged", []longFunc{fn("a", 120, "")}, []longFunc{fn("a", 120, "")}, false},
		{"new", []longFunc{fn("a", 101, "")}, nil, true},
		{"new port", []longFunc{fn("a", 101, "port")}, nil, true},
		{"grew", []longFunc{fn("a", 121, "")}, []longFunc{fn("a", 120, "")}, true},
		{"port grew", []longFunc{fn("a", 121, "port")}, []longFunc{fn("a", 120, "port")}, true},
		{"shrank", []longFunc{fn("a", 119, "")}, []longFunc{fn("a", 120, "")}, true},
		{"port shrank", []longFunc{fn("a", 119, "port")}, []longFunc{fn("a", 120, "port")}, false},
		{"tool shrank", []longFunc{fn("a", 119, "tool")}, []longFunc{fn("a", 120, "tool")}, false},
		{"went", nil, []longFunc{fn("a", 120, "")}, true},
		{"port went", nil, []longFunc{fn("a", 120, "port")}, true},
	} {
		if got := len(longFuncProblems(tc.current, tc.listed)) > 0; got != tc.fails {
			t.Errorf("%s: fails = %v, want %v", tc.name, got, tc.fails)
		}
	}

	for _, entry := range []string{"server/x/x.go a", "server/x/x.go a b", "server/x/x.go a 0", "server/x/x.go a 120 hot", "server/x/x.go a 120 port x"} {
		if _, err := parseLongFunc(entry); err == nil {
			t.Errorf("parseLongFunc(%q) accepted a malformed entry", entry)
		}
	}
	if got, err := parseLongFunc("server/x/x.go a 120 port"); err != nil || got != fn("a", 120, "port") {
		t.Errorf("parseLongFunc: %+v, %v, want %+v", got, err, fn("a", 120, "port"))
	}
}
