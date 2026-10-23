package nether

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

// Only nether_fossil enables terrain adaptation in the pinned Nether registry.
// Keep reference discovery independent of material/carving to avoid a terrain
// query recursively requesting its own structure context.
func (g *Nether) fossilBeard(pos world.ChunkPos) *structure.Beardifier {
	var storage [16]structure.RigidPiece
	pieces := storage[:0]
	g.forFossilReferences(pos, func(start *netherFossilStart) {
		pieces = append(pieces, structure.RigidPiece{Bounds: start.box, Adjustment: structure.TerrainBeardThin})
	})
	if len(pieces) == 0 {
		return nil
	}
	return structure.NewBeardifier(pieces, nil)
}

type fossilReference struct {
	key   uint64
	start *netherFossilStart
}

// forFossilReferences follows createReferences and the LongOpenHashSet iteration
// used by StructureManager. The order matters for float32 density accumulation
// and for overlapping piece writes. Zero is the set's separately stored key.
func (g *Nether) forFossilReferences(pos world.ChunkPos, visit func(*netherFossilStart)) {
	var initial [32]fossilReference
	table := initial[:]
	var zero *netherFossilStart
	count := 0
	minX, minZ := pos[0]*16-12, pos[1]*16-12
	maxX, maxZ := pos[0]*16+27, pos[1]*16+27
	for sourceX := pos[0] - 8; sourceX <= pos[0]+8; sourceX++ {
		for sourceZ := pos[1] - 8; sourceZ <= pos[1]+8; sourceZ++ {
			start, err := g.fossilStart(sourceX, sourceZ)
			if err != nil {
				panic(err)
			}
			if start == nil || start.box.Max[0] < minX || start.box.Min[0] > maxX || start.box.Max[2] < minZ || start.box.Min[2] > maxZ {
				continue
			}
			key := uint64(uint32(sourceX)) | uint64(uint32(sourceZ))<<32
			if key == 0 {
				zero = start
			} else {
				insertFossilReference(table, fossilReference{key: key, start: start})
			}
			count++
			if count > len(table)*3/4 {
				larger := make([]fossilReference, len(table)*2)
				for i := len(table) - 1; i >= 0; i-- {
					if table[i].start != nil {
						insertFossilReference(larger, table[i])
					}
				}
				table = larger
			}
		}
	}
	if zero != nil {
		visit(zero)
	}
	for i := len(table) - 1; i >= 0; i-- {
		if table[i].start != nil {
			visit(table[i].start)
		}
	}
}

func insertFossilReference(table []fossilReference, entry fossilReference) {
	hash := entry.key * 0x9e3779b97f4a7c15
	hash ^= hash >> 32
	hash ^= hash >> 16
	mask := len(table) - 1
	slot := int(hash) & mask
	for table[slot].start != nil {
		slot = (slot + 1) & mask
	}
	table[slot] = entry
}
