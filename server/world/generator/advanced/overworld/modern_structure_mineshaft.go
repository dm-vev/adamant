package overworld

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func init() { registerModernStructureBuilder("mineshaft", buildModernMineshaft) }

type minePiece struct {
	box                structure.Box
	kind, facing, wood string
	sections           int
	rails, spider      bool
	depth              int
	entrances          []structure.Box
}

func (p minePiece) Bounds() structure.Box { return p.box }
func (p minePiece) Place(clip structure.Bounds, env structure.ProcessorEnvironment) (structure.Placement, error) {
	if mineInvalid(p, clip, env) {
		return structure.Placement{}, nil
	}
	out := structure.Placement{}
	journal := newJournalStructureWorld(env, clip, &out)
	put := func(x, y, z int32, name string, props map[string]any) {
		if !mineCanBeReplaced(journal, p.wood, x, y, z) {
			return
		}
		state := mineTransformState(p.facing, world.BlockState{Name: "minecraft:" + name, Properties: props})
		if journal.set(x, y, z, state) && (name == "oak_fence" || name == "dark_oak_fence" || name == "torch" || name == "wall_torch") {
			out.PostProcessing = append(out.PostProcessing, cube.Pos{int(x), int(y), int(z)})
		}
	}
	plank, log, fence := "oak_planks", "oak_log", "oak_fence"
	if p.wood == "mesa" {
		plank, log, fence = "dark_oak_planks", "dark_oak_log", "dark_oak_fence"
	}
	min, max := p.box.Min, p.box.Max
	corridorLength := int32(p.sections*5 - 1)
	random := env.Random
	if p.kind == "corridor" {
		for y := int32(0); y <= 1; y++ {
			for x := int32(0); x <= 2; x++ {
				for z := int32(0); z <= corridorLength; z++ {
					wx, wy, wz := mineLocal(p, x, y, z)
					put(wx, wy, wz, "cave_air", nil)
				}
			}
		}
		for y := int32(2); y <= 2; y++ {
			for x := int32(0); x <= 2; x++ {
				for z := int32(0); z <= corridorLength; z++ {
					// generateMaybeBox places unless the draw exceeds the
					// probability.
					if !(random.NextFloat() > 0.8) {
						wx, wy, wz := mineLocal(p, x, y, z)
						put(wx, wy, wz, "cave_air", nil)
					}
				}
			}
		}
		if p.spider {
			for y := int32(0); y <= 1; y++ {
				for x := int32(0); x <= 2; x++ {
					for z := int32(0); z <= corridorLength; z++ {
						// The cobwebs of spider corridors have to be inside:
						// generateMaybeBox with hasToBeInside set.
						if !(random.NextFloat() > 0.6) && mineInterior(p, clip, env, journal, x, y, z) {
							wx, wy, wz := mineLocal(p, x, y, z)
							put(wx, wy, wz, "cobweb", nil)
						}
					}
				}
			}
		}
	} else if p.kind == "room" {
		generateBox := func(x0, y0, z0, x1, y1, z1 int32) {
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					for z := z0; z <= z1; z++ {
						put(x, y, z, "cave_air", nil)
					}
				}
			}
		}
		generateBox(min[0], min[1]+1, min[2], max[0], min32(min[1]+3, max[1]), max[2])
		for _, entrance := range p.entrances {
			generateBox(entrance.Min[0], entrance.Max[1]-2, entrance.Min[2], entrance.Max[0], entrance.Max[1], entrance.Max[2])
		}
		dx := float32(max[0] - min[0] + 1)
		dy := float32(max[1] - (min[1] + 4) + 1)
		dz := float32(max[2] - min[2] + 1)
		cx := float32(min[0]) + dx/2
		cz := float32(min[2]) + dz/2
		for y := min[1] + 4; y <= max[1]; y++ {
			ny := float32(y-(min[1]+4)) / dy
			for x := min[0]; x <= max[0]; x++ {
				nx := (float32(x) - cx) / (dx * 0.5)
				for z := min[2]; z <= max[2]; z++ {
					nz := (float32(z) - cz) / (dz * 0.5)
					if nx*nx+ny*ny+nz*nz <= 1.05 {
						put(x, y, z, "cave_air", nil)
					}
				}
			}
		}
	}
	if p.kind == "corridor" {

		spiderPlaced := false
		for section := range p.sections {
			z := int32(2 + section*5)
			supported := true
			for x := int32(0); x <= 2; x++ {
				wx, wy, wz := mineLocal(p, x, 3, z)
				if !journal.inside(wx, wy, wz) || mineJournalAir(journal, wx, wy, wz) {
					supported = false
				}
			}
			if supported {
				for y := int32(0); y < 2; y++ {
					wx, wy, wz := mineLocal(p, 0, y, z)
					put(wx, wy, wz, fence, map[string]any{"west": true, "waterlogged": false})
				}
				for y := int32(0); y < 2; y++ {
					wx, wy, wz := mineLocal(p, 2, y, z)
					put(wx, wy, wz, fence, map[string]any{"east": true, "waterlogged": false})
				}
				if random.NextInt(4) == 0 {
					for _, x := range []int32{0, 2} {
						wx, wy, wz := mineLocal(p, x, 2, z)
						put(wx, wy, wz, plank, nil)
					}
				} else {
					for x := int32(0); x <= 2; x++ {
						wx, wy, wz := mineLocal(p, x, 2, z)
						put(wx, wy, wz, plank, nil)
					}
					for _, torchZ := range []int32{z - 1, z + 1} {
						if random.NextFloat() < 0.05 {
							wx, wy, wz := mineLocal(p, 1, 2, torchZ)
							facing := "north"
							if torchZ < z {
								facing = "south"
							}
							put(wx, wy, wz, "wall_torch", map[string]any{"facing": facing})
						}
					}
				}
			}
			for _, candidate := range [][3]int32{{0, 2, z - 1}, {2, 2, z - 1}, {0, 2, z + 1}, {2, 2, z + 1}} {
				mineMaybeCobweb(p, clip, env, journal, random, 0.1, candidate[0], candidate[1], candidate[2])
			}
			for _, candidate := range [][3]int32{{0, 2, z - 2}, {2, 2, z - 2}, {0, 2, z + 2}, {2, 2, z + 2}} {
				mineMaybeCobweb(p, clip, env, journal, random, 0.05, candidate[0], candidate[1], candidate[2])
			}
			for _, spot := range [][3]int32{{2, 0, z - 1}, {0, 0, z + 1}} {
				if random.NextInt(100) == 0 {
					mineLootChest(p, &out, journal, random, spot[0], spot[1], spot[2], "minecraft:chests/abandoned_mineshaft")
				}
			}
			if p.spider && !spiderPlaced && (env.PieceState == nil || !env.PieceState.SpawnerPlaced) {
				sz := z - 1 + random.NextInt(3)
				wx, wy, wz := mineLocal(p, 1, 0, sz)
				if journal.inside(wx, wy, wz) && mineInterior(p, clip, env, journal, 1, 0, sz) {
					spiderPlaced = true
					if env.PieceState != nil {
						env.PieceState.SpawnerPlaced = true
					}
					writeIndex := len(out.Blocks)
					if journal.set(wx, wy, wz, world.BlockState{Name: "minecraft:spawner"}) && writeIndex < len(out.Blocks) {
						out.Blocks[writeIndex].NBT = map[string]any{"id": "minecraft:mob_spawner", "SpawnData": map[string]any{"entity": map[string]any{"id": "minecraft:cave_spider"}}}
					}
				}
			}
		}
		for x := int32(0); x <= 2; x++ {
			for z := int32(0); z <= corridorLength; z++ {
				if !mineInterior(p, clip, env, journal, x, -1, z) {
					continue
				}
				wx, wy, wz := mineLocal(p, x, -1, z)
				if !mineSturdy(journal, wx, wy, wz, 1) {
					journal.set(wx, wy, wz, world.BlockState{Name: "minecraft:" + plank})
				}
			}
		}
		if p.sections > 0 {
			last := int32(2)
			if p.sections > 1 {
				last = corridorLength - 2
			}
			plankName := "minecraft:oak_planks"
			if p.wood == "mesa" {
				plankName = "minecraft:dark_oak_planks"
			}
			for _, z := range []int32{2, last} {
				for _, x := range []int32{0, 2} {
					if !mineInterior(p, clip, env, journal, x, -1, z) {
						continue
					}
					wx, wy, wz := mineLocal(p, x, -1, z)
					if mineJournalName(journal, wx, wy, wz) == plankName {
						mineSupportColumn(p, env, journal, x, -1, z, log, fence)
					}
				}
			}
		}
		if p.rails {
			for z := int32(0); z <= corridorLength; z++ {
				wx, wy, wz := mineLocal(p, 1, 0, z)
				if !journal.inside(wx, wy, wz) {
					continue
				}
				fx, fy, fz := mineLocal(p, 1, -1, z)
				shape, err := gamedata.BlockGenerationShapeID(journal.getBlockID(fx, fy, fz))
				if err != nil {
					panic(err)
				}
				if !shape.SolidRender {
					continue
				}
				chance := float32(0.9)
				if mineInterior(p, clip, env, journal, 1, 0, z) {
					chance = 0.7
				}
				if random.NextFloat() < chance {
					put(wx, wy, wz, "rail", map[string]any{"shape": "north_south", "waterlogged": false})
				}
			}
		}
	}
	if p.kind == "crossing" {
		two := max[1]-min[1]+1 > 3
		lo, hi := min[1], max[1]
		generateBox := func(x0, y0, z0, x1, y1, z1 int32) {
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					for z := z0; z <= z1; z++ {
						put(x, y, z, "cave_air", nil)
					}
				}
			}
		}
		if two {
			generateBox(min[0]+1, lo, min[2], max[0]-1, lo+2, max[2])
			generateBox(min[0], lo, min[2]+1, max[0], lo+2, max[2]-1)
			generateBox(min[0]+1, hi-2, min[2], max[0]-1, hi, max[2])
			generateBox(min[0], hi-2, min[2]+1, max[0], hi, max[2]-1)
			generateBox(min[0]+1, lo+3, min[2]+1, max[0]-1, lo+3, max[2]-1)
		} else {
			generateBox(min[0]+1, lo, min[2], max[0]-1, hi, max[2])
			generateBox(min[0], lo, min[2]+1, max[0], hi, max[2]-1)
		}
		for _, corner := range [][2]int32{{min[0] + 1, min[2] + 1}, {min[0] + 1, max[2] - 1}, {max[0] - 1, min[2] + 1}, {max[0] - 1, max[2] - 1}} {
			if !mineJournalAir(journal, corner[0], hi+1, corner[1]) {
				for y := lo; y <= hi; y++ {
					put(corner[0], y, corner[1], plank, nil)
				}
			}
		}
		for x := min[0]; x <= max[0]; x++ {
			for z := min[2]; z <= max[2]; z++ {
				y := lo - 1
				if !journal.inside(x, y+1, z) || y+1 >= mineOceanFloorHeight(env, journal, x, z) || mineSturdy(journal, x, y, z, 1) {
					continue
				}
				journal.set(x, y, z, world.BlockState{Name: "minecraft:" + plank})
			}
		}
	}
	if p.kind == "stairs" {
		generateBox := func(x0, y0, z0, x1, y1, z1 int32) {
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					for z := z0; z <= z1; z++ {
						wx, wy, wz := mineLocal(p, x, y, z)
						put(wx, wy, wz, "cave_air", nil)
					}
				}
			}
		}
		generateBox(0, 5, 0, 2, 7, 1)
		generateBox(0, 0, 7, 2, 2, 8)
		for i := int32(0); i < 5; i++ {
			y0 := int32(5) - i
			if i < 4 {
				y0--
			}
			generateBox(0, y0, 2+i, 2, 7-i, 2+i)
		}
	}
	return out, nil
}

