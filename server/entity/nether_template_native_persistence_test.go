package entity

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

func TestNetherTemplateActorsRetainSourceStateAcrossMCDB(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_nether_template_spawn.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []netherTemplateSpawnCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	sourceFor := func(id string, babyHoglin bool) map[string]any {
		t.Helper()
		for _, source := range cases {
			if source.ID != id {
				continue
			}
			nbt := sourceSpawnNBT(t, source.NBT)
			nbt["id"] = id
			if id == "minecraft:hoglin" && babyHoglin && nbtconv.Int32(nbt, "Age") >= 0 {
				continue
			}
			return nbt
		}
		t.Fatalf("Source final Nether fixture missing %s (baby=%t)", id, babyHoglin)
		return nil
	}
	piglinNBT := sourceFor("minecraft:piglin", false)
	bruteNBT := sourceFor("minecraft:piglin_brute", false)
	hoglinNBT := sourceFor("minecraft:hoglin", true)

	dir := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Entities: DefaultRegistry, Synchronous: true}.New()
	}
	w := open()
	defer func() { _ = w.Close() }()
	pos := mgl64.Vec3{.5, 65, .5}
	spawn := func(nbt map[string]any) *world.EntityHandle {
		t.Helper()
		h, err := NewFromJavaNBT(nbt, world.EntitySpawnOpts{Position: pos, SpawnReason: "structure", SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		var added *world.EntityHandle
		if err := w.Do(func(tx *world.Tx) { added = tx.AddEntity(h).H() }).Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		return added
	}
	piglinHandle, bruteHandle, hoglinHandle := spawn(piglinNBT), spawn(bruteNBT), spawn(hoglinNBT)
	var piglin *Piglin
	var brute *PiglinBrute
	var hoglin *Hoglin
	if err := w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			switch e.H().UniqueID() {
			case piglinHandle.UniqueID():
				piglin = e.(*Piglin)
			case bruteHandle.UniqueID():
				brute = e.(*PiglinBrute)
			case hoglinHandle.UniqueID():
				hoglin = e.(*Hoglin)
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if piglin == nil || brute == nil || hoglin == nil {
		t.Fatal("registered Source Nether actors were not added to the world")
	}
	checkSourceStack := func(label string, actual item.Stack, raw any) {
		t.Helper()
		want, err := item.StackFromJavaNBT(raw.(map[string]any))
		if err != nil {
			t.Fatal(err)
		}
		gotNBT, err := item.StackToJavaNBT(actual)
		if err != nil {
			t.Fatal(err)
		}
		wantNBT, err := item.StackToJavaNBT(want)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotNBT, wantNBT) {
			t.Errorf("%s=%#v, Source=%#v", label, gotNBT, wantNBT)
		}
	}
	piglinEquipment := piglinNBT["equipment"].(map[string]any)
	main, _ := piglin.HeldItems()
	checkSourceStack("piglin main hand", main, piglinEquipment["mainhand"])
	if piglin.Health() != float64(nbtconv.Float32(piglinNBT, "Health")) {
		t.Fatalf("piglin health=%g, Source=%v", piglin.Health(), piglinNBT["Health"])
	}
	if piglin.p().baby != nbtconv.Bool(piglinNBT, "IsBaby") {
		t.Fatal("Source authored piglin baby/adult state was not applied")
	}
	bruteEquipment := bruteNBT["equipment"].(map[string]any)
	bruteMain, _ := brute.HeldItems()
	checkSourceStack("brute main hand", bruteMain, bruteEquipment["mainhand"])
	if brute.Health() != float64(nbtconv.Float32(bruteNBT, "Health")) {
		t.Fatalf("brute health=%g, Source=%v", brute.Health(), bruteNBT["Health"])
	}
	if !brute.p().homeSet || brute.p().homeDimension != "minecraft:the_nether" || brute.p().home != (cube.Pos{12, 72, -9}) {
		t.Fatalf("brute HOME=%+v dimension=%q set=%t", brute.p().home, brute.p().homeDimension, brute.p().homeSet)
	}
	if hoglin.beast().age != -24000 || hoglin.Health() != float64(nbtconv.Float32(hoglinNBT, "Health")) {
		t.Fatalf("hoglin age/health=%d/%g, Source=%v/%v", hoglin.beast().age, hoglin.Health(), hoglinNBT["Age"], hoglinNBT["Health"])
	}
	// Clearing a mutable native item must take precedence over retained Source
	// gear when the actor is serialized and decoded after restart.
	piglin.SetHeldItems(item.Stack{}, item.Stack{})
	w.Save()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = open()
	if err := w.Do(func(tx *world.Tx) {
		tx.Block(cube.PosFromVec3(pos))
		piglin, brute, hoglin = nil, nil, nil
		for e := range tx.Entities() {
			switch e.H().UniqueID() {
			case piglinHandle.UniqueID():
				piglin = e.(*Piglin)
			case bruteHandle.UniqueID():
				brute = e.(*PiglinBrute)
			case hoglinHandle.UniqueID():
				hoglin = e.(*Hoglin)
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if piglin == nil || brute == nil || hoglin == nil {
		t.Fatal("Source Nether actors were lost on MCDB reopen")
	}
	if got, _ := piglin.HeldItems(); !got.Empty() {
		t.Fatalf("cleared Piglin main hand resurrected after MCDB reopen: %v", got)
	}
	if piglin.Health() != float64(nbtconv.Float32(piglinNBT, "Health")) || piglin.p().baby != nbtconv.Bool(piglinNBT, "IsBaby") {
		t.Fatalf("Piglin health/baby changed on MCDB reopen: %g/%t", piglin.Health(), piglin.p().baby)
	}
	piglinSaved := ReadNativeMobJavaState(PiglinType{}.EncodeNBT(piglin.Ent.data))
	brain, _ := piglinSaved["Brain"].(map[string]any)
	memories, _ := brain["memories"].(map[string]any)
	if _, ok := memories["minecraft:hunted_recently"]; !ok {
		t.Fatalf("Piglin hunted_recently memory lost on MCDB reopen: %#v", brain)
	}
	if !brute.p().homeSet || brute.p().homeDimension != "minecraft:the_nether" || brute.p().home != (cube.Pos{12, 72, -9}) || brute.Health() != float64(nbtconv.Float32(bruteNBT, "Health")) {
		t.Fatalf("Brute state changed on MCDB reopen: HOME=%+v/%q health=%g", brute.p().home, brute.p().homeDimension, brute.Health())
	}
	if hoglin.beast().age != -24000 || hoglin.Health() != float64(nbtconv.Float32(hoglinNBT, "Health")) {
		t.Fatalf("Hoglin state changed on MCDB reopen: age/health=%d/%g", hoglin.beast().age, hoglin.Health())
	}
}
