package world

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// TestTransactionsLeaveNoLightToSpread generates chunks on workers and
// checks that every transaction of the world starts with the light of the
// chunks that became complete before it already spread, so that a tick never
// has to spread the light of every chunk generated since the last one.
func TestTransactionsLeaveNoLightToSpread(t *testing.T) {
	w := Config{Generator: stoneTestGenerator{}, GeneratorWorkers: 2, RandomTickSpeed: -1, Provider: NopProvider{}}.New()
	defer w.Close()
	loader := NewLoader(6, w, NopViewer{})
	<-w.Exec(func(tx *Tx) { loader.Move(tx, mgl64.Vec3{8, 80, 8}) })
	deadline := time.Now().Add(time.Minute)
	checked, spread := 0, false
	for time.Now().Before(deadline) {
		ready := 0
		<-w.Exec(func(tx *Tx) {
			if n := len(w.pendingSpreads); n > 0 {
				t.Errorf("transaction started with the light of %d areas still to spread", n)
			}
			checked++
			loader.Load(tx, 64)
			for _, c := range w.chunks {
				if c.Ready() {
					ready++
				}
			}
			spread = spread || ready > 9
		})
		if ready >= 100 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !spread || checked < 20 {
		t.Fatalf("only %d transactions checked while chunks generated", checked)
	}
}

// stoneTestGenerator generates stone up to y 60 with glowstone here and
// there, whose light spreads into the chunks around. It takes a while for
// each chunk, so that chunks keep becoming complete between transactions.
type stoneTestGenerator struct{}

func (stoneTestGenerator) GenerateChunk(pos ChunkPos, c *chunk.Chunk, _ GenerationContext) GenerationResult {
	time.Sleep(2 * time.Millisecond)
	stone, _ := chunk.StateToRuntimeID("minecraft:stone", nil)
	glow, _ := chunk.StateToRuntimeID("minecraft:glowstone", nil)
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(c.Range().Min()); y < 60; y++ {
				c.SetBlock(x, y, z, 0, stone)
			}
		}
	}
	c.SetBlock(0, 60, 0, 0, glow)
	c.SetBlock(15, 30, 15, 0, glow)
	return GenerationResult{}
}

func (stoneTestGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{8, 61, 8} }
