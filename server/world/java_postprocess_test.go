package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

var postprocessTestPos = cube.Pos{8, 64, 8}

type postprocessTestGenerator struct {
	blocks map[cube.Pos]world.BlockState
	actors []chunk.BlockEntity
	ticks  []chunk.ScheduledTick
}

func (postprocessTestGenerator) DefaultSpawn(world.Dimension) cube.Pos { return postprocessTestPos }
func (g postprocessTestGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	var result world.GenerationResult
	if pos != (world.ChunkPos{}) {
		return result
	}
	for pos, state := range g.blocks {
		native, err := world.ResolveState(world.DefaultBlockRegistry, state)
		if err != nil {
			panic(err)
		}
		world.SetNativeState(c, uint8(pos[0]), int16(pos[1]), uint8(pos[2]), native)
	}
	result.BlockEntities = g.actors
	result.ScheduledTicks = g.ticks
	result.MarkForPostProcessing(postprocessTestPos, c.Range())
	return result
}

func TestGenerationPostprocessingSourceShapeTransitions(t *testing.T) {
	state := func(name string, properties map[string]any) world.BlockState {
		return world.BlockState{Name: "minecraft:" + name, Properties: properties}
	}
	p := postprocessTestPos
	for _, tc := range []struct {
		name   string
		blocks map[cube.Pos]world.BlockState
		want   world.BlockState
	}{
		{
			name: "dripleaf stem retains waterlogging",
			blocks: map[cube.Pos]world.BlockState{
				p:                     state("big_dripleaf", map[string]any{"waterlogged": true, "facing": "east"}),
				p.Side(cube.FaceDown): state("clay", nil),
				p.Side(cube.FaceUp):   state("big_dripleaf", nil),
			},
			want: state("big_dripleaf_stem", map[string]any{"waterlogged": true, "facing": "east"}),
		},
		{
			name: "cave vine head retains berries",
			blocks: map[cube.Pos]world.BlockState{
				p:                   state("cave_vines_plant", map[string]any{"berries": true}),
				p.Side(cube.FaceUp): state("stone", nil),
			},
			want: state("cave_vines", map[string]any{"berries": true}),
		},
		{
			name: "attached stem requires soil despite fruit",
			blocks: map[cube.Pos]world.BlockState{
				p:                     state("attached_pumpkin_stem", map[string]any{"facing": "east"}),
				p.Side(cube.FaceEast): state("pumpkin", nil),
			},
			want: state("air", nil),
		},
		{
			name:   "stem requires its own substrate tag",
			blocks: map[cube.Pos]world.BlockState{p: state("pumpkin_stem", nil), p.Side(cube.FaceDown): state("dirt", nil)},
			want:   state("air", nil),
		},
		{
			name:   "wither rose support override",
			blocks: map[cube.Pos]world.BlockState{p: state("wither_rose", nil), p.Side(cube.FaceDown): state("netherrack", nil)},
			want:   state("wither_rose", nil),
		},
		{
			name:   "fungus support override",
			blocks: map[cube.Pos]world.BlockState{p: state("crimson_fungus", nil), p.Side(cube.FaceDown): state("crimson_nylium", nil)},
			want:   state("crimson_fungus", nil),
		},
		{
			name:   "cactus flower support override",
			blocks: map[cube.Pos]world.BlockState{p: state("cactus_flower", nil), p.Side(cube.FaceDown): state("cactus", nil)},
			want:   state("cactus_flower", nil),
		},
		{
			name: "contained water is not a liquid block above cactus",
			blocks: map[cube.Pos]world.BlockState{
				p: state("cactus", nil), p.Side(cube.FaceDown): state("sand", nil),
				p.Side(cube.FaceUp): state("oak_slab", map[string]any{"type": "top", "waterlogged": true}),
			},
			want: state("cactus", nil),
		},
		{
			name: "chorus flower accepts cave air",
			blocks: map[cube.Pos]world.BlockState{
				p: state("chorus_flower", nil), p.Side(cube.FaceDown): state("cave_air", nil),
				p.Side(cube.FaceEast): state("chorus_plant", nil),
			},
			want: state("chorus_flower", nil),
		},
		{
			name: "chorus plant accepts void air",
			blocks: map[cube.Pos]world.BlockState{
				p: state("chorus_plant", nil), p.Side(cube.FaceDown): state("void_air", nil),
				p.Side(cube.FaceUp): state("stone", nil), p.Side(cube.FaceEast): state("chorus_plant", nil),
				p.Side(cube.FaceEast).Side(cube.FaceDown): state("end_stone", nil),
			},
			want: state("chorus_plant", map[string]any{"east": true}),
		},
		{
			name: "upward vine uses upper support face",
			blocks: map[cube.Pos]world.BlockState{
				p: state("vine", map[string]any{"up": true}), p.Side(cube.FaceUp): state("oak_slab", map[string]any{"type": "top"}),
			},
			want: state("vine", map[string]any{"up": true}),
		},
		{
			name: "bottom slab cannot support upward vine",
			blocks: map[cube.Pos]world.BlockState{
				p: state("vine", map[string]any{"up": true}), p.Side(cube.FaceUp): state("oak_slab", map[string]any{"type": "bottom"}),
			},
			want: state("air", nil),
		},
		{
			name: "wire normalizes stale cross and ignores vertical observer",
			blocks: map[cube.Pos]world.BlockState{
				p:                     state("redstone_wire", map[string]any{"north": "side", "south": "side", "west": "side", "east": "side"}),
				p.Side(cube.FaceDown): state("stone", nil), p.Side(cube.FaceEast): state("repeater", map[string]any{"facing": "east"}),
				p.Side(cube.FaceWest): state("observer", map[string]any{"facing": "up"}),
			},
			want: state("redstone_wire", map[string]any{"north": "none", "south": "none", "west": "side", "east": "side"}),
		},
		{
			name:   "path survives fence gate above",
			blocks: map[cube.Pos]world.BlockState{p: state("dirt_path", nil), p.Side(cube.FaceUp): state("oak_fence_gate", nil)},
			want:   state("dirt_path", nil),
		},
		{
			name: "gate ignores vertical neighbors",
			blocks: map[cube.Pos]world.BlockState{
				p:                      state("oak_fence_gate", map[string]any{"facing": "east"}),
				p.Side(cube.FaceNorth): state("cobblestone_wall", nil), p.Side(cube.FaceSouth): state("cobblestone_wall", nil),
			},
			want: state("oak_fence_gate", map[string]any{"in_wall": true}),
		},
		{
			name:   "standing lantern uses support below",
			blocks: map[cube.Pos]world.BlockState{p: state("lantern", nil), p.Side(cube.FaceDown): state("stone", nil)},
			want:   state("lantern", map[string]any{"hanging": false}),
		},
		{
			name:   "hanging lantern uses support above",
			blocks: map[cube.Pos]world.BlockState{p: state("lantern", map[string]any{"hanging": true}), p.Side(cube.FaceUp): state("stone", nil)},
			want:   state("lantern", map[string]any{"hanging": true}),
		},
		{
			name:   "wall connects to perpendicular gate",
			blocks: map[cube.Pos]world.BlockState{p: state("cobblestone_wall", nil), p.Side(cube.FaceNorth): state("oak_fence_gate", map[string]any{"facing": "east"})},
			want:   state("cobblestone_wall", map[string]any{"north": "low"}),
		},
		{
			name: "door adopts different wood upper half",
			blocks: map[cube.Pos]world.BlockState{
				p: state("oak_door", map[string]any{"half": "lower"}), p.Side(cube.FaceDown): state("stone", nil),
				p.Side(cube.FaceUp): state("spruce_door", map[string]any{"half": "upper", "facing": "west", "open": true}),
			},
			want: state("spruce_door", map[string]any{"half": "lower", "facing": "west", "open": true}),
		},
		{
			name: "copper chest adopts connected oxidation",
			blocks: map[cube.Pos]world.BlockState{
				p:                     state("copper_chest", map[string]any{"facing": "north", "type": "single"}),
				p.Side(cube.FaceEast): state("oxidized_copper_chest", map[string]any{"facing": "north", "type": "right"}),
			},
			want: state("oxidized_copper_chest", map[string]any{"facing": "north", "type": "left"}),
		},
		{
			name:   "cake loses unsupported base",
			blocks: map[cube.Pos]world.BlockState{p: state("cake", nil)},
			want:   state("air", nil),
		},
		{
			name:   "upward speleothem uses base behind tip",
			blocks: map[cube.Pos]world.BlockState{p: state("pointed_dripstone", map[string]any{"vertical_direction": "up"}), p.Side(cube.FaceDown): state("stone", nil)},
			want:   state("pointed_dripstone", map[string]any{"thickness": "tip"}),
		},
		{
			name:   "downward speleothem uses ceiling behind tip",
			blocks: map[cube.Pos]world.BlockState{p: state("pointed_dripstone", map[string]any{"vertical_direction": "down"}), p.Side(cube.FaceUp): state("stone", nil)},
			want:   state("pointed_dripstone", map[string]any{"thickness": "tip"}),
		},
		{
			name:   "snowy block handles lateral identity updates",
			blocks: map[cube.Pos]world.BlockState{p: state("grass_block", nil), p.Side(cube.FaceUp): state("snow", nil)},
			want:   state("grass_block", map[string]any{"snowy": true}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir()
			db, err := mcdb.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			w := world.Config{Synchronous: true, Provider: db, Generator: postprocessTestGenerator{blocks: tc.blocks}, RandomTickSpeed: -1}.New()
			closed := false
			t.Cleanup(func() {
				if !closed {
					_ = w.Close()
				}
			})
			<-w.Exec(func(tx *world.Tx) {
				for dx := range 3 {
					for dz := range 3 {
						tx.Block(cube.Pos{(dx - 1) * 16, 64, (dz - 1) * 16})
					}
				}
			})
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				id, err := tx.BlockStateID(p)
				if err != nil {
					t.Fatal(err)
				}
				got, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{StateID: id})
				if err != nil {
					t.Fatal(err)
				}
				if got.Name != tc.want.Name {
					t.Errorf("postprocessed state = %v, want %v", got, tc.want)
				}
				for key, want := range tc.want.Properties {
					if got.Properties[key] != want {
						t.Errorf("%s = %v, want %v", key, got.Properties[key], want)
					}
				}
			})
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			closed = true
			db, err = mcdb.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			column, err := db.LoadColumn(world.ChunkPos{}, world.Overworld)
			if err != nil {
				t.Fatal(err)
			}
			for _, tick := range column.ScheduledTicks {
				if tick.Kind == chunk.BlockTick && tick.Pos == p {
					t.Errorf("surviving block received a destruction tick: %#v", tick)
				}
			}
		})
	}
}

