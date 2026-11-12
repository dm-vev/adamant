package world_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

// checkpointWorld is a world over an mcdb.DB saved by checkpoints committed
// as a server commits them.
type checkpointWorld struct {
	t  *testing.T
	db *mcdb.DB
	w  *world.World
	id uint64
}

func newCheckpointWorld(t *testing.T) *checkpointWorld {
	t.Helper()
	db, err := (mcdb.Config{}).Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Generator: world.NopGenerator{}, RandomTickSpeed: -1, ChunkUnloadInterval: time.Hour}.New()
	t.Cleanup(func() { _ = w.Close() })
	return &checkpointWorld{t: t, db: db, w: w}
}

// checkpoint captures the world into a commit of its DB, runs between, if
// not nil, after the capture and before the write, then writes and commits
// the checkpoint. It returns the columns captured.
func (cw *checkpointWorld) checkpoint(between func()) int {
	cw.t.Helper()
	commit := cw.db.BeginCommit()
	cps, err := world.CaptureAllInto(context.Background(), []*world.World{cw.w}, commit, nil)
	if err != nil {
		cw.t.Fatal(err)
	}
	if between != nil {
		between()
	}
	if err := cps[0].Write(); err != nil {
		cw.t.Fatal(err)
	}
	cw.id++
	err = commit.Commit(cw.id)
	cps[0].Committed(err)
	if err != nil {
		cw.t.Fatal(err)
	}
	return cps[0].Columns()
}

// stored reads the column at pos as the DB holds it.
func (cw *checkpointWorld) stored(pos world.ChunkPos) *chunk.Column {
	cw.t.Helper()
	col, err := cw.db.LoadColumn(pos, world.Overworld)
	if err != nil {
		cw.t.Fatalf("load column %v: %v", pos, err)
	}
	return col
}

// storedBlock returns the name of the block at pos as the DB holds it.
func (cw *checkpointWorld) storedBlock(pos cube.Pos) string {
	cw.t.Helper()
	col := cw.stored(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)})
	b, _ := world.BlockByRuntimeID(col.Chunk.Block(uint8(pos[0]&15), int16(pos[1]), uint8(pos[2]&15), 0))
	name, _ := b.EncodeBlock()
	return name
}

// storedItems returns the count of the items in the container at pos, as
// the DB holds it.
func (cw *checkpointWorld) storedItems(pos cube.Pos) int {
	cw.t.Helper()
	col := cw.stored(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)})
	for _, be := range col.BlockEntities {
		if be.Pos != pos {
			continue
		}
		n := 0
		items, _ := be.Data["Items"].([]any)
		for _, it := range items {
			switch c := it.(map[string]any)["Count"].(type) {
			case uint8:
				n += int(c)
			case int8:
				n += int(c)
			case int16:
				n += int(c)
			case int32:
				n += int(c)
			}
		}
		return n
	}
	cw.t.Fatalf("no block entity stored at %v", pos)
	return 0
}

// storedEntities returns how many times the entity with the unique ID
// passed is stored in the columns at the positions passed.
func (cw *checkpointWorld) storedEntities(id int64, positions ...world.ChunkPos) int {
	cw.t.Helper()
	n := 0
	for _, pos := range positions {
		for _, e := range cw.stored(pos).Entities {
			if e.ID == id {
				n++
			}
		}
	}
	return n
}

func addDiamonds(t *testing.T, tx *world.Tx, pos cube.Pos, n int) {
	t.Helper()
	if _, err := tx.Block(pos).(block.Chest).Inventory(tx, pos).AddItem(item.NewStack(item.Diamond{}, n)); err != nil {
		t.Fatalf("add diamonds to the chest at %v: %v", pos, err)
	}
}

// TestCheckpointWritesColumnsAsCaptured checks that a checkpoint writes the
// columns as they were when it captured them, although they changed before
// it wrote them: blocks of a column modified, saved whole, and the contents
// of a chest in a column only touched, saved without its blocks, which the
// DB keeps. The next checkpoint writes the changes.
func TestCheckpointWritesColumnsAsCaptured(t *testing.T) {
	t.Parallel()
	cw := newCheckpointWorld(t)
	a, chest := cube.Pos{1, 10, 1}, cube.Pos{33, 10, 1}
	<-cw.w.Exec(func(tx *world.Tx) {
		tx.SetBlock(a, block.Stone{}, nil)
		tx.SetBlock(chest, block.NewChest(), nil)
		addDiamonds(t, tx, chest, 1)
	})
	cw.checkpoint(nil)

	<-cw.w.Exec(func(tx *world.Tx) {
		tx.SetBlock(a, block.Dirt{}, nil)
		addDiamonds(t, tx, chest, 2)
		if tx.ColumnModified(world.ChunkPos{2, 0}) || !tx.ColumnTouched(world.ChunkPos{2, 0}) {
			t.Error("filling a chest did not touch its column alone")
		}
	})
	n := cw.checkpoint(func() {
		<-cw.w.Exec(func(tx *world.Tx) {
			tx.SetBlock(a, block.Gold{}, nil)
			addDiamonds(t, tx, chest, 4)
		})
	})
	if n != 2 {
		t.Errorf("the checkpoint captured %d columns, want 2", n)
	}
	if got := cw.storedBlock(a); got != "minecraft:dirt" {
		t.Errorf("block stored at %v = %v, want the dirt captured", a, got)
	}
	if got := cw.storedItems(chest); got != 3 {
		t.Errorf("chest stored with %d diamonds, want the 3 captured", got)
	}
	if got := cw.storedBlock(chest); got != "minecraft:chest" {
		t.Errorf("block stored at %v = %v after its column was saved without its blocks, want a chest", chest, got)
	}

	if n := cw.checkpoint(nil); n != 2 {
		t.Errorf("the next checkpoint captured %d columns, want 2", n)
	}
	if got := cw.storedBlock(a); got != "minecraft:gold_block" {
		t.Errorf("block stored at %v by the next checkpoint = %v, want gold", a, got)
	}
	if got := cw.storedItems(chest); got != 7 {
		t.Errorf("chest stored by the next checkpoint with %d diamonds, want 7", got)
	}
	if n := cw.checkpoint(nil); n != 0 {
		t.Errorf("a checkpoint with nothing changed captured %d columns, want none", n)
	}
}

