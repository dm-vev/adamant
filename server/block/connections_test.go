package block

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPersistedConnectionsDetermineCollisionArms(t *testing.T) {
	for _, b := range []world.Block{
		GlassPane{NorthConnection: true, EastConnection: true},
		WoodFence{NorthConnection: true, EastConnection: true},
		NetherBrickFence{NorthConnection: true, EastConnection: true},
	} {
		boxes := b.Model().BBox(cube.Pos{}, nil)
		for _, sample := range []struct {
			point    mgl64.Vec3
			collides bool
		}{
			{mgl64.Vec3{0.5, 0.75, 0.1}, true},
			{mgl64.Vec3{0.9, 0.75, 0.5}, true},
			{mgl64.Vec3{0.5, 0.75, 0.5}, true},
			{mgl64.Vec3{0.5, 0.75, 0.9}, false},
			{mgl64.Vec3{0.1, 0.75, 0.5}, false},
			{mgl64.Vec3{0.1, 0.75, 0.1}, false},
		} {
			collides := false
			for _, box := range boxes {
				collides = collides || box.Vec3Within(sample.point)
			}
			if collides != sample.collides {
				t.Errorf("%T collision at %v = %t, want %t", b, sample.point, collides, sample.collides)
			}
		}
	}
}

func TestAllThinAndFenceConnectionStatesAreUnique(t *testing.T) {
	families := []struct {
		name   string
		states []world.Block
	}{
		{"glass pane", allGlassPane()},
		{"stained glass pane", allStainedGlassPane()},
		{"hardened glass pane", allHardenedGlassPane()},
		{"hardened stained glass pane", allHardenedStainedGlassPanes()},
		{"iron bars", allIronBars()},
		{"copper bars", allCopperBars()},
		{"wood fence", allFence()},
		{"nether brick fence", allNetherBrickFence()},
	}
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			seen := make(map[string]struct{}, len(family.states))
			counts := make(map[string]int)
			for _, state := range family.states {
				name, properties := state.EncodeBlock()
				key := fmt.Sprint(name, properties)
				if _, exists := seen[key]; exists {
					t.Fatalf("duplicate connection state: %s", key)
				}
				seen[key] = struct{}{}
				counts[name]++
			}
			for name, count := range counts {
				if count != 16 {
					t.Errorf("%s variant states = %d, want 16", name, count)
				}
			}
		})
	}
}

