package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

type shulkerMetadataViewer struct {
	world.NopViewer
	session *Session
}

func (v shulkerMetadataViewer) ViewEntityState(e world.Entity) { v.session.ViewEntityState(e) }

func TestShulkerClientStateSurvivesReloadAndReattachment(t *testing.T) {
	path := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	}
	// Java numbers the dye colours from white, Bedrock variants from black.
	cases := []struct {
		face                     cube.Face
		javaColour, colour, peek byte
	}{
		{cube.FaceDown, 16, 16, 0},
		{cube.FaceUp, 15, 0, 0},
		{cube.FaceNorth, 0, 15, 73},
		{cube.FaceSouth, 10, 5, 30},
		{cube.FaceWest, 4, 11, 100},
		{cube.FaceEast, 11, 4, 73},
	}
	viewer := testSession("shulker-metadata")
	assertMetadata := func(m v2193.Metadata, pos cube.Pos, face cube.Face, colour, peek byte) {
		t.Helper()
		// Captured BDS 1.26.52.3 keys and wire types: Variant int32,
		// Peek int32, face byte, Attached int16, attachment BlockPos.
		for key, want := range map[uint32]any{
			protocol.EntityDataKeyVariant:          int32(colour),
			protocol.EntityDataKeyPeekID:           int32(peek),
			protocol.EntityDataKeyAttachFace:       byte(face),
			protocol.EntityDataKeyAttached:         int16(1),
			protocol.EntityDataKeyAttachedPosition: protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		} {
			if got := m[key]; got != want {
				t.Errorf("shulker at %v metadata %d = %v (%T), want %v (%T)", pos, key, got, got, want, want)
			}
		}
	}
	w := open()
	defer func() { _ = w.Close() }()
	<-w.Exec(func(tx *world.Tx) {
		for i, tc := range cases {
			pos := cube.Pos{2 + 2*i, 70, 8}
			tx.SetBlock(pos.Side(tc.face), block.Stone{}, nil)
			handle, err := entity.NewFromStructureNBT(map[string]any{
				"id": "minecraft:shulker", "AttachFace": byte(tc.face), "Color": tc.javaColour, "Peek": tc.peek,
			}, world.EntitySpawnOpts{Position: mgl64.Vec3{float64(pos[0]) + .5, 70, 8.5}})
			if err != nil {
				t.Fatal(err)
			}
			shulker := tx.AddEntity(handle)
			viewer.ViewEntity(shulker)
			added := (viewer.testPacket()).(*v2193.AddActorPacket)
			assertMetadata(added.EntityMetadata, pos, tc.face, tc.colour, tc.peek)
		}
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = open()
	<-w.Exec(func(tx *world.Tx) {
		tx.Block(cube.Pos{8, 70, 8})
		found := make([]bool, len(cases))
		var east *entity.Shulker
		for actor := range tx.Entities() {
			shulker, ok := actor.(*entity.Shulker)
			if !ok {
				continue
			}
			pos := cube.PosFromVec3(shulker.Position())
			i := (pos[0] - 2) / 2
			if i < 0 || i >= len(cases) {
				t.Fatalf("unexpected restored shulker at %v", pos)
			}
			found[i] = true
			tc := cases[i]
			viewer.ViewEntity(shulker)
			added := (viewer.testPacket()).(*v2193.AddActorPacket)
			assertMetadata(added.EntityMetadata, pos, tc.face, tc.colour, tc.peek)
			if got := shulker.H().NBT(tx)["Variant"]; got != int32(tc.colour) {
				t.Errorf("saved shell Variant = %v, want %d", got, tc.colour)
			}
			if tc.face == cube.FaceEast {
				east = shulker
			}
		}
		for i, present := range found {
			if !present {
				t.Fatalf("shulker with face %v missing after MCDB reload", cases[i].face)
			}
		}
		loader := world.NewLoader(0, w, shulkerMetadataViewer{session: viewer})
		loader.Move(tx, east.Position())
		loader.Load(tx, 1)
		handle := east.H()
		tx.Defer(func(tx *world.Tx) {
			defer loader.Close(tx)
			actor, _ := handle.Entity(tx)
			east := actor.(*entity.Shulker)
			pos := cube.PosFromVec3(east.Position())
			tx.SetBlock(pos.Side(cube.FaceEast), block.Air{}, nil)
			tx.SetBlock(pos.Side(cube.FaceNorth), block.Stone{}, nil)
			east.Tick(tx, 1)
			if pk := viewer.testPacket(); pk != nil {
				updated, ok := pk.(*v2193.SetActorDataPacket)
				if !ok {
					t.Fatalf("attachment change sent %T, want SetActorData", pk)
				}
				// Shulker.findNewAttachment keeps the shell as open as it was.
				assertMetadata(updated.EntityMetadata, pos, cube.FaceNorth, 4, 73)
			} else {
				t.Fatal("attachment/peek changed without a client metadata update")
			}
		})
	})
}
