package entity

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestSmallFireballType(t *testing.T) {
	entityType, ok := DefaultRegistry.Lookup("minecraft:small_fireball")
	if !ok || entityType != SmallFireballType {
		t.Fatalf("small fireball is not registered: %v", entityType)
	}

	box := SmallFireballType.BBox(nil)
	if box.Width() != 0.3125 || box.Height() != 0.3125 || box.Length() != 0.3125 {
		t.Fatalf("unexpected small fireball bounding box: %#v", box)
	}

	data := &world.EntityData{}
	SmallFireballType.DecodeNBT(map[string]any{}, data)
	if _, ok := data.Data.(*ProjectileBehaviour); !ok {
		t.Fatalf("unexpected small fireball behaviour: %T", data.Data)
	}
	if nbt := SmallFireballType.EncodeNBT(data); len(nbt) != 0 {
		t.Fatalf("unexpected small fireball NBT: %#v", nbt)
	}
}

func TestSmallFireballHit(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	cowHandle := NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 2, 0}})
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(cowHandle)
		cowEntity, _ := cowHandle.Entity(tx)
		cow := cowEntity.(*Cow)
		result, ok := trace.EntityIntercept(cow, mgl64.Vec3{0, 2.5, -2}, mgl64.Vec3{0, 2.5, 2})
		if !ok {
			t.Fatal("expected ray to hit cow")
		}
		smallFireballHit(nil, tx, result)
		if cow.Health() != 5 || cow.OnFireDuration() != 5*time.Second {
			t.Fatalf("unexpected cow state after hit: health=%v fire=%v", cow.Health(), cow.OnFireDuration())
		}

		pos := cube.Pos{2, 1, 0}
		tx.SetBlock(pos, block.Cobblestone{}, nil)
		blockResult, ok := trace.BlockIntercept(pos, tx, tx.Block(pos), mgl64.Vec3{2.5, 3, 0.5}, mgl64.Vec3{2.5, 1.5, 0.5})
		if !ok {
			t.Fatal("expected ray to hit block")
		}
		smallFireballHit(nil, tx, blockResult)
		if _, ok := tx.Block(pos.Side(cube.FaceUp)).(block.Fire); !ok {
			t.Fatalf("expected fire above hit block, got %T", tx.Block(pos.Side(cube.FaceUp)))
		}
	})
}