func TestThinAndFenceNeighbourUpdatesTrackChangesAndPreserveState(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 64, 0}

	runWorld(w, func(tx *world.Tx) {
		pane := StainedGlassPane{NorthConnection: true, Colour: item.ColourMagenta()}
		tx.SetBlock(pos, pane, nil)
		tx.SetBlock(pos.Side(cube.FaceEast), Stone{}, nil)
		pane.NeighbourUpdateTick(pos, cube.Pos{}, tx)
		got := tx.Block(pos).(StainedGlassPane)
		if !got.EastConnection || got.NorthConnection || got.SouthConnection || got.WestConnection || got.Colour != pane.Colour {
			t.Fatalf("pane did not recompute neighbors preserving variant: %#v", got)
		}
		tx.SetBlock(pos.Side(cube.FaceEast), Air{}, nil)
		got.NeighbourUpdateTick(pos, cube.Pos{}, tx)
		got = tx.Block(pos).(StainedGlassPane)
		if got.EastConnection {
			t.Fatalf("pane retained connection after neighbor removal: %#v", got)
		}

		fence := WoodFence{Wood: OakWood(), NorthConnection: true}
		tx.SetBlock(pos, fence, nil)
		tx.SetBlock(pos.Side(cube.FaceWest), Stone{}, nil)
		fence.NeighbourUpdateTick(pos, cube.Pos{}, tx)
		gotFence := tx.Block(pos).(WoodFence)
		if !gotFence.WestConnection || gotFence.NorthConnection || gotFence.EastConnection || gotFence.SouthConnection || gotFence.Wood != fence.Wood {
			t.Fatalf("fence did not recompute neighbors preserving variant: %#v", gotFence)
		}
		tx.SetBlock(pos.Side(cube.FaceWest), Air{}, nil)
		gotFence.NeighbourUpdateTick(pos, cube.Pos{}, tx)
		gotFence = tx.Block(pos).(WoodFence)
		if gotFence.WestConnection {
			t.Fatalf("fence retained connection after neighbor removal: %#v", gotFence)
		}
	})
}
func TestMissingConnectionReconstructionChangesOnlyAbsentFields(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 64, 0}

	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceNorth), Stone{}, nil)
		tx.SetBlock(pos.Side(cube.FaceEast), Stone{}, nil)
		pane := StainedGlassPane{
			Colour: item.ColourMagenta(), NorthConnection: false, EastConnection: false,
			SouthConnection: true, WestConnection: true,
		}
		reconstructed := pane.ReconstructMissingState(pos, []string{"minecraft:connection_east"}, tx).(StainedGlassPane)
		if reconstructed.EastConnection != true || reconstructed.NorthConnection || !reconstructed.SouthConnection ||
			!reconstructed.WestConnection || reconstructed.Colour != pane.Colour {
			t.Fatalf("pane reconstruction changed explicit fields or lost variant: %#v", reconstructed)
		}

		fence := WoodFence{NorthConnection: false, EastConnection: true, SouthConnection: true, WestConnection: false}
		reconstructedFence := fence.ReconstructMissingState(pos, []string{"minecraft:connection_north"}, tx).(WoodFence)
		if !reconstructedFence.NorthConnection || !reconstructedFence.EastConnection || !reconstructedFence.SouthConnection ||
			reconstructedFence.WestConnection {
			t.Fatalf("fence reconstruction changed explicit fields: %#v", reconstructedFence)
		}

		tx.SetBlock(pos.Side(cube.FaceEast), TripwireHook{Facing: cube.West}, nil)
		wire := Tripwire{
			Attached: true, Disarmed: true, Powered: true, Suspended: true,
			NorthConnection: true, EastConnection: false, SouthConnection: true, WestConnection: false,
		}
		reconstructedWire := wire.ReconstructMissingState(pos, []string{"minecraft:connection_east"}, tx).(Tripwire)
		if !reconstructedWire.EastConnection || !reconstructedWire.NorthConnection || !reconstructedWire.SouthConnection ||
			reconstructedWire.WestConnection || !reconstructedWire.Attached || !reconstructedWire.Disarmed ||
			!reconstructedWire.Powered || !reconstructedWire.Suspended {
			t.Fatalf("tripwire reconstruction changed explicit or non-connection state: %#v", reconstructedWire)
		}
		unchanged := wire.ReconstructMissingState(pos, []string{"other_missing_property"}, tx).(Tripwire)
		if unchanged != wire {
			t.Fatalf("unrelated missing property changed tripwire: got %#v, want %#v", unchanged, wire)
		}
	})
}

func TestPaneAndFencePlacementComputesPersistedConnections(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 64, 0}

	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceNorth), Stone{}, nil)
		tx.SetBlock(pos.Side(cube.FaceEast), Stone{}, nil)
		ctx := &item.UseContext{CountSub: 1}
		if !(GlassPane{}).UseOnBlock(pos, cube.FaceUp, mgl64.Vec3{}, tx, nil, ctx) {
			t.Fatal("pane placement was rejected")
		}
		pane := tx.Block(pos).(GlassPane)
		if !pane.NorthConnection || !pane.EastConnection || pane.SouthConnection || pane.WestConnection {
			t.Fatalf("placed pane has incorrect connections: %#v", pane)
		}

		tx.SetBlock(pos, Air{}, nil)
		ctx = &item.UseContext{CountSub: 1}
		if !(WoodFence{Wood: OakWood()}).UseOnBlock(pos, cube.FaceUp, mgl64.Vec3{}, tx, nil, ctx) {
			t.Fatal("fence placement was rejected")
		}
		fence := tx.Block(pos).(WoodFence)
		if !fence.NorthConnection || !fence.EastConnection || fence.SouthConnection || fence.WestConnection {
			t.Fatalf("placed fence has incorrect connections: %#v", fence)
		}
	})
}
