package world

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// playerSpawnBox is the player's bounding box relative to its feet.
var playerSpawnBox = cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)

// PlayerSpawnPosition ports PlayerSpawnFinder.findSpawn for the overworld,
// the only level Java respawns players at without a respawn block: unless
// the default game mode is adventure, players spawn at a random safe surface
// position within respawn_radius of the suggested spawn (capped by the
// distance to the world border). Up to 1024 candidates are tried in a
// coprime-stride order before falling back to the suggestion itself. Other
// dimensions keep their fixed spawn.
func (tx *Tx) PlayerSpawnPosition(suggestion cube.Pos) mgl64.Vec3 {
	w := tx.World()
	if w.Dimension() != Overworld {
		return suggestion.Vec3Middle()
	}
	if w.DefaultGameMode() == GameModeAdventure {
		return tx.fixupSpawnHeight(suggestion)
	}
	radius := max(0, w.GameRuleInt("respawn_radius"))
	w.set.Lock()
	minX, maxX, minZ, maxZ := w.border.bounds()
	w.set.Unlock()
	x, z := float64(suggestion[0]), float64(suggestion[2])
	distToBorder := int(math.Floor(min(x-minX, maxX-x, z-minZ, maxZ-z)))
	radius = min(radius, distToBorder)
	if distToBorder <= 1 {
		radius = 1
	}
	side := int64(radius)*2 + 1
	count := int(min(1024, side*side))
	coprime := 17
	if count <= 16 {
		coprime = count - 1
	}
	offset := rand.IntN(count)
	for i := range count {
		value := (offset + coprime*i) % count
		targetX := suggestion[0] + value%int(side) - radius
		targetZ := suggestion[2] + value/int(side) - radius
		if pos, ok := tx.levelRespawnPos(targetX, targetZ); ok && tx.noCollisionNoLiquid(pos) {
			return pos.Vec3Middle()
		}
	}
	return tx.fixupSpawnHeight(suggestion)
}

// fixupSpawnHeight moves the suggestion up out of blocks, then down onto the
// first obstruction below it.
func (tx *Tx) fixupSpawnHeight(pos cube.Pos) mgl64.Vec3 {
	r := tx.Range()
	for !tx.noCollisionNoLiquid(pos) && pos[1] < r.Max() {
		pos[1]++
	}
	pos[1]--
	for tx.noCollisionNoLiquid(pos) && pos[1] > r.Min() {
		pos[1]--
	}
	pos[1]++
	return pos.Vec3Middle()
}

// levelRespawnPos ports PlayerSpawnFinder.getLevelRespawnPos: columns whose
// surface is liquid above the ocean floor are rejected; otherwise the player
// stands on the first block below the motion-blocking height with a full top
// face, unless liquid is reached first.
func (tx *Tx) levelRespawnPos(x, z int) (cube.Pos, bool) {
	r := tx.Range()
	surface := max(tx.HighestBlock(x, z), r.Min())
	topY, floorY := r.Min()-1, r.Min()-1
	for y := surface; y >= r.Min(); y-- {
		pos := cube.Pos{x, y, z}
		blocks := len(tx.Block(pos).Model().BBox(pos, tx)) > 0
		_, liquid := tx.Liquid(pos)
		if topY < r.Min() && (blocks || liquid) {
			topY = y
		}
		if blocks {
			floorY = y
			break
		}
	}
	if topY < r.Min() || surface <= topY && surface > floorY {
		return cube.Pos{}, false
	}
	for y := topY + 1; y >= r.Min(); y-- {
		pos := cube.Pos{x, y, z}
		if _, liquid := tx.Liquid(pos); liquid {
			break
		}
		if tx.Block(pos).Model().FaceSolid(pos, cube.FaceUp, tx) {
			return pos.Side(cube.FaceUp), true
		}
	}
	return cube.Pos{}, false
}

// noCollisionNoLiquid reports whether a player standing at the bottom centre
// of pos would intersect no block collision box and no liquid.
func (tx *Tx) noCollisionNoLiquid(pos cube.Pos) bool {
	box := playerSpawnBox.Translate(pos.Vec3Middle())
	lo, hi := box.Min(), box.Max()
	for bx := int(math.Floor(lo[0])); bx <= int(math.Floor(hi[0])); bx++ {
		for by := int(math.Floor(lo[1])); by <= int(math.Floor(hi[1])); by++ {
			for bz := int(math.Floor(lo[2])); bz <= int(math.Floor(hi[2])); bz++ {
				p := cube.Pos{bx, by, bz}
				if p.OutOfBounds(tx.Range()) {
					continue
				}
				if _, liquid := tx.Liquid(p); liquid {
					return false
				}
				for _, bb := range tx.Block(p).Model().BBox(p, tx) {
					if bb.Translate(p.Vec3()).IntersectsWith(box) {
						return false
					}
				}
			}
		}
	}
	return true
}
