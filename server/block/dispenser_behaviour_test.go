package block_test

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

var updateDispenseGolden = flag.Bool("update-dispense", false, "rewrite testdata/dispense_behaviours.golden")

// dispenseTick runs a scheduled tick of the dispenser at pos.
type dispenseTick func(d block.Dispenser, pos cube.Pos, tx *world.Tx, r *rand.Rand)

func scheduledDispenseTick(d block.Dispenser, pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	d.ScheduledTick(pos, tx, r)
}

// dispenseScenario is what surrounds the dispenser when it dispenses: each
// one lets some behaviour succeed or fall back.
type dispenseScenario struct {
	name     string
	facing   cube.Face
	peaceful bool
	setup    func(tx *world.Tx, front cube.Pos)
}

var dispenseScenarios = []dispenseScenario{
	{name: "air", facing: cube.FaceEast},
	{name: "air up", facing: cube.FaceUp},
	{name: "stone down", facing: cube.FaceDown},
	{name: "water", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front, block.Water{Depth: 8, Still: true}, nil)
	}},
	{name: "water below", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front.Side(cube.FaceDown), block.Water{Depth: 8, Still: true}, nil)
	}},
	{name: "lava", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front, block.Lava{Depth: 8, Still: true}, nil)
	}},
	{name: "rail", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front, block.Rail{Direction: block.RailEastWest}, nil)
	}},
	{name: "sloped rail below", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front.Side(cube.FaceDown), block.PoweredRail{Direction: block.RailAscendingEast}, nil)
	}},
	{name: "sapling", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front.Side(cube.FaceDown), block.Dirt{}, nil)
		tx.SetBlock(front, block.TreeSapling{Type: "oak_sapling"}, nil)
	}},
	{name: "tnt", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front, block.TNT{}, nil)
	}},
	{name: "water cauldron", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front, block.Cauldron{FillLevel: 6, Liquid: block.CauldronWater}, nil)
	}},
	{name: "full beehive", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front, block.Beehive{Facing: cube.West, HoneyLevel: 5}, nil)
	}},
	{name: "happy ghast", facing: cube.FaceEast, setup: func(tx *world.Tx, front cube.Pos) {
		// Above the floor of the cell: the harness looks for a mob whose
		// position is inside the cell, which excludes its floor.
		tx.AddEntity(entity.NewHappyGhast(world.EntitySpawnOpts{Position: front.Vec3Middle().Add(mgl64.Vec3{0, 0.25, 0})}))
	}},
	{name: "peaceful", facing: cube.FaceEast, peaceful: true},
}

// dispenseSlotsPerWorld is how many scenarios one world runs, each in a
// chunk of its own: every scenario with a stack of one and of two.
var dispenseSlotsPerWorld = len(dispenseScenarios) * 2

// dispenseSlotChunks are the chunks of the slots: those a loader of radius
// three around the origin views, which number 37.
var dispenseSlotChunks = func() (chunks []world.ChunkPos) {
	for z := int32(-3); z <= 3; z++ {
		for x := int32(-3); x <= 3; x++ {
			if x*x+z*z <= 12 {
				chunks = append(chunks, world.ChunkPos{x, z})
			}
		}
	}
	return chunks
}()

// dispenseSlotOrigin returns the position of the dispenser of a slot, in the
// middle of a chunk of its own.
func dispenseSlotOrigin(slot int) cube.Pos {
	c := dispenseSlotChunks[slot]
	return cube.Pos{int(c[0])*16 + 8, 65, int(c[1])*16 + 8}
}

// dispenseRecorder records the sounds and particles a dispense shows.
type dispenseRecorder struct {
	world.NopViewer
	origin cube.Pos
	desc   describer
	events []string
}

func (r *dispenseRecorder) ViewSound(pos mgl64.Vec3, s world.Sound) {
	r.events = append(r.events, fmt.Sprintf("sound %v %s", relativeVec(pos, r.origin), r.desc.describe(s)))
}

