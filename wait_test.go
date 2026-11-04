package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// updateWaits rewrites the ratchet of TestOwnersDoNotWait to exactly the waits that exist now.
var updateWaits = flag.Bool("update-waits", false, "rewrite testdata/boundary/waits.txt to the current waits for world owners inside transactions")

// waitRatchet is the path of the ratchet of R5, relative to the repository root.
const waitRatchet = "testdata/boundary/waits.txt"

// waitHeader starts the ratchet file.
const waitHeader = `# Waits for a world owner inside a transaction that still exist (R5).
#
# Each line is "<file> <callee>": a file below server/, protocol/ or cmd/, relative to the repository root and
# written with slashes, followed by the function it calls with a transaction in scope that waits for a world
# owner (see wait_test.go). The owner it waits for may be the one running the transaction, or one waiting for
# it: either deadlocks, and any other stalls the world of the transaction. There is one line for each use.
#
# This list is a ratchet. It may only shrink: an entry that is no longer used fails the test until it is
# removed. Adding an entry needs a justification in review.
#
# Regenerate it with:
#
#	go test -run TestOwnersDoNotWait -update-waits .
#
# Blank lines and lines starting with # are ignored. Entries are sorted.
`

// modulePatterns are the packages the rules of mutation_test.go and wait_test.go load: the game, the adapters
// and the tools. R4 looks at the adapters among them only.
var modulePatterns = []string{"./server/...", "./protocol/...", "./cmd/..."}

// loadedModule is the module, type-checked from source once for every rule that needs it.
type loadedModule struct {
	// abs is the absolute path of the repository root.
	abs string
	// pkgs are the packages of modulePatterns.
	pkgs []*packages.Package
	// byPath holds pkgs and every package they import, directly or not, by path.
	byPath map[string]*types.Package
}

var (
	moduleOnce sync.Once
	module     *loadedModule
	moduleErr  error
)

// loadModule loads the packages of modulePatterns of the repository, once per test binary. Loading them from
// source takes seconds; R4 and R5 share it.
func loadModule() (*loadedModule, error) {
	moduleOnce.Do(func() {
		abs, err := filepath.Abs(".")
		if err != nil {
			moduleErr = err
			return
		}
		var patterns []string
		for _, p := range modulePatterns {
			if _, err := os.Stat(filepath.Join(abs, strings.TrimSuffix(p, "/..."))); err == nil {
				patterns = append(patterns, p)
			}
		}
		conf := &packages.Config{
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
			Dir:  abs,
		}
		pkgs, err := packages.Load(conf, patterns...)
		if err != nil {
			moduleErr = err
			return
		}
		var errs []string
		var roots []*types.Package
		for _, p := range pkgs {
			for _, e := range p.Errors {
				errs = append(errs, e.Error())
			}
			roots = append(roots, p.Types)
		}
		if len(errs) > 0 {
			moduleErr = fmt.Errorf("load the module: %s", strings.Join(errs, "; "))
			return
		}
		module = &loadedModule{abs: abs, pkgs: pkgs, byPath: importedPackages(roots)}
	})
	return module, moduleErr
}

