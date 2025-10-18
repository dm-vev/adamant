package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func newTestWorld(t *testing.T) *World {
	t.Helper()
	conf := Config{
		Dim:       Overworld,
		Provider:  NopProvider{},
		Generator: NopGenerator{},
	}
	w := conf.New()
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Fatalf("failed closing world: %v", err)
		}
	})
	return w
}

func withTx(t *testing.T, w *World, f func(tx *Tx)) {
	t.Helper()
	done := w.Exec(func(tx *Tx) {
		f(tx)
	})
	<-done
}

func buildObsidianFrame(tx *Tx, corner cube.Pos, width, height int, axis cube.Axis) {
	obsidian := obsidianBlock()
	// Build the horizontal caps.
	for x := -1; x <= width; x++ {
		base := corner.Add(axisOffset(axis, x))
		tx.SetBlock(base.Add(cube.Pos{0, -1, 0}), obsidian, nil)
		tx.SetBlock(base.Add(cube.Pos{0, height, 0}), obsidian, nil)
	}
	// Build the vertical posts.
	for y := 0; y < height; y++ {
		tx.SetBlock(corner.Add(axisOffset(axis, -1)).Add(cube.Pos{0, y, 0}), obsidian, nil)
		tx.SetBlock(corner.Add(axisOffset(axis, width)).Add(cube.Pos{0, y, 0}), obsidian, nil)
	}
	// Ensure the interior is clear.
	air := airBlock()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			tx.SetBlock(corner.Add(axisOffset(axis, x)).Add(cube.Pos{0, y, 0}), air, nil)
		}
	}
}

func airBlock() Block {
	b, ok := BlockByName("minecraft:air", nil)
	if !ok {
		panic("minecraft:air block state not registered")
	}
	return b
}

func TestTryCreateNetherPortalFillsInterior(t *testing.T) {
	w := newTestWorld(t)
	axes := []cube.Axis{cube.X, cube.Z}
	for i, axis := range axes {
		axis := axis
		t.Run(axis.String(), func(t *testing.T) {
			corner := cube.Pos{int(i*16 + 8), 64, int(i*16 + 8)}
			width, height := 2, 3
			withTx(t, w, func(tx *Tx) {
				buildObsidianFrame(tx, corner, width, height, axis)
				if !TryCreateNetherPortal(tx, corner) {
					t.Fatalf("expected portal creation to succeed for axis %v", axis)
				}
				for y := 0; y < height; y++ {
					for x := 0; x < width; x++ {
						pos := corner.Add(axisOffset(axis, x)).Add(cube.Pos{0, y, 0})
						name, props := tx.Block(pos).EncodeBlock()
						if name != "minecraft:nether_portal" {
							t.Fatalf("expected portal block at %v, got %s", pos, name)
						}
						if got := props["portal_axis"]; got != axis.String() {
							t.Fatalf("expected portal axis %q at %v, got %v", axis.String(), pos, got)
						}
					}
				}
				frame, ok := NetherPortalFrameAt(tx, corner, axis)
				if !ok {
					t.Fatalf("expected to resolve portal frame for axis %v", axis)
				}
				if frame.Width() != width || frame.Height() != height {
					t.Fatalf("unexpected frame dimensions: got %dx%d, want %dx%d", frame.Width(), frame.Height(), width, height)
				}
				if frame.Corner() != corner {
					t.Fatalf("unexpected frame corner: got %v, want %v", frame.Corner(), corner)
				}
			})
		})
	}
}

func TestTryCreateNetherPortalRejectsUnderSizedFrame(t *testing.T) {
	w := newTestWorld(t)
	axis := cube.Z
	corner := cube.Pos{200, 64, 200}
	width, height := 1, 3
	withTx(t, w, func(tx *Tx) {
		buildObsidianFrame(tx, corner, width, height, axis)
		if TryCreateNetherPortal(tx, corner) {
			t.Fatal("expected portal creation to fail for undersized frame")
		}
		for y := 0; y < height; y++ {
			pos := corner.Add(cube.Pos{0, y, 0})
			name, _ := tx.Block(pos).EncodeBlock()
			if name != "minecraft:air" {
				t.Fatalf("expected air to remain at %v, got %s", pos, name)
			}
		}
	})
}
