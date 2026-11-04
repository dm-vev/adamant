package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// updateEntityLayers rewrites the ratchet of TestEntityLayers to exactly what core files of server/entity take
// from its families now.
var updateEntityLayers = flag.Bool("update-entity-layers", false, "rewrite testdata/boundary/entity_layers.txt to the unexported identifiers that core files of server/entity take from its families now")

const (
	// entityGroups is the path of the file group table of server/entity, relative to the repository root.
	entityGroups = "testdata/boundary/entity_groups.txt"
	// entityLayers is the path of the ratchet of TestEntityLayers, relative to the repository root.
	entityLayers = "testdata/boundary/entity_layers.txt"
	// entityPkg is the package whose files are grouped.
	entityPkg = rootModule + "/server/entity"

	// coreGroup is the group of entity files every family may build on: Ent, living, damage, effects, physics,
	// NBT, registration. aiGroup is the AI engine of the mobs. Every other group is a family.
	coreGroup = "core"
	aiGroup   = "ai"
)

// entityLayersHeader starts the ratchet file.
const entityLayersHeader = `# Unexported identifiers of the families of server/entity that its core files still use.
#
# Each line is "<file> <group> <identifier>": a core file of server/entity (see entity_groups.txt), followed by
# the family group that declares an unexported identifier the file uses, and the identifier. A field or a method
# is written Type.name, after the type that declares it. There is one line for each identifier a file uses,
# however often it uses it.
#
# Core is what every family builds on, so it should name no family: a family answers core through an interface
# it implements, and a helper that several families share belongs in a core file. The AI engine is not a family
# here: core and AI still use each other (E4 of the monster split separates them).
#
# This list is a ratchet. It may only shrink: an entry that is no longer used fails the test until it is
# removed, and a new use fails the test. Adding an entry needs a justification in review.
#
# Regenerate it with:
#
#	go test -run TestEntityLayers -update-entity-layers .
#
# Blank lines and lines starting with # are ignored. Entries are sorted.
`

// entityLayerUse is the use of an identifier of one group of server/entity by a file of another.
type entityLayerUse struct {
	file, user, home, name string
	exported               bool
}

func (u entityLayerUse) line() string { return u.file + " " + u.home + " " + u.name }

// readEntityGroups reads the file group table: the group of each file of server/entity, by base name.
func readEntityGroups(p string) (map[string]string, error) {
	entries, err := readAllowlist(p)
	if err != nil {
		return nil, err
	}
	groups := make(map[string]string, len(entries))
	for i, entry := range entries {
		f := strings.Fields(entry)
		if len(f) != 2 {
			return nil, fmt.Errorf("%s: malformed entry %q, want \"<file> <group>\"", p, entry)
		}
		if i > 0 && entries[i-1] > entry {
			return nil, fmt.Errorf("%s: entry %q is out of order", p, entry)
		}
		if _, ok := groups[f[0]]; ok {
			return nil, fmt.Errorf("%s: file %s listed twice", p, f[0])
		}
		groups[f[0]] = f[1]
	}
	return groups, nil
}

// entityPackage returns the package server/entity of the module, without its tests.
func entityPackage(m *loadedModule) (*packages.Package, error) {
	for _, p := range m.pkgs {
		if p.PkgPath == entityPkg {
			return p, nil
		}
	}
	return nil, fmt.Errorf("package %s not loaded", entityPkg)
}

// entityDecls returns the group and the name of each object declared in a file of p, as the ratchet writes it:
// a package-level object by its name, and a field or method by Type.name. Objects local to a function are left
// out, since no other file can use them.
func entityDecls(p *packages.Package, groupOf func(file *ast.File) string) map[types.Object][2]string {
	decls := map[types.Object][2]string{}
	def := func(id *ast.Ident, g, name string) {
		if o := p.TypesInfo.Defs[id]; o != nil {
			decls[o] = [2]string{g, name}
		}
	}
	for _, f := range p.Syntax {
		g := groupOf(f)
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				def(d.Name, g, funcDeclName(d))
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						def(spec.Name, g, spec.Name.Name)
						// Fields and interface methods, nested ones included, are named after the type.
						ast.Inspect(spec.Type, func(n ast.Node) bool {
							if field, ok := n.(*ast.Field); ok {
								for _, id := range field.Names {
									def(id, g, spec.Name.Name+"."+id.Name)
								}
								if id := embeddedFieldIdent(field); id != nil {
									def(id, g, spec.Name.Name+"."+id.Name)
								}
							}
							return true
						})
					case *ast.ValueSpec:
						for _, id := range spec.Names {
							def(id, g, id.Name)
						}
					}
				}
			}
		}
	}
	return decls
}

// embeddedFieldIdent returns the identifier that names an embedded field, or nil if the field has names.
func embeddedFieldIdent(field *ast.Field) *ast.Ident {
	if len(field.Names) > 0 {
		return nil
	}
	e := field.Type
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.SelectorExpr:
			return t.Sel
		case *ast.Ident:
			return t
		default:
			return nil
		}
	}
}