func TestGenerationPostprocessingCopperChestActor(t *testing.T) {
	for _, kind := range []string{"single", "left"} {
		t.Run(kind, func(t *testing.T) {
			p := postprocessTestPos
			other := p.Side(cube.FaceEast)
			first, second := block.NewCopperChest(), block.NewCopperChest()
			first.CustomName = "retained copper"
			if err := first.Inventory(nil, p).SetItem(0, item.NewStack(block.Stone{}, 7)); err != nil {
				t.Fatal(err)
			}
			if err := second.Inventory(nil, other).SetItem(0, item.NewStack(block.Stone{}, 11)); err != nil {
				t.Fatal(err)
			}
			name := "minecraft:copper_chest"
			if kind == "left" {
				name = "minecraft:oxidized_copper_chest"
			}
			generator := postprocessTestGenerator{
				blocks: map[cube.Pos]world.BlockState{
					p:     {Name: name, Properties: map[string]any{"facing": "north", "type": kind}},
					other: {Name: "minecraft:oxidized_copper_chest", Properties: map[string]any{"facing": "north", "type": "right"}},
				},
				actors: []chunk.BlockEntity{{Pos: p, Data: first.EncodeNBT()}, {Pos: other, Data: second.EncodeNBT()}},
			}
			path := t.TempDir()
			open := func() *world.World {
				db, err := mcdb.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				return world.Config{Synchronous: true, Provider: db, Generator: generator, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
			}
			w := open()
			t.Cleanup(func() {
				if w != nil {
					_ = w.Close()
				}
			})
			<-w.Exec(func(tx *world.Tx) {
				for dx := range 3 {
					for dz := range 3 {
						tx.Block(cube.Pos{(dx - 1) * 16, 64, (dz - 1) * 16})
					}
				}
			})
			w.AdvanceTick()
			check := func(tx *world.Tx) {
				chest, ok := tx.Block(p).(block.CopperChest)
				if !ok {
					t.Fatalf("actor type = %T", tx.Block(p))
				}
				if chest.CustomName != "retained copper" {
					t.Fatalf("custom name = %q", chest.CustomName)
				}
				inv := chest.Inventory(tx, p)
				if inv.Size() != 54 {
					t.Fatalf("paired inventory size = %d, want 54", inv.Size())
				}
				count := 0
				for _, stack := range inv.Items() {
					count += stack.Count()
				}
				if count != 18 {
					t.Fatalf("paired contents = %d, want 18", count)
				}
				for pos, want := range map[cube.Pos]string{p: "left", other: "right"} {
					id, err := tx.BlockStateID(pos)
					if err != nil {
						t.Fatal(err)
					}
					state, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{StateID: id})
					if err != nil {
						t.Fatal(err)
					}
					if state.Name != "minecraft:oxidized_copper_chest" || state.Properties["type"] != want {
						t.Fatalf("opened chest source state = %v, want oxidized %s", state, want)
					}
				}
			}
			<-w.Exec(check)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			w = nil
			w = open()
			<-w.Exec(check)
			<-w.Exec(func(tx *world.Tx) {
				chest := tx.Block(p).(block.CopperChest)
				tx.SetBlock(p, block.Air{}, nil)
				chest.BreakInfo().BreakHandler(p, tx, nil)
				remaining, ok := tx.Block(other).(block.CopperChest)
				if !ok {
					t.Fatalf("remaining actor type = %T", tx.Block(other))
				}
				inv := remaining.Inventory(tx, other)
				if inv.Size() != 27 {
					t.Fatalf("remaining inventory size = %d, want 27", inv.Size())
				}
				count := 0
				for _, stack := range inv.Items() {
					count += stack.Count()
				}
				if count != 11 {
					t.Fatalf("remaining half contents = %d, want 11", count)
				}
				if _, ok := tx.Block(p).(block.Air); !ok {
					t.Fatalf("breaking chest recreated %T", tx.Block(p))
				}
			})
		})
	}
}

