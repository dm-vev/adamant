package nether

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type complexSourcePiece struct {
	Box         [6]int32 `json:"box"`
	Depth       int      `json:"depth"`
	Orientation string   `json:"orientation"`
	Tag         struct {
		ID       string `json:"id"`
		Chest    *int   `json:"Chest"`
		Mob      *int   `json:"Mob"`
		FillSeed *int32 `json:"Seed"`
		X        int32  `json:"PosX"`
		Y        int32  `json:"PosY"`
		Z        int32  `json:"PosZ"`
		Rotation string `json:"rotation"`
		Ground   int32  `json:"ground_level_delta"`
		Element  struct {
			Location   string `json:"location"`
			Projection string `json:"projection"`
		} `json:"pool_element"`
		Junctions []struct {
			X          int32  `json:"source_x"`
			Y          int32  `json:"source_ground_y"`
			Z          int32  `json:"source_z"`
			Delta      int32  `json:"delta_y"`
			Projection string `json:"dest_proj"`
		} `json:"junctions"`
	} `json:"tag"`
}
type complexSourceRow struct {
	Seed      int64                `json:"seed"`
	Chunk     [2]int32             `json:"chunk"`
	Structure string               `json:"structure"`
	Pieces    []complexSourcePiece `json:"pieces"`
	Prepared  struct {
		Valid    bool     `json:"valid"`
		Position [3]int32 `json:"position"`
		Allowed  bool     `json:"allowed_biome"`
		Biome    string   `json:"biome"`
		Next     int64    `json:"next"`
	} `json:"prepared"`
}

func compareNetherStartOracle(data []byte) error {
	var input struct {
		Graphs []complexSourceRow `json:"graphs"`
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return err
	}
	generators := map[int64]*Nether{}
	templates := map[string]*structure.Template{}
	checked := map[*structure.Template]string{}
	for _, row := range input.Graphs {
		g := generators[row.Seed]
		if g == nil {
			g = NewNether(row.Seed)
			generators[row.Seed] = g
		}
		random := largeFeatureRandom(row.Seed, int(row.Chunk[0]), int(row.Chunk[1]))
		prepared, valid, err := structure.PrepareJigsaw(structure.JigsawOptions{
			Pool: "bastion/starts", Origin: [3]int32{row.Chunk[0] * 16, 33, row.Chunk[1] * 16},
			MaxDepth: 6, MaxDistance: 80, HeightRange: cube.Range{0, 255}, Seed: row.Seed,
			HeightAt: func(x, z int32) int32 { return g.featureSource.BaseHeight(x, z, false) },
		}, &random)
		if err != nil {
			return err
		}
		if valid != row.Prepared.Valid {
			return fmt.Errorf("seed=%d chunk=%v prepared=%v, source=%v", row.Seed, row.Chunk, valid, row.Prepared.Valid)
		}
		if valid {
			position := prepared.Position()
			biome := g.featureSource.NoiseBiomeAt(position[0]>>2, position[1]>>2, position[2]>>2)
			allowed, err := gamedata.BiomeTagContains("#minecraft:has_structure/bastion_remnant", biome)
			if err != nil {
				return err
			}
			if position != row.Prepared.Position || biome != row.Prepared.Biome || allowed != row.Prepared.Allowed {
				return fmt.Errorf("seed=%d chunk=%v prepared position=%v/%v biome=%s/%s allowed=%v/%v", row.Seed, row.Chunk, position, row.Prepared.Position, biome, row.Prepared.Biome, allowed, row.Prepared.Allowed)
			}
		}
		if next := random.NextLong(); next != row.Prepared.Next {
			return fmt.Errorf("seed=%d chunk=%v prepared RNG=%d, source=%d", row.Seed, row.Chunk, next, row.Prepared.Next)
		}
		start, err := g.complexStart(row.Chunk[0], row.Chunk[1])
		if err != nil {
			return err
		}
		if start == nil {
			return fmt.Errorf("seed=%d chunk=%v rejected source %s", row.Seed, row.Chunk, row.Structure)
		}
		if start.name != row.Structure {
			return fmt.Errorf("seed=%d chunk=%v selected=%s, source=%s", row.Seed, row.Chunk, start.name, row.Structure)
		}
		if start.fortress != nil {
			if len(start.fortress.pieces) != len(row.Pieces) {
				return fmt.Errorf("seed=%d chunk=%v fortress pieces=%d, source=%d", row.Seed, row.Chunk, len(start.fortress.pieces), len(row.Pieces))
			}
			for i, piece := range start.fortress.pieces {
				if err := compareComplexFortressPiece(piece, row.Pieces[i]); err != nil {
					return fmt.Errorf("seed=%d chunk=%v fortress piece=%d: %w", row.Seed, row.Chunk, i, err)
				}
			}
			continue
		}
		if len(start.bastion.Pieces) != len(row.Pieces) {
			return fmt.Errorf("seed=%d chunk=%v bastion pieces=%d, source=%d", row.Seed, row.Chunk, len(start.bastion.Pieces), len(row.Pieces))
		}
		for i := range start.bastion.Pieces {
			piece, want := &start.bastion.Pieces[i], row.Pieces[i]
			box := [6]int32{piece.Bounds.Min[0], piece.Bounds.Min[1], piece.Bounds.Min[2], piece.Bounds.Max[0], piece.Bounds.Max[1], piece.Bounds.Max[2]}
			rotation := [...]string{"NONE", "CLOCKWISE_90", "CLOCKWISE_180", "COUNTERCLOCKWISE_90"}[int(piece.Rotation)]
			if box != want.Box || piece.Origin != [3]int32{want.Tag.X, want.Tag.Y, want.Tag.Z} || rotation != want.Tag.Rotation || piece.GroundLevelDelta != want.Tag.Ground || piece.Projection != structure.Rigid || want.Tag.Element.Projection != "rigid" || len(piece.Junctions) != len(want.Tag.Junctions) {
				return fmt.Errorf("seed=%d chunk=%v bastion piece=%d box=%v/%v origin=%v rotation=%s/%s", row.Seed, row.Chunk, i, box, want.Box, piece.Origin, rotation, want.Tag.Rotation)
			}
			for j, junction := range piece.Junctions {
				wantJ := want.Tag.Junctions[j]
				if junction.X != wantJ.X || junction.Y != wantJ.Y || junction.Z != wantJ.Z || junction.DeltaY != wantJ.Delta || junction.DestProjection != structure.Rigid || wantJ.Projection != "rigid" {
					return fmt.Errorf("seed=%d chunk=%v bastion piece=%d junction=%d differs from source", row.Seed, row.Chunk, i, j)
				}
			}
			name := strings.TrimPrefix(want.Tag.Element.Location, "minecraft:")
			if checked[piece.Template] != name {
				expected := templates[name]
				if expected == nil {
					expected, err = structure.LoadOrCreate(name)
					if err != nil {
						return err
					}
					templates[name] = expected
				}
				if !reflect.DeepEqual(piece.Template, expected) {
					return fmt.Errorf("seed=%d chunk=%v bastion piece=%d differs from source template %s", row.Seed, row.Chunk, i, name)
				}
				checked[piece.Template] = name
			}
		}
	}
	return nil
}