func mineLocal(p minePiece,x,y,z int32)(int32,int32,int32){
	b:=p.box
	switch p.facing{case "north":return b.Min[0]+x,b.Min[1]+y,b.Max[2]-z;case "south":return b.Min[0]+x,b.Min[1]+y,b.Min[2]+z;case "west":return b.Max[0]-z,b.Min[1]+y,b.Min[2]+x;default:return b.Min[0]+z,b.Min[1]+y,b.Min[2]+x}
}
func mineBlockDirection(pieceDirection, local string) string {
	switch pieceDirection {
	case "south":
		switch local {
		case "north":
			return "south"
		case "south":
			return "north"
		}
	case "east":
		switch local {
		case "north":
			return "east"
		case "east":
			return "south"
		case "south":
			return "west"
		case "west":
			return "north"
		}
	case "west":
		switch local {
		case "north":
			return "west"
		case "east":
			return "south"
		case "south":
			return "east"
		case "west":
			return "north"
		}
	}
	return local
}
func mineTransformState(pieceDirection string, state world.BlockState) world.BlockState {
	if len(state.Properties) == 0 {
		return state
	}
	props := make(map[string]any, len(state.Properties))
	for key, value := range state.Properties {
		if direction, ok := value.(string); ok && key == "facing" {
			value = mineBlockDirection(pieceDirection, direction)
		}
		if connected, ok := value.(bool); ok && connected && (key == "north" || key == "south" || key == "west" || key == "east") {
			key = mineBlockDirection(pieceDirection, key)
		}
		if shape, ok := value.(string); ok && key == "shape" && state.Name == "minecraft:rail" && (pieceDirection == "east" || pieceDirection == "west") {
			if shape == "north_south" {
				value = "east_west"
			} else if shape == "east_west" {
				value = "north_south"
			}
		}
		props[key] = value
	}
	state.Properties = props
	return state
}
func mineJournalName(j *journalStructureWorld, x, y, z int32) string {
	name, ok := gamedata.BlockStateName(j.getBlockID(x, y, z))
	if !ok {
		panic("overworld: unknown mineshaft source block state")
	}
	return name
}
func mineJournalAir(j *journalStructureWorld, x, y, z int32) bool {
	switch mineJournalName(j, x, y, z) {
	case "minecraft:air", "minecraft:cave_air", "minecraft:void_air":
		return true
	}
	return false
}
func mineDirectName(j *journalStructureWorld, x, y, z int32) string {
	name, ok := gamedata.BlockStateName(j.stateID(x, y, z))
	if !ok {
		panic("overworld: unknown mineshaft source block state")
	}
	return name
}
func mineDirectAir(j *journalStructureWorld, x, y, z int32) bool {
	switch mineDirectName(j, x, y, z) {
	case "minecraft:air", "minecraft:cave_air", "minecraft:void_air":
		return true
	}
	return false
}
func mineCanBeReplaced(j *journalStructureWorld, wood string, x, y, z int32) bool {
	name := mineJournalName(j, x, y, z)
	planks, log, fence := "minecraft:oak_planks", "minecraft:oak_log", "minecraft:oak_fence"
	if wood == "mesa" {
		planks, log, fence = "minecraft:dark_oak_planks", "minecraft:dark_oak_log", "minecraft:dark_oak_fence"
	}
	return name != planks && name != log && name != fence && name != "minecraft:iron_chain"
}
func mineLiquidState(id uint32) bool {
	name, ok := gamedata.BlockStateName(id)
	if !ok {
		panic("overworld: unknown mineshaft liquid-query state")
	}
	if name == "minecraft:water" || name == "minecraft:lava" {
		return true
	}
	waterlogged, ok := gamedata.BlockStateBool(id, "waterlogged")
	return ok && waterlogged
}
func mineLiquid(env structure.ProcessorEnvironment, x, y, z int32) bool {
	id, err := env.BlockStateID(x, y, z)
	if err != nil {
		panic(err)
	}
	return mineLiquidState(id)
}
func mineInvalid(p minePiece, clip structure.Bounds, env structure.ProcessorEnvironment) bool {
	b := p.box
	x0 := max32(b.Min[0]-1, clip.Min[0])
	x1 := min32(b.Max[0]+1, clip.Max[0])
	y0 := max32(b.Min[1]-1, clip.Min[1])
	y1 := min32(b.Max[1]+1, clip.Max[1])
	z0 := max32(b.Min[2]-1, clip.Min[2])
	z1 := min32(b.Max[2]+1, clip.Max[2])
	if x0 > x1 || y0 > y1 || z0 > z1 {
		return false
	}
	biome := env.BiomeAt((x0+x1)/2, (y0+y1)/2, (z0+z1)/2)
	blocked, err := gamedata.BiomeTagContains("minecraft:mineshaft_blocking", biome)
	if err != nil {
		panic(err)
	}
	if blocked {
		return true
	}
	liquid := func(x, y, z int32) bool { return mineLiquid(env, x, y, z) }
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			if liquid(x, y0, z) || liquid(x, y1, z) {
				return true
			}
		}
	}
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			if liquid(x, y, z0) || liquid(x, y, z1) {
				return true
			}
		}
	}
	for z := z0; z <= z1; z++ {
		for y := y0; y <= y1; y++ {
			if liquid(x0, y, z) || liquid(x1, y, z) {
				return true
			}
		}
	}
	return false
}
func mineReplaceableByStructures(j *journalStructureWorld, x, y, z int32) bool {
	name := mineDirectName(j, x, y, z)
	return mineDirectAir(j, x, y, z) || name == "minecraft:water" || name == "minecraft:lava" || name == "minecraft:glow_lichen" || name == "minecraft:seagrass" || name == "minecraft:tall_seagrass"
}
func mineFalling(name string) bool {
	switch name {
	case "minecraft:sand", "minecraft:red_sand", "minecraft:gravel", "minecraft:anvil", "minecraft:chipped_anvil", "minecraft:damaged_anvil", "minecraft:dragon_egg", "minecraft:suspicious_sand", "minecraft:suspicious_gravel":
		return true
	}
	return strings.HasSuffix(name, "_concrete_powder")
}
func mineSupportColumn(p minePiece, env structure.ProcessorEnvironment, journal *journalStructureWorld, x, y, z int32, log, fence string) {
	wx, wy, wz := mineLocal(p, x, y, z)
	if !journal.inside(wx, wy, wz) {
		return
	}
	belowActive, aboveActive := true, true
	for distance := int32(1); belowActive || aboveActive; distance++ {
		if belowActive {
			yy := wy - distance
			name := mineDirectName(journal, wx, yy, wz)
			empty := mineReplaceableByStructures(journal, wx, yy, wz) && name != "minecraft:lava"
			if !empty && mineSturdy(journal, wx, yy, wz, 1) {
				for blockY := yy + 1; blockY < wy; blockY++ {
					journal.set(wx, blockY, wz, world.BlockState{Name: "minecraft:" + log, Properties: map[string]any{"axis": "y"}})
				}
				return
			}
			belowActive = distance <= 20 && empty && yy > int32(env.Range.Min()+1)
		}
		if aboveActive {
			yy := wy + distance
			name := mineDirectName(journal, wx, yy, wz)
			if !mineReplaceableByStructures(journal, wx, yy, wz) && !mineFalling(name) && journal.shape(wx, yy, wz).Center&1 != 0 {
				journal.set(wx, wy+1, wz, world.BlockState{Name: "minecraft:" + fence, Properties: map[string]any{"north": false, "south": false, "east": false, "west": false, "waterlogged": false}})
				for blockY := wy + 2; blockY < yy; blockY++ {
					journal.set(wx, blockY, wz, world.BlockState{Name: "minecraft:iron_chain", Properties: map[string]any{"axis": "y", "waterlogged": false}})
				}
				return
			}
			aboveActive = distance <= 50 && mineReplaceableByStructures(journal, wx, yy, wz) && yy < int32(env.Range.Max())
		}
	}
}
func mineOceanFloorState(id uint32) bool {
	name, ok := gamedata.BlockStateName(id)
	if !ok {
		panic("overworld: unknown mineshaft heightmap block state")
	}
	matches, err := gamedata.BlockTagContains("minecraft:blocks_motion_in_heightmap", name)
	if err != nil {
		panic(err)
	}
	return matches
}

