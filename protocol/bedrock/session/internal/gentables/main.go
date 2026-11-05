// Command gentables writes the type switches that the tables of the session
// hold the literal cases of: of the sounds the game plays (sound_table.go)
// and of the particles it shows (particle_table.go).
//
// A table lists, as composite literals of plain strings and numbers, the
// types of server/world/sound or server/world/particle whose packets are the
// same whatever the value of the type is, bar a field a row names. This
// command reads the rows from the Go file of the table, checks every row
// against the types of the package it names, and writes:
//
//   - the file of the switch (sound_switch_gen.go, particle_switch_gen.go): a
//     method of Session that switches over the type of the value, writes its
//     packets, and reports whether the type was one of the table's;
//   - the test file of the types (sound_types_gen_test.go,
//     particle_types_gen_test.go): the zero value of every type of the package
//     that implements the interface of the game, taken from go/types, so that
//     no type is forgotten by the tests that compare the packets of every
//     type.
//
// It runs in the directory of the session, from the go:generate directives of
// the tables.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"log"
	"os"
	"slices"
	"strconv"

	"golang.org/x/tools/go/packages"
)

// row is a row of a table: the fields it names, by the names of the table,
// with strings unquoted and other values as written.
type row map[string]string

func main() {
	kind := flag.String("kind", "", "the kind of table: sound or particle")
	flag.Parse()
	var k kindSpec
	switch *kind {
	case "sound":
		k = soundKind
	case "particle":
		k = particleKind
	default:
		log.Fatalf("gentables: unknown kind %q", *kind)
	}
	log.SetFlags(0)
	log.SetPrefix("gentables: ")

	rows, err := readRows(k.table, k.variable)
	if err != nil {
		log.Fatal(err)
	}
	types, err := gameTypes(k)
	if err != nil {
		log.Fatal(err)
	}
	if err := check(k, rows, types); err != nil {
		log.Fatal(err)
	}
	if err := writeFile(k.out, k.generate(rows)); err != nil {
		log.Fatal(err)
	}
	if err := writeFile(k.testOut, k.generateTypes(types)); err != nil {
		log.Fatal(err)
	}
}

// readRows reads the rows of the composite literal of the variable passed in
// the Go file passed, in the order they are in.
func readRows(file, variable string) ([]row, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil, err
	}
	var lit *ast.CompositeLit
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, s := range g.Specs {
			v, ok := s.(*ast.ValueSpec)
			if !ok || len(v.Names) != 1 || v.Names[0].Name != variable || len(v.Values) != 1 {
				continue
			}
			lit, _ = v.Values[0].(*ast.CompositeLit)
		}
	}
	if lit == nil {
		return nil, fmt.Errorf("%s: no composite literal for %s", file, variable)
	}
	var rows []row
	for _, e := range lit.Elts {
		c, ok := e.(*ast.CompositeLit)
		if !ok {
			return nil, fmt.Errorf("%s: row at %v is not a composite literal", file, fset.Position(e.Pos()))
		}
		r := row{}
		for _, kv := range c.Elts {
			kve, ok := kv.(*ast.KeyValueExpr)
			if !ok {
				return nil, fmt.Errorf("%s: field at %v has no name", file, fset.Position(kv.Pos()))
			}
			key := kve.Key.(*ast.Ident).Name
			switch v := kve.Value.(type) {
			case *ast.BasicLit:
				if v.Kind == token.STRING {
					s, err := strconv.Unquote(v.Value)
					if err != nil {
						return nil, err
					}
					r[key] = s
				} else {
					r[key] = v.Value
				}
			case *ast.Ident:
				r[key] = v.Name
			default:
				return nil, fmt.Errorf("%s: field %s at %v is not a literal", file, key, fset.Position(kv.Pos()))
			}
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// gameTypes returns the names of the types of the package of the kind that
// implement the interface of the game, sorted.
func gameTypes(k kindSpec) ([]string, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps}
	pkgs, err := packages.Load(cfg, k.worldPkg, k.pkg)
	if err != nil {
		return nil, err
	}
	var world, game *packages.Package
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			return nil, p.Errors[0]
		}
		switch p.PkgPath {
		case k.worldPkg:
			world = p
		case k.pkg:
			game = p
		}
	}
	if world == nil || game == nil {
		return nil, fmt.Errorf("packages %s and %s not both loaded", k.worldPkg, k.pkg)
	}
	obj := world.Types.Scope().Lookup(k.iface)
	if obj == nil {
		return nil, fmt.Errorf("%s has no %s", k.worldPkg, k.iface)
	}
	iface, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return nil, fmt.Errorf("%s.%s is not an interface", k.worldPkg, k.iface)
	}
	var names []string
	for _, name := range game.Types.Scope().Names() {
		tn, ok := game.Types.Scope().Lookup(name).(*types.TypeName)
		if !ok || !tn.Exported() || tn.IsAlias() {
			continue
		}
		if _, isIface := tn.Type().Underlying().(*types.Interface); isIface {
			continue
		}
		if types.Implements(tn.Type(), iface) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// check reports a row naming a type that is not one of the game's, a type with
// two rows, and a field the kind does not know.
func check(k kindSpec, rows []row, names []string) error {
	seen := map[string]bool{}
	for _, r := range rows {
		name := r[k.key]
		if !slices.Contains(names, name) {
			return fmt.Errorf("%s: row for %q, which is no type of %s that implements %s", k.table, name, k.pkg, k.iface)
		}
		if seen[name] {
			return fmt.Errorf("%s: two rows for %q", k.table, name)
		}
		seen[name] = true
		for field := range r {
			if !slices.Contains(k.fields, field) {
				return fmt.Errorf("%s: row for %q has the unknown field %s", k.table, name, field)
			}
		}
	}
	return nil
}

// writeFile formats the source passed and writes it to the file passed.
func writeFile(file string, src []byte) error {
	out, err := format.Source(src)
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", file, err, src)
	}
	return os.WriteFile(file, out, 0o644)
}

// kindSpec holds what differs between the tables.
type kindSpec struct {
	// table is the Go file with the rows, variable the name of the slice of
	// them, key the field naming the type of a row and fields every field a
	// row may have.
	table, variable, key string
	fields               []string
	// pkg is the package of the types, worldPkg the one of the interface they
	// implement and iface its name.
	pkg, worldPkg, iface string
	// out and testOut are the files written.
	out, testOut string
	// generate returns the source of the switch and generateTypes that of the
	// test file listing the types.
	generate      func(rows []row) []byte
	generateTypes func(names []string) []byte
}

// header is the comment of a generated file.
const header = "// Code generated by internal/gentables; DO NOT EDIT.\n\n"

// typesFile returns the source of a test file of package session_test that
// holds the zero value of each type passed, in the variable passed.
func typesFile(variable, purpose, pkgName, importPath, iface string, names []string) []byte {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("package session_test\n\nimport (\n")
	b.WriteString("\t\"github.com/df-mc/dragonfly/server/world\"\n")
	fmt.Fprintf(&b, "\t%q\n)\n\n", importPath)
	fmt.Fprintf(&b, "// %s holds the zero value of every type of the package %s\n// that implements world.%s, %s\nvar %s = []world.%s{\n", variable, pkgName, iface, purpose, variable, iface)
	for _, n := range names {
		fmt.Fprintf(&b, "\t%s.%s{},\n", pkgName, n)
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// has reports whether the row names the field.
func (r row) has(field string) bool { _, ok := r[field]; return ok }