func (r *dispenseRecorder) ViewParticle(pos mgl64.Vec3, p world.Particle) {
	r.events = append(r.events, fmt.Sprintf("particle %v %s", relativeVec(pos, r.origin), r.desc.describe(p)))
}

// dispenseStore keeps the columns a world saves.
type dispenseStore struct {
	world.NopProvider
	mu      sync.Mutex
	columns map[world.ChunkPos]*chunk.Column
}

func (s *dispenseStore) StoreColumn(pos world.ChunkPos, _ world.Dimension, col *chunk.Column) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.columns[pos] = col
	return nil
}

// dispenseOutcome is what one dispense did: the events in order, and the
// blocks, entities and scheduled ticks of its chunk afterwards.
type dispenseOutcome struct {
	events   []string
	blocks   []string
	entities []string
	ticks    []string
}

func (o dispenseOutcome) String() string {
	var b strings.Builder
	for _, part := range [][]string{o.events, o.blocks, o.entities, o.ticks} {
		for _, line := range part {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// dispenseAll dispenses a stack of it, of one and of two items, in every
// scenario, in one world, and returns the outcome of each slot, described by
// desc.
func dispenseAll(t *testing.T, it world.Item, tick dispenseTick, desc describer) []dispenseOutcome {
	t.Helper()
	store := &dispenseStore{NopProvider: world.NopProvider{WorldData: &world.WorldDataMemory{}}, columns: make(map[world.ChunkPos]*chunk.Column)}
	w := world.Config{
		Log:             slog.New(slog.DiscardHandler),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Entities:        entity.DefaultRegistry,
		RandSource:      rand.NewPCG(1, 2),
		Provider:        store,
	}.New()
	defer func() { _ = w.Close() }()
	recorder := &dispenseRecorder{desc: desc}
	loader := world.NewLoader(3, w, recorder)
	outcomes := make([]dispenseOutcome, dispenseSlotsPerWorld)
	<-w.Exec(func(tx *world.Tx) {
		loader.Load(tx, len(dispenseSlotChunks))
		for slot := range dispenseSlotsPerWorld {
			origin := dispenseSlotOrigin(slot)
			setUpDispense(tx, origin, it, slot)
			recorder.origin, recorder.events = origin, nil
			difficulty := w.Difficulty()
			if dispenseScenarios[slot/2].peaceful {
				w.SetDifficulty(world.DifficultyPeaceful)
			}
			if p := catchDispensePanic(func() {
				tick(tx.Block(origin).(block.Dispenser), origin, tx, rand.New(rand.NewPCG(3, 4)))
			}); p != nil {
				recorder.events = append(recorder.events, fmt.Sprintf("panicked: %v", p))
			}
			w.SetDifficulty(difficulty)
			outcomes[slot].events = recorder.events
			outcomes[slot].blocks = dispenseBlocks(tx, origin, desc)
		}
		loader.Close(tx)
	})
	w.Save()
	for slot := range outcomes {
		origin := dispenseSlotOrigin(slot)
		col := store.columns[world.ChunkPos{int32(origin[0] >> 4), int32(origin[2] >> 4)}]
		if col == nil {
			t.Fatalf("slot %d was not saved", slot)
		}
		outcomes[slot].entities, outcomes[slot].ticks = dispenseSaved(col, origin, desc)
	}
	return outcomes
}

// catchDispensePanic runs f and returns what it panicked with, so that a
// dispense that panics does not end the transaction of the other slots.
func catchDispensePanic(f func()) (p any) {
	defer func() { p = recover() }()
	f()
	return nil
}

// setUpDispense builds the scenario of a slot around a dispenser at origin
// that holds a stack of it.
func setUpDispense(tx *world.Tx, origin cube.Pos, it world.Item, slot int) {
	sc, count := dispenseScenarios[slot/2], 1+slot%2
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			tx.SetBlock(origin.Add(cube.Pos{x, -1, z}), block.Stone{}, nil)
		}
	}
	tx.SetBlock(origin.Side(cube.FaceWest), block.RedstoneComparator{Facing: cube.West}, nil)
	d := block.NewDispenser()
	d.Facing, d.Triggered = sc.facing, true
	tx.SetBlock(origin, d, nil)
	if sc.setup != nil {
		sc.setup(tx, origin.Side(sc.facing))
	}
	_ = d.Inventory(tx, origin).SetItem(4, item.NewStack(it, min(count, item.NewStack(it, 1).MaxCount())))
}

// dispenseBlocks describes the blocks and liquids around the dispenser at
// origin, with their block entities.
func dispenseBlocks(tx *world.Tx, origin cube.Pos, desc describer) (lines []string) {
	for x := -2; x <= 2; x++ {
		for y := -2; y <= 2; y++ {
			for z := -2; z <= 2; z++ {
				pos := origin.Add(cube.Pos{x, y, z})
				b := tx.Block(pos)
				// Bone meal draws whether a sapling grows from the global
				// source.
				if sapling, ok := b.(block.TreeSapling); ok {
					sapling.Stage = -1
					b = sapling
				}
				line := fmt.Sprintf("block %v %s", cube.Pos{x, y, z}, desc.describe(b))
				if nbt, ok := b.(world.NBTer); ok {
					line += " " + desc.describe(nbt.EncodeNBT())
				}
				if l, ok := tx.Liquid(pos); ok && l != b {
					line += " liquid " + desc.describe(l)
				}
				if _, air := b.(block.Air); air && !strings.Contains(line, "liquid") {
					continue
				}
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// dispenseSaved describes the entities and scheduled ticks of a saved
// column, relative to origin and sorted.
func dispenseSaved(col *chunk.Column, origin cube.Pos, desc describer) (entities, ticks []string) {
	for _, e := range col.Entities {
		data := make(map[string]any, len(e.Data))
		for k, v := range e.Data {
			data[k] = v
		}
		if pos, ok := data["Pos"].([]float32); ok && len(pos) == 3 {
			data["Pos"] = []float32{pos[0] - float32(origin[0]), pos[1] - float32(origin[1]), pos[2] - float32(origin[2])}
		}
		// SpawnEgg.spawn draws the yaw of the mob from the global source,
		// not from the world's.
		if java, ok := data["adamant:java"].(map[string]any); ok && java["SpawnReason"] == "dispenser" {
			data["Rotation"], data["Yaw"] = "random", "random"
		}
		// dropItem draws the motion of a block's drop from the global source;
		// a dispenser ejects with no pickup delay.
		if motion, ok := data["Motion"].([]float32); ok && data["identifier"] == "minecraft:item" && data["PickupDelay"] != int64(0) && len(motion) == 3 {
			data["Motion"] = []any{"random", motion[1], "random"}
		}
		// Chicken draws its first egg time from the global source.
		if _, ok := data["EggLayTime"]; ok {
			data["EggLayTime"] = "random"
		}
		// PrimedTnt draws its horizontal kick from the global source.
		if motion, ok := data["Motion"].([]float32); ok && data["identifier"] == "minecraft:tnt" && len(motion) == 3 {
			data["Motion"] = []any{"random", motion[1], "random"}
		}
		entities = append(entities, "entity "+desc.describe(stripDispenseIDs(data)))
	}
	for _, s := range col.ScheduledTicks {
		s.Pos = s.Pos.Sub(origin)
		// Fire.Place draws its first delay from the global source.
		if s.Type == "minecraft:fire" {
			s.Delay = -1
		}
		// The native block is a runtime ID, which changes with the palette;
		// the type names the block.
		s.NativeBlock = 0
		ticks = append(ticks, "tick "+desc.describe(s))
	}
	slices.Sort(entities)
	slices.Sort(ticks)
	return entities, ticks
}

// stripDispenseIDs removes the random identifiers of an entity from its NBT.
func stripDispenseIDs(v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, x := range v {
			switch k {
			case "UniqueID", "UUID", "OwnerNew", "OwnerID", "TargetID", "LeasherID", "SkinID":
				continue
			}
			m[k] = stripDispenseIDs(x)
		}
		return m
	case []any:
		s := make([]any, len(v))
		for i, x := range v {
			s[i] = stripDispenseIDs(x)
		}
		return s
	}
	return v
}

func relativeVec(pos mgl64.Vec3, origin cube.Pos) mgl64.Vec3 {
	return pos.Sub(origin.Vec3())
}

// describeValue formats v deterministically: blocks and items by their
// encodings, maps sorted, and pointers by what they point to.
func describeValue(v any) string { return describer{}.describe(v) }

// describer formats values as describeValue does. With a dispensed item, it
// writes <dispensed> for that item and for the fields of an item NBT
// compound that are as the item encodes them, so that the items dropped
// alike are described alike.
type describer struct {
	item world.Item
	nbt  map[string]any
}

func newDispensedDescriber(it world.Item) describer {
	return describer{item: it, nbt: item.WriteNBT(item.NewStack(it, 1))}
}

func (d describer) describe(v any) string {
	var b strings.Builder
	d.describeInto(&b, reflect.ValueOf(v), 0)
	return b.String()
}

// dispensedNBT reports whether m is the NBT of a stack of the dispensed item.
func (d describer) dispensedNBT(m map[string]any) bool {
	return d.nbt != nil && m["Name"] == d.nbt["Name"] && describeValue(m["Damage"]) == describeValue(d.nbt["Damage"])
}

func (d describer) describeInto(b *strings.Builder, v reflect.Value, depth int) {
	if depth > 12 {
		b.WriteString("…")
		return
	}
	if !v.IsValid() {
		b.WriteString("nil")
		return
	}
	if v.CanInterface() {
		switch x := v.Interface().(type) {
		case world.Block:
			if v.Kind() != reflect.Interface && v.Kind() != reflect.Pointer {
				// A sound may carry a block with no state, as the liquid of a
				// bucket, whose encoding panics: it is described by its fields.
				if name, props, ok := encodeDescribedBlock(x); ok {
					fmt.Fprintf(b, "%s", name)
					d.describeInto(b, reflect.ValueOf(props), depth+1)
					return
				}
			}
		case world.Item:
			if v.Kind() != reflect.Interface && v.Kind() != reflect.Pointer {
				if d.item != nil && reflect.DeepEqual(x, d.item) {
					b.WriteString("<dispensed>")
					return
				}
				name, meta := x.EncodeItem()
				fmt.Fprintf(b, "%s:%d", name, meta)
				if n, ok := x.(world.NBTer); ok {
					d.describeInto(b, reflect.ValueOf(n.EncodeNBT()), depth+1)
				}
				return
			}
		case item.Stack:
			fmt.Fprintf(b, "%dx", x.Count())
			d.describeInto(b, reflect.ValueOf(x.Item()), depth+1)
			if x.Durability() != x.MaxDurability() {
				fmt.Fprintf(b, " durability %d", x.Durability())
			}
			return
		}
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			b.WriteString("nil")
			return
		}
		d.describeInto(b, v.Elem(), depth+1)
	case reflect.Map:
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
		var dispensed map[string]any
		if m, ok := v.Interface().(map[string]any); ok && d.dispensedNBT(m) {
			dispensed = m
		}
		b.WriteByte('{')
		if dispensed != nil {
			b.WriteString("<dispensed>")
		}
		for i, k := range keys {
			if dispensed != nil {
				if key := k.String(); key != "Count" && key != "Slot" && describeValue(dispensed[key]) == describeValue(d.nbt[key]) {
					continue
				}
			}
			if i > 0 || dispensed != nil {
				b.WriteByte(' ')
			}
			fmt.Fprintf(b, "%v:", k)
			d.describeInto(b, v.MapIndex(k), depth+1)
		}
		b.WriteByte('}')
	case reflect.Slice, reflect.Array:
		b.WriteByte('[')
		for i := range v.Len() {
			if i > 0 {
				b.WriteByte(' ')
			}
			d.describeInto(b, v.Index(i), depth+1)
		}
		b.WriteByte(']')
	case reflect.Struct:
		fmt.Fprintf(b, "%s{", v.Type().Name())
		for i := range v.NumField() {
			if i > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(b, "%s:", v.Type().Field(i).Name)
			d.describeInto(b, v.Field(i), depth+1)
		}
		b.WriteByte('}')
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		b.WriteString(v.Kind().String())
	default:
		fmt.Fprintf(b, "%v", v)
	}
}

// encodeDescribedBlock encodes b, reporting false if its encoding panics.
func encodeDescribedBlock(b world.Block) (name string, props map[string]any, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	name, props = b.EncodeBlock()
	return name, props, true
}

// dispenseItems returns every registered item, sorted by description.
func dispenseItems() []world.Item {
	type described struct {
		it   world.Item
		desc string
	}
	var all []described
	for _, it := range world.Items() {
		all = append(all, described{it, describeValue(it)})
	}
	slices.SortStableFunc(all, func(a, b described) int { return cmp.Compare(a.desc, b.desc) })
	items := make([]world.Item, len(all))
	for i, d := range all {
		items[i] = d.it
	}
	return items
}

// forEachDispensed calls f for each item on two goroutines.
func forEachDispensed(items []world.Item, f func(i int, it world.Item)) {
	var wg sync.WaitGroup
	next := make(chan int)
	for range 2 {
		wg.Go(func() {
			for i := range next {
				f(i, items[i])
			}
		})
	}
	for i := range items {
		next <- i
	}
	close(next)
	wg.Wait()
}

// dispenseSummary is the golden form of an outcome: the hash of the events,
// the blocks and the position, motion and kind of each entity, which does
// not change when the NBT of an entity gains a field.
func dispenseSummary(o dispenseOutcome) string {
	h := sha256.New()
	for _, part := range [][]string{o.events, o.blocks, o.ticks} {
		for _, line := range part {
			h.Write([]byte(line + "\n"))
		}
	}
	for _, line := range o.entities {
		h.Write([]byte(dispenseEntitySummary(line) + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// dispenseEntitySummary keeps the identifier, position, motion, rotation and
// item of an entity line.
func dispenseEntitySummary(line string) string {
	var kept []string
	for _, key := range []string{"identifier:", "Pos:", "Motion:", "Rotation:", "Item:"} {
		i := strings.Index(line, " "+key)
		if i < 0 {
			i = strings.Index(line, "{"+key)
		}
		if i < 0 {
			continue
		}
		rest := line[i+1:]
		kept = append(kept, rest[:valueEnd(rest)])
	}
	return strings.Join(kept, " ")
}

// valueEnd returns the end of the key:value at the start of s.
func valueEnd(s string) int {
	depth := 0
	for i, r := range s {
		switch r {
		case '{', '[':
			depth++
		case '}', ']':
			if depth == 0 {
				return i
			}
			depth--
		case ' ':
			if depth == 0 {
				return i
			}
		}
	}
	return len(s)
}

func dispenseSlotName(slot int) string {
	return fmt.Sprintf("%s/%d", dispenseScenarios[slot/2].name, 1+slot%2)
}

// dispenseGoldenPath holds a summary of the outcome of dispensing each
// registered item in each scenario.
const dispenseGoldenPath = "testdata/dispense_behaviours.golden"

// dispenseDomain returns the items the dispense tests run: every registered
// item, or with -short the first of each Go type, as the behaviour of an
// item is that of its type.
func dispenseDomain() []world.Item {
	items := dispenseItems()
	if !testing.Short() {
		return items
	}
	seen := map[reflect.Type]bool{}
	var sample []world.Item
	for _, it := range items {
		if t := reflect.TypeOf(it); !seen[t] {
			seen[t] = true
			sample = append(sample, it)
		}
	}
	return sample
}

// TestDispenseBehavioursGolden dispenses every registered item in every
// scenario and compares a summary of each outcome with the golden file. The
// file lists the summaries most items share, those of an item dropped, and
// then each item with another outcome in some scenario.
func TestDispenseBehavioursGolden(t *testing.T) {
	t.Parallel()
	items := dispenseDomain()
	summaries := make([][]string, len(items))
	forEachDispensed(items, func(i int, it world.Item) {
		outcomes := dispenseAll(t, it, scheduledDispenseTick, newDispensedDescriber(it))
		summaries[i] = make([]string, len(outcomes))
		for slot, o := range outcomes {
			summaries[i][slot] = dispenseSummary(o)
		}
	})
	if *updateDispenseGolden {
		if testing.Short() {
			t.Fatal("-update-dispense needs every item: run without -short")
		}
		writeDispenseGolden(t, items, summaries)
		return
	}
	defaults, want := readDispenseGolden(t)
	for i, it := range items {
		desc := describeValue(it)
		for slot, got := range summaries[i] {
			name := dispenseSlotName(slot)
			expected, ok := want[desc][name]
			if !ok {
				expected = defaults[dispenseClass(it)][name]
			}
			if got != expected {
				t.Errorf("%s in %s: outcome %s, want %s", desc, name, got, expected)
			}
		}
		delete(want, desc)
	}
	if !testing.Short() {
		for desc := range want {
			t.Errorf("%s is in %s but no longer registered", desc, dispenseGoldenPath)
		}
	}
}

// dispenseClass is the default line of the golden file an item falls back
// to: an item that does not stack dispenses a stack of one in the slots of
// two.
func dispenseClass(it world.Item) string {
	if item.NewStack(it, 1).MaxCount() == 1 {
		return "default unstackable"
	}
	return "default"
}

// writeDispenseGolden writes the summaries of the items to the golden file.
func writeDispenseGolden(t *testing.T, items []world.Item, summaries [][]string) {
	defaults := map[string][]string{}
	for _, class := range []string{"default", "default unstackable"} {
		defaults[class] = make([]string, dispenseSlotsPerWorld)
		for slot := range defaults[class] {
			counts := map[string]int{}
			for i, it := range items {
				if dispenseClass(it) == class {
					counts[summaries[i][slot]]++
				}
			}
			best := ""
			for s, n := range counts {
				if n > counts[best] || n == counts[best] && s < best {
					best = s
				}
			}
			defaults[class][slot] = best
		}
	}
	var b strings.Builder
	b.WriteString("# The outcome of dispensing each registered item, by scenario and stack size, as a hash of what the\n")
	b.WriteString("# dispense did in the world (dispenser_behaviour_test.go). Fields are separated by tabs. The default\n")
	b.WriteString("# lines hold the outcomes most items share, of stackable and unstackable items; each other line an item\n")
	b.WriteString("# and where its outcome differs from its default line.\n")
	b.WriteString("# Regenerate with: go test ./server/block -run TestDispenseBehavioursGolden -update-dispense\n")
	for _, class := range []string{"default", "default unstackable"} {
		b.WriteString(class)
		for slot, s := range defaults[class] {
			fmt.Fprintf(&b, "\t%s=%s", dispenseSlotName(slot), s)
		}
		b.WriteByte('\n')
	}
	for i, it := range items {
		var line []string
		for slot, s := range summaries[i] {
			if s != defaults[dispenseClass(it)][slot] {
				line = append(line, dispenseSlotName(slot)+"="+s)
			}
		}
		if len(line) != 0 {
			fmt.Fprintf(&b, "%s\t%s\n", describeValue(it), strings.Join(line, "\t"))
		}
	}
	if err := os.WriteFile(dispenseGoldenPath, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readDispenseGolden reads the default outcomes of each scenario and the
// outcomes of the items listed from the golden file.
func readDispenseGolden(t *testing.T) (defaults, items map[string]map[string]string) {
	data, err := os.ReadFile(dispenseGoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	defaults, items = map[string]map[string]string{}, map[string]map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		outcomes := map[string]string{}
		for _, f := range fields[1:] {
			slot, summary, _ := strings.Cut(f, "=")
			outcomes[slot] = summary
		}
		if strings.HasPrefix(fields[0], "default") {
			defaults[fields[0]] = outcomes
		} else {
			items[fields[0]] = outcomes
		}
	}
	return defaults, items
}
