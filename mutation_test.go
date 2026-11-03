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
	"testing"

	"golang.org/x/tools/go/packages"
)

// updateMutations rewrites the ratchet of TestAdaptersKeepOffTheWorld to exactly the uses that exist now.
var updateMutations = flag.Bool("update-mutations", false, "rewrite testdata/boundary/mutations.txt to the current uses of the world by adapters")

const (
	// mutationRatchet is the path of the ratchet of R4, relative to the repository root.
	mutationRatchet = "testdata/boundary/mutations.txt"

	worldPkg = rootModule + "/server/world"
)

// mutationHeader starts the ratchet file.
const mutationHeader = `# Uses of the world by protocol adapters that still exist (R4).
#
# Each line is "<file> <callee>": a file of an adapter, relative to the repository root and written with
# slashes, followed by a function or method that the rules of mutation_test.go (see docs/protocol-boundary.md)
# forbid it to use: it changes the state of the game or sets off one of its effects. There is one line for each
# use, so a callee used twice in a file is listed twice.
#
# This list is a ratchet. It may only shrink as game logic leaves the adapters: an entry that is no longer used
# fails the test until it is removed. Adding an entry needs a justification in review.
#
# Regenerate it with:
#
#	go test -run TestAdaptersKeepOffTheWorld -update-mutations .
#
# Blank lines and lines starting with # are ignored. Entries are sorted.
`

// adapterZones are the directories of adapter code, relative to the repository root: protocol/ holds every
// adapter, and nothing under server/ is one.
var adapterZones = []string{"protocol"}

// inAdapter reports if a slash-separated file or package directory relative to the repository root is adapter
// code.
func inAdapter(rel string) bool {
	return slices.ContainsFunc(adapterZones, func(zone string) bool { return under(rel, zone) })
}

// adapterPackage reports if a package path is that of an adapter.
func adapterPackage(pkgPath string) bool {
	rel, ok := strings.CutPrefix(pkgPath, rootModule+"/")
	return ok && inAdapter(rel)
}

// worldReads are the methods of *world.Tx and *world.World that read the world, schedule a transaction, or hand
// out what the adapter must release. Every other method of the two is a mutation or an effect, so a new method is
// forbidden until it is listed here: the list fails closed.
var worldReads = map[string][]string{
	"Tx": {
		"Biome", "Block", "BlockFaceSturdy", "BlockLight", "BlockLoaded", "BlockStateID", "CaptureStructure",
		"ChunkLoaded", "ChunkReadySignal", "ChunkState", "ColumnBlockVersion", "CurrentTick", "Defer", "DeferErr",
		"Entities", "EntitiesOfType", "EntitiesOfTypesWithin", "EntitiesWithin", "EntityByUUID", "EntityByUniqueID",
		"EntityChunk", "EntityTicking", "Features", "ForEachMapFrame", "HasScheduledBlockUpdate", "HighestBlock",
		"HighestLightBlocker", "IsDarkOutside", "IsVillage", "Light", "LightBlockedInto", "Liquid",
		"LoadedBlockEntities", "LocalDifficulty", "MayContainLiquid", "MovementBlockProperties", "NativeState", "POI",
		"PlayerSpawnPosition", "Players", "PlayersNear", "Raining", "RainingAt", "Range", "RawBrightness", "ReadyChunk",
		"RedstoneConductivePower", "RedstoneDirectPower", "RedstoneDirectPowerFrom", "RedstonePower",
		"RedstonePowerFrom", "RedstoneStrongPower", "RedstoneStrongPowerFrom", "ReleaseViewers", "SkyDarken",
		"SkyLight", "Sleepers", "SnowingAt", "SunAngle", "Temperature", "Thundering", "ThunderingAt", "Viewers",
		"VillageDistance", "World",
	},
	"World": {
		"AutoSaveEnabled", "BlockRegistry", "Border", "ClampToBorder", "CreakingActive", "CurrentTick",
		"DefaultGameMode", "Difficulty", "Dimension", "Do", "DoAfter", "EntityCount", "EntityRegistry", "Exec",
		"FallDamage", "ForcedChunks", "Frozen", "GameRule", "GameRuleBool", "GameRuleInt", "Handler",
		"HighestLightBlocker", "IsChunkForced", "LoadedChunkCount", "MapData", "MapDataByJavaID", "MapIDByJavaID",
		"MonsterSpawningEnabled", "MoonBrightness", "Name", "PlayerSpawn", "PlayersSleepingPercentage", "Range",
		"RegionPhases", "Seed", "Spawn", "SpawnMobsEnabled", "SpawnWardensEnabled", "Sprinting", "StructureTemplate",
		"TPS", "TickRate", "TickTimes", "Time", "TimeCycle", "TrackedPosition", "WeatherCycleEnabled", "WithinBorder",
		"WithinBorderBounds",
	},
}