// entityLayerUses returns every use, by a file of server/entity, of an identifier that a file of another group
// declares, once for each file and identifier, sorted. It also returns the package's files by base name.
func entityLayerUses(groups map[string]string) (uses []entityLayerUse, files []string, err error) {
	m, err := loadModule()
	if err != nil {
		return nil, nil, err
	}
	p, err := entityPackage(m)
	if err != nil {
		return nil, nil, err
	}
	base := func(f *ast.File) string { return filepath.Base(p.Fset.Position(f.Pos()).Filename) }
	for _, f := range p.Syntax {
		files = append(files, base(f))
	}
	slices.Sort(files)
	decls := entityDecls(p, func(f *ast.File) string { return groups[base(f)] })

	seen := map[string]bool{}
	for _, f := range p.Syntax {
		file := base(f)
		user := groups[file]
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			o := p.TypesInfo.Uses[id]
			d, ok := decls[o]
			if !ok || d[0] == user {
				return true
			}
			u := entityLayerUse{file: file, user: user, home: d[0], name: d[1], exported: o.Exported()}
			if !seen[u.line()] {
				seen[u.line()] = true
				uses = append(uses, u)
			}
			return true
		})
	}
	slices.SortFunc(uses, func(a, b entityLayerUse) int { return strings.Compare(a.line(), b.line()) })
	return uses, files, nil
}

// coreTakesFromFamily reports if a use is one the ratchet holds: a core file using an unexported identifier of a
// family.
func coreTakesFromFamily(u entityLayerUse) bool {
	return u.user == coreGroup && u.home != coreGroup && u.home != aiGroup && !u.exported
}

// entityLayerMatrix writes, for each pair of groups, how many distinct unexported identifiers the first takes
// from the second, as comment lines.
func entityLayerMatrix(uses []entityLayerUse) string {
	pairs := map[[2]string]map[string]bool{}
	for _, u := range uses {
		if u.exported {
			continue
		}
		k := [2]string{u.user, u.home}
		if pairs[k] == nil {
			pairs[k] = map[string]bool{}
		}
		pairs[k][u.home+" "+u.name] = true
	}
	var keys [][2]string
	for k := range pairs {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b [2]string) int { return strings.Compare(a[0]+" "+a[1], b[0]+" "+b[1]) })
	var b strings.Builder
	b.WriteString("# Distinct unexported identifiers that each group takes from each other, as of the last regeneration.\n")
	b.WriteString("# They are recorded here, not checked: only the lines above are.\n#\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "#\t%-10s from %-10s %4d\n", k[0], k[1], len(pairs[k]))
	}
	return b.String()
}

// TestEntityLayers holds the core files of server/entity to the unexported identifiers of its families that
// they use today, so that core can only stop naming families, until it names none.
func TestEntityLayers(t *testing.T) {
	groups, err := readEntityGroups(entityGroups)
	if err != nil {
		t.Fatal(err)
	}
	uses, files, err := entityLayerUses(groups)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if _, ok := groups[f]; !ok {
			t.Errorf("server/entity/%s has no group: add it to %s with the group of the family it belongs to, or core", f, entityGroups)
		}
	}
	for f := range groups {
		if !slices.Contains(files, f) {
			t.Errorf("%s lists %s, which server/entity no longer has: remove it", entityGroups, f)
		}
	}
	if t.Failed() {
		return
	}
	var current []string
	distinct := map[string]bool{}
	for _, u := range uses {
		if coreTakesFromFamily(u) {
			current = append(current, u.line())
			distinct[u.home+" "+u.name] = true
		}
	}

	if *updateEntityLayers {
		var b strings.Builder
		b.WriteString(entityLayersHeader)
		b.WriteString("\n")
		for _, l := range current {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
		b.WriteString(entityLayerMatrix(uses))
		if err := os.MkdirAll(path.Dir(entityLayers), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(entityLayers, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d uses of %d distinct identifiers to %s", len(current), len(distinct), entityLayers)
		return
	}

	listed, err := readAllowlist(entityLayers)
	if err != nil {
		t.Fatalf("read the ratchet: %v (generate it with: go test -run TestEntityLayers -update-entity-layers .)", err)
	}
	listedSet := map[string]bool{}
	for i, entry := range listed {
		if len(strings.Fields(entry)) != 3 {
			t.Errorf("%s: malformed entry %q, want \"<file> <group> <identifier>\"", entityLayers, entry)
		}
		if listedSet[entry] {
			t.Errorf("%s: duplicate entry %q", entityLayers, entry)
		}
		if i > 0 && listed[i-1] > entry {
			t.Errorf("%s: entry %q is out of order, regenerate with -update-entity-layers", entityLayers, entry)
		}
		listedSet[entry] = true
	}
	currentSet := map[string]bool{}
	for _, l := range current {
		currentSet[l] = true
		if !listedSet[l] {
			file, rest, _ := strings.Cut(l, " ")
			home, name, _ := strings.Cut(rest, " ")
			t.Errorf("server/entity/%s (core) uses %s of the %s family: move what several families share into a core file, or let the family answer core through an interface", file, name, home)
		}
	}
	for _, entry := range listed {
		if !currentSet[entry] {
			t.Errorf("stale ratchet entry %q: the core file no longer uses it, remove it from %s (-update-entity-layers)", entry, entityLayers)
		}
	}
}