func mineOceanFloorHeight(env structure.ProcessorEnvironment, journal *journalStructureWorld, x, z int32) int32 {
	baseHeight := env.BaseHeight(x, z, true)
	height := baseHeight
	topRemoved, highestAdded := false, height
	for pos, id := range journal.writes {
		if pos[0] != x || pos[2] != z {
			continue
		}
		if pos[1] == height-1 && !mineOceanFloorState(id) {
			topRemoved = true
		}
		if pos[1] >= height && mineOceanFloorState(id) && pos[1]+1 > highestAdded {
			highestAdded = pos[1] + 1
		}
	}
	if topRemoved {
		height = int32(env.Range.Min())
		for y := baseHeight - 2; y >= int32(env.Range.Min()); y-- {
			if mineOceanFloorState(journal.stateID(x, y, z)) {
				height = y + 1
				break
			}
		}
	}
	if highestAdded > height {
		return highestAdded
	}
	return height
}

func mineInterior(p minePiece, clip structure.Bounds, env structure.ProcessorEnvironment, journal *journalStructureWorld, x, y, z int32) bool {
	wx, wy, wz := mineLocal(p, x, y+1, z)
	return journal.inside(wx, wy, wz) && wy < mineOceanFloorHeight(env, journal, wx, wz)
}
func mineSturdy(journal *journalStructureWorld, x, y, z int32, face uint8) bool {
	return journal.shape(x, y, z).Sturdy&(1<<face) != 0
}
func mineMaybeCobweb(p minePiece, clip structure.Bounds, env structure.ProcessorEnvironment, journal *journalStructureWorld, random *vanilla.Random, probability float32, x, y, z int32) {
	wx, wy, wz := mineLocal(p, x, y, z)
	if !mineInterior(p, clip, env, journal, x, y, z) || random.NextFloat() >= probability {
		return
	}
	sturdy := 0
	for _, d := range [][4]int32{{1, 0, 0, 4}, {-1, 0, 0, 5}, {0, 1, 0, 0}, {0, -1, 0, 1}, {0, 0, 1, 2}, {0, 0, -1, 3}} {
		nx, ny, nz := wx+d[0], wy+d[1], wz+d[2]
		if !journal.inside(nx, ny, nz) {
			continue
		}
		if mineSturdy(journal, nx, ny, nz, uint8(d[3])) {
			sturdy++
			if sturdy >= 2 {
				if mineCanBeReplaced(journal, p.wood, wx, wy, wz) {
					journal.set(wx, wy, wz, world.BlockState{Name: "minecraft:cobweb"})
				}
				return
			}
		}
	}
}
func mineLootChest(p minePiece, out *structure.Placement, journal *journalStructureWorld, random *vanilla.Random, x, y, z int32, loot string) {
	wx, wy, wz := mineLocal(p, x, y, z)
	if !journal.inside(wx, wy, wz) || !mineDirectAir(journal, wx, wy, wz) || mineDirectAir(journal, wx, wy-1, wz) {
		return
	}
	shape := "east_west"
	if random.NextBoolean() {
		shape = "north_south"
	}
	rail := mineTransformState(p.facing, world.BlockState{Name: "minecraft:rail", Properties: map[string]any{"shape": shape, "waterlogged": false}})
	journal.set(wx, wy, wz, rail)
	lootSeed := random.NextLong()
	out.Entities = append(out.Entities, structure.Entity{Pos: [3]float64{float64(wx) + 0.5, float64(wy) + 0.5, float64(wz) + 0.5}, BlockPos: [3]int32{wx, wy, wz}, NBT: map[string]any{"id": "minecraft:chest_minecart", "LootTable": loot, "LootTableSeed": lootSeed}, SpawnReason: "CHUNK_GENERATION"})
}

