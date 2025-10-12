package populate

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/pmgen/rand"
	"github.com/go-gl/mathgl/mgl64"
)

type Ore struct {
	Types []OreType
}

func (o Ore) Populate(w *world.World, pos world.ChunkPos, _ *chunk.Chunk, r *rand.Random) {
	<-w.ExecNoChunkGen(func(tx *world.Tx) {
		for _, ore := range o.Types {
			for i := 0; i < ore.ClusterCount; i++ {
				p := cube.Pos{
					int(r.Range(pos[0]<<4, (pos[0]<<4)+15)),
					int(r.Range(int32(ore.MinHeight), int32(ore.MaxHeight))),
					int(r.Range(pos[1]<<4, (pos[1]<<4)+15)),
				}
				if tx.Block(p) == ore.Replaces {
					ore.Place(tx, p, r)
				}
			}
		}
	})
}

type OreType struct {
	Material, Replaces        world.Block
	ClusterCount, ClusterSize int
	MinHeight, MaxHeight      int
}

func (o OreType) Place(tx *world.Tx, pos cube.Pos, r *rand.Random) {
	clusterSize := float64(o.ClusterSize)
	vec := pos.Vec3()
	angle := r.Float64() * math.Pi
	offset := mgl64.Vec2{math.Cos(angle), math.Sin(angle)}.Mul(clusterSize / 8)
	x1, x2 := (vec[0])+8+offset[0], (vec[0])+8-offset[0]
	z1, z2 := (vec[2])+8+offset[1], (vec[2])+8-offset[1]
	y1, y2 := (vec[1])+float64(r.Int31n(3))+2, vec[1]+float64(r.Int31n(3))+2
	for i := float64(0); i <= clusterSize; i++ {
		seedX := x1 + (x2-x1)*i/clusterSize
		seedY := y1 + (y2-y1)*i/clusterSize
		seedZ := z1 + (z2-z1)*i/clusterSize
		size := ((math.Sin(i*(math.Pi/clusterSize))+1)*r.Float64()*clusterSize/16 + 1) / 2

		startX := float64(int(seedX - size))
		startY := float64(int(seedY - size))
		startZ := float64(int(seedZ - size))
		endX := float64(int(seedX + size))
		endY := float64(int(seedY + size))
		endZ := float64(int(seedZ + size))

		for xx := startX; xx <= endX; xx++ {
			sizeX := (xx + 0.5 - seedX) / size
			sizeX *= sizeX

			if sizeX < 1 {
				for yy := startY; yy <= endY; yy++ {
					sizeY := (yy + 0.5 - seedY) / size
					sizeY *= sizeY

					if yy > 0 && (sizeX+sizeY) < 1 {
						for zz := startZ; zz <= endZ; zz++ {
							sizeZ := (zz + 0.5 - seedZ) / size
							sizeZ *= sizeZ

							target := cube.Pos{int(xx), int(yy), int(zz)}

							if (sizeX+sizeY+sizeZ) < 1 && tx.Block(target) == o.Replaces {
								tx.SetBlock(target, o.Material, nil)
							}
						}
					}
				}
			}
		}
	}
}
