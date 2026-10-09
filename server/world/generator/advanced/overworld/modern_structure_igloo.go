package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("igloo", buildModernIgloo) }

type modernIglooPiece struct {
	template *javastructure.Template
	box javastructure.Box
	origin [3]int32
	rotation javastructure.Rotation
	pivot [3]int32
	seed int64
}

func (p modernIglooPiece) Bounds() javastructure.Box { return p.box }

func (p modernIglooPiece) Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement, error) {
	if env.Random == nil {
		return javastructure.Placement{}, fmt.Errorf("igloo placement requires Java structure random")
	}
	pivot := javastructure.TransformPosition(p.pivot, javastructure.NoMirror, p.rotation)
	origin := [3]int32{p.origin[0] + p.pivot[0] - pivot[0], p.origin[1] + p.pivot[1] - pivot[1], p.origin[2] + p.pivot[2] - pivot[2]}
	placed, err := p.template.Place(0, origin, p.rotation, javastructure.NoMirror, clip, p.seed)
	if err != nil {
		return javastructure.Placement{}, err
	}
	for _, marker := range placed.Blocks {
		if marker.State.Name != "minecraft:structure_block" {
			continue
		}
		if marker.NBT["metadata"] == "chest" {
			chestPos := marker.Pos
			chestPos[1]--
			for _, chest := range placed.Blocks {
				if chest.Pos != chestPos {
					continue
				}
				chest.NBT = cloneStructureNBT(chest.NBT)
				if chest.NBT == nil {
					chest.NBT = make(map[string]any)
				}
				chest.NBT["id"] = "minecraft:chest"
				chest.NBT["LootTable"] = "minecraft:chests/igloo_chest"
				chest.NBT["LootTableSeed"] = env.Random.NextLong()
				placed.PostShapeBlocks = append(placed.PostShapeBlocks, chest)
				break
			}
		}
		placed.PostShapeBlocks = append(placed.PostShapeBlocks, javastructure.Block{
			Pos: marker.Pos, State: world.BlockState{Name: "minecraft:air"},
		})
	}
	return placed, nil
}

func buildModernIgloo(in modernStructureBuildInput) (modernStructureStart, bool, error) {
	if in.random == nil || in.source == nil { return modernStructureStart{}, false, fmt.Errorf("igloo requires structure random and world source") }
	rotation := javastructure.Rotation(in.random.NextInt(4))
	base := [3]int32{in.chunkX * 16, 90, in.chunkZ * 16}
	pieces := make([]modernStructurePiece, 0, 10)
	if in.random.NextDouble() < 0.5 {
		depth := int32(in.random.NextInt(8) + 4)
		p, err := makeModernIglooPiece("igloo/bottom", base, rotation, depth*3, [3]int32{3, 6, 7}, [3]int32{0, -3, -2}, in.seed, in.source)
		if err != nil { return modernStructureStart{}, false, err }; pieces = append(pieces, p)
		for i := int32(0); i < depth-1; i++ {
			p, err := makeModernIglooPiece("igloo/middle", base, rotation, i*3, [3]int32{1, 3, 1}, [3]int32{2, -3, 4}, in.seed, in.source)
			if err != nil { return modernStructureStart{}, false, err }; pieces = append(pieces, p)
		}
	}
	p, err := makeModernIglooPiece("igloo/top", base, rotation, 0, [3]int32{3, 5, 5}, [3]int32{}, in.seed, in.source)
	if err != nil { return modernStructureStart{}, false, err }; pieces = append(pieces, p)
	box := emptyStructureBox()
	for _, piece := range pieces { box = unionStructureBox(box, piece.Bounds()) }
	return modernStructureStart{name: in.name, pieces: pieces, box: box, terrain: terrainAdjustment(stringValue(in.config["terrain_adaptation"]))}, true, nil
}

func makeModernIglooPiece(name string, base [3]int32, rotation javastructure.Rotation, depth int32, pivot, offset [3]int32, seed int64, source ModernFeatureSource) (modernIglooPiece, error) {
	t, err := javastructure.Load(name)
	if err != nil { return modernIglooPiece{}, err }
	origin := [3]int32{base[0] + offset[0], base[1] + offset[1] - depth, base[2] + offset[2]}
	entranceRelative := [3]int32{3-offset[0], 0, -offset[2]}
	pivoted := [3]int32{entranceRelative[0]-pivot[0], entranceRelative[1]-pivot[1], entranceRelative[2]-pivot[2]}
	entranceOffset := javastructure.TransformPosition(pivoted, javastructure.NoMirror, rotation)
	entranceX, entranceZ := origin[0]+pivot[0]+entranceOffset[0], origin[2]+pivot[2]+entranceOffset[2]
	origin[1] += source.BaseHeight(entranceX, entranceZ, false)-91
	rotatedPivot := javastructure.TransformPosition(pivot, javastructure.NoMirror, rotation)
	placeOrigin := [3]int32{origin[0] + pivot[0] - rotatedPivot[0], origin[1] + pivot[1] - rotatedPivot[1], origin[2] + pivot[2] - rotatedPivot[2]}
	box := modernTemplateBounds(t, placeOrigin, rotation)
	return modernIglooPiece{template:t, box:box, origin:origin, rotation:rotation, pivot:pivot, seed:seed}, nil
}