func compareComplexFortressPiece(piece bridgePiece, want complexSourcePiece) error {
	base := piece.pieceBase()
	box := base.bb
	actualBox := [6]int32{int32(box.minX), int32(box.minY), int32(box.minZ), int32(box.maxX), int32(box.maxY), int32(box.maxZ)}
	orientation := ""
	switch base.facing {
	case cube.North:
		orientation = "north"
	case cube.East:
		orientation = "east"
	case cube.South:
		orientation = "south"
	case cube.West:
		orientation = "west"
	}
	if actualBox != want.Box || base.componentType != want.Depth || orientation != want.Orientation {
		return fmt.Errorf("geometry %T box=%v/%v depth=%d/%d facing=%s/%s", piece, actualBox, want.Box, base.componentType, want.Depth, orientation, want.Orientation)
	}
	id := ""
	chest, mob := false, false
	var fillSeed int32
	switch p := piece.(type) {
	case *netherBridgeCorridor:
		id = "nesclt"
		chest = p.chest
	case *netherBridgeCorridor2:
		id = "nescrt"
		chest = p.chest
	case *netherBridgeCorridor3:
		id = "neccs"
	case *netherBridgeCorridor4:
		id = "nectb"
	case *netherBridgeCorridor5:
		id = "nesc"
	case *netherBridgeCrossing:
		id = "nerc"
	case *netherBridgeCrossing2:
		id = "nescsc"
	case *netherBridgeCrossing3:
		id = "nebcr"
	case *netherBridgeEnd:
		id = "nebef"
		fillSeed = p.fillSeed
	case *netherBridgeEntrance:
		id = "nece"
	case *netherBridgeNetherStalkRoom:
		id = "necsr"
	case *netherBridgeStairs:
		id = "nesr"
	case *netherBridgeStraight:
		id = "nebs"
	case *netherBridgeThrone:
		id = "nemt"
		mob = p.hasSpawner
	default:
		return fmt.Errorf("unknown piece %T", piece)
	}
	if "minecraft:"+id != want.Tag.ID {
		return fmt.Errorf("piece %T id=%s, source=%s", piece, id, want.Tag.ID)
	}
	if want.Tag.Chest != nil && chest != (*want.Tag.Chest != 0) {
		return fmt.Errorf("piece %T chest=%v, source=%v", piece, chest, *want.Tag.Chest != 0)
	}
	if want.Tag.Mob != nil && mob != (*want.Tag.Mob != 0) {
		return fmt.Errorf("piece %T spawner=%v, source=%v", piece, mob, *want.Tag.Mob != 0)
	}
	if want.Tag.FillSeed != nil && fillSeed != *want.Tag.FillSeed {
		return fmt.Errorf("piece %T fillSeed=%d, source=%d", piece, fillSeed, *want.Tag.FillSeed)
	}
	return nil
}
