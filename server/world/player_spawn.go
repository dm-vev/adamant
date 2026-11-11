package world

import (
	"context"
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
// dimensions keep their fixed spawn. The chunks of the candidates are loaded,
// or generated, as they are reached, while the owner waits: FindPlayerSpawn
// searches without waiting.
func (tx *Tx) PlayerSpawnPosition(suggestion cube.Pos) mgl64.Vec3 {
	s := newSpawnSearch(tx, suggestion)
	for {
		if pos, done := s.check(tx); done {
			return pos
		}
	}
}

// FindPlayerSpawn finds the position a player spawns at around suggestion
// as PlayerSpawnPosition does, trying the same candidates in the same order,
// but loads the chunk of a candidate without the owner waiting for it, and
// tries the candidate once it loaded, as Java's PlayerSpawnFinder does: the
// search of a first join holds the owner for the checks alone. It must be
// called off the owner, and returns ctx.Err() if ctx ends, or ErrWorldClosed
// if the world closes, before the search ends.
func (w *World) FindPlayerSpawn(ctx context.Context, suggestion cube.Pos) (mgl64.Vec3, error) {
	ctx, err := callContext(ctx)
	if err != nil {
		return mgl64.Vec3{}, err
	}
	if err := checkOwnerWait("FindPlayerSpawn"); err != nil {
		return mgl64.Vec3{}, err
	}
	found := make(chan mgl64.Vec3, 1)
	w.Do(func(tx *Tx) {
		s := newSpawnSearch(tx, suggestion)
		w.searchSpawn(tx, &s, found)
	})
	select {
	case pos := <-found:
		return pos, nil
	case <-ctx.Done():
		return mgl64.Vec3{}, ctx.Err()
	case <-w.closeStarted:
		return mgl64.Vec3{}, ErrWorldClosed
	}
}

// searchSpawn checks the candidates of s whose chunks are loaded and sends
// the position found to found. At the first candidate whose chunk is not, it
// returns, and goes on once the chunk loaded.
func (w *World) searchSpawn(tx *Tx, s *spawnSearch, found chan<- mgl64.Vec3) {
	for {
		if pos := s.chunk(); !w.chunkLoaded(pos) {
			w.loadChunkAsync(tx, pos, func(tx *Tx, _ *Column) { w.searchSpawn(tx, s, found) })
			return
		}
		if pos, done := s.check(tx); done {
			found <- pos
			return
		}
	}
}

// spawnSearch is the state of a search of PlayerSpawnPosition: the
// candidates around the suggestion and the next one to check.
type spawnSearch struct {
	suggestion cube.Pos
	// middle is set for a search whose result is the middle of the
	// suggestion, and fixed for one whose result is the suggestion moved
	// out of blocks: neither tries candidates.
	middle, fixed bool
	radius, side  int
	count         int
	coprime       int
	offset        int
	next          int
}

// newSpawnSearch returns the search of PlayerSpawnPosition around
// suggestion.
func newSpawnSearch(tx *Tx, suggestion cube.Pos) spawnSearch {
	w := tx.World()
	s := spawnSearch{suggestion: suggestion}
	if w.Dimension() != Overworld {
		s.middle = true
		return s
	}
	if w.DefaultGameMode() == GameModeAdventure {
		s.fixed = true
		return s
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
	s.radius, s.side = radius, int(side)
	s.count = int(min(1024, side*side))
	s.coprime = 17
	if s.count <= 16 {
		s.coprime = s.count - 1
	}
	s.offset = rand.IntN(s.count)
	return s
}

// candidate returns the column of the next candidate, or false once every
// candidate was tried.
func (s *spawnSearch) candidate() (x, z int, ok bool) {
	if s.middle || s.fixed || s.next >= s.count {
		return 0, 0, false
	}
	value := (s.offset + s.coprime*s.next) % s.count
	return s.suggestion[0] + value%s.side - s.radius, s.suggestion[2] + value/s.side - s.radius, true
}

// chunk returns the chunk the next check of s reads.
func (s *spawnSearch) chunk() ChunkPos {
	if x, z, ok := s.candidate(); ok {
		return chunkPosFromBlockPos(cube.Pos{x, 0, z})
	}
	return chunkPosFromBlockPos(s.suggestion)
}

// check checks the next candidate of s, returning the position found and
// true if the search ended.
func (s *spawnSearch) check(tx *Tx) (mgl64.Vec3, bool) {
	if s.middle {
		return s.suggestion.Vec3Middle(), true
	}
	x, z, ok := s.candidate()
	if !ok {
		return tx.fixupSpawnHeight(s.suggestion), true
	}
	s.next++
	if pos, ok := tx.levelRespawnPos(x, z); ok && tx.noCollisionNoLiquid(pos) {
		return pos.Vec3Middle(), true
	}
	return mgl64.Vec3{}, false
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
