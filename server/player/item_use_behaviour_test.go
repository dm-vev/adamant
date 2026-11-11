package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// useCase is one run of TestUseItemBehaviour or TestUseItemOnBlockBehaviour: setup puts the player and the world
// in a state, then act uses the item (or the block) and the player ticks ticks times.
type useCase struct {
	name  string
	ticks int
	setup func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler)
	act   func(tx *world.Tx, p *Player)
}

// TestUseItemBehaviour pins what Player.UseItem does over a table of items and states: the events in order,
// the calls the client and a viewer saw, and the state of the player and of its inventory after the use and
// after a few ticks, as recorded before the method was split.
func TestUseItemBehaviour(t *testing.T) {
	hold := func(s item.Stack) func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
		return func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) { p.SetHeldItems(s, item.Stack{}) }
	}
	arrows := func(p *Player) { _, _ = p.Inventory().AddItem(item.NewStack(item.Arrow{}, 64)) }
	use := func(tx *world.Tx, p *Player) { p.UseItem() }
	twice := func(tx *world.Tx, p *Player) { p.UseItem(); p.UseItem() }
	cases := []useCase{
		{name: "empty hand", ticks: 2, act: use},
		{name: "bow", ticks: 30, act: use, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
			arrows(p)
		}},
		{name: "bow repeated", ticks: 10, act: twice, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
			arrows(p)
		}},
		{name: "bow without arrows", ticks: 4, act: use, setup: hold(item.NewStack(item.Bow{}, 1))},
		{name: "crossbow", ticks: 40, act: use, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Crossbow{}, 1), item.Stack{})
			arrows(p)
		}},
		{name: "crossbow loaded", ticks: 4, act: use, setup: hold(item.NewStack(item.Crossbow{Item: item.NewStack(item.Arrow{}, 1)}, 1))},
		{name: "snowball", ticks: 2, act: use, setup: hold(item.NewStack(item.Snowball{}, 16))},
		{name: "snowball twice", ticks: 2, act: twice, setup: hold(item.NewStack(item.Snowball{}, 16))},
		{name: "ender pearl", ticks: 2, act: twice, setup: hold(item.NewStack(item.EnderPearl{}, 16))},
		{name: "egg creative", ticks: 2, act: use, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetGameMode(world.GameModeCreative)
			p.SetHeldItems(item.NewStack(item.Egg{}, 16), item.Stack{})
		}},
		{name: "bread", ticks: 40, act: use, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.hunger.foodLevel = 8
			p.SetHeldItems(item.NewStack(item.Bread{}, 4), item.Stack{})
		}},
		{name: "bread full", ticks: 4, act: use, setup: hold(item.NewStack(item.Bread{}, 4))},
		{name: "potion", ticks: 40, act: use, setup: hold(item.NewStack(item.Potion{Type: potion.Swiftness()}, 1))},
		{name: "splash potion", ticks: 2, act: use, setup: hold(item.NewStack(item.SplashPotion{Type: potion.Swiftness()}, 1))},
		{name: "milk bucket", ticks: 40, act: use, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.AddEffect(effect.New(effect.Speed, 1, time.Minute))
			p.SetHeldItems(item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1), item.Stack{})
		}},
		{name: "water bucket in the air", ticks: 2, act: use, setup: hold(item.NewStack(item.Bucket{Content: item.LiquidBucketContent(block.Water{})}, 1))},
		{name: "fishing rod", ticks: 3, act: twice, setup: hold(item.NewStack(item.FishingRod{}, 1))},
		{name: "spear", ticks: 20, act: use, setup: hold(item.NewStack(item.Spear{Tier: item.ToolTierIron}, 1))},
		{name: "wooden spear", ticks: 3, act: use, setup: hold(item.NewStack(item.Spear{Tier: item.ToolTierWood}, 1))},
		{name: "trident", ticks: 30, act: use, setup: hold(item.NewStack(item.Trident{}, 1))},
		{name: "shield", ticks: 3, act: use, setup: hold(item.NewStack(item.Shield{}, 1))},
		{name: "sword", ticks: 3, act: use, setup: hold(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1))},
		{name: "event cancelled", ticks: 3, act: use, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			h.cancel["ItemUse"] = true
			p.SetHeldItems(item.NewStack(item.Snowball{}, 16), item.Stack{})
		}},
		{name: "cooldown", ticks: 3, act: use, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.EnderPearl{}, 16), item.Stack{})
			p.SetCooldown(item.EnderPearl{}, time.Second)
		}},
		{name: "adventure", ticks: 3, act: use, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetGameMode(world.GameModeAdventure)
			p.SetHeldItems(item.NewStack(item.Snowball{}, 16), item.Stack{})
		}},
		{name: "spectator", ticks: 3, act: use, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetGameMode(world.GameModeSpectator)
			p.SetHeldItems(item.NewStack(item.Snowball{}, 16), item.Stack{})
		}},
		{name: "switch while using", ticks: 4, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
			arrows(p)
			p.UseItem()
		}, act: func(tx *world.Tx, p *Player) {
			p.SetHeldItems(item.NewStack(item.Snowball{}, 16), item.Stack{})
			p.UseItem()
		}},
		{name: "release", ticks: 4, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
			arrows(p)
			p.UseItem()
			for i := range 25 {
				p.Tick(tx, int64(100+i))
			}
		}, act: func(tx *world.Tx, p *Player) { p.ReleaseItem() }},
	}
	tr := &transcript{}
	for _, c := range cases {
		runUseCase(t, tr, c)
	}
	checkTranscript(t, "use_item", tr)
}