func TestGenerationPostprocessingActorProjection(t *testing.T) {
	p := postprocessTestPos
	shulker := block.NewShulkerBox()
	shulker.CustomName = "source cargo"
	if err := shulker.Inventory(nil, p).SetItem(0, item.NewStack(block.Stone{}, 17)); err != nil {
		t.Fatal(err)
	}
	banner := block.Banner{
		Attach:   block.StandingAttachment(0),
		Patterns: []block.BannerPatternLayer{{Type: block.BorderBannerPattern(), Colour: item.Colours()[14]}},
	}
	for _, tc := range []struct {
		name   string
		blocks map[cube.Pos]world.BlockState
		actor  map[string]any
		check  func(*testing.T, world.Block)
	}{
		{
			name: "bed occupancy transition retains colour",
			blocks: map[cube.Pos]world.BlockState{
				p:                     {Name: "minecraft:blue_bed", Properties: map[string]any{"facing": "east", "part": "foot"}},
				p.Side(cube.FaceEast): {Name: "minecraft:blue_bed", Properties: map[string]any{"facing": "east", "part": "head", "occupied": true}},
				p.Side(cube.FaceDown): {Name: "minecraft:stone"},
				p.Side(cube.FaceEast).Side(cube.FaceDown): {Name: "minecraft:stone"},
			},
			actor: map[string]any{"id": "Bed", "color": uint8(0)},
			check: func(t *testing.T, b world.Block) {
				bed, ok := b.(block.Bed)
				if !ok {
					t.Errorf("bed actor = %T", b)
					return
				}
				if bed.Colour.Uint8() != 11 || bed.Facing != cube.East || bed.Head || !bed.Occupied {
					t.Errorf("restored bed = %+v", bed)
				}
			},
		},
		{
			name: "banner orientation and colour retain pattern",
			blocks: map[cube.Pos]world.BlockState{
				p:                     {Name: "minecraft:blue_banner", Properties: map[string]any{"rotation": int32(3)}},
				p.Side(cube.FaceDown): {Name: "minecraft:stone"},
			},
			actor: banner.EncodeNBT(),
			check: func(t *testing.T, b world.Block) {
				banner, ok := b.(block.Banner)
				if !ok {
					t.Errorf("banner actor = %T", b)
					return
				}
				if banner.Colour.Uint8() != 11 || banner.Attach != block.StandingAttachment(3) {
					t.Errorf("restored banner state = %+v", banner)
				}
				if len(banner.Patterns) != 1 || banner.Patterns[0].Type != block.BorderBannerPattern() || banner.Patterns[0].Colour.Uint8() != 14 {
					t.Errorf("restored banner patterns = %+v", banner.Patterns)
				}
			},
		},
		{
			name: "shulker orientation and colour retain contents",
			blocks: map[cube.Pos]world.BlockState{
				p: {Name: "minecraft:red_shulker_box", Properties: map[string]any{"facing": "west"}},
			},
			actor: shulker.EncodeNBT(),
			check: func(t *testing.T, b world.Block) {
				shulker, ok := b.(block.ShulkerBox)
				if !ok {
					t.Errorf("shulker actor = %T", b)
					return
				}
				if shulker.Type.String() != "red_shulker_box" || shulker.Facing != cube.FaceWest || shulker.CustomName != "source cargo" {
					t.Errorf("restored shulker state = %+v", shulker)
				}
				stacks := shulker.Inventory(nil, p).Items()
				if len(stacks) != 1 || stacks[0].Count() != 17 {
					t.Errorf("restored shulker contents = %v", stacks)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir()
			generator := postprocessTestGenerator{blocks: tc.blocks, actors: []chunk.BlockEntity{{Pos: p, Data: tc.actor}}}
			open := func() *world.World {
				db, err := mcdb.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				return world.Config{Synchronous: true, Provider: db, Generator: generator, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
			}
			w := open()
			t.Cleanup(func() {
				if w != nil {
					_ = w.Close()
				}
			})
			<-w.Exec(func(tx *world.Tx) {
				for dx := range 3 {
					for dz := range 3 {
						tx.Block(cube.Pos{(dx - 1) * 16, 64, (dz - 1) * 16})
					}
				}
			})
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) { tc.check(t, tx.Block(p)) })
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			w = nil
			w = open()
			<-w.Exec(func(tx *world.Tx) { tc.check(t, tx.Block(p)) })
		})
	}
}

