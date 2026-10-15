package world

import (
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

type spectatorViewer struct{ NopViewer }

func (spectatorViewer) Spectating(*Tx) bool { return true }

// TestSpectatorsGenerateChunks checks ChunkMap.skipPlayer: a spectator loads
// chunks while spectators_generate_chunks is on, and with it off only sees the
// chunks something else loaded.
func TestSpectatorsGenerateChunks(t *testing.T) {
	w := Config{Synchronous: true, Provider: NopProvider{}, Generator: NopGenerator{}}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.SetGameRule("spectators_generate_chunks", false); err != nil {
		t.Fatal(err)
	}
	spectator := NewLoader(1, w, spectatorViewer{})
	<-w.Exec(func(tx *Tx) {
		spectator.Move(tx, mgl64.Vec3{})
		spectator.Load(tx, 32)
	})
	if _, ok := spectator.Chunk(ChunkPos{}); ok {
		t.Fatal("spectator loaded a chunk with spectators_generate_chunks off")
	}

	player := NewLoader(0, w, nopViewer{})
	<-w.Exec(func(tx *Tx) {
		player.Move(tx, mgl64.Vec3{})
		player.Load(tx, 32)
		spectator.Load(tx, 32)
	})
	<-w.Exec(func(*Tx) {})
	if _, ok := spectator.Chunk(ChunkPos{}); !ok {
		t.Fatal("spectator did not see a chunk another loader loaded")
	}
	if _, ok := spectator.Chunk(ChunkPos{1, 0}); ok {
		t.Fatal("spectator loaded a chunk nothing else loaded")
	}

	if err := w.SetGameRule("spectators_generate_chunks", true); err != nil {
		t.Fatal(err)
	}
	<-w.Exec(func(tx *Tx) { spectator.Load(tx, 32) })
	<-w.Exec(func(*Tx) {})
	if _, ok := spectator.Chunk(ChunkPos{1, 0}); !ok {
		t.Fatal("spectator did not load chunks with spectators_generate_chunks on")
	}
}
