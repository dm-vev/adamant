package session

import (
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// metadataWorld returns a world of the default entity registry that ticks
// only when told to. It logs nothing: entities constructed from their ID
// alone, as a falling block of no block, may fail to save when it closes.
func metadataWorld(tb testing.TB) *world.World {
	w := world.Config{Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	tb.Cleanup(func() { _ = w.Close() })
	return w
}

// registryEntities adds to tx an entity of every type of the default entity
// registry, in the order of their IDs, and returns them. Each is constructed
// from its ID alone, with the options of a spawn, but the text and the
// firework, which Java NBT does not construct.
func registryEntities(tx *world.Tx) ([]world.Entity, error) {
	var ids []string
	for _, t := range entity.DefaultRegistry.Types() {
		ids = append(ids, t.EncodeEntity())
	}
	slices.Sort(ids)
	var ents []world.Entity
	for i, id := range ids {
		opts := world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i%16*4) + 0.5, 100, float64(i/16*4) + 0.5}, SpawnReason: "structure", SpawnFinalized: true}
		var h *world.EntityHandle
		switch id {
		case entity.TextType.EncodeEntity():
			h = entity.NewText("text", opts.Position)
		case entity.FireworkType.EncodeEntity():
			h = entity.NewFirework(opts, item.Firework{})
		default:
			var err error
			if h, err = entity.NewFromStructureNBT(map[string]any{"id": id}, opts); err != nil {
				return nil, fmt.Errorf("%s: %w", id, err)
			}
		}
		ents = append(ents, tx.AddEntity(h))
	}
	return ents, nil
}

// metadataPanics reports whether the session panics writing the metadata
// of e.
func metadataPanics(s *Session, e world.Entity) (panics bool) {
	defer func() { panics = recover() != nil }()
	s.parseEntityMetadata(e)
	return false
}

// BenchmarkViewEntityState measures the metadata a session sends when the
// state of an entity changes, for the first 50 types of the default entity
// registry by ID whose metadata it writes: an op is one update of each.
func BenchmarkViewEntityState(b *testing.B) {
	w := metadataWorld(b)
	s := testSession("view-entity-state")
	s.br = world.DefaultBlockRegistry
	<-w.Exec(func(tx *world.Tx) {
		all, err := registryEntities(tx)
		if err != nil {
			b.Error(err)
			return
		}
		var ents []world.Entity
		for _, e := range all {
			if len(ents) < 50 && !metadataPanics(s, e) {
				ents = append(ents, e)
			}
		}
		if len(ents) < 50 {
			b.Errorf("the session writes the metadata of %d types, want 50", len(ents))
			return
		}
		b.ReportAllocs()
		for b.Loop() {
			for _, e := range ents {
				s.ViewEntityState(e)
			}
			// The session of a test holds what it writes: drop it, so that
			// the backlog does not close the session.
			s.packetsMu.Lock()
			clear(s.heldPackets)
			s.heldPackets = s.heldPackets[:0]
			s.packetsMu.Unlock()
		}
	})
}