// txEntries are the functions below server/ with a *world.Tx parameter that adapters may call: reads, and the
// entry points through which a client answers the game. They are written as calleeName writes them.
var txEntries = map[string]bool{
	"(*world.EntityHandle).Entity": true,
	"(block.Container).Inventory":  true,
	"(form.Form).SubmitJSON":       true,
	"(dialogue.Dialogue).Submit":   true,
	"(dialogue.Dialogue).Close":    true,
}

// txEntryTypes are the types every method of which adapters may call with a *world.Tx: the loader of the chunks
// a client sees is the adapter's own.
var txEntryTypes = map[string]bool{
	"*world.Loader": true,
}

// mutators are the functions below server/ that change the state of the game or set off its effects without a
// *world.Tx parameter, as calleeName writes them.
var mutators = func() map[string]bool {
	m := map[string]bool{}
	for recv, names := range map[string][]string{
		"*inventory.Inventory":         {"SetItem", "AddItem", "RemoveItem", "RemoveItemFunc", "Swap", "Clear", "Close", "SlotFunc", "SlotValidatorFunc", "Handle"},
		"*inventory.Armour":            {"Set", "SetHelmet", "SetChestplate", "SetLeggings", "SetBoots", "Clear", "Close"},
		"*world.EntityHandle":          {"Close"},
		"*chat.Chat":                   {"Subscribe", "Unsubscribe", "Write", "WriteString", "Writef", "Writet"},
		"entity.TradingMerchant":       {"StartTrading", "StopTrading"},
		"entity.VehicleInput":          {"SetVehicleInput"},
		"entity.VehicleInputWithRider": {"SetVehicleInputFrom"},
		"entity.VehicleJumpInput":      {"SetVehicleJumping"},
		"block.Crafter":                {"NotifySlotState"},
		"*player.Player":               {"SetExperienceLevel", "AddExperience", "RemoveExperience", "ResetEnchantmentSeed"},
	} {
		for _, name := range names {
			m["("+recv+")."+name] = true
		}
	}
	return m
}()

// mutatorNames are the bare names of methods that change the game when an interface of an adapter asserts to
// them: those of mutators and a few more that interfaces of the session ask for.
var mutatorNames = func() map[string]bool {
	m := map[string]bool{"AddViewer": true, "RemoveViewer": true, "ResetExperience": true, "UpdateCommandBlock": true, "RideJump": true}
	for name := range mutators {
		m[name[strings.LastIndex(name, ".")+1:]] = true
	}
	return m
}()

// ownInterfaces are the interfaces declared in an adapter that only the adapter's own types implement, so that
// their methods are adapter code, checked where they are declared, rather than a way into the game.
var ownInterfaces = map[string]bool{
	// The handlers of the packets of the Bedrock session.
	"session.packetHandler": true,
	// The connections of the Bedrock listener to the signaling services of
	// Xbox Live, which the listener dials itself.
	"bedrockadapter.xboxSignaling": true,
}

// mutationHints holds a one-line hint for each rule of R4.
var mutationHints = map[string]string{
	"R4.1": "the adapter changes the world or sets off an effect through the transaction or the world: ask the game through client.Controllable or server/menu",
	"R4.2": "the adapter calls game code that takes a transaction: ask the game through client.Controllable or server/menu, or list it in txEntries if it only reads",
	"R4.3": "the adapter changes the state of the game: ask the game through client.Controllable or server/menu",
	"R4.4": "an interface of the adapter reaches past the contract into the game: ask the game through client.Controllable or server/menu",
	"R4.5": "the adapter shows other viewers an effect: the game shows its viewers what they see",
}

// mutationUse is a use of the world by an adapter: the line of the ratchet and the rule it breaks.
type mutationUse struct {
	file, callee, rule string
}

func (u mutationUse) line() string { return u.file + " " + u.callee }