type mineGraph struct { pieces []minePiece; boxes []structure.Box; r *vanilla.Random; wood string; origin [3]int32 }

func (g *mineGraph) add(p minePiece) bool { for _,b:=range g.boxes {if boxesOverlap(b,p.box){return false}};g.pieces=append(g.pieces,p);g.boxes=append(g.boxes,p.box);return true }
func boxesOverlap(a,b structure.Box)bool {for i:=range 3 {if a.Max[i]<b.Min[i]||b.Max[i]<a.Min[i] {return false}};return true}
func (g *mineGraph) child(x, y, z int32, dir string, depth int) {
	if depth > 9 || abs32(x-g.origin[0]) > 80 || abs32(z-g.origin[2]) > 80 {
		return
	}
	selection := g.r.NextInt(100)
	p := minePiece{wood: g.wood, facing: dir, depth: depth}
	var box structure.Box
	if selection >= 80 {
		height := int32(2)
		if g.r.NextInt(4) == 0 {
			height = 6
		}
		p.kind = "crossing"
		switch dir {
		case "north":
			box = mbox(x-1, y, z-4, x+3, y+height, z)
		case "south":
			box = mbox(x-1, y, z, x+3, y+height, z+4)
		case "west":
			box = mbox(x-4, y, z-1, x, y+height, z+3)
		default:
			box = mbox(x, y, z-1, x+4, y+height, z+3)
		}
		if !g.canAdd(box) {
			return
		}
	} else if selection >= 70 {
		p.kind = "stairs"
		switch dir {
		case "north":
			box = mbox(x, y-5, z-8, x+2, y+2, z)
		case "south":
			box = mbox(x, y-5, z, x+2, y+2, z+8)
		case "west":
			box = mbox(x-8, y-5, z, x, y+2, z+2)
		default:
			box = mbox(x, y-5, z, x+8, y+2, z+2)
		}
		if !g.canAdd(box) {
			return
		}
	} else {
		p.kind = "corridor"
		length := g.r.NextInt(3) + 2
		for ; length > 0; length-- {
			n := length * 5
			switch dir {
			case "north":
				box = mbox(x, y, z-(n-1), x+2, y+2, z)
			case "south":
				box = mbox(x, y, z, x+2, y+2, z+n-1)
			case "west":
				box = mbox(x-(n-1), y, z, x, y+2, z+2)
			default:
				box = mbox(x, y, z, x+n-1, y+2, z+2)
			}
			if g.canAdd(box) {
				break
			}
		}
		if length == 0 {
			return
		}
		p.sections = int(max32(box.Max[0]-box.Min[0]+1, box.Max[2]-box.Min[2]+1) / 5)
		p.rails = g.r.NextInt(3) == 0
		p.spider = !p.rails && g.r.NextInt(23) == 0
	}
	p.box = box
	if !g.add(p) {
		return
	}
	g.branch(p, depth)
}
func(g *mineGraph)canAdd(box structure.Box)bool{for _,other:=range g.boxes{if boxesOverlap(other,box){return false}};return true}
func (g *mineGraph) branch(p minePiece, depth int) {
	b := p.box
	if p.kind == "room" {
		heightSpace := b.Max[1] - b.Min[1] - 3
		if heightSpace <= 0 {
			heightSpace = 1
		}
		for side := range 4 {
			span := b.Max[0] - b.Min[0] + 1
			if side >= 2 {
				span = b.Max[2] - b.Min[2] + 1
			}
			pos := int32(0)
			for pos < span {
				pos += g.r.NextInt(span)
				if pos+3 > span {
					break
				}
				y := b.Min[1] + g.r.NextInt(heightSpace) + 1
				x, z := b.Min[0]+pos, b.Min[2]+pos
				dir := "north"
				switch side {
				case 0:
					z = b.Min[2] - 1
				case 1:
					z = b.Max[2] + 1
					dir = "south"
				case 2:
					x, z = b.Min[0]-1, b.Min[2]+pos
					dir = "west"
				case 3:
					x, z = b.Max[0]+1, b.Min[2]+pos
					dir = "east"
				}
				childIndex := len(g.pieces)
				g.child(x, y, z, dir, depth+1)
				if childIndex < len(g.pieces) {
					cb := g.pieces[childIndex].box
					entrance := cb
					switch side {
					case 0:
						entrance.Min[2] = b.Min[2]
						entrance.Max[2] = b.Min[2] + 1
					case 1:
						entrance.Min[2] = b.Max[2] - 1
						entrance.Max[2] = b.Max[2]
					case 2:
						entrance.Min[0] = b.Min[0]
						entrance.Max[0] = b.Min[0] + 1
					case 3:
						entrance.Min[0] = b.Max[0] - 1
						entrance.Max[0] = b.Max[0]
					}
					for i := range g.pieces {
						if g.pieces[i].kind == "room" && g.pieces[i].box == b {
							g.pieces[i].entrances = append(g.pieces[i].entrances, entrance)
							break
						}
					}
				}
				pos += 4
			}
		}
		return
	}
	if p.kind == "corridor" {
		end := g.r.NextInt(4)
		y := func() int32 { return b.Min[1] - 1 + g.r.NextInt(3) }
		switch p.facing {
		case "north":
			if end <= 1 {
				g.child(b.Min[0], y(), b.Min[2]-1, "north", depth+1)
			} else if end == 2 {
				g.child(b.Min[0]-1, y(), b.Min[2], "west", depth+1)
			} else {
				g.child(b.Max[0]+1, y(), b.Min[2], "east", depth+1)
			}
		case "south":
			if end <= 1 {
				g.child(b.Min[0], y(), b.Max[2]+1, "south", depth+1)
			} else if end == 2 {
				g.child(b.Min[0]-1, y(), b.Max[2]-3, "west", depth+1)
			} else {
				g.child(b.Max[0]+1, y(), b.Max[2]-3, "east", depth+1)
			}
		case "west":
			if end <= 1 {
				g.child(b.Min[0]-1, y(), b.Min[2], "west", depth+1)
			} else if end == 2 {
				g.child(b.Min[0], y(), b.Min[2]-1, "north", depth+1)
			} else {
				g.child(b.Min[0], y(), b.Max[2]+1, "south", depth+1)
			}
		case "east":
			if end <= 1 {
				g.child(b.Max[0]+1, y(), b.Min[2], "east", depth+1)
			} else if end == 2 {
				g.child(b.Max[0]-3, y(), b.Min[2]-1, "north", depth+1)
			} else {
				g.child(b.Max[0]-3, y(), b.Max[2]+1, "south", depth+1)
			}
		}
		if depth < 8 {
			if p.facing != "north" && p.facing != "south" {
				for x := b.Min[0] + 3; x+3 <= b.Max[0]; x += 5 {
					switch g.r.NextInt(5) {
					case 0:
						g.child(x, b.Min[1], b.Min[2]-1, "north", depth+2)
					case 1:
						g.child(x, b.Min[1], b.Max[2]+1, "south", depth+2)
					}
				}
			} else {
				for z := b.Min[2] + 3; z+3 <= b.Max[2]; z += 5 {
					switch g.r.NextInt(5) {
					case 0:
						g.child(b.Min[0]-1, b.Min[1], z, "west", depth+2)
					case 1:
						g.child(b.Max[0]+1, b.Min[1], z, "east", depth+2)
					}
				}
			}
		}
		return
	}
	if p.kind == "crossing" {
		switch p.facing {
		case "north":
			g.child(b.Min[0]+1, b.Min[1], b.Min[2]-1, "north", depth+1)
			g.child(b.Min[0]-1, b.Min[1], b.Min[2]+1, "west", depth+1)
			g.child(b.Max[0]+1, b.Min[1], b.Min[2]+1, "east", depth+1)
		case "south":
			g.child(b.Min[0]+1, b.Min[1], b.Max[2]+1, "south", depth+1)
			g.child(b.Min[0]-1, b.Min[1], b.Min[2]+1, "west", depth+1)
			g.child(b.Max[0]+1, b.Min[1], b.Min[2]+1, "east", depth+1)
		case "west":
			g.child(b.Min[0]+1, b.Min[1], b.Min[2]-1, "north", depth+1)
			g.child(b.Min[0]+1, b.Min[1], b.Max[2]+1, "south", depth+1)
			g.child(b.Min[0]-1, b.Min[1], b.Min[2]+1, "west", depth+1)
		case "east":
			g.child(b.Min[0]+1, b.Min[1], b.Min[2]-1, "north", depth+1)
			g.child(b.Min[0]+1, b.Min[1], b.Max[2]+1, "south", depth+1)
			g.child(b.Max[0]+1, b.Min[1], b.Min[2]+1, "east", depth+1)
		}
		if b.Max[1]-b.Min[1] > 3 {
			if g.r.NextBoolean() {
				g.child(b.Min[0]+1, b.Min[1]+4, b.Min[2]-1, "north", depth+1)
			}
			if g.r.NextBoolean() {
				g.child(b.Min[0]-1, b.Min[1]+4, b.Min[2]+1, "west", depth+1)
			}
			if g.r.NextBoolean() {
				g.child(b.Max[0]+1, b.Min[1]+4, b.Min[2]+1, "east", depth+1)
			}
			if g.r.NextBoolean() {
				g.child(b.Min[0]+1, b.Min[1]+4, b.Max[2]+1, "south", depth+1)
			}
		}
		return
	}
	if p.kind == "stairs" {
		x, y, z := b.Min[0], b.Min[1], b.Min[2]
		switch p.facing {
		case "north":
			z--
		case "south":
			z = b.Max[2] + 1
		case "west":
			x--
		case "east":
			x = b.Max[0] + 1
		}
		g.child(x, y, z, p.facing, depth+1)
	}
}
func mbox(x0,y0,z0,x1,y1,z1 int32)structure.Box{return structure.Box{Min:[3]int32{x0,y0,z0},Max:[3]int32{x1,y1,z1}}}
func abs32(v int32)int32{if v<0{return -v};return v}
func max32(a,b int32)int32{if a>b{return a};return b}
func min32(a,b int32)int32{if a<b{return a};return b}

