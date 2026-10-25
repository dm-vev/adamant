package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SpawnOriginalMobs ports NaturalSpawner.spawnMobsForChunkGeneration: a newly
// generated chunk spawns groups of the creatures of its biome while a random
// draw stays below the biome's creature spawn probability.
func SpawnOriginalMobs(tx *world.Tx, chunk world.ChunkPos) {
	w := tx.World()
	if !w.SpawnMobsEnabled() {
		return
	}
	xo, zo := int(chunk[0])*16, int(chunk[1])*16
	biome := world.JavaBiomeName(tx.Biome(cube.Pos{xo, tx.Range()[1], zo}))
	all, err := world.JavaBiomeNaturalSpawns(biome)
	if err != nil {
		return
	}
	var rows []world.NaturalSpawnEntry
	total := 0
	for _, e := range all {
		if e.Category == "creature" {
			rows = append(rows, e)
			total += e.Weight
		}
	}
	if total <= 0 {
		return
	}
	probability := world.JavaBiomeAttribute(biome, "minecraft:gameplay/creature_world_gen_spawn_probability", .1)
	r := vanilla.NewWorldgenLegacyRandom(0)
	r.SetDecorationSeed(w.Seed(), int32(xo), int32(zo))
	// WorldGenRegion.getRandom, which the spawn rules draw from.
	ruleRandom := vanilla.NewLegacyRandom(w.Seed() ^ int64(chunk[0])*341873128712 ^ int64(chunk[1])*132897987541)
	spawn := w.EntityRegistry().Config().SpawnMob
	if spawn == nil {
		return
	}
	for r.NextFloat() < probability {
		pick := int(r.NextInt(int32(total)))
		var data world.NaturalSpawnEntry
		for _, e := range rows {
			if pick -= e.Weight; pick < 0 {
				data = e
				break
			}
		}
		count := sampleSpawnCount(data, &r)
		group := &world.EntitySpawnGroup{}
		x, z := xo+int(r.NextInt(16)), zo+int(r.NextInt(16))
		startX, startZ := x, z
		t, ok := w.EntityRegistry().Lookup(data.ID)
		if !ok {
			continue
		}
		width := t.BBox(nil).Width()
		for range count {
			success := false
			for attempts := 0; !success && attempts < 4; attempts++ {
				pos := topNonCollidingPos(tx, data.ID, x, z)
				if spawnPositionOk(tx, data.ID, pos) {
					fx := math.Min(math.Max(float64(x), float64(xo)+width), float64(xo)+16-width)
					fz := math.Min(math.Max(float64(z), float64(zo)+width), float64(zo)+16-width)
					at := mgl64.Vec3{fx, float64(pos[1]), fz}
					if !spawnNoCollision(tx, t.BBox(nil).Translate(at)) || !checkSpawnRules(tx, data.ID, cube.PosFromVec3(at), &ruleRandom) {
						continue
					}
					handle := spawn(data.ID, world.EntitySpawnOpts{Position: at, Rotation: cube.Rotation{float64(r.NextFloat() * 360), 0},
						SpawnReason: "chunk_generation", SpawnRandom: &ruleRandom, SpawnGroup: group})
					if handle == nil {
						continue
					}
					e := handle.WorldlessEntity(tx)
					if m, ok := mobOf(e); ok && mobSpawnWalkTarget(m, data.ID, cube.PosFromVec3(at)) && mobSpawnObstruction(tx, data.ID, handle.Type().BBox(e).Translate(at)) {
						tx.AddEntity(handle)
						success = true
					}
				}
				x += int(r.NextInt(5)) - int(r.NextInt(5))
				for z += int(r.NextInt(5)) - int(r.NextInt(5)); x < xo || x >= xo+16 || z < zo || z >= zo+16; z = startZ + int(r.NextInt(5)) - int(r.NextInt(5)) {
					x = startX + int(r.NextInt(5)) - int(r.NextInt(5))
				}
			}
		}
	}
}

// topNonCollidingPos ports NaturalSpawner.getTopNonCollidingPos: the first
// free block above the surface of the column, below the ceiling in the
// Nether, adjusted for how the mob spawns.
func topNonCollidingPos(tx *world.Tx, id string, x, z int) cube.Pos {
	_, leaves := spawnPlacementOf(id)
	pos := cube.Pos{x, tx.JavaMotionBlockingHeight(x, z, !leaves), z}
	if tx.World().Dimension() == world.Nether {
		for {
			pos = pos.Side(cube.FaceDown)
			if isAirAt(tx, pos) || pos[1] <= tx.Range()[0] {
				break
			}
		}
		for {
			pos = pos.Side(cube.FaceDown)
			if !isAirAt(tx, pos) || pos[1] <= tx.Range()[0] {
				break
			}
		}
	}
	if placement, _ := spawnPlacementOf(id); placement == placementOnGround {
		// SpawnPlacementTypes.ON_GROUND.adjustSpawnPosition: into a block
		// below that can be walked through, such as thin snow.
		below := pos.Side(cube.FaceDown)
		if shape, ok := blockShape(tx, below); ok && shape.PathfindableLand {
			return below
		}
	}
	return pos
}

func isAirAt(tx *world.Tx, pos cube.Pos) bool {
	b, ok := tx.BlockLoaded(pos)
	if !ok {
		return false
	}
	return isAir(b)
}