// calleeName writes a function as the ratchet lists it: a method as (receiver).Name and a function as
// package.Name, with packages by their names. A method of an interface without a name is (interface).Name.
func calleeName(fn *types.Func) string {
	qual := func(p *types.Package) string { return p.Name() }
	sig := fn.Type().(*types.Signature)
	if recv := sig.Recv(); recv != nil {
		t := recv.Type()
		if _, anonymous := t.(*types.Interface); anonymous {
			return "(interface)." + fn.Name()
		}
		if ptr, ok := t.(*types.Pointer); ok {
			if n, ok := ptr.Elem().(*types.Named); ok {
				return "(*" + types.TypeString(n.Origin(), qual) + ")." + fn.Name()
			}
		}
		if n, ok := t.(*types.Named); ok {
			t = n.Origin()
		}
		return "(" + types.TypeString(t, qual) + ")." + fn.Name()
	}
	if fn.Pkg() == nil {
		return fn.Name()
	}
	return fn.Pkg().Name() + "." + fn.Name()
}

// mutationChecker decides which functions adapters may not use.
type mutationChecker struct {
	// contract holds the methods of client.Controllable, which adapters may call: the client asks the game.
	contract map[*types.Func]bool
}

// newMutationChecker returns a checker that allows the methods of client.Controllable, if pkgs holds the client
// package among the packages they import.
func newMutationChecker(imported func(path string) *types.Package) *mutationChecker {
	c := &mutationChecker{contract: map[*types.Func]bool{}}
	if p := imported(rootModule + "/server/client"); p != nil {
		if obj, ok := p.Scope().Lookup("Controllable").(*types.TypeName); ok {
			ms := types.NewMethodSet(obj.Type())
			for i := range ms.Len() {
				if fn, ok := ms.At(i).Obj().(*types.Func); ok {
					c.contract[fn] = true
				}
			}
		}
	}
	return c
}