func buildModernMineshaft(in modernStructureBuildInput) (modernStructureStart, bool, error) {
	if in.random == nil || in.source == nil {
		return modernStructureStart{}, false, fmt.Errorf("mineshaft requires structure random and world source")
	}
	in.random.NextDouble() // Java MineshaftStructure consumes this before room generation.
	wood, _ := in.config["mineshaft_type"].(string)
	if wood != "mesa" {
		wood = "normal"
	}
	x, z := in.chunkX*16+2, in.chunkZ*16+2
	g := mineGraph{r: in.random, wood: wood, origin: [3]int32{x, 50, z}}
	room := minePiece{kind: "room", wood: wood, box: mbox(x, 50, z, x+7+int32(in.random.NextInt(6)), 54+int32(in.random.NextInt(6)), z+7+int32(in.random.NextInt(6)))}
	g.add(room)
	g.branch(room, 0)
	// Java moves normal shafts below sea level; mesa shafts float between sea level and raw surface.
	center := emptyStructureBox()
	for _, p := range g.pieces {
		center = unionStructureBox(center, p.box)
	}
	shift := int32(0)
	if wood == "mesa" {
		cx := center.Min[0] + (center.Max[0]-center.Min[0]+1)/2
		cy := center.Min[1] + (center.Max[1]-center.Min[1]+1)/2
		cz := center.Min[2] + (center.Max[2]-center.Min[2]+1)/2
		surface := in.source.BaseHeight(cx, cz, false)
		target := in.source.SeaLevel()
		if surface > target {
			target += in.random.NextInt(surface - target + 1)
		}
		shift = target - cy
	} else {
		span := center.Max[1] - center.Min[1] + 1
		y1 := span + modernMinY + 1
		maxY := in.source.SeaLevel() - 10
		if y1 < maxY {
			y1 += in.random.NextInt(maxY - y1)
		}
		shift = y1 - center.Max[1]
	}
	// MineshaftStructure's stub sits at the chunk's middle X and minimum Z,
	// 50 plus the shift; its biome decides between the normal and mesa shafts.
	if allowed, err := structureStubBiomeAllowed(in.source, in.config, in.chunkX*16+8, 50+shift, in.chunkZ*16); err != nil || !allowed {
		return modernStructureStart{}, false, err
	}
	for i := range g.pieces {
		g.pieces[i].box.Min[1] += shift
		g.pieces[i].box.Max[1] += shift
		for j := range g.pieces[i].entrances {
			g.pieces[i].entrances[j].Min[1] += shift
			g.pieces[i].entrances[j].Max[1] += shift
		}
	}
	center = emptyStructureBox()
	pieces := make([]modernStructurePiece, len(g.pieces))
	rigid := make([]structure.RigidPiece, len(g.pieces))
	for i, p := range g.pieces {
		center = unionStructureBox(center, p.box)
		pieces[i] = p
		rigid[i] = structure.RigidPiece{Bounds: p.box, Adjustment: structure.TerrainNone}
	}
	return modernStructureStart{name: in.name, pieces: pieces, rigid: rigid, box: center, terrain: structure.TerrainNone}, true, nil
}
