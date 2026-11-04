package world_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// itemStore is a world provider that keeps the number of items in each
// column it last stored.
type itemStore struct {
	world.NopProvider
	mu    sync.Mutex
	items map[world.ChunkPos]int
}

func (p *itemStore) StoreColumn(pos world.ChunkPos, _ world.Dimension, col *chunk.Column) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.items == nil {
		p.items = make(map[world.ChunkPos]int)
	}
	n := 0
	for _, e := range col.Entities {
		if e.Data["identifier"] == "minecraft:item" {
			n++
		}
	}
	p.items[pos] = n
	return nil
}

// stored returns the number of items in the columns last stored.
func (p *itemStore) stored() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, items := range p.items {
		n += items
	}
	return n
}

// TestTransfersAreHeldDuringCheckpoints moves an item from one world to
// another while they are captured. Captured while it is between them, the
// item is in neither. HoldTransfers waits for it to land, while refusing new
// transfers out of the worlds held: captured then, the item is in its
// destination only. A hold that runs out of time lifts itself.
func TestTransfersAreHeldDuringCheckpoints(t *testing.T) {
	pa := &itemStore{NopProvider: world.NopProvider{WorldData: &world.WorldDataMemory{}}}
	pb := &itemStore{NopProvider: world.NopProvider{WorldData: &world.WorldDataMemory{}}}
	a := world.Config{Provider: pa, Generator: world.NopGenerator{}, RandomTickSpeed: -1}.New()
	b := world.Config{Dim: world.Nether, Provider: pb, Generator: world.NopGenerator{}, RandomTickSpeed: -1}.New()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	worlds := []*world.World{a, b}
	// A save skips columns still generating: the column of the item is
	// generated in both worlds first, as reading a block waits for it.
	pos := mgl64.Vec3{8.5, 10, 8.5}
	for _, w := range worlds {
		<-w.Exec(func(tx *world.Tx) { tx.Block(cube.PosFromVec3(pos)) })
	}
	h := entity.NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(item.Diamond{}, 1))
	<-a.Exec(func(tx *world.Tx) { tx.AddEntity(h) })
	checkpoint := func(step string, inA, inB int) {
		t.Helper()
		cps, err := world.CaptureAll(context.Background(), worlds, nil)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		for _, cp := range cps {
			if err := cp.Write(); err != nil {
				t.Fatalf("%s: %v", step, err)
			}
		}
		if got, gotB := pa.stored(), pb.stored(); got != inA || gotB != inB {
			t.Errorf("%s: the checkpoint holds %d items in the Overworld and %d in the Nether, want %d and %d", step, got, gotB, inA, inB)
		}
	}
	checkpoint("before the transfer", 1, 0)
	transferOut := func(w *world.World) {
		t.Helper()
		<-w.Exec(func(tx *world.Tx) {
			transfer, ok := world.BeginTransfer(tx)
			if !ok {
				t.Fatal("a transfer was refused with no hold")
			}
			e, ok := h.Entity(tx)
			if !ok {
				t.Fatal("the item is not in the world it leaves")
			}
			if transfer.Remove(tx, e) != h {
				t.Fatal("Remove did not return the handle of the item")
			}
		})
	}
	refused := func(w *world.World) (refused bool) {
		<-w.Exec(func(tx *world.Tx) {
			transfer, ok := world.BeginTransfer(tx)
			transfer.Cancel()
			refused = !ok
		})
		return
	}

	transferOut(a)
	checkpoint("between the worlds, without a hold", 0, 0)

	held := make(chan func(), 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		release, err := world.HoldTransfers(ctx, worlds)
		if err != nil {
			t.Errorf("HoldTransfers: %v", err)
		}
		held <- release
	}()
	select {
	case <-held:
		t.Fatal("HoldTransfers returned while the item was between the worlds")
	case <-time.After(100 * time.Millisecond):
	}
	if !refused(a) || !refused(b) {
		t.Error("a transfer out of a world held began")
	}
	<-b.Exec(func(tx *world.Tx) { tx.AddEntity(h) })
	var release func()
	select {
	case release = <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("HoldTransfers did not return once the item landed")
	}
	if release == nil {
		t.FailNow()
	}
	checkpoint("held", 0, 1)
	release()
	release()
	if refused(a) || refused(b) {
		t.Error("a transfer was refused once the hold was lifted")
	}

	// A hold that runs out of time lifts itself.
	transferOut(b)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := world.HoldTransfers(ctx, worlds); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("HoldTransfers with the item between the worlds and 100ms: %v, want the deadline exceeded", err)
	}
	if refused(a) || refused(b) {
		t.Error("a hold that gave up still refuses transfers")
	}
	// Closing the handle ends its transfer too.
	_ = h.Close()
	release, err := world.HoldTransfers(context.Background(), worlds)
	if err != nil {
		t.Fatalf("HoldTransfers once the item was closed: %v", err)
	}
	release()
}