// isTx reports if t is *world.Tx.
func isTx(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := ptr.Elem().(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == worldPkg && n.Obj().Name() == "Tx"
}

// takesTx reports if a signature has a *world.Tx parameter.
func takesTx(sig *types.Signature) bool {
	for v := range sig.Params().Variables() {
		if isTx(v.Type()) {
			return true
		}
	}
	return false
}

// worldReceiver returns "Tx" or "World" if fn is a method of *world.Tx or *world.World.
func worldReceiver(sig *types.Signature) string {
	recv := sig.Recv()
	if recv == nil {
		return ""
	}
	ptr, ok := recv.Type().(*types.Pointer)
	if !ok {
		return ""
	}
	n, ok := ptr.Elem().(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != worldPkg {
		return ""
	}
	if name := n.Obj().Name(); name == "Tx" || name == "World" {
		return name
	}
	return ""
}

// viewerInterface reports if the receiver of sig is the world.Viewer interface itself: a method called on
// another viewer, which the adapter's own session never is to itself.
func viewerInterface(sig *types.Signature) bool {
	recv := sig.Recv()
	if recv == nil {
		return false
	}
	n, ok := recv.Type().(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == worldPkg && n.Obj().Name() == "Viewer"
}

// rule returns the rule of R4 that a use of fn by adapter code breaks, if any.
//
//   - R4.1: a method of *world.Tx or *world.World other than those of worldReads.
//   - R4.2: a function or method declared below server/, outside server/client (the contract), server/menu (its
//     operations) and the adapters, with a *world.Tx parameter, other than txEntries and txEntryTypes.
//   - R4.3: a function of mutators.
//   - R4.4: a method of an interface declared in an adapter, with a *world.Tx parameter or a name of
//     mutatorNames: an interface the adapter asserts a value of the game to. The interfaces of ownInterfaces,
//     which only the adapter implements, are left out.
//   - R4.5: a View method of world.Viewer called on a viewer: an effect shown to others.
//
// The methods of client.Controllable are allowed by construction.
func (c *mutationChecker) rule(fn *types.Func) (string, bool) {
	fn = fn.Origin()
	if c.contract[fn] {
		return "", false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || fn.Pkg() == nil {
		return "", false
	}
	pkg := fn.Pkg().Path()
	name := calleeName(fn)
	if recv := worldReceiver(sig); recv != "" {
		if slices.Contains(worldReads[recv], fn.Name()) {
			return "", false
		}
		return "R4.1", true
	}
	if viewerInterface(sig) && strings.HasPrefix(fn.Name(), "View") {
		return "R4.5", true
	}
	if mutators[name] {
		return "R4.3", true
	}
	if adapterPackage(pkg) {
		if recv := sig.Recv(); recv != nil && !ownInterfaces[types.TypeString(recv.Type(), func(p *types.Package) string { return p.Name() })] {
			if _, ok := recv.Type().Underlying().(*types.Interface); ok && (takesTx(sig) || mutatorNames[fn.Name()]) {
				return "R4.4", true
			}
		}
		return "", false
	}
	if !under(pkg, rootModule+"/server") || under(pkg, rootModule+"/server/client") || under(pkg, rootModule+"/server/menu") {
		return "", false
	}
	if !takesTx(sig) || txEntries[name] {
		return "", false
	}
	if recv := sig.Recv(); recv != nil && txEntryTypes[types.TypeString(recv.Type(), func(p *types.Package) string { return p.Name() })] {
		return "", false
	}
	return "R4.2", true
}

// uses returns the uses of the world in the syntax of a file of adapter code, rel, type-checked with info. Every
// selector is looked at, so that a method value is a use as much as a call is.
func (c *mutationChecker) uses(rel string, f *ast.File, info *types.Info) []mutationUse {
	var out []mutationUse
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		fn, ok := info.Uses[sel.Sel].(*types.Func)
		if !ok {
			return true
		}
		if rule, bad := c.rule(fn); bad {
			out = append(out, mutationUse{file: rel, callee: calleeName(fn.Origin()), rule: rule})
		}
		return true
	})
	return out
}

// importedPackages returns the packages passed and every package they import, directly or not, by path.
func importedPackages(roots []*types.Package) map[string]*types.Package {
	byPath := map[string]*types.Package{}
	var visit func(p *types.Package)
	visit = func(p *types.Package) {
		if p == nil || byPath[p.Path()] != nil {
			return
		}
		byPath[p.Path()] = p
		for _, imp := range p.Imports() {
			visit(imp)
		}
	}
	for _, p := range roots {
		visit(p)
	}
	return byPath
}

// adapterUses loads the adapter packages of the repository at root and returns their uses of the world, sorted.
func adapterUses(root string) ([]mutationUse, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var patterns []string
	for _, zone := range adapterZones {
		if _, err := os.Stat(filepath.Join(abs, zone)); err == nil {
			patterns = append(patterns, "./"+zone+"/...")
		}
	}
	conf := &packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir:   abs,
		Tests: false,
	}
	pkgs, err := packages.Load(conf, patterns...)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("load the adapters: %s", strings.Join(errs, "; "))
	}
	byPath := importedPackages(roots)
	checker := newMutationChecker(func(path string) *types.Package { return byPath[path] })

	var out []mutationUse
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			rel, err := filepath.Rel(abs, p.Fset.Position(f.Pos()).Filename)
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(rel)
			if !inAdapter(rel) || strings.HasSuffix(rel, "_test.go") {
				continue
			}
			out = append(out, checker.uses(rel, f, p.TypesInfo)...)
		}
	}
	slices.SortFunc(out, func(a, b mutationUse) int { return strings.Compare(a.line(), b.line()) })
	return out, nil
}

