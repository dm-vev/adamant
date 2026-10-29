package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

type hopperNativeItemBody struct {
	minecartMotionNativeBody
	ActorID string `json:"actor_id"`
	ItemID  string `json:"id"`
	Count   int
}

func TestHopperItemCollectionMatchesBDS(t *testing.T) {
	replayHopperItemsBDS(t, "bedrock_1_26_51_hopper_items.json", 98)
}

func TestHopperItemSlotAndActorOrderMatchesBDS(t *testing.T) {
	replayHopperItemsBDS(t, "bedrock_1_26_51_hopper_item_order.json", 8)
}

func TestHopperItemCollectionWithCoverMatchesBDS(t *testing.T) {
	replayHopperItemsBDS(t, "bedrock_1_26_51_hopper_item_covers.json", 36)
}

func replayHopperItemsBDS(t *testing.T, filename string, count int) {
	data, err := os.ReadFile("../block/testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	type nativeSlot struct {
		ID    string
		Count int
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Initial, Count              int
				Profile                     string
				Above                       string
				Destination, Locked, Second bool
				InitialSlots                []*nativeSlot `json:"initial_slots"`
				Extra                       []struct{ Time int }
			}
			Base, Source cube.Pos
			Destination  *cube.Pos
			SetupTick    int64         `json:"setup_tick"`
			LoadControl  []*nativeSlot `json:"load_control"`
			Spawns       []struct {
				Tick int64
				Body hopperNativeItemBody
			}
			Samples []struct {
				Tick             int64
				Block            pistonEntityNativeBlock
				Cover            *pistonEntityNativeBlock
				Destination      *pistonEntityNativeBlock
				Items            []*nativeSlot
				DestinationItems []*nativeSlot `json:"destination_items"`
				Drops            []hopperNativeItemBody
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native hopper/item capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/initial%d/drop%d/destination%t/locked%t/second%t", c.Profile, c.Initial, c.Count, c.Destination, c.Locked, c.Second), func(t *testing.T) {
			for i := range trace.Samples {
				cover := trace.Samples[i].Cover
				if cover == nil {
					if c.Above != "" {
						t.Fatal("missing native cover control")
					}
					continue
				}
				if cardinal, ok := cover.States["minecraft:cardinal_direction"].(string); ok {
					faces := map[string]int{"north": 2, "south": 3, "west": 4, "east": 5}
					if face, ok := cover.States["facing_direction"].(float64); ok {
						if int(face) != faces[cardinal] {
							t.Fatal("native cover facing aliases disagree")
						}
						delete(cover.States, "facing_direction")
					}
					if cover.ID == "minecraft:lectern" {
						if cardinal != "south" || cover.States["direction"] != float64(0) {
							t.Fatal("unexpected native lectern direction aliases")
						}
						delete(cover.States, "direction")
					}
				}
				if cover.ID == "minecraft:white_shulker_box" {
					if cover.States["color"] != "white" {
						t.Fatal("native shulker colour alias disagrees with ID")
					}
					delete(cover.States, "color")
				}
			}
			wantSpawns := 1
			if c.Second {
				wantSpawns++
			}
			wantSpawns += len(c.Extra)
			if len(trace.Samples) != 120 || len(trace.Spawns) != wantSpawns || len(trace.LoadControl) != 5 || trace.Spawns[0].Tick != trace.SetupTick+40 {
				t.Fatal("invalid native hopper/item controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			if err := w.SetGameRule("spawn_mobs", false); err != nil {
				t.Fatal(err)
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := -8; x <= 8; x++ {
					for z := -8; z <= 8; z++ {
						tx.SetBlock(cube.Pos{trace.Base[0] + x, 64, trace.Base[2] + z}, block.Bedrock{}, nil)
					}
				}
				h := block.NewHopper()
				h.Facing = cube.FaceNorth
				tx.SetBlock(trace.Base, h, nil)
				if c.Above != "" {
					tx.SetBlock(trace.Base.Side(cube.FaceUp), hopperItemCover(t, c.Above), nil)
				}
				if trace.Destination != nil {
					tx.SetBlock(*trace.Destination, block.NewBarrel(), nil)
				}
				if c.Locked {
					tx.SetBlock(trace.Source, block.RedstoneBlock{}, nil)
				}
			})
			handles := make(map[string]*world.EntityHandle)
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native hopper/item clock mismatch")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, frame.Block)
					if frame.Cover != nil {
						comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base.Side(cube.FaceUp), *frame.Cover)
					}
					compareInv := func(pos cube.Pos, want []*nativeSlot) {
						slots := tx.Block(pos).(block.Container).Inventory(tx, pos).Slots()
						if len(slots) != len(want) {
							t.Fatal("native hopper container size differs")
						}
						for i, stack := range slots {
							if want[i] == nil {
								if !stack.Empty() {
									t.Errorf("tick %d %v slot %d got %v, want empty", frame.Tick, pos, i, stack)
								}
								continue
							}
							id := ""
							if !stack.Empty() {
								id, _ = stack.Item().EncodeItem()
							}
							if id != want[i].ID || stack.Count() != want[i].Count {
								t.Errorf("tick %d %v slot %d got %v, native=%+v", frame.Tick, pos, i, stack, want[i])
							}
						}
					}
					compareInv(trace.Base, frame.Items)
					if trace.Destination != nil {
						if frame.Destination == nil {
							t.Fatal("missing native destination snapshot")
						}
						comparePistonEntityNativeBlock(t, tx, frame.Tick, *trace.Destination, *frame.Destination)
						compareInv(*trace.Destination, frame.DestinationItems)
					}
					active := make(map[*world.EntityHandle]*Ent)
					for e := range tx.Entities() {
						if e.H().Type() == ItemType {
							active[e.H()] = e.(*Ent)
						}
					}
					if len(active) != len(frame.Drops) {
						t.Fatalf("tick %d dropped actors=%d, native=%d", frame.Tick, len(active), len(frame.Drops))
					}
					for _, body := range frame.Drops {
						e, ok := active[handles[body.ActorID]]
						if !ok {
							t.Fatal("native item identity was lost or replaced")
						}
						stack := e.Behaviour().(*ItemBehaviour).Item()
						id, _ := stack.Item().EncodeItem()
						if id != body.ItemID || stack.Count() != body.Count {
							t.Errorf("tick %d item=%v, native=%s x%d", frame.Tick, stack, body.ItemID, body.Count)
						}
						compareMinecartMotionNativeBody(t, frame.Tick, e, e.Position(), body.minecartMotionNativeBody)
					}
					if frame.Tick == trace.SetupTick+40 {
						inv := tx.Block(trace.Base).(block.Container).Inventory(tx, trace.Base)
						left := c.Initial
						if c.InitialSlots != nil {
							if len(c.InitialSlots) != inv.Size() {
								t.Fatal("invalid native initial slot count")
							}
							for slot, stack := range c.InitialSlots {
								if stack != nil {
									it, ok := world.ItemByName(stack.ID, 0)
									if !ok {
										t.Fatal("unknown native initial slot item")
									}
									if err := inv.SetItem(slot, item.NewStack(it, stack.Count)); err != nil {
										t.Fatal(err)
									}
								}
							}
						}
						for i := 0; i < inv.Size() && left > 0; i++ {
							n := min(left, 64)
							if err := inv.SetItem(i, item.NewStack(item.Diamond{}, n)); err != nil {
								t.Fatal(err)
							}
							left -= n
						}
						if left != 0 {
							t.Fatal("invalid initial hopper capacity")
						}
						compareInv(trace.Base, trace.LoadControl)
					}
					for _, spawn := range trace.Spawns {
						if spawn.Tick == frame.Tick {
							if handles[spawn.Body.ActorID] != nil {
								t.Fatal("duplicate native item identity")
							}
							it, ok := world.ItemByName(spawn.Body.ItemID, 0)
							if !ok {
								t.Fatal("unknown native item")
							}
							h := NewItem(world.EntitySpawnOpts{Position: spawn.Body.Position.vec(), Velocity: spawn.Body.Velocity.vec()}, item.NewStack(it, spawn.Body.Count))
							e := tx.AddEntity(h)
							handles[spawn.Body.ActorID] = h
							compareMinecartMotionNativeBody(t, frame.Tick, e, e.Position(), spawn.Body.minecartMotionNativeBody)
						}
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}

func hopperItemCover(t *testing.T, name string) world.Block {
	t.Helper()
	switch name {
	case "minecraft:stone":
		return block.Stone{}
	case "minecraft:glass":
		return block.Glass{}
	case "minecraft:chest":
		b := block.NewChest()
		b.Facing = cube.North
		return b
	case "minecraft:ender_chest":
		b := block.NewEnderChest()
		b.Facing = cube.North
		return b
	case "minecraft:barrel":
		return block.NewBarrel()
	case "minecraft:furnace":
		return block.NewFurnace(cube.South)
	case "minecraft:blast_furnace":
		return block.NewBlastFurnace(cube.South)
	case "minecraft:smoker":
		return block.NewSmoker(cube.South)
	case "minecraft:brewing_stand":
		return block.NewBrewingStand()
	case "minecraft:dispenser":
		return block.NewDispenser()
	case "minecraft:dropper":
		return block.NewDropper()
	case "minecraft:crafter":
		b := block.NewCrafter()
		b.Facing = cube.FaceDown
		b.Top = cube.FaceEast
		return b
	case "minecraft:shulker_box":
		b := block.NewShulkerBox()
		b.Type = block.WhiteShulkerBox()
		return b
	case "minecraft:chiseled_bookshelf":
		b := block.NewChiseledBookshelf()
		b.Facing = cube.South
		return b
	case "minecraft:composter":
		return block.Composter{}
	case "minecraft:decorated_pot":
		return block.DecoratedPot{Facing: cube.South}
	case "minecraft:jukebox":
		return block.Jukebox{}
	case "minecraft:lectern":
		return block.Lectern{Facing: cube.South}
	default:
		t.Fatalf("unknown native hopper cover %s", name)
		return nil
	}
}
