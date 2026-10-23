package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func init() { registerModernStructureBuilder("jungle_temple", buildModernJungleTemple) }

const (
	jungleTempleMainChest uint8 = 1 << iota
	jungleTempleHiddenChest
	jungleTempleTrap1
	jungleTempleTrap2
)

type modernJungleTemple struct {
	box structure.Box
	dir int
}

func (p modernJungleTemple) Bounds() structure.Box { return p.box }
func (p modernJungleTemple) pos(x, y, z int32) [3]int32 {
	b := p.box
	switch p.dir {
	case 0:
		return [3]int32{b.Min[0] + x, b.Min[1] + y, b.Max[2] - z}
	case 1:
		return [3]int32{b.Min[0] + z, b.Min[1] + y, b.Min[2] + x}
	case 2:
		return [3]int32{b.Min[0] + x, b.Min[1] + y, b.Min[2] + z}
	default:
		return [3]int32{b.Max[0] - z, b.Min[1] + y, b.Min[2] + x}
	}
}

func (p modernJungleTemple) Place(clip structure.Bounds, env structure.ProcessorEnvironment) (structure.Placement, error) {
	out := structure.Placement{}
	if env.Random == nil {
		return out, fmt.Errorf("jungle temple placement requires structure random")
	}
	var temporary structure.StructurePieceState
	if env.PieceState == nil {
		env.PieceState = &temporary
	}
	ready, err := updateScatteredGroundHeight(&p.box, clip, env)
	if err != nil || !ready {
		return out, err
	}
	flags, rng := env.PieceState, env.Random
	direction := [4]string{"north", "east", "south", "west"}[p.dir]
	journal := newJournalStructureWorld(env, clip, &out)
	put := func(x, y, z int32, name string, props map[string]any) {
		journal.placeSourcePiece(p.pos(x, y, z), world.BlockState{Name: "minecraft:" + name, Properties: props}, direction)
	}
	box := func(x0, y0, z0, x1, y1, z1 int32, name string) {
		state := world.BlockState{Name: "minecraft:" + name}
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				for z := z0; z <= z1; z++ {
					journal.placeSourceState(p.pos(x, y, z), state)
				}
			}
		}
	}
	stone := func(x0, y0, z0, x1, y1, z1 int32) {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				for z := z0; z <= z1; z++ {
					state := world.BlockState{Name: "minecraft:mossy_cobblestone"}
					if rng.NextFloat() < 0.4 {
						state.Name = "minecraft:cobblestone"
					}
					journal.placeSourceState(p.pos(x, y, z), state)
				}
			}
		}
	}
	air := func(x0, y0, z0, x1, y1, z1 int32) { box(x0, y0, z0, x1, y1, z1, "air") }
	var stairs [4]world.BlockState
	for index, facing := range [4]string{"north", "east", "south", "west"} {
		stairs[index] = sourcePieceState(world.BlockState{Name: "minecraft:cobblestone_stairs", Properties: map[string]any{"facing": facing, "half": "bottom", "shape": "straight", "waterlogged": false}}, direction)
	}
	stair := func(x, y, z int32, facing string) {
		index := 0
		switch facing {
		case "east":
			index = 1
		case "south":
			index = 2
		case "west":
			index = 3
		}
		journal.placeSourceState(p.pos(x, y, z), stairs[index])
	}
	chest := func(slot uint8, x, y, z int32) {
		if flags.LootPlaced&slot == 0 && journal.createSourceChest(p.pos(x, y, z), "minecraft:chests/jungle_temple", rng) {
			flags.LootPlaced |= slot
		}
	}
	dispenser := func(slot uint8, x, y, z int32, facing string) {
		if flags.LootPlaced&slot == 0 && journal.createSourceDispenser(p.pos(x, y, z), facing, direction, "minecraft:chests/jungle_temple_dispenser", rng) {
			flags.LootPlaced |= slot
		}
	}

	stone(0, -4, 0, 11, 0, 14)
	stone(2, 1, 2, 9, 2, 2)
	stone(2, 1, 12, 9, 2, 12)
	stone(2, 1, 3, 2, 2, 11)
	stone(9, 1, 3, 9, 2, 11)
	stone(1, 3, 1, 10, 6, 1)
	stone(1, 3, 13, 10, 6, 13)
	stone(1, 3, 2, 1, 6, 12)
	stone(10, 3, 2, 10, 6, 12)
	stone(2, 3, 2, 9, 3, 12)
	stone(2, 6, 2, 9, 6, 12)
	stone(3, 7, 3, 8, 7, 11)
	stone(4, 8, 4, 7, 8, 10)
	air(3, 1, 3, 8, 2, 11)
	air(4, 3, 6, 7, 3, 9)
	air(2, 4, 2, 9, 5, 12)
	air(4, 6, 5, 7, 6, 9)
	air(5, 7, 6, 6, 7, 8)
	air(5, 1, 2, 6, 2, 2)
	air(5, 2, 12, 6, 2, 12)
	air(5, 5, 1, 6, 5, 1)
	air(5, 5, 13, 6, 5, 13)
	for _, v := range [4][3]int32{{1, 5, 5}, {10, 5, 5}, {1, 5, 9}, {10, 5, 9}} {
		put(v[0], v[1], v[2], "air", nil)
	}
	for _, z := range [2]int32{0, 14} {
		for _, x := range [4]int32{2, 4, 7, 9} {
			stone(x, 4, z, x, 5, z)
		}
	}
	stone(5, 6, 0, 6, 6, 0)
	for _, x := range [2]int32{0, 11} {
		for z := int32(2); z <= 12; z += 2 {
			stone(x, 4, z, x, 5, z)
		}
		stone(x, 6, 5, x, 6, 5)
		stone(x, 6, 9, x, 6, 9)
	}
	for _, v := range [4][2]int32{{2, 2}, {9, 2}, {2, 12}, {9, 12}} {
		stone(v[0], 7, v[1], v[0], 9, v[1])
	}
	for _, v := range [4][2]int32{{4, 4}, {7, 4}, {4, 10}, {7, 10}} {
		stone(v[0], 9, v[1], v[0], 9, v[1])
	}
	stone(5, 9, 7, 6, 9, 7)
	stair(5, 9, 6, "north")
	stair(6, 9, 6, "north")
	stair(5, 9, 8, "south")
	stair(6, 9, 8, "south")
	for x := int32(4); x <= 7; x++ {
		stair(x, 0, 0, "north")
	}
	for _, x := range [2]int32{4, 7} {
		for i := int32(1); i <= 3; i++ {
			stair(x, i, 7+i, "north")
		}
	}
	stone(4, 1, 9, 4, 1, 9)
	stone(7, 1, 9, 7, 1, 9)
	stone(4, 1, 10, 7, 2, 10)
	stone(5, 4, 5, 6, 4, 5)
	stair(4, 4, 5, "east")
	stair(7, 4, 5, "west")
	for i := int32(0); i < 4; i++ {
		stair(5, -i, 6+i, "south")
		stair(6, -i, 6+i, "south")
		air(5, -i, 7+i, 6, -i, 9+i)
	}
	air(1, -3, 12, 10, -1, 13)
	air(1, -3, 1, 3, -1, 13)
	air(1, -3, 1, 9, -1, 5)
	for z := int32(1); z <= 13; z += 2 {
		stone(1, -3, z, 1, -2, z)
	}
	for z := int32(2); z <= 12; z += 2 {
		stone(1, -1, z, 3, -1, z)
	}
	stone(2, -2, 1, 5, -2, 1)
	stone(7, -2, 1, 9, -2, 1)
	stone(6, -3, 1, 6, -3, 1)
	stone(6, -1, 1, 6, -1, 1)
	put(1, -3, 8, "tripwire_hook", map[string]any{"facing": "east", "attached": true, "powered": false})
	put(4, -3, 8, "tripwire_hook", map[string]any{"facing": "west", "attached": true, "powered": false})
	for _, x := range [2]int32{2, 3} {
		put(x, -3, 8, "tripwire", map[string]any{"east": true, "west": true, "attached": true})
	}
	wireNS := map[string]any{"north": "side", "south": "side", "east": "none", "west": "none", "power": 0}
	for z := int32(7); z >= 2; z-- {
		put(5, -3, z, "redstone_wire", wireNS)
	}
	put(5, -3, 1, "redstone_wire", map[string]any{"north": "side", "west": "side", "east": "none", "south": "none", "power": 0})
	put(4, -3, 1, "redstone_wire", map[string]any{"east": "side", "west": "side", "north": "none", "south": "none", "power": 0})
	put(3, -3, 1, "mossy_cobblestone", nil)
	dispenser(jungleTempleTrap1, 3, -2, 1, "north")
	put(3, -2, 2, "vine", map[string]any{"south": true})
	put(7, -3, 1, "tripwire_hook", map[string]any{"facing": "north", "attached": true, "powered": false})
	put(7, -3, 5, "tripwire_hook", map[string]any{"facing": "south", "attached": true, "powered": false})
	for z := int32(2); z <= 4; z++ {
		put(7, -3, z, "tripwire", map[string]any{"north": true, "south": true, "attached": true})
	}
	put(8, -3, 6, "redstone_wire", map[string]any{"east": "side", "west": "side", "north": "none", "south": "none", "power": 0})
	put(9, -3, 6, "redstone_wire", map[string]any{"west": "side", "south": "side", "east": "none", "north": "none", "power": 0})
	put(9, -3, 5, "redstone_wire", map[string]any{"north": "side", "south": "up", "east": "none", "west": "none", "power": 0})
	put(9, -3, 4, "mossy_cobblestone", nil)
	put(9, -2, 4, "redstone_wire", wireNS)
	dispenser(jungleTempleTrap2, 9, -2, 3, "west")
	put(8, -1, 3, "vine", map[string]any{"east": true})
	put(8, -2, 3, "vine", map[string]any{"east": true})
	chest(jungleTempleMainChest, 8, -3, 3)
	for _, v := range [9][3]int32{{9, -3, 2}, {8, -3, 1}, {4, -3, 5}, {5, -2, 5}, {5, -1, 5}, {6, -3, 5}, {7, -2, 5}, {7, -1, 5}, {8, -3, 5}} {
		put(v[0], v[1], v[2], "mossy_cobblestone", nil)
	}
	stone(9, -1, 1, 9, -1, 5)
	air(8, -3, 8, 10, -1, 10)
	for x := int32(8); x <= 10; x++ {
		put(x, -2, 11, "chiseled_stone_bricks", nil)
	}
	for x := int32(8); x <= 10; x++ {
		put(x, -2, 12, "lever", map[string]any{"face": "wall", "facing": "north", "powered": false})
	}
	stone(8, -3, 8, 8, -3, 10)
	stone(10, -3, 8, 10, -3, 10)
	put(10, -2, 9, "mossy_cobblestone", nil)
	put(8, -2, 9, "redstone_wire", wireNS)
	put(8, -2, 10, "redstone_wire", wireNS)
	put(10, -1, 9, "redstone_wire", map[string]any{"north": "side", "south": "side", "east": "side", "west": "side", "power": 0})
	put(9, -2, 8, "sticky_piston", map[string]any{"facing": "up", "extended": false})
	put(10, -2, 8, "sticky_piston", map[string]any{"facing": "west", "extended": false})
	put(10, -1, 8, "sticky_piston", map[string]any{"facing": "west", "extended": false})
	put(10, -2, 10, "repeater", map[string]any{"facing": "north", "delay": 1, "locked": false, "powered": false})
	chest(jungleTempleHiddenChest, 9, -3, 10)
	return out, nil
}

func buildModernJungleTemple(in modernStructureBuildInput) (modernStructureStart, bool, error) {
	if in.random == nil {
		return modernStructureStart{}, false, fmt.Errorf("jungle temple requires structure random")
	}
	dir := int(in.random.NextInt(4))
	x, z := in.chunkX*16, in.chunkZ*16
	minX, maxX, minZ, maxZ := x, x+11, z, z+14
	if dir == 1 || dir == 3 {
		maxX, maxZ = x+14, z+11
	}
	box := structure.Box{Min: [3]int32{minX, 64, minZ}, Max: [3]int32{maxX, 73, maxZ}}
	piece := modernJungleTemple{box: box, dir: dir}
	return modernStructureStart{name: in.name, pieces: []modernStructurePiece{piece}, box: box, terrain: terrainAdjustment(stringValue(in.config["terrain_adaptation"]))}, true, nil
}
