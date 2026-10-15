package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSetBlockEntityDropsCachedPayloads changes a block entity without a
// block update. Chunk and sub-chunk responses must not keep serving the
// block entity data cached before the change.
func TestSetBlockEntityDropsCachedPayloads(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	l := world.NewLoader(0, w, world.NopViewer{})
	t.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { l.Close(tx) })
		_ = w.Close()
	})
	pos := cube.Pos{1, 64, 1}
	ind := int16(64>>4) - int16(w.Range()[0]>>4)
	<-w.Exec(func(tx *world.Tx) {
		l.Move(tx, mgl64.Vec3{1, 64, 1})
		l.Load(tx, 1)
		tx.SetBlock(pos.Side(cube.FaceDown), block.Stone{}, nil)
		tx.SetBlock(pos, block.Bell{Attach: block.StandingBellAttachment()}, nil)
	})
	<-w.Exec(func(tx *world.Tx) {
		col, ok := l.Chunk(world.ChunkPos{0, 0})
		if !ok {
			t.Fatal("chunk not loaded")
		}
		col.CacheSubChunkBlockEntityPayload(ind, []byte{1})
		col.CacheChunkBlockEntityPayload(false, []byte{1})
		tx.SetBlockEntity(pos, block.Bell{Attach: block.StandingBellAttachment(), Powered: false})
		if _, ok := col.CachedSubChunkBlockEntityPayload(ind); ok {
			t.Fatal("sub-chunk block entity payload survived SetBlockEntity")
		}
		if _, ok := col.CachedChunkBlockEntityPayload(false); ok {
			t.Fatal("chunk block entity payload survived SetBlockEntity")
		}
	})
}
