package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
)

type circuitReloadProvider struct {
	world.NopProvider
	columns map[world.ChunkPos]*chunk.Column
}

func (p *circuitReloadProvider) LoadColumn(pos world.ChunkPos, _ world.Dimension) (*chunk.Column, error) {
	if c := p.columns[pos]; c != nil {
		return c, nil
	}
	return nil, leveldb.ErrNotFound
}

func (p *circuitReloadProvider) StoreColumn(pos world.ChunkPos, _ world.Dimension, col *chunk.Column) error {
	p.columns[pos] = col
	return nil
}

func TestRepeaterCircuitInstalledFromSavedChunk(t *testing.T) {
	p := &circuitReloadProvider{NopProvider: world.NopProvider{Set: (world.NopProvider{}).Settings()}, columns: make(map[world.ChunkPos]*chunk.Column)}
	conf := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: p}
	pos := cube.Pos{0, 65, 0}
	w := conf.New()
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(pos, RedstoneRepeater{Facing: cube.West, Delay: 3}, nil)
		tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// A closed provider cannot be retained again. Reopen the same stored
	// columns through a fresh provider, as a real database reopen would do.
	conf.Provider = &circuitReloadProvider{NopProvider: world.NopProvider{Set: p.Settings().Snapshot()}, columns: p.columns}
	w = conf.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		_ = tx.Block(pos)
		_ = tx.Block(pos.Side(cube.FaceWest))
	})
	for range 16 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if !tx.Block(pos).(RedstoneRepeater).Powered {
			t.Error("saved repeater never acquired its capacitor")
		}
		tx.SetBlock(pos.Side(cube.FaceWest), Air{}, nil)
	})
	for range 16 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if tx.Block(pos).(RedstoneRepeater).Powered {
			t.Error("reloaded capacitor did not react to input removal")
		}
	})
}

func TestRepeaterPublicationRespectsReplacementAndDelay(t *testing.T) {
	for _, scenario := range []string{"removed", "replaced", "rotated", "delay"} {
		t.Run(scenario, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End}.New()
			t.Cleanup(func() { _ = w.Close() })
			pos := cube.Pos{0, 65, 0}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
				tx.SetBlock(pos, RedstoneRepeater{Facing: cube.West}, nil)
				tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
			})
			// The even circuit step has evaluated, but the following chunk
			// publication has not yet happened.
			for w.CurrentTick()&1 != 0 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				switch scenario {
				case "removed":
					tx.SetBlock(pos, Stone{}, nil)
				case "replaced":
					tx.SetBlock(pos, Air{}, nil)
					tx.SetBlock(pos, RedstoneRepeater{Facing: cube.West}, nil)
				case "rotated":
					tx.SetBlock(pos, RedstoneRepeater{Facing: cube.East}, nil)
				case "delay":
					tx.SetBlock(pos, RedstoneRepeater{Facing: cube.West, Delay: 3}, nil)
				}
			})
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				b := tx.Block(pos)
				if scenario == "removed" {
					if _, ok := b.(Stone); !ok {
						t.Error("publication restored a removed repeater")
					}
					return
				}
				r := b.(RedstoneRepeater)
				if scenario == "delay" {
					if r.Delay != 3 || !r.Powered {
						t.Errorf("publication lost the delay change or output: %+v", r)
					}
				} else if r.Powered {
					t.Error("publication powered a replacement component")
				}
			})
		})
	}
}

type cancelCircuitOutput struct{ world.NopHandler }

func (cancelCircuitOutput) HandleRedstoneUpdate(ctx *world.Context, update world.RedstoneUpdate) {
	if _, ok := update.After.(RedstoneRepeater); ok {
		ctx.Cancel()
	}
}

func TestRepeaterCircuitOutputCancellation(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 65, 0}
	w.Handle(cancelCircuitOutput{})
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(pos, RedstoneRepeater{Facing: cube.West}, nil)
		tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
	})
	for range 12 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if tx.Block(pos).(RedstoneRepeater).Powered {
			t.Error("cancelled output was published")
		}
	})
	w.Handle(world.NopHandler{})
	for range 12 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if !tx.Block(pos).(RedstoneRepeater).Powered {
			t.Error("cancelled output could not be retried")
		}
	})
}

func TestRepeaterDelayActivationRefreshesCapacitor(t *testing.T) {
	for _, rising := range []bool{false, true} {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End}.New()
		t.Cleanup(func() { _ = w.Close() })
		pos := cube.Pos{0, 65, 0}
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
			tx.SetBlock(pos, RedstoneRepeater{Facing: cube.West, Delay: 3}, nil)
			if !rising {
				tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
			}
		})
		for range 20 {
			w.AdvanceTick()
		}
		<-w.Exec(func(tx *world.Tx) {
			// Native RepeaterBlock::use calls updateDelay immediately. A
			// 4->1 activation must update the capacitor, unlike /setblock.
			r := tx.Block(pos).(RedstoneRepeater)
			r.Activate(pos, cube.FaceUp, tx, nil, nil)
			var source world.Block = Air{}
			if rising {
				source = RedstoneBlock{}
			}
			tx.SetBlock(pos.Side(cube.FaceWest), source, nil)
		})
		want := 2 + int(1-(w.CurrentTick()&1))
		got := ticksUntil(w, 20, func(tx *world.Tx) bool { return tx.Block(pos).(RedstoneRepeater).Powered == rising })
		if got != want {
			t.Errorf("rising=%v: activated 1-step delay switched after %d ticks, want %d", rising, got, want)
		}
	}
}