// files calls f with every file of the packages loaded that is not a test, with its path relative to the
// repository root, written with slashes.
func (m *loadedModule) files(f func(rel string, file *ast.File, p *packages.Package) error) error {
	for _, p := range m.pkgs {
		for _, file := range p.Syntax {
			rel, err := filepath.Rel(m.abs, p.Fset.Position(file.Pos()).Filename)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			if err := f(rel, file, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// Names of R5, as types.Func.FullName writes them.
const (
	playerPkg = rootModule + "/server/player"
	eventPkg  = rootModule + "/server/event"

	worldExec      = "(*" + worldPkg + ".World).Exec"
	worldExecOwner = "(*" + worldPkg + ".World).exec"
)

// waitPrimitives are the functions that wait for a world owner: that of a world, of an entity or of a task.
// Receiving from what World.Exec returns is one too, found by the syntax of the receive. Every function that
// calls one of them without a transaction in scope waits as well (R5.2).
var waitPrimitives = map[string]bool{
	worldPkg + ".Call":                      true,
	worldPkg + ".CallEntity":                true,
	worldPkg + ".CallRef":                   true,
	"(*" + worldPkg + ".Task).Wait":         true,
	"(*" + worldPkg + ".World).Save":        true,
	"(*" + worldPkg + ".World).Close":       true,
	"(*" + worldPkg + ".World).AdvanceTick": true,
	worldPkg + ".CaptureAll":                true,
	worldPkg + ".HoldTransfers":             true,
	playerPkg + ".Call":                     true,
}

// waitSchedulers are the functions whose function arguments run later, in their own transaction if they take
// one, or on a goroutine of their own: a literal passed to them does not run in the transaction around it, and
// what it waits for is not waited for by the function that passes it.
var waitSchedulers = map[string]bool{
	"(*" + worldPkg + ".World).Do":             true,
	"(*" + worldPkg + ".World).DoAfter":        true,
	"(*" + worldPkg + ".EntityHandle).Do":      true,
	"(*" + worldPkg + ".EntityHandle).DoAfter": true,
	"(" + worldPkg + ".EntityRef[T]).Do":       true,
	"(" + worldPkg + ".EntityRef[T]).DoAfter":  true,
	"(*" + worldPkg + ".Tx).Defer":             true,
	"(*" + worldPkg + ".Tx).DeferErr":          true,
	"(*" + worldPkg + ".Task).OnDone":          true,
	playerPkg + ".Do":                          true,
	playerPkg + ".DoAfter":                     true,
	"(*" + playerPkg + ".Player).Do":           true,
	"(*" + playerPkg + ".Player).DoAfter":      true,
	"(*" + playerPkg + ".Context).Defer":       true,
	"(*" + playerPkg + ".Context).DeferErr":    true,
	"time.AfterFunc":                           true,
}

// waitHints holds a one-line hint for each rule of R5.
var waitHints = map[string]string{
	"R5.1": "the transaction waits for a world owner, which may be its own or one waiting for it: read what the server knows (Server.Online), schedule with Do and act in the callback, or run the wait on a goroutine",
	"R5.2": "the transaction calls a function that waits for a world owner: give it the transaction, or schedule the work with Do",
}

// waitUse is a wait for a world owner inside a transaction: the line of the ratchet and the rule it breaks.
type waitUse struct {
	file, callee, rule string
	pos                token.Position
}

func (u waitUse) line() string { return u.file + " " + u.callee }

// waitCall is a call made in the body of a function.
type waitCall struct {
	// callee is the full name of the function called, or "<-" and that of World.Exec for a receive from it.
	callee string
	pos    token.Pos
	// tx says whether a transaction is in scope where the call is made.
	tx bool
	// summary says whether the call is made by the function itself as it runs, rather than on a goroutine or
	// in a callback it schedules: it counts toward whether the function waits.
	summary bool
	// static says whether the callee is known: a function or a concrete method.
	static bool
}

// waitFunc is a function declared in a file checked, and the calls of its body.
type waitFunc struct {
	key   string
	tx    bool
	rel   string
	fset  *token.FileSet
	calls []waitCall
}

// waitChecker finds the waits for a world owner inside transactions.
type waitChecker struct {
	// entity is world.Entity: a value of a concrete type implementing it is only valid inside its transaction.
	entity *types.Interface
	funcs  []*waitFunc
}

// newWaitChecker returns a checker that knows world.Entity if the world package is among those imported.
func newWaitChecker(imported func(path string) *types.Package) *waitChecker {
	c := &waitChecker{}
	if p := imported(worldPkg); p != nil {
		if obj, ok := p.Scope().Lookup("Entity").(*types.TypeName); ok {
			c.entity, _ = obj.Type().Underlying().(*types.Interface)
		}
	}
	return c
}

// bound reports whether a value of t is only valid inside a transaction: a *world.Tx, the context of an event
// (*world.Context, *player.Context, *event.Context[T]), or a value of a concrete type implementing world.Entity.
func (c *waitChecker) bound(t types.Type) bool {
	if isTx(t) {
		return true
	}
	if ptr, ok := t.(*types.Pointer); ok {
		if n, ok := ptr.Elem().(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Name() == "Context" {
			switch n.Obj().Pkg().Path() {
			case worldPkg, playerPkg, eventPkg:
				return true
			}
		}
	}
	if c.entity != nil {
		if _, iface := t.Underlying().(*types.Interface); !iface && types.Implements(t, c.entity) {
			return true
		}
	}
	return false
}

// inTx reports whether a function of the signature passed has a transaction in scope: a parameter or receiver
// of a bound type.
func (c *waitChecker) inTx(sig *types.Signature) bool {
	if sig == nil {
		return false
	}
	if r := sig.Recv(); r != nil && c.bound(r.Type()) {
		return true
	}
	for v := range sig.Params().Variables() {
		if c.bound(v.Type()) {
			return true
		}
	}
	return false
}

// add records the functions declared in a file, rel, type-checked with info.
func (c *waitChecker) add(rel string, fset *token.FileSet, f *ast.File, info *types.Info) {
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		obj, ok := info.Defs[fd.Name].(*types.Func)
		if !ok {
			continue
		}
		fn := &waitFunc{key: obj.Origin().FullName(), tx: c.inTx(obj.Type().(*types.Signature)), rel: rel, fset: fset}
		c.walk(info, fd.Body, fn.tx, true, map[types.Object]bool{}, func(call waitCall) { fn.calls = append(fn.calls, call) })
		c.funcs = append(c.funcs, fn)
	}
}

// litTx reports whether a function literal takes a transaction of its own.
func (c *waitChecker) litTx(info *types.Info, lit *ast.FuncLit) bool {
	sig, _ := info.TypeOf(lit).(*types.Signature)
	return c.inTx(sig)
}

// execCall reports whether e is a call of World.Exec (or of exec, inside the world package).
func execCall(info *types.Info, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	if !ok {
		return false
	}
	name := fn.Origin().FullName()
	return name == worldExec || name == worldExecOwner
}

// walk records the calls of n. tx says whether a transaction is in scope and summary whether the calls are
// made by the function as it runs. execs holds the variables assigned what World.Exec returns, so that a
// receive from one is a wait.
//
// A literal inherits the scope of the code around it, except the operand of a go statement, which runs on a
// goroutine of its own, and a literal passed to one of waitSchedulers, which runs later, in a transaction of its
// own if it takes one. Neither counts toward the function around it.
func (c *waitChecker) walk(info *types.Info, n ast.Node, tx, summary bool, execs map[types.Object]bool, rec func(waitCall)) {
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GoStmt:
			call := n.Call
			switch fun := ast.Unparen(call.Fun).(type) {
			case *ast.FuncLit:
				c.walk(info, fun.Body, c.litTx(info, fun), false, execs, rec)
			case *ast.SelectorExpr:
				// The receiver is evaluated here; the call runs on the goroutine.
				c.walk(info, fun.X, tx, summary, execs, rec)
			}
			for _, a := range call.Args {
				c.walk(info, a, tx, summary, execs, rec)
			}
			return false
		case *ast.FuncLit:
			c.walk(info, n.Body, tx || c.litTx(info, n), summary, execs, rec)
			return false
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i, rhs := range n.Rhs {
					if id, ok := n.Lhs[i].(*ast.Ident); ok && execCall(info, rhs) {
						if obj := info.ObjectOf(id); obj != nil {
							execs[obj] = true
						}
					}
				}
			}
		case *ast.ValueSpec:
			if len(n.Names) == len(n.Values) {
				for i, v := range n.Values {
					if execCall(info, v) {
						if obj := info.ObjectOf(n.Names[i]); obj != nil {
							execs[obj] = true
						}
					}
				}
			}
		case *ast.UnaryExpr:
			if n.Op != token.ARROW {
				return true
			}
			recv := execCall(info, n.X)
			if id, ok := ast.Unparen(n.X).(*ast.Ident); ok && execs[info.ObjectOf(id)] {
				recv = true
			}
			if recv {
				rec(waitCall{callee: "<-" + worldExec, pos: n.Pos(), tx: tx, summary: summary, static: true})
			}
		case *ast.CallExpr:
			fn, ok := typeutil.Callee(info, n).(*types.Func)
			if !ok {
				return true
			}
			fn = fn.Origin()
			name := fn.FullName()
			static := true
			if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
				if _, iface := recv.Type().Underlying().(*types.Interface); iface {
					static = false
				}
			}
			rec(waitCall{callee: name, pos: n.Pos(), tx: tx, summary: summary, static: static})
			if !waitSchedulers[name] {
				return true
			}
			c.walk(info, n.Fun, tx, summary, execs, rec)
			for _, a := range n.Args {
				if lit, ok := ast.Unparen(a).(*ast.FuncLit); ok {
					c.walk(info, lit.Body, c.litTx(info, lit), false, execs, rec)
					continue
				}
				c.walk(info, a, tx, summary, execs, rec)
			}
			return false
		}
		return true
	})
}

