package session

import (
	"bytes"
	"maps"
	"math"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/go-gl/mathgl/mgl64"
)

// corruptNBTValues stand in for a field of entity NBT that holds something
// else than it should, as a hand-edited, older or foreign save may.
var corruptNBTValues = []any{
	nil, // The field is missing.
	byte(255),
	int32(-1),
	int64(math.MaxInt64),
	float32(math.NaN()),
	"minecraft:nothing",
	map[string]any{},
	map[string]any{"id": "minecraft:stone", "Name": int32(1), "name": int32(1)},
	[]any{},
	[]any{"x", int32(1)},
	[]float32{},
}

// entityNBTOf returns the native NBT an entity of the type named saves with
// after it was made from Java NBT with nothing but its id, or nil if none
// can be made: TestEveryEntityTypeLivesThroughItsLife reports a type that
// panics doing so.
func entityNBTOf(w *world.World, name string) (data map[string]any) {
	defer func() {
		if recover() != nil {
			data = nil
		}
	}()
	h, err := entity.NewFromStructureNBT(map[string]any{"id": name}, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 0.5}})
	if err != nil {
		return nil
	}
	<-w.Exec(func(tx *world.Tx) {
		defer func() {
			if recover() != nil {
				data = nil
			}
		}()
		e := tx.AddEntity(h)
		data = tx.World().EntityNBT(h)
		tx.RemoveEntity(e)
	})
	return data
}

// corruptions returns the NBT of data with, for each field of it and of the
// compounds in it, the field missing or holding each of corruptNBTValues.
func corruptions(data map[string]any) []map[string]any {
	var out []map[string]any
	for _, key := range slices.Sorted(maps.Keys(data)) {
		for _, v := range corruptNBTValues {
			c := maps.Clone(data)
			if v == nil {
				delete(c, key)
			} else {
				c[key] = v
			}
			out = append(out, c)
		}
		if nested, ok := data[key].(map[string]any); ok {
			for _, n := range corruptions(nested) {
				c := maps.Clone(data)
				c[key] = n
				out = append(out, c)
			}
		}
	}
	return out
}

// patchOf returns the MergeNBT patch that turns from into to: nil deletes a
// field.
func patchOf(from, to map[string]any) map[string]any {
	patch := maps.Clone(to)
	for key := range from {
		if _, ok := to[key]; !ok {
			patch[key] = nil
		}
	}
	return patch
}

// decodeCorrupt has an entity of the type named decoded from data as Java
// NBT, and data merged into an entity of the type as its native NBT, and
// lives each through a few ticks. It returns the panic of either, if one
// panicked.
func decodeCorrupt(w *world.World, viewer *Session, name string, base, data map[string]any) (string, any) {
	java := maps.Clone(data)
	java["id"] = name
	var h *world.EntityHandle
	if step, err := func() (step string, err any) {
		defer func() { err = recover() }()
		h, _ = entity.NewFromStructureNBT(java, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 0.5}})
		return "decode java nbt", nil
	}(); err != nil {
		return step, err
	}
	if h != nil {
		if step, err := liveThrough(w, viewer, h, nil, 3); err != nil {
			return "java nbt: " + step, err
		}
	}
	h, err := entity.NewFromStructureNBT(map[string]any{"id": name}, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 0.5}})
	if err != nil {
		return "", nil
	}
	if step, err := liveThrough(w, viewer, h, patchOf(base, data), 3); err != nil {
		return "native nbt: " + step, err
	}
	return "", nil
}

// TestEveryEntityTypeDecodesCorruptNBT decodes every type of the default
// entity registry from its saved NBT with each field of it missing or
// holding something else, as Java NBT and as native NBT, and lives it
// through a few ticks: the entity loads with defaults or is dropped, and
// nothing panics.
func TestEveryEntityTypeDecodesCorruptNBT(t *testing.T) {
	t.Parallel()
	cases := 0
	for _, name := range entityTypeNames() {
		w, viewer := lifecycleWorld(t)
		base := entityNBTOf(w, name)
		if base == nil {
			continue
		}
		failed := map[string]bool{}
		for _, data := range corruptions(base) {
			cases++
			if step, err := decodeCorrupt(w, viewer, name, base, data); err != nil && !failed[step] {
				failed[step] = true
				t.Errorf("%s from %v: %s", name, data, lifecyclePanic(name, step, err))
			}
		}
		_ = w.Close()
	}
	t.Logf("%d corrupt NBT of %d types decoded", cases, len(entityTypeNames()))
}

// fuzzWorld is the world FuzzEntityNBT decodes entities into. An input
// keeps the chunk it put its entity in loaded, wherever its position was, so
// the world is made afresh every fuzzWorldInputs inputs.
var fuzzWorld struct {
	w      *world.World
	viewer *Session
	inputs int
}

const fuzzWorldInputs = 256

// fuzzWorldFor returns the world for the next input of FuzzEntityNBT.
func fuzzWorldFor() (*world.World, *Session) {
	if fuzzWorld.w == nil || fuzzWorld.inputs >= fuzzWorldInputs {
		if fuzzWorld.w != nil {
			_ = fuzzWorld.w.Close()
		}
		fuzzWorld.w = world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
		fuzzWorld.viewer = testSession("fuzz")
		fuzzWorld.viewer.br = world.DefaultBlockRegistry
		fuzzWorld.inputs = 0
	}
	fuzzWorld.inputs++
	return fuzzWorld.w, fuzzWorld.viewer
}

// FuzzEntityNBT decodes an entity of a registered type from fuzzed NBT, as
// Java NBT and as native NBT, and lives it through a few ticks.
func FuzzEntityNBT(f *testing.F) {
	names := entityTypeNames()
	for i, name := range names {
		w, _ := fuzzWorldFor()
		for _, data := range []map[string]any{{}, entityNBTOf(w, name)} {
			if data == nil {
				continue
			}
			var buf bytes.Buffer
			if err := nbt.NewEncoderWithEncoding(&buf, nbt.LittleEndian).Encode(data); err != nil {
				f.Fatalf("encode the NBT of %s: %v", name, err)
			}
			f.Add(uint16(i), buf.Bytes())
		}
	}
	f.Fuzz(func(t *testing.T, typ uint16, b []byte) {
		var data map[string]any
		if err := nbt.UnmarshalEncoding(b, &data, nbt.LittleEndian); err != nil || data == nil {
			return
		}
		name := names[int(typ)%len(names)]
		w, viewer := fuzzWorldFor()
		if step, err := decodeCorrupt(w, viewer, name, map[string]any{}, data); err != nil {
			t.Fatal(lifecyclePanic(name, step, err))
		}
	})
}