// TestUseItemOnBlockBehaviour pins what Player.UseItemOnBlock does over a table of items and blocks, as recorded
// before the method was split.
func TestUseItemOnBlockBehaviour(t *testing.T) {
	ground := tickBenchmarkGround
	floor := cube.Pos{0, int(ground) - 1, 0}
	ahead := cube.Pos{2, int(ground), 0}
	holdAt := func(s item.Stack, blocks map[cube.Pos]world.Block) func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
		return func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(s, item.Stack{})
			for pos, b := range blocks {
				tx.SetBlock(pos, b, nil)
			}
		}
	}
	on := func(pos cube.Pos, face cube.Face) func(tx *world.Tx, p *Player) {
		return func(tx *world.Tx, p *Player) { p.UseItemOnBlock(pos, face, mgl64.Vec3{0.5, 0.5, 0.5}) }
	}
	stone := item.NewStack(block.Stone{}, 16)
	chestAhead := map[cube.Pos]world.Block{ahead: block.NewChest()}
	cases := []useCase{
		{name: "place stone on the floor", act: on(floor.Add(cube.Pos{2, 0, 0}), cube.FaceUp), setup: holdAt(stone, nil)},
		{name: "place stone creative", act: on(floor.Add(cube.Pos{2, 0, 0}), cube.FaceUp), setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetGameMode(world.GameModeCreative)
			p.SetHeldItems(stone, item.Stack{})
		}},
		{name: "place stone last of the stack", act: on(floor.Add(cube.Pos{2, 0, 0}), cube.FaceUp), setup: holdAt(item.NewStack(block.Stone{}, 1), nil)},
		{name: "place stone twice", act: func(tx *world.Tx, p *Player) {
			on(floor.Add(cube.Pos{2, 0, 0}), cube.FaceUp)(tx, p)
			on(floor.Add(cube.Pos{2, 0, 0}), cube.FaceUp)(tx, p)
		}, setup: holdAt(stone, nil)},
		{name: "place stone side", act: on(floor.Add(cube.Pos{2, 0, 0}), cube.FaceNorth), setup: holdAt(stone, nil)},
		{name: "place stone in the player", act: on(floor, cube.FaceUp), setup: holdAt(stone, nil)},
		{name: "place stone into the world's top", act: on(cube.Pos{2, 319, 0}, cube.FaceUp), setup: holdAt(stone, map[cube.Pos]world.Block{{2, 319, 0}: block.Stone{}})},
		{name: "place on replaceable", act: on(ahead, cube.FaceUp), setup: holdAt(stone, map[cube.Pos]world.Block{ahead: block.ShortGrass{}})},
		{name: "place event cancelled", act: on(floor.Add(cube.Pos{2, 0, 0}), cube.FaceUp), setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			h.cancel["BlockPlace"] = true
			p.SetHeldItems(stone, item.Stack{})
		}},
		{name: "use event cancelled", act: on(floor.Add(cube.Pos{2, 0, 0}), cube.FaceUp), setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			h.cancel["ItemUseOnBlock"] = true
			p.SetHeldItems(stone, item.Stack{})
		}},
		{name: "air", act: on(cube.Pos{2, int(ground) + 5, 0}, cube.FaceUp), setup: holdAt(stone, nil)},
		{name: "out of reach", act: on(cube.Pos{40, int(ground) - 1, 0}, cube.FaceUp), setup: holdAt(stone, nil)},
		{name: "open chest", act: on(ahead, cube.FaceWest), setup: holdAt(stone, chestAhead)},
		{name: "open chest empty hand", act: on(ahead, cube.FaceWest), setup: holdAt(item.Stack{}, chestAhead)},
		{name: "sneak place against chest", act: on(ahead, cube.FaceWest), setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			holdAt(stone, chestAhead)(t, tx, p, h)
			p.sneaking = true
		}},
		{name: "lever", act: on(ahead, cube.FaceWest), setup: holdAt(stone, map[cube.Pos]world.Block{ahead: block.Lever{Facing: cube.FaceWest}})},
		{name: "door", act: on(ahead, cube.FaceWest), setup: holdAt(item.Stack{}, map[cube.Pos]world.Block{ahead: block.WoodDoor{Wood: block.OakWood()}})},
		{name: "bell", act: on(ahead, cube.FaceWest), setup: holdAt(item.Stack{}, map[cube.Pos]world.Block{ahead: block.Bell{}})},
		{name: "hoe on grass", act: on(floor, cube.FaceUp), setup: holdAt(item.NewStack(item.Hoe{Tier: item.ToolTierIron}, 1), nil)},
		{name: "shovel on grass", act: on(floor, cube.FaceUp), setup: holdAt(item.NewStack(item.Shovel{Tier: item.ToolTierIron}, 1), nil)},
		{name: "flint and steel", act: on(floor, cube.FaceUp), setup: holdAt(item.NewStack(item.FlintAndSteel{}, 1), nil)},
		{name: "water bucket", act: on(floor, cube.FaceUp), setup: holdAt(item.NewStack(item.Bucket{Content: item.LiquidBucketContent(block.Water{})}, 1), nil)},
		{name: "empty bucket on water", act: on(ahead, cube.FaceUp), setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			holdAt(item.NewStack(item.Bucket{}, 1), nil)(t, tx, p, h)
			tx.SetLiquid(ahead, block.Water{Still: true, Depth: 8})
		}},
		{name: "adventure", act: on(floor.Add(cube.Pos{2, 0, 0}), cube.FaceUp), setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetGameMode(world.GameModeAdventure)
			p.SetHeldItems(stone, item.Stack{})
		}},
	}
	tr := &transcript{}
	for _, c := range cases {
		runUseCase(t, tr, c)
	}
	checkTranscript(t, "use_item_on_block", tr)
}

