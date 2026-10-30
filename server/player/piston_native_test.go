package player

import (
	"encoding/json"
	"fmt"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

type pistonBreakNativeEvent struct {
	EventType int32
	Position  mgl32.Vec3
	EventData int32
}

type pistonBreakNativeViewer struct {
	world.NopViewer
	br     world.BlockRegistry
	events []pistonBreakNativeEvent
}

func (v *pistonBreakNativeViewer) ViewParticle(pos mgl64.Vec3, p world.Particle) {
	if b, ok := p.(particle.BlockBreak); ok {
		hash, _ := v.br.RuntimeIDToHash(v.br.BlockRuntimeID(b.Block))
		v.events = append(v.events, pistonBreakNativeEvent{v2193.LevelEventParticlesDestroyBlock,
			mgl32.Vec3{float32(pos[0]), float32(pos[1]), float32(pos[2])}, int32(hash)})
	}
}

type pistonPlayerNativeSnapshot struct {
	Base, Head, Source tripwireBreakNativeBlock
	Actor              *struct {
		State    string
		Moving   bool
		Attached []struct{ X, Y, Z int }
	}
}

// Actual player breaks use native before/after absolute clocks. Destruction
// particle payloads/order are checked separately from the final block states.
func TestPistonPlayerBreakMatchesBDS(t *testing.T) {
	for _, mode := range []string{"creative", "survival"} {
		t.Run(mode, func(t *testing.T) { testPistonPlayerNativeBreak(t, mode) })
	}
}

func testPistonPlayerNativeBreak(t *testing.T, mode string) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_piston_player_break_" + mode + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				GameMode string `json:"game_mode"`
				Sticky   bool
				Facing   int
				Target   string
			}
			Base, Head, Source cube.Pos
			Feet               mgl64.Vec3
			SetupTick          int64 `json:"setup_tick"`
			Changes            []struct {
				GameTick int64 `json:"game_tick"`
				Held     string
				Before   pistonPlayerNativeSnapshot
			}
			Results []struct {
				GameTick int64 `json:"game_tick"`
				After    pistonPlayerNativeSnapshot
			}
			Samples []struct {
				Tick     int64
				GameTick int64 `json:"game_tick"`
				pistonPlayerNativeSnapshot
			}
			LevelEvents []pistonBreakNativeEvent `json:"level_events"`
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 24 {
		t.Fatal("incomplete native piston player-break fixture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("sticky%t/face%d/%s", c.Sticky, c.Facing, c.Target), func(t *testing.T) {
			if c.GameMode != mode || len(trace.Changes) != 1 || len(trace.Results) != 1 || len(trace.Samples) < 60 {
				t.Fatal("invalid native piston player-break case")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.Overworld, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			viewer := &pistonBreakNativeViewer{br: world.DefaultBlockRegistry}
			loader := world.NewLoader(2, w, viewer)
			t.Cleanup(func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() })
			if err := w.SetGameRule("block_drops", false); err != nil {
				t.Fatal(err)
			}
			if err := w.SetGameRule("spawn_mobs", false); err != nil {
				t.Fatal(err)
			}
			var handle *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, mgl64.Vec3{.5, 65, .5})
				loader.Load(tx, 25)
				for x := -5; x <= 7; x++ {
					for z := -5; z <= 7; z++ {
						tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(trace.Base, block.Piston{Facing: cube.Face(c.Facing), Sticky: c.Sticky}, nil)
				tx.SetBlock(trace.Source, block.RedstoneBlock{}, nil)
				var gameMode world.GameMode = world.GameModeCreative
				if mode == "survival" {
					gameMode = world.GameModeSurvival
				}
				handle = (world.EntitySpawnOpts{Position: trace.Feet}).New(Type, Config{Name: "RedstoneAudit", Position: trace.Feet, GameMode: gameMode})
				p := tx.AddEntity(handle).(*Player)
				if mode == "survival" {
					p.SetHeldItems(item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1), item.Stack{})
				}
			})
			compare := func(tx *world.Tx, tick int64, s pistonPlayerNativeSnapshot) {
				compareTripwireBreakNative(t, tx, tick, trace.Base, s.Base)
				compareTripwireBreakNative(t, tx, tick, trace.Head, s.Head)
				compareTripwireBreakNative(t, tx, tick, trace.Source, s.Source)
				p, ok := tx.Block(trace.Base).(block.Piston)
				if s.Actor == nil {
					if ok {
						t.Errorf("tick %d unexpected piston actor", tick)
					}
					return
				}
				if !ok {
					t.Errorf("tick %d missing native piston actor", tick)
					return
				}
				states := [...]string{"Retracted", "Expanding", "Expanded", "Retracting"}
				if int(p.State) >= len(states) || states[p.State] != s.Actor.State || p.Moving != s.Actor.Moving {
					t.Errorf("tick %d actor=%d/%t, native=%s/%t", tick, p.State, p.Moving, s.Actor.State, s.Actor.Moving)
				}
				want := make([]cube.Pos, 0, len(s.Actor.Attached))
				for _, a := range s.Actor.Attached {
					want = append(want, cube.Pos{a.X, a.Y, a.Z})
				}
				if !slices.Equal(p.Attached, want) {
					t.Errorf("tick %d attached=%v, native=%v", tick, p.Attached, want)
				}
			}
			applied, after := 0, 0
			for _, s := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != s.Tick {
						t.Fatalf("kernel tick %d, native %d", tx.CurrentTick(), s.Tick)
					}
					compare(tx, s.Tick, s.pistonPlayerNativeSnapshot)
					if s.GameTick == trace.Changes[0].GameTick {
						compare(tx, s.Tick, trace.Changes[0].Before)
						p, ok := handle.Entity(tx)
						if !ok {
							t.Fatal("missing native break player")
						}
						player := p.(*Player)
						held, _ := player.HeldItems()
						id := "minecraft:air"
						if !held.Empty() {
							id, _ = held.Item().EncodeItem()
						}
						if id != trace.Changes[0].Held {
							t.Fatal("held item differs from native")
						}
						pos := trace.Base
						if c.Target == "head" {
							pos = trace.Head
						}
						player.BreakBlock(pos)
						applied++
					}
					if s.GameTick == trace.Results[0].GameTick {
						compare(tx, s.Tick, trace.Results[0].After)
						after++
					}
				})
				if t.Failed() {
					break
				}
			}
			if t.Failed() {
				return
			}
			if applied != 1 || after != 1 {
				t.Fatalf("applied %d breaks, %d after-event checks", applied, after)
			}
			var expected []pistonBreakNativeEvent
			for _, e := range trace.LevelEvents {
				if e.EventType == v2193.LevelEventParticlesDestroyBlock {
					expected = append(expected, e)
				}
			}
			if len(expected) == 0 {
				t.Fatal("missing native destruction events")
			}
			if !slices.Equal(viewer.events, expected) {
				t.Errorf("destruction events=%v, native=%v", viewer.events, expected)
			}
		})
	}
}