func TestGenerationPostprocessingPlantTickIdentity(t *testing.T) {
	names := []string{"weeping_vines", "twisting_vines", "cave_vines", "kelp"}
	stale := cube.Pos{10, 64, 4}
	generator := postprocessTestGenerator{blocks: make(map[cube.Pos]world.BlockState)}
	for index, name := range names {
		for _, y := range []int{64, 80} {
			pos := cube.Pos{2 + index*2, y, 4}
			state := world.BlockState{Name: "minecraft:" + name}
			if name == "cave_vines" {
				state.Properties = map[string]any{"berries": true}
			}
			generator.blocks[pos] = state
			generator.ticks = append(generator.ticks, chunk.ScheduledTick{Pos: pos, Kind: chunk.BlockTick, Type: state.Name})
			if y == 80 {
				face := cube.FaceUp
				if name == "twisting_vines" || name == "kelp" {
					face = cube.FaceDown
				}
				generator.blocks[pos.Side(face)] = world.BlockState{Name: "minecraft:stone"}
			}
		}
	}
	// Head and body share a native palette state, but a body tick must not
	// execute against a replacement head. Keep it unsupported to observe this.
	generator.blocks[stale] = world.BlockState{Name: "minecraft:weeping_vines"}
	generator.ticks = append(generator.ticks, chunk.ScheduledTick{Pos: stale, Kind: chunk.BlockTick, Type: "minecraft:weeping_vines_plant"})
	path := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Synchronous: true, Provider: db, Generator: generator, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
	}
	w := open()
	t.Cleanup(func() {
		if w != nil {
			_ = w.Close()
		}
	})
	<-w.Exec(func(tx *world.Tx) { tx.Block(stale) })
	for range 5 {
		w.AdvanceTick()
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = nil
	w = open()
	<-w.Exec(func(tx *world.Tx) {
		for index, name := range names {
			pos := cube.Pos{2 + index*2, 64, 4}
			id, err := tx.BlockStateID(pos)
			if err != nil {
				t.Error(err)
				continue
			}
			state, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{StateID: id})
			if err != nil {
				t.Error(err)
				continue
			}
			if state.Name != "minecraft:"+name {
				t.Errorf("cold plant = %s, want %s", state.Name, name)
			}
		}
		for dx := range 3 {
			for dz := range 3 {
				tx.Block(cube.Pos{(dx - 1) * 16, 64, (dz - 1) * 16})
			}
		}
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		for index, name := range names {
			pos := cube.Pos{2 + index*2, 64, 4}
			if name == "kelp" {
				if _, ok := tx.Block(pos).(block.Kelp); ok {
					t.Error("unsupported kelp survived")
				}
				if liquid, ok := tx.Liquid(pos); !ok || liquid.LiquidDepth() != 8 {
					t.Errorf("kelp lost source water: %v", liquid)
				}
			} else if _, ok := tx.Block(pos).(block.Air); !ok {
				t.Errorf("unsupported %s survived as %T", name, tx.Block(pos))
			}
			supported := cube.Pos{pos[0], 80, pos[2]}
			id, err := tx.BlockStateID(supported)
			if err != nil {
				t.Error(err)
				continue
			}
			state, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{StateID: id})
			if err != nil {
				t.Error(err)
				continue
			}
			if state.Name != "minecraft:"+name {
				t.Errorf("supported plant = %s, want %s", state.Name, name)
			}
		}
		if _, ok := tx.Block(stale).(block.WeepingVines); !ok {
			t.Error("stale body tick destroyed replacement head")
		}
		berries := 0
		for e := range tx.Entities() {
			if ent, ok := e.(*entity.Ent); ok {
				if drop, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
					stack := drop.Item()
					if _, ok := stack.Item().(item.GlowBerries); ok {
						berries += stack.Count()
					}
				}
			}
		}
		if berries != 1 {
			t.Errorf("berry drops = %d, want 1", berries)
		}
	})
}