func TestAdaptersKeepOffTheWorld(t *testing.T) {
	current, err := adapterUses(".")
	if err != nil {
		t.Fatal(err)
	}

	if *updateMutations {
		var b strings.Builder
		b.WriteString(mutationHeader)
		b.WriteString("\n")
		for _, u := range current {
			b.WriteString(u.line() + "\n")
		}
		if err := os.MkdirAll(path.Dir(mutationRatchet), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mutationRatchet, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d uses to %s", len(current), mutationRatchet)
		return
	}

	listed, err := readAllowlist(mutationRatchet)
	if err != nil {
		t.Fatalf("read the ratchet: %v (generate it with: go test -run TestAdaptersKeepOffTheWorld -update-mutations .)", err)
	}
	left := map[string]int{}
	for i, entry := range listed {
		if len(strings.Fields(entry)) != 2 {
			t.Errorf("%s: malformed entry %q, want \"<file> <callee>\"", mutationRatchet, entry)
		}
		if i > 0 && listed[i-1] > entry {
			t.Errorf("%s: entry %q is out of order, regenerate with -update-mutations", mutationRatchet, entry)
		}
		left[entry]++
	}
	// The ratchet and the uses are compared as multisets: each use takes a line.
	for _, u := range current {
		if left[u.line()] > 0 {
			left[u.line()]--
			continue
		}
		t.Errorf("%s uses %s: breaks %s: %s", u.file, u.callee, u.rule, mutationHints[u.rule])
	}
	for _, entry := range listed {
		if left[entry] > 0 {
			left[entry]--
			t.Errorf("stale ratchet entry %q: the adapter no longer uses it there (as often), remove it from %s", entry, mutationRatchet)
		}
	}
}

// mutationFixture is adapter code of each kind TestMutationRules classifies.
const mutationFixture = `package session

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

type viewer interface{ ViewEmote(world.Entity, [16]byte) }

type packetHandler interface{ Handle(tx *world.Tx) error }

type smelter interface{ ResetExperience() int }

func use(tx *world.Tx, h *world.EntityHandle, l *world.Loader, c client.Controllable, inv *inventory.Inventory, m *menu.Menu, v world.Viewer, own viewer, b any) {
	pos := cube.Pos{}
	tx.SetBlock(pos, nil, nil)                                  // violation: R4.1
	f := tx.AddEntity                                           // violation: R4.1
	_ = f
	b.(interface{ RemoveViewer(*world.Tx, cube.Pos) }).RemoveViewer(tx, pos) // violation: R4.4
	b.(interface{ StartTrading(world.Entity) }).StartTrading(c)            // violation: R4.4
	chat.Global.Writet(chat.MessageQuit, "x")                    // violation: R4.3
	_ = inv.SetItem(0, item.Stack{})                             // violation: R4.3
	tx.PlaySound(mgl64.Vec3{}, sound.Click{})                    // violation: R4.1
	v.ViewEmote(c, [16]byte{})                                   // violation: R4.5
	_ = tx.World().SetGameRule("x", true)                        // violation: R4.1
	_ = tx.Block(pos)
	_, _ = h.Entity(tx)
	l.Move(tx, mgl64.Vec3{})
	c.BreakBlock(pos)
	m.Close(tx, h)
	own.ViewEmote(c, [16]byte{})
	_ = tx.World().Dimension()
	_, _ = inv.Item(0)
	var ph packetHandler
	_ = ph.Handle(tx)
	_ = b.(smelter).ResetExperience() // violation: R4.4
}
`

// TestMutationRules classifies the uses of the fixture, adapter code type-checked against the packages of the
// game, each marked with the rule it breaks.
func TestMutationRules(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "protocol/bedrock/session/fixture.go", mutationFixture, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	// The packages the fixture imports, from their export data.
	var paths []string
	for _, spec := range f.Imports {
		paths = append(paths, strings.Trim(spec.Path.Value, `"`))
	}
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps}, paths...)
	if err != nil {
		t.Fatal(err)
	}
	var roots []*types.Package
	for _, p := range pkgs {
		for _, e := range p.Errors {
			t.Errorf("load %s: %v", p.PkgPath, e)
		}
		roots = append(roots, p.Types)
	}
	imported := importedPackages(roots)
	conf := types.Config{Importer: importerFunc(func(path string) (*types.Package, error) {
		if p := imported[path]; p != nil {
			return p, nil
		}
		return nil, fmt.Errorf("package %s was not loaded", path)
	})}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	if _, err := conf.Check(rootModule+"/protocol/bedrock/session", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	checker := newMutationChecker(func(path string) *types.Package { return imported[path] })
	if len(checker.contract) == 0 {
		t.Fatal("the methods of client.Controllable were not found")
	}

	var want []string
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if rule, ok := strings.CutPrefix(c.Text, "// violation: "); ok {
				want = append(want, fmt.Sprintf("%d %s", fset.Position(c.Pos()).Line, rule))
			}
		}
	}
	var got []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if fn, ok := info.Uses[sel.Sel].(*types.Func); ok {
			if rule, bad := checker.rule(fn); bad {
				got = append(got, fmt.Sprintf("%d %s", fset.Position(sel.Pos()).Line, rule))
			}
		}
		return true
	})
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("uses classified as violations: %v, want %v", got, want)
	}

	// The rules by path: what is adapter code and what is game code.
	for _, tc := range []struct {
		rel  string
		want bool
	}{
		{"server/session/session.go", false},
		{"server/netconn/conn.go", false},
		{"protocol/bedrock/session/session.go", true},
		{"protocol/java/text/json.go", true},
		{"server/sessionfoo/x.go", false},
		{"server/player/player.go", false},
		{"protocolfoo/x.go", false},
	} {
		if got := inAdapter(tc.rel); got != tc.want {
			t.Errorf("inAdapter(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

// importerFunc is a types.Importer of a function.
type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }
