package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/types"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/types/typeutil"
)

// updateAPI rewrites the snapshots of TestExportedAPI to exactly the API and the implementations that exist now.
var updateAPI = flag.Bool("update-api", false, "rewrite testdata/api/api.txt and testdata/api/impl.txt to the current exported API and interface implementations")

const (
	// apiSnapshot is the path of the snapshot of the exported API, relative to the repository root.
	apiSnapshot = "testdata/api/api.txt"
	// implSnapshot is the path of the snapshot of what implements what, relative to the repository root.
	implSnapshot = "testdata/api/impl.txt"
)

// apiHeader starts the snapshot of the exported API.
const apiHeader = `# The exported API of the module.
#
# Each line is "<package> <kind> <name> <type>", separated by tabs: every exported package-level object of the
# packages below server/ and protocol/ that are not internal, and every exported method of the pointer method
# set of each exported named type, promoted methods included. Packages are written relative to the module.
#
# The API may change, but only deliberately: a change of this file is reviewed like the change of code that
# made it. A pure move of code leaves it identical.
#
# Regenerate it with:
#
#	go test -run TestExportedAPI -update-api .
#
# Blank lines and lines starting with # are ignored. Entries are sorted.
`

// implHeader starts the snapshot of what implements what.
const implHeader = `# What implements what in the module.
#
# Each line is "<type> <interface> <receiver>", separated by tabs: a named concrete type with methods and a
# non-empty named interface of the packages below server/, protocol/ and cmd/, exported or not, that the type
# implements by value ("value") or by pointer only ("*"). Packages are written relative to the module.
#
# A type that loses a method still compiles when the only users of the interface it no longer implements are
# type assertions, such as e.(crawler) in the session. This file is what catches it: the line disappears.
#
# Regenerate it with:
#
#	go test -run TestExportedAPI -update-api .
#
# Blank lines and lines starting with # are ignored. Entries are sorted.
`

// apiPackage writes a package path as the snapshots do: relative to the module, or "." for the root.
func apiPackage(p string) string {
	if p == rootModule {
		return "."
	}
	return strings.TrimPrefix(p, rootModule+"/")
}

// apiSnapshots returns the lines of the snapshots of the exported API and of what implements what, each sorted.
// The module is loaded once for R4, R5 and this (loadModule).
func apiSnapshots() (api, impl []string, err error) {
	m, err := loadModule()
	if err != nil {
		return nil, nil, err
	}
	qual := func(p *types.Package) string { return apiPackage(p.Path()) }
	var msets typeutil.MethodSetCache

	var concretes, ifaces []*types.TypeName
	seen := map[string]bool{}
	for _, p := range m.pkgs {
		if p.Types == nil || !under(p.PkgPath, rootModule) || seen[p.PkgPath] {
			continue
		}
		seen[p.PkgPath] = true
		internal := strings.Contains(p.PkgPath, "/internal") || under(p.PkgPath, rootModule+"/cmd")
		pkg := apiPackage(p.PkgPath)
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if tn, ok := obj.(*types.TypeName); ok && !tn.IsAlias() {
				if it, ok := tn.Type().Underlying().(*types.Interface); ok {
					if it.NumMethods() > 0 {
						ifaces = append(ifaces, tn)
					}
				} else if msets.MethodSet(types.NewPointer(tn.Type())).Len() > 0 {
					concretes = append(concretes, tn)
				}
			}
			if !obj.Exported() || internal {
				continue
			}
			switch obj := obj.(type) {
			case *types.TypeName:
				api = append(api, fmt.Sprintf("%s\ttype\t%s\t%s", pkg, name, types.TypeString(obj.Type().Underlying(), qual)))
				if obj.IsAlias() {
					api = append(api, fmt.Sprintf("%s\talias\t%s\t= %s", pkg, name, types.TypeString(obj.Type(), qual)))
					continue
				}
				if types.IsInterface(obj.Type()) {
					continue
				}
				ms := msets.MethodSet(types.NewPointer(obj.Type()))
				for sel := range ms.Methods() {
					if fn := sel.Obj(); fn.Exported() {
						api = append(api, fmt.Sprintf("%s\tmethod\t%s.%s\t%s", pkg, name, fn.Name(), types.TypeString(fn.Type(), qual)))
					}
				}
			case *types.Func:
				api = append(api, fmt.Sprintf("%s\tfunc\t%s\t%s", pkg, name, types.TypeString(obj.Type(), qual)))
			case *types.Var:
				api = append(api, fmt.Sprintf("%s\tvar\t%s\t%s", pkg, name, types.TypeString(obj.Type(), qual)))
			case *types.Const:
				api = append(api, fmt.Sprintf("%s\tconst\t%s\t%s = %s", pkg, name, types.TypeString(obj.Type(), qual), obj.Val().ExactString()))
			}
		}
	}

	// A pair is checked by types.Implements only if every method name of the interface is in the pointer method
	// set of the type: most pairs fail on the names alone, and comparing names is cheap.
	names := func(ms *types.MethodSet) map[string]bool {
		out := make(map[string]bool, ms.Len())
		for sel := range ms.Methods() {
			out[sel.Obj().Id()] = true
		}
		return out
	}
	ifaceNames := make([][]string, len(ifaces))
	for i, tn := range ifaces {
		it := tn.Type().Underlying().(*types.Interface)
		for fn := range it.Methods() {
			ifaceNames[i] = append(ifaceNames[i], fn.Id())
		}
	}
	for _, t := range concretes {
		ptr := types.NewPointer(t.Type())
		have := names(msets.MethodSet(ptr))
		for i, tn := range ifaces {
			if !allIn(ifaceNames[i], have) {
				continue
			}
			it := tn.Type().Underlying().(*types.Interface)
			byValue, byPointer := types.Implements(t.Type(), it), types.Implements(ptr, it)
			if !byValue && !byPointer {
				continue
			}
			recv := "*"
			if byValue {
				recv = "value"
			}
			impl = append(impl, fmt.Sprintf("%s.%s\t%s.%s\t%s", apiPackage(t.Pkg().Path()), t.Name(), apiPackage(tn.Pkg().Path()), tn.Name(), recv))
		}
	}
	slices.Sort(api)
	slices.Sort(impl)
	return api, impl, nil
}