func runUseCase(t *testing.T, tr *transcript, c useCase) {
	t.Helper()
	w := tickBenchmarkWorld(t)
	viewer := clienttest.New()
	loader := world.NewLoader(2, w, viewer)
	r := clienttest.New()
	start := mgl64.Vec3{0.5, tickBenchmarkGround, 0.5}
	handle := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
		Name: "user", Position: start, GameMode: world.GameModeSurvival, Client: r,
	})
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, start)
		loader.Load(tx, 25)
		p := tx.AddEntity(handle).(*Player)
		th := &traceHandler{tr: tr, cancel: map[string]bool{}}
		p.Handle(th)
		p.data.Rot = cube.Rotation{-90, 0}
		// Past the grace of the join.
		for tick := range int64(62) {
			p.ClientMove(clientMove(p.Position()))
			p.Tick(tx, tick)
		}
		tr.linef("== %s", c.name)
		if c.setup != nil {
			c.setup(t, tx, p, th)
		}
		viewer.Reset()
		r.Reset()
		tr.linef("start %s", digest(p))
		c.act(tx, p)
		tr.linef("acted %s", digest(p))
		tr.calls("viewer", viewer)
		tr.calls("client", r)
		for i := range c.ticks {
			p.Tick(tx, int64(100+i))
		}
		if c.ticks > 0 {
			tr.linef("ticked %s", digest(p))
			tr.calls("viewer", viewer)
			tr.calls("client", r)
		}
		tr.linef("inventory %s", describeInventory(p))
		for _, pos := range []cube.Pos{{2, int(tickBenchmarkGround) - 1, 0}, {2, int(tickBenchmarkGround), 0}, {0, int(tickBenchmarkGround) - 1, 0}} {
			name, props := tx.Block(pos).EncodeBlock()
			liquid, _ := tx.Liquid(pos)
			tr.linef("block %v %s%v liquid=%T", pos, name, props, liquid)
		}
		_ = p.Close()
	})
	<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
}

// describeInventory lists the non-empty slots of the inventory of the player.
func describeInventory(p *Player) string {
	var s string
	for slot, i := range p.Inventory().Slots() {
		if !i.Empty() {
			s += " " + string(rune('0'+slot%10)) + ":" + describeStack(i)
		}
	}
	return s
}
