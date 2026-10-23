package session

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestPreparedVillagerBabyMetadataSurvivesReloadAndGrowth(t *testing.T) {
	path := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Entities: entity.DefaultRegistry, Synchronous: true}.New()
	}
	source := map[string]any{
		"id": "minecraft:villager", "Age": int32(-1), "Health": float32(20),
		"VillagerDataFinalized": byte(1),
		"VillagerData":          map[string]any{"type": "minecraft:desert", "profession": "minecraft:none", "level": int32(1)},
	}
	pos := mgl64.Vec3{8.5, 65, 8.5}
	handle, err := entity.NewFromStructureNBT(source, world.EntitySpawnOpts{Position: pos, SpawnReason: "structure", SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	w := open()
	viewer := testSession("prepared-villager")
	viewer.br = world.DefaultBlockRegistry
	assertBaby := func(metadata protocol.EntityMetadata, want bool) {
		t.Helper()
		if got := metadata.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagBaby); got != want {
			t.Errorf("client Baby=%t, want %t", got, want)
		}
	}
	if err := w.Do(func(tx *world.Tx) {
		villager := tx.AddEntity(handle).(*entity.Villager)
		viewer.ViewEntity(villager)
		added, ok := (<-viewer.packets).(*packet.AddActor)
		if !ok {
			t.Fatal("villager did not produce an AddActor packet")
		}
		assertBaby(added.EntityMetadata, true)
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
		tx.Block(cube.PosFromVec3(pos))
		var villager *entity.Villager
		for actor := range tx.Entities() {
			if actor.H().UniqueID() == handle.UniqueID() {
				villager, _ = actor.(*entity.Villager)
				break
			}
		}
		if villager == nil {
			t.Fatal("prepared villager missing after MCDB reload")
		}
		viewer.ViewEntity(villager)
		added := (<-viewer.packets).(*packet.AddActor)
		assertBaby(added.EntityMetadata, true)
		villager.Tick(tx, 1)
		if got := villager.Age(); got != 0 {
			t.Fatalf("age after growth tick=%d, want 0", got)
		}
		viewer.ViewEntityState(villager)
		grown := (<-viewer.packets).(*packet.SetActorData)
		assertBaby(grown.EntityMetadata, false)
		villager.SetAge(2400)
		viewer.ViewEntityState(villager)
		cooldown := (<-viewer.packets).(*packet.SetActorData)
		assertBaby(cooldown.EntityMetadata, false)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