// TestEntityMovingDuringACheckpointIsSavedOnce checks that an entity that
// moves into another column between the capture and the write of a
// checkpoint is stored once, in the column it was captured in, and in the
// one it moved into by the next checkpoint, also once; and that a move
// before a capture stores it once too.
func TestEntityMovingDuringACheckpointIsSavedOnce(t *testing.T) {
	t.Parallel()
	cw := newCheckpointWorld(t)
	from, to, further := world.ChunkPos{0, 0}, world.ChunkPos{1, 0}, world.ChunkPos{2, 0}
	h := entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 20, 8.5}}, item.NewStack(item.Diamond{}, 1))
	// A viewer makes the entity tick, as one near a player does: it moves
	// into the column it was teleported into at its next tick.
	loader := world.NewLoader(4, cw.w, world.NopViewer{})
	<-cw.w.Exec(func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{24, 20, 8})
		loader.Load(tx, 81)
		for _, pos := range []world.ChunkPos{from, to, further} {
			tx.SetBlock(cube.Pos{int(pos[0])*16 + 8, 19, 8}, block.Stone{}, nil)
		}
		tx.AddEntity(h)
	})
	t.Cleanup(func() { <-cw.w.Exec(loader.Close) })
	cw.checkpoint(nil)
	id := h.UniqueID()
	if n := cw.storedEntities(id, from, to, further); n != 1 {
		t.Fatalf("entity stored %d times, want once", n)
	}

	moveTo := func(pos world.ChunkPos) {
		t.Helper()
		<-cw.w.Exec(func(tx *world.Tx) {
			e, ok := h.Entity(tx)
			if !ok {
				t.Fatal("the entity left the world")
			}
			e.(*entity.Ent).Teleport(mgl64.Vec3{float64(pos[0])*16 + 8.5, 20, 8.5})
		})
		// The entity moves into the column at its next tick.
		deadline := time.Now().Add(10 * time.Second)
		for {
			var at world.ChunkPos
			<-cw.w.Exec(func(tx *world.Tx) { at, _ = tx.EntityColumn(h) })
			if at == pos {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("the entity did not move into %v", pos)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	cw.checkpoint(func() { moveTo(to) })
	if n := cw.storedEntities(id, from); n != 1 {
		t.Errorf("entity stored %d times in the column it was captured in, want once", n)
	}
	if n := cw.storedEntities(id, to, further); n != 0 {
		t.Errorf("entity that moved after the capture stored %d times in other columns, want none", n)
	}

	cw.checkpoint(nil)
	if n := cw.storedEntities(id, to); n != 1 {
		t.Errorf("the next checkpoint stored the entity %d times in the column it moved into, want once", n)
	}
	if n := cw.storedEntities(id, from, further); n != 0 {
		t.Errorf("the next checkpoint left the entity %d times in other columns, want none", n)
	}

	moveTo(further)
	cw.checkpoint(nil)
	if n, m := cw.storedEntities(id, further), cw.storedEntities(id, from, to); n != 1 || m != 0 {
		t.Errorf("entity that moved before a capture stored %d times where it moved and %d elsewhere, want once and none", n, m)
	}
}

// TestColumnUnloadedDuringACheckpoint checks that a column unloaded between
// the capture and the write of a checkpoint is stored as captured, then as it
// unloaded by the next checkpoint, and loads with its changes meanwhile; and
// that a column unloaded with nothing changed since its last save is not
// stored again.
func TestColumnUnloadedDuringACheckpoint(t *testing.T) {
	t.Parallel()
	cw := newCheckpointWorld(t)
	a, chest := cube.Pos{1, 10, 1}, cube.Pos{33, 10, 1}
	<-cw.w.Exec(func(tx *world.Tx) {
		tx.SetBlock(a, block.Stone{}, nil)
		tx.SetBlock(chest, block.NewChest(), nil)
		addDiamonds(t, tx, chest, 1)
	})
	cw.checkpoint(nil)

	<-cw.w.Exec(func(tx *world.Tx) {
		tx.SetBlock(a, block.Dirt{}, nil)
		addDiamonds(t, tx, chest, 2)
	})
	cw.checkpoint(func() {
		<-cw.w.Exec(func(tx *world.Tx) {
			tx.SetBlock(a, block.Gold{}, nil)
			addDiamonds(t, tx, chest, 4)
			if n, _, _ := cw.w.CollectGarbage(tx); n != 2 {
				t.Errorf("unloaded %d columns, want 2", n)
			}
		})
	})
	if got, items := cw.storedBlock(a), cw.storedItems(chest); got != "minecraft:dirt" || items != 3 {
		t.Errorf("stored %v and %d diamonds, want the dirt and 3 diamonds captured", got, items)
	}
	<-cw.w.Exec(func(tx *world.Tx) {
		if got := tx.Block(a); got != (block.Gold{}) {
			t.Errorf("block at %v loaded again = %#v, want the gold it unloaded with", a, got)
		}
		// Loaded from staging, and staged again as it unloads.
		if n, _, _ := cw.w.CollectGarbage(tx); n != 1 {
			t.Errorf("unloaded %d columns, want 1", n)
		}
	})
	cw.checkpoint(nil)
	if got, items := cw.storedBlock(a), cw.storedItems(chest); got != "minecraft:gold_block" || items != 7 {
		t.Errorf("next checkpoint stored %v and %d diamonds, want the gold and 7 diamonds the columns unloaded with", got, items)
	}

	// Loaded and unloaded with nothing changed, neither is stored again.
	<-cw.w.Exec(func(tx *world.Tx) {
		tx.Block(a)
		_ = tx.Block(cube.Pos{chest[0], 0, chest[2]})
		cw.w.CollectGarbage(tx)
	})
	if n := cw.checkpoint(nil); n != 0 {
		t.Errorf("a checkpoint after unloading columns that did not change stored %d, want none", n)
	}
}

// BenchmarkCheckpointOwnerPause measures how long a checkpoint holds the
// owner of a world over an mcdb.DB with many columns loaded and few changed
// since the last checkpoint: of the columns loaded, 1,000 hold two items
// each that nothing touched; between two checkpoints 100 columns are
// modified and the chests of 100 others filled. The columns are written
// outside the time measured. owner-ms/op is the time the capture took on
// the owner, which ns/op adds the wait for the owner to reach it to.
func BenchmarkCheckpointOwnerPause(b *testing.B) {
	for _, loaded := range []int{10_000, 50_000} {
		b.Run(fmt.Sprintf("loaded=%d", loaded), func(b *testing.B) {
			db, err := (mcdb.Config{}).Open(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			w := world.Config{Provider: db, Generator: world.NopGenerator{}, RandomTickSpeed: -1, ChunkUnloadInterval: time.Hour}.New()
			defer w.Close()
			side := int(math.Sqrt(float64(loaded)))
			at := func(i int) cube.Pos { return cube.Pos{(i%side)*16 + 1, 10, (i/side)*16 + 1} }
			for start := 0; start < loaded; start += 1000 {
				<-w.Exec(func(tx *world.Tx) {
					for i := start; i < min(start+1000, loaded); i++ {
						tx.Block(at(i))
					}
				})
			}
			<-w.Exec(func(tx *world.Tx) {
				for i := range 1000 {
					for j := range 2 {
						pos := at(i * loaded / 1000).Vec3Middle().Add(mgl64.Vec3{float64(j), 1, 0})
						tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(item.Diamond{}, 1)))
					}
				}
				for i := range 100 {
					tx.SetBlock(at(i*loaded/100+1), block.NewChest(), nil)
				}
			})
			save := func() {
				cps, err := world.CaptureAll(context.Background(), []*world.World{w}, nil)
				if err != nil {
					b.Fatal(err)
				}
				_ = cps[0].Write()
			}
			save()
			blocks := []world.Block{block.Stone{}, block.Dirt{}}
			var held time.Duration
			b.ResetTimer()
			for i := range b.N {
				b.StopTimer()
				<-w.Exec(func(tx *world.Tx) {
					for j := range 100 {
						tx.SetBlock(at(j*loaded/100+2), blocks[i%2], nil)
						pos := at(j*loaded/100 + 1)
						if _, err := tx.Block(pos).(block.Chest).Inventory(tx, pos).AddItem(item.NewStack(item.Diamond{}, 1)); err != nil {
							b.Fatal(err)
						}
					}
				})
				b.StartTimer()
				cps, err := world.CaptureAll(context.Background(), []*world.World{w}, nil)
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				if cps[0].Columns() != 200 {
					b.Fatalf("captured %d columns, want 200", cps[0].Columns())
				}
				held += cps[0].CaptureTime()
				_ = cps[0].Write()
				b.StartTimer()
			}
			// ns/op includes the wait for the transaction the owner runs
			// when the checkpoint starts, such as a tick.
			b.ReportMetric(float64(held.Microseconds())/1000/float64(b.N), "owner-ms/op")
		})
	}
}