// waiting returns the functions that wait for a world owner: the primitives, and every function without a
// transaction in scope that calls one, as it runs, through a function or concrete method (R5.2). Calls through
// interfaces are not followed: every implementation of a dispatch interface would wait as soon as one does.
// Functions with a transaction are not summarised: their own waits are reported where they are.
func (c *waitChecker) waiting() map[string]bool {
	waits := map[string]bool{}
	for k := range waitPrimitives {
		waits[k] = true
	}
	for changed := true; changed; {
		changed = false
		for _, fn := range c.funcs {
			if fn.tx || waits[fn.key] {
				continue
			}
			for _, call := range fn.calls {
				if call.summary && call.static && waits[call.callee] || call.summary && strings.HasPrefix(call.callee, "<-") {
					waits[fn.key] = true
					changed = true
					break
				}
			}
		}
	}
	return waits
}

// uses returns the waits for a world owner made with a transaction in scope, sorted.
func (c *waitChecker) uses() []waitUse {
	waits := c.waiting()
	var out []waitUse
	for _, fn := range c.funcs {
		for _, call := range fn.calls {
			if !call.tx {
				continue
			}
			rule := ""
			switch {
			case strings.HasPrefix(call.callee, "<-") || waitPrimitives[call.callee]:
				rule = "R5.1"
			case call.static && waits[call.callee]:
				rule = "R5.2"
			default:
				continue
			}
			out = append(out, waitUse{file: fn.rel, callee: call.callee, rule: rule, pos: fn.fset.Position(call.pos)})
		}
	}
	slices.SortFunc(out, func(a, b waitUse) int { return strings.Compare(a.line(), b.line()) })
	return out
}

