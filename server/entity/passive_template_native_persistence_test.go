package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

// These are captured Source outputs, not expected values produced by a native
// codec. Select distinct visible states so a shared template path is not tested
// repeatedly just because the golden contains several RNG seeds.
func TestPreparedPassiveSourceStateSurvivesMCDB(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_passive_template_authored_spawn.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID  string `json:"id"`
		NBT string `json:"nbt_binary"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	selected := map[string]map[string]any{}
	for _, source := range cases {
		switch source.ID {
		case "minecraft:cat", "minecraft:cow", "minecraft:pig", "minecraft:sheep", "minecraft:villager", "minecraft:iron_golem", "minecraft:allay":
		default:
			continue
		}
		nbt := sourceSpawnNBT(t, source.NBT)
		nbt["id"] = source.ID
		key := fmt.Sprintf("%s/%v/%v/%v/%v/%v", source.ID, nbt["variant"], nbt["sound_variant"], nbt["Color"], nbt["Age"], nbt["VillagerData"])
		if selected[key] == nil {
			selected[key] = nbt
		}
	}
	path := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Entities: DefaultRegistry, Synchronous: true}.New()
	}
	w := open()
	expected := map[int64]map[string]any{}
	positions := map[world.ChunkPos]cube.Pos{}
	if err := w.Do(func(tx *world.Tx) {
		index := 0
		for _, nbt := range selected {
			pos := mgl64.Vec3{float64(index*4) + .5, 65, .5}
			index++
			handle, err := NewFromStructureNBT(nbt, world.EntitySpawnOpts{Position: pos, SpawnReason: "structure", SpawnFinalized: true})
			if err != nil {
				t.Error(err)
				continue
			}
			actor := tx.AddEntity(handle)
			expected[handle.UniqueID()] = nbt
			p := cube.PosFromVec3(pos)
			positions[world.ChunkPos{int32(p[0] >> 4), 0}] = p
			checkPreparedPassiveSourceState(t, actor, nbt)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Save()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = open()
	defer w.Close()
	if err := w.Do(func(tx *world.Tx) {
		for _, pos := range positions {
			tx.Block(pos)
		}
		found := map[int64]bool{}
		for actor := range tx.Entities() {
			nbt, ok := expected[actor.H().UniqueID()]
			if !ok {
				continue
			}
			checkPreparedPassiveSourceState(t, actor, nbt)
			found[actor.H().UniqueID()] = true
		}
		for id := range expected {
			if !found[id] {
				t.Errorf("Source NPC %d missing on MCDB reopen", id)
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func checkPreparedPassiveSourceState(t *testing.T, actor world.Entity, source map[string]any) {
	t.Helper()
	if hp, ok := source["Health"].(float32); ok {
		if got := actor.(interface{ Health() float64 }).Health(); got != float64(hp) {
			t.Errorf("%s health=%g, Source=%g", source["id"], got, hp)
		}
	}
	age := int(nbtconv.Int32(source, "Age"))
	variant, _ := source["variant"].(string)
	sound, _ := source["sound_variant"].(string)
	sound = strings.TrimPrefix(sound, "minecraft:")
	check := func(name string, got, want any) {
		t.Helper()
		if got != want {
			t.Errorf("%s %s=%v, Source=%v", source["id"], name, got, want)
		}
	}
	switch actor := actor.(type) {
	case *Cat:
		names := map[string]int{"minecraft:tabby": 0, "minecraft:black": 1, "minecraft:red": 2, "minecraft:siamese": 3, "minecraft:british_shorthair": 4, "minecraft:calico": 5, "minecraft:persian": 6, "minecraft:ragdoll": 7, "minecraft:white": 8, "minecraft:jellie": 9, "minecraft:all_black": 10}
		check("variant", actor.b().variant, names[variant])
		check("sound", actor.b().soundVariant, sound)
		check("age", actor.b().ageTicks, age)
		check("baby", actor.b().Baby(), age < 0)
	case *Cow:
		values := map[string]int{"minecraft:temperate": 0, "minecraft:warm": 1, "minecraft:cold": 2}
		b := actor.behaviour()
		check("variant", b.variant, values[variant])
		check("sound", b.soundVariant, sound)
		check("age", b.ageTicks, age)
		check("baby", b.Baby(), age < 0)
	case *Pig:
		values := map[string]int{"minecraft:temperate": 0, "minecraft:warm": 1, "minecraft:cold": 2}
		b := actor.behaviour()
		check("variant", b.variant, values[variant])
		check("sound", b.soundVariant, sound)
		check("age", b.age, age)
		check("baby", b.Baby(), age < 0)
	case *Sheep:
		check("color", actor.WoolColor(), int(nbtconv.Uint8(source, "Color")))
		check("age", actor.behaviour().ageTicks, age)
		check("baby", actor.behaviour().Baby(), age < 0)
	case *Villager:
		data := source["VillagerData"].(map[string]any)
		check("type", actor.Variant(), data["type"])
		check("profession", actor.Profession(), data["profession"])
		check("baby", actor.Baby(), age < 0)
	}
}