// allIn reports if every name of names is in set.
func allIn(names []string, set map[string]bool) bool {
	for _, n := range names {
		if !set[n] {
			return false
		}
	}
	return true
}

// readSnapshot returns the entries of a snapshot file: its lines other than blank lines and comments, unchanged.
func readSnapshot(p string) ([]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries, sc.Err()
}

// writeSnapshot writes a header and lines to the file p.
func writeSnapshot(p, header string, lines []string) error {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	if err := os.MkdirAll(path.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

// compareSnapshot reports each line that is in current but not in the snapshot p, and each one the other way
// round, both in order and at most limit of each.
func compareSnapshot(t *testing.T, p string, current []string) {
	t.Helper()
	listed, err := readSnapshot(p)
	if err != nil {
		t.Fatalf("read the snapshot: %v (generate it with: go test -run TestExportedAPI -update-api .)", err)
	}
	const limit = 50
	added, removed := 0, 0
	i, j := 0, 0
	for i < len(current) || j < len(listed) {
		switch {
		case j == len(listed) || (i < len(current) && current[i] < listed[j]):
			if added++; added <= limit {
				t.Errorf("%s: new line, not in the snapshot: %q", p, current[i])
			}
			i++
		case i == len(current) || listed[j] < current[i]:
			if removed++; removed <= limit {
				t.Errorf("%s: line gone, in the snapshot but no longer true: %q", p, listed[j])
			}
			j++
		default:
			i++
			j++
		}
	}
	if added > limit || removed > limit {
		t.Errorf("%s: %d lines new and %d gone in all", p, added, removed)
	}
	if added > 0 || removed > 0 {
		t.Errorf("%s differs: if the change is deliberate, review it and regenerate with: go test -run TestExportedAPI -update-api .", p)
	}
	if !slices.IsSorted(listed) {
		t.Errorf("%s: entries are out of order, regenerate with -update-api", p)
	}
}

// TestExportedAPI compares the exported API of the module and what implements what to their snapshots, so that
// a change of either is a reviewed diff and a pure move of code is shown to change neither.
func TestExportedAPI(t *testing.T) {
	api, impl, err := apiSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if *updateAPI {
		if err := writeSnapshot(apiSnapshot, apiHeader, api); err != nil {
			t.Fatal(err)
		}
		if err := writeSnapshot(implSnapshot, implHeader, impl); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d lines to %s and %d to %s", len(api), apiSnapshot, len(impl), implSnapshot)
		return
	}
	compareSnapshot(t, apiSnapshot, api)
	compareSnapshot(t, implSnapshot, impl)
}