// ownerWaits loads the module and returns its waits for a world owner inside transactions.
func ownerWaits() ([]waitUse, error) {
	m, err := loadModule()
	if err != nil {
		return nil, err
	}
	c := newWaitChecker(func(path string) *types.Package { return m.byPath[path] })
	if c.entity == nil {
		return nil, fmt.Errorf("world.Entity was not found")
	}
	err = m.files(func(rel string, f *ast.File, p *packages.Package) error {
		c.add(rel, p.Fset, f, p.TypesInfo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c.uses(), nil
}

func TestOwnersDoNotWait(t *testing.T) {
	current, err := ownerWaits()
	if err != nil {
		t.Fatal(err)
	}

	if *updateWaits {
		var b strings.Builder
		b.WriteString(waitHeader)
		b.WriteString("\n")
		for _, u := range current {
			b.WriteString(u.line() + "\n")
		}
		if err := os.MkdirAll(path.Dir(waitRatchet), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(waitRatchet, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d waits to %s", len(current), waitRatchet)
		return
	}

	listed, err := readAllowlist(waitRatchet)
	if err != nil {
		t.Fatalf("read the ratchet: %v (generate it with: go test -run TestOwnersDoNotWait -update-waits .)", err)
	}
	left := map[string]int{}
	for i, entry := range listed {
		if len(strings.Fields(entry)) != 2 {
			t.Errorf("%s: malformed entry %q, want \"<file> <callee>\"", waitRatchet, entry)
		}
		if i > 0 && listed[i-1] > entry {
			t.Errorf("%s: entry %q is out of order, regenerate with -update-waits", waitRatchet, entry)
		}
		left[entry]++
	}
	// The ratchet and the waits are compared as multisets: each wait takes a line.
	for _, u := range current {
		if left[u.line()] > 0 {
			left[u.line()]--
			continue
		}
		t.Errorf("%s:%d waits for a world owner through %s inside a transaction: breaks %s: %s", u.file, u.pos.Line, u.callee, u.rule, waitHints[u.rule])
	}
	for _, entry := range listed {
		if left[entry] > 0 {
			left[entry]--
			t.Errorf("stale ratchet entry %q: the file no longer waits there (as often), remove it from %s", entry, waitRatchet)
		}
	}
}

// waitFixture is code of each kind TestWaitRules classifies.
const waitFixture = `package fixture

import (
	"context"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type command struct{ other *world.World }

func (c command) Run(_ cmd.Source, _ *cmd.Output, tx *world.Tx) {
	_, _ = world.Call(context.Background(), c.other, func(*world.Tx) (int, error) { return 0, nil }) // violation: R5.1
	go func() {
		_, _ = world.Call(context.Background(), c.other, func(*world.Tx) (int, error) { return 0, nil })
	}()
	_ = c.other.Do(func(*world.Tx) {}).Wait(context.Background()) // violation: R5.1
	done := c.other.Exec(func(*world.Tx) {})
	<-done // violation: R5.1
	wait(c.other) // violation: R5.2
	go wait(c.other)
	go c.other.Save()
	tx.Defer(func(*world.Tx) {
		c.other.Do(func(*world.Tx) {})
	})
	_ = func() {
		c.other.Save() // violation: R5.1
	}
}

func chat(ctx *player.Context, h *world.EntityHandle) {
	_, _ = player.Call(context.Background(), h, func(*world.Tx, *player.Player) (int, error) { return 0, nil }) // violation: R5.1
	ctx.Defer(func(*player.Context) {
		go wait(nil)
	})
}

func schedule(w, other *world.World) {
	w.Do(func(*world.Tx) {
		<-other.Exec(func(*world.Tx) {}) // violation: R5.1
	})
	go func() {
		w.Do(func(*world.Tx) {
			wait(other) // violation: R5.2
		})
	}()
	wait(other)
	<-other.Exec(func(*world.Tx) {})
}

func wait(w *world.World) {
	_ = w.Do(func(*world.Tx) {}).Wait(context.Background())
}

func waitLater(w *world.World, h *world.EntityHandle) {
	w.Do(func(*world.Tx) {}).OnDone(func(error) { wait(w) })
	time.AfterFunc(time.Second, func() { wait(w) })
	world.NewEntityRef[*player.Player](h).Do(func(*world.Tx, *player.Player) {
		wait(w) // violation: R5.2
	})
	player.DoAfter(h, time.Second, func(*world.Tx, *player.Player) {
		go wait(w)
	})
}

func noWait(_ cmd.Source, tx *world.Tx) {
	waitLater(tx.World(), nil)
	schedule(tx.World(), nil) // violation: R5.2
}

func main() {
	wait(nil)
}
`

// TestWaitRules classifies the calls of the fixture, type-checked against the packages of the game, each wait
// inside a transaction marked with the rule it breaks.
func TestWaitRules(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "server/fixture/fixture.go", waitFixture, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	m, err := loadModule()
	if err != nil {
		t.Fatal(err)
	}
	conf := types.Config{Importer: importerFunc(func(path string) (*types.Package, error) {
		if p := m.byPath[path]; p != nil {
			return p, nil
		}
		return nil, fmt.Errorf("package %s was not loaded", path)
	})}
	info := &types.Info{
		Types:     map[ast.Expr]types.TypeAndValue{},
		Defs:      map[*ast.Ident]types.Object{},
		Uses:      map[*ast.Ident]types.Object{},
		Instances: map[*ast.Ident]types.Instance{},
	}
	if _, err := conf.Check(rootModule+"/server/fixture", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	c := newWaitChecker(func(path string) *types.Package { return m.byPath[path] })
	if c.entity == nil {
		t.Fatal("world.Entity was not found")
	}
	c.add("server/fixture/fixture.go", fset, f, info)

	var want []string
	for _, cg := range f.Comments {
		for _, cm := range cg.List {
			if rule, ok := strings.CutPrefix(cm.Text, "// violation: "); ok {
				want = append(want, fmt.Sprintf("%d %s", fset.Position(cm.Pos()).Line, rule))
			}
		}
	}
	var got []string
	for _, u := range c.uses() {
		got = append(got, fmt.Sprintf("%d %s", u.pos.Line, u.rule))
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("waits classified as violations: %v, want %v", got, want)
	}

	// What waits and what does not, by summary.
	waits := c.waiting()
	for name, want := range map[string]bool{
		rootModule + "/server/fixture.wait":      true,
		rootModule + "/server/fixture.schedule":  true,
		rootModule + "/server/fixture.waitLater": false,
		rootModule + "/server/fixture.main":      true,
		rootModule + "/server/fixture.noWait":    false,
	} {
		if waits[name] != want {
			t.Errorf("waiting()[%s] = %v, want %v", name, waits[name], want)
		}
	}

	// Every name the rule knows is that of a function of the module: a rename fails here rather than letting
	// the rule miss it.
	known := map[string]bool{}
	for _, p := range m.byPath {
		for _, name := range p.Scope().Names() {
			switch obj := p.Scope().Lookup(name).(type) {
			case *types.Func:
				known[obj.FullName()] = true
			case *types.TypeName:
				if n, ok := obj.Type().(*types.Named); ok {
					for fn := range n.Methods() {
						known[fn.FullName()] = true
					}
				}
			}
		}
	}
	for _, names := range []map[string]bool{waitPrimitives, waitSchedulers, {worldExec: true, worldExecOwner: true}} {
		for name := range names {
			if !known[name] {
				t.Errorf("%s is not a function of the module", name)
			}
		}
	}

	// The scope of a transaction, by the types of parameters.
	world := m.byPath[worldPkg]
	tx := types.NewPointer(world.Scope().Lookup("Tx").Type())
	ctx := types.NewPointer(world.Scope().Lookup("Context").Type())
	pctx := types.NewPointer(m.byPath[playerPkg].Scope().Lookup("Context").Type())
	p := types.NewPointer(m.byPath[playerPkg].Scope().Lookup("Player").Type())
	h := types.NewPointer(world.Scope().Lookup("EntityHandle").Type())
	entity := world.Scope().Lookup("Entity").Type()
	for _, tc := range []struct {
		name string
		t    types.Type
		want bool
	}{
		{"*world.Tx", tx, true},
		{"*world.Context", ctx, true},
		{"*player.Context", pctx, true},
		{"*player.Player", p, true},
		{"*world.EntityHandle", h, false},
		{"world.Entity", entity, false},
	} {
		if got := c.bound(tc.t); got != tc.want {
			t.Errorf("bound(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
