package session

import (
	"fmt"
	"io"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// lifecycleWorld returns a world of the default entity registry with a stone
// floor at y 64 about the origin, and a session to view its entities.
func lifecycleWorld(t testing.TB) (*world.World, *Session) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -8; x < 8; x++ {
			for z := -8; z < 8; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
	})
	viewer := testSession("lifecycle")
	viewer.br = world.DefaultBlockRegistry
	return w, viewer
}

// entityTypeNames returns the names of every type of the default entity
// registry, sorted.
func entityTypeNames() []string {
	var names []string
	for _, t := range entity.DefaultRegistry.Types() {
		names = append(names, t.EncodeEntity())
	}
	slices.Sort(names)
	return names
}

// liveThrough adds the entity of h to w, merges patch into its native NBT
// if not nil, has viewer view it, ticks it, saves it and closes it, as a
// world and its clients do over the life of an entity. It returns the step
// that panicked and its value, if one did.
func liveThrough(w *world.World, viewer *Session, h *world.EntityHandle, patch map[string]any, ticks int) (step string, err any) {
	<-w.Exec(func(tx *world.Tx) {
		defer func() {
			if r := recover(); r != nil {
				err = r
			}
		}()
		step = "add"
		e := tx.AddEntity(h)
		if patch != nil {
			step = "merge native nbt"
			_ = h.MergeNBT(tx, patch)
			if e, _ = h.Entity(tx); e == nil {
				return
			}
		}
		step = "view"
		viewer.parseEntityMetadata(e)
		viewer.ViewEntity(e)
		step = "save"
		tx.World().EntityNBT(h)
		step = "tick"
		for i := range ticks {
			e, ok := h.Entity(tx)
			if !ok {
				break
			}
			if ticker, ok := e.(world.TickerEntity); ok {
				ticker.Tick(tx, int64(i))
			}
		}
		e, ok := h.Entity(tx)
		if !ok {
			return
		}
		step = "save after ticking"
		tx.World().EntityNBT(h)
		step = "view again"
		viewer.parseEntityMetadata(e)
		step = "close"
		if c, ok := e.(io.Closer); ok {
			_ = c.Close()
		} else {
			tx.RemoveEntity(e)
		}
	})
	return step, err
}

// TestEveryEntityTypeLivesThroughItsLife spawns every type of the default
// entity registry, from its spawner and from Java NBT with nothing but its
// id, and views, ticks, saves and closes it: none of it may panic.
func TestEveryEntityTypeLivesThroughItsLife(t *testing.T) {
	t.Parallel()
	w, viewer := lifecycleWorld(t)
	opts := func() world.EntitySpawnOpts {
		return world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 0.5}}
	}
	spawnMob := entity.DefaultRegistry.Config().SpawnMob
	for _, name := range entityTypeNames() {
		sources := map[string]func() *world.EntityHandle{
			"java nbt": func() *world.EntityHandle {
				h, err := entity.NewFromStructureNBT(map[string]any{"id": name}, opts())
				if err != nil {
					return nil
				}
				return h
			},
			"spawner": func() *world.EntityHandle { return spawnMob(name, opts()) },
		}
		for _, source := range []string{"java nbt", "spawner"} {
			var h *world.EntityHandle
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s from %s: spawning panicked: %v", name, source, r)
					}
				}()
				h = sources[source]()
			}()
			if h == nil {
				continue
			}
			if step, err := liveThrough(w, viewer, h, nil, 100); err != nil {
				t.Errorf("%s from %s: %s panicked: %v", name, source, step, err)
			}
		}
	}
}

// lifecyclePanic formats a panic of liveThrough for a fuzz failure.
func lifecyclePanic(name, step string, err any) string {
	return fmt.Sprintf("%s: %s panicked: %v", name, step, err)
}
