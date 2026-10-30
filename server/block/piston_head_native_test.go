package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type nativePistonHeadSnapshot struct {
	Base, Head, Source, Notifier nativeTripwireBlock
	Actor                        *nativePistonActor
}

func TestPistonHeadSurvivalMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_piston_head_survival.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Sticky                bool
				Facing, Phase, Offset int
				Stage, Edit           string
			}
			Base, Head, Source, Notifier cube.Pos
			SetupTick                    int64 `json:"setup_tick"`
			Changes                      []struct {
				Tick    int64
				Powered bool
			}
			Injection struct {
				Tick          int64
				Before, After nativePistonHeadSnapshot
				Edits         []struct {
					Pos   cube.Pos
					Block nativeTripwireBlock
				}
			}
			Samples []struct {
				Tick int64
				nativePistonHeadSnapshot
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 720 {
		t.Fatal("incomplete native piston-head survival fixture")
	}
	byName := make(map[string][]world.Block)
	for _, b := range world.DefaultBlockRegistry.Blocks() {
		name, _ := b.EncodeBlock()
		byName[name] = append(byName[name], b)
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("sticky%t/face%d/phase%d/%s%d/%s", c.Sticky, c.Facing, c.Phase, c.Stage, c.Offset, c.Edit), func(t *testing.T) {
			if len(trace.Samples) != 100 || len(trace.Changes) != 2 || trace.Changes[0].Tick&1 != int64(c.Phase) || trace.Injection.Tick == 0 {
				t.Fatal("invalid native piston-head trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			if err := w.SetGameRule("block_drops", false); err != nil {
				t.Fatal(err)
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(trace.Base, Piston{Facing: cube.Face(c.Facing), Sticky: c.Sticky}, nil)
			})
			compare := func(tx *world.Tx, tick int64, s nativePistonHeadSnapshot) {
				for i, b := range []nativeTripwireBlock{s.Base, s.Head, s.Source, s.Notifier} {
					pos := []cube.Pos{trace.Base, trace.Head, trace.Source, trace.Notifier}[i]
					compareSculkContactNativeBlock(t, tx, tick, pos, b)
				}
				if s.Actor != nil && s.Actor.Attached == nil {
					t.Fatal("missing native piston attached positions")
				}
				comparePistonNativeActor(t, tx, tick, trace.Base, s.Actor)
			}
			applied := 0
			for _, s := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != s.Tick {
						t.Fatal("piston-head replay clock mismatch")
					}
					compare(tx, s.Tick, s.nativePistonHeadSnapshot)
					for _, change := range trace.Changes {
						if change.Tick == s.Tick {
							var b world.Block = Air{}
							if change.Powered {
								b = RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, b, nil)
						}
					}
					if trace.Injection.Tick == s.Tick {
						compare(tx, s.Tick, trace.Injection.Before)
						for _, edit := range trace.Injection.Edits {
							b := pistonNativeBlock(byName, edit.Block)
							if b == nil {
								t.Fatal("unmapped native piston-head edit")
							}
							// A command permutation change retains the live block actor.
							if p, ok := b.(Piston); ok {
								if live, ok := tx.Block(edit.Pos).(Piston); ok && live.Sticky == p.Sticky {
									live.Facing = p.Facing
									b = live
								}
							}
							tx.SetBlock(edit.Pos, b, nil)
						}
						compare(tx, s.Tick, trace.Injection.After)
						applied++
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != 1 {
				t.Fatalf("applied %d native head injections", applied)
			}
		})
	}
}
