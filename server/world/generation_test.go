package world_test

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

type payloadGenerator struct {
	handle *world.EntityHandle
}

func (*payloadGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 70, 0} }

func (g *payloadGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) world.GenerationResult {
	if pos != (world.ChunkPos{2, 0}) {
		return world.GenerationResult{}
	}
	c.SetBlock(4, 64, 4, 0, world.BlockRuntimeID(block.NewEndGateway()))
	water := world.BlockRuntimeID(block.Water{Depth: 8, Still: true})
	c.SetBlock(8, 64, 8, 0, water)
	target := mgl64.Vec3{20, 90, -30}
	g.handle = (world.EntitySpawnOpts{Position: mgl64.Vec3{34.5, 70, 2.5}}).New(entity.EndCrystalType, entity.EndCrystalBehaviourConfig{ShowBase: true, BeamTarget: &target})
	return world.GenerationResult{
		Entities:        []*world.EntityHandle{g.handle},
		BlockEntities:   []chunk.BlockEntity{{Pos: cube.Pos{36, 64, 4}, Data: generatedGatewayNBT()}},
		ScheduledBlocks: []chunk.ScheduledBlockUpdate{{Pos: cube.Pos{40, 64, 8}, Block: water, Tick: 3}},
	}
}

func generatedGatewayNBT() map[string]any {
	return map[string]any{
		"Age": int64(73), "TeleportCooldown": int32(11), "ExactTeleport": uint8(1),
		"ExitPortal": map[string]any{"X": int32(-500), "Y": int32(72), "Z": int32(600)},
	}
}

func checkGeneratedPayload(t *testing.T, w *world.World) {
	t.Helper()
	task := w.Do(func(tx *world.Tx) {
		gateway, ok := tx.Block(cube.Pos{36, 64, 4}).(block.EndGateway)
		if !ok {
			t.Error("generated gateway missing")
			return
		}
		want := block.NewEndGateway().DecodeNBT(generatedGatewayNBT()).(block.EndGateway).EncodeNBT()
		if got := gateway.EncodeNBT(); !reflect.DeepEqual(got, want) {
			t.Errorf("generated gateway NBT = %v, want %v", got, want)
		}
		count := 0
		for e := range tx.Entities() {
			count++
			crystal, ok := e.(*entity.EndCrystal)
			if !ok {
				t.Errorf("unexpected generated entity %T", e)
				continue
			}
			beam, has := crystal.BeamTarget()
			if !crystal.ShowBase() || !has || beam != (mgl64.Vec3{20, 90, -30}) {
				t.Errorf("crystal properties lost: base=%t beam=%v present=%t", crystal.ShowBase(), beam, has)
			}
		}
		if count != 1 {
			t.Errorf("generated entities = %d, want exactly one", count)
		}
	})
	<-task.Done()
	if err := task.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedPayloadPersistsWithoutDuplicateActivation(t *testing.T) {
	for _, synchronous := range []bool{true, false} {
		name := "worker"
		if synchronous {
			name = "synchronous"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := (mcdb.Config{}).Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			g := &payloadGenerator{}
			w := world.Config{Provider: db, Generator: g, Entities: entity.DefaultRegistry, Synchronous: synchronous, GeneratorWorkers: 1, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			checkGeneratedPayload(t, w)
			checkGeneratedPayload(t, w)
			w.Save()
			checkGeneratedPayload(t, w)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if !g.handle.Closed() {
				t.Error("generated entity was not closed")
			}

			db, err = (mcdb.Config{}).Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			// No generator: everything observed below must come from disk.
			w = world.Config{Provider: db, Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
			checkGeneratedPayload(t, w)
			for i := range 3 {
				w.AdvanceTick()
				task := w.Do(func(tx *world.Tx) {
					_, flowing := tx.Block(cube.Pos{40, 63, 8}).(block.Water)
					if flowing != (i == 2) {
						t.Errorf("water below source after tick %d = %t, want %t", i+1, flowing, i == 2)
					}
				})
				<-task.Done()
				if err := task.Err(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestReadOnlyWorldClosesGeneratedEntities(t *testing.T) {
	g := &payloadGenerator{}
	w := world.Config{Generator: g, Entities: entity.DefaultRegistry, Synchronous: true, ReadOnly: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	checkGeneratedPayload(t, w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !g.handle.Closed() {
		t.Fatal("read-only world leaked generated entity")
	}
}
