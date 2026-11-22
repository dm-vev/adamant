package world

import (
	"iter"
	"math"
	"slices"

	"github.com/go-gl/mathgl/mgl64"
)

// playerGridCell is the size in blocks of the cells of a playerGrid.
const playerGridCell = 64

// playerGridReach is the distance in blocks beyond which PlayersNear walks
// every player rather than the cells of the grid.
const playerGridReach = 512

// playerGridCellEntry stores one populated cell and its reusable player index slice.
type playerGridCellEntry struct {
	cell    [2]int32
	indices []int32
}

// playerGrid holds the players of a world by cells of playerGridCell blocks,
// so that the players near a position are found among few rather than among
// every player of the world, as mobs ask several times a tick.
type playerGrid struct {
	// moves is the playerMoves of the world the grid was built at, plus one.
	moves uint64
	// list is the playerList of the world the grid was built from. entries
	// holds indices into list.
	list []*EntityHandle
	// pos caches each player's position at the time the grid was built, in
	// list order, for contiguous distance filtering.
	pos []mgl64.Vec3
	// cells maps a cell coordinate to its index in entries[:nCells].
	cells map[[2]int32]int
	// entries holds the populated cells with reusable index slices.
	entries []playerGridCellEntry
	nCells  int
}

// playerGridCellOf returns the cell of a playerGrid holding x and z.
func playerGridCellOf(x, z float64) [2]int32 {
	return [2]int32{int32(math.Floor(x / playerGridCell)), int32(math.Floor(z / playerGridCell))}
}

// PlayerMoved tells the world of tx that a player in it moved, so that
// finding the players near a position finds it where it is now. Whatever
// sets the position of a player calls it once it did.
func (tx *Tx) PlayerMoved() {
	if tx == nil {
		return
	}
	if w := tx.World(); w != nil {
		w.playerMoves.Add(1)
	}
}

// playersByCell returns the grid of the players of the world, built again
// if a player came, left or moved since, or nil if the grid is out of date
// during a region phase, whose regions may not build it.
func (w *World) playersByCell(tx *Tx) *playerGrid {
	moves := w.playerMoves.Load()
	g := &w.playerGrid
	if g.moves == moves+1 {
		return g
	}
	if tx.region != nil {
		return nil
	}
	g.moves, g.list = moves+1, w.playerList
	if cap(g.pos) < len(g.list) {
		g.pos = make([]mgl64.Vec3, len(g.list))
	} else {
		g.pos = g.pos[:len(g.list)]
	}
	if g.cells == nil {
		g.cells = make(map[[2]int32]int)
	} else {
		clear(g.cells)
	}
	g.nCells = 0
	for i, handle := range g.list {
		p := handle.data.Pos
		g.pos[i] = p
		c := playerGridCellOf(p[0], p[2])
		idx, ok := g.cells[c]
		if !ok {
			idx = g.nCells
			g.nCells++
			g.cells[c] = idx
			if idx == len(g.entries) {
				g.entries = append(g.entries, playerGridCellEntry{cell: c})
			} else {
				g.entries[idx].cell = c
				g.entries[idx].indices = g.entries[idx].indices[:0]
			}
		}
		g.entries[idx].indices = append(g.entries[idx].indices, int32(i))
	}
	return g
}

// collectNearPlayers appends indices of players in indices whose cached position
// in gPos is within r2 (squared distance) of pos, returning the extended slice
// and whether at least one index was appended.
func collectNearPlayers(dst []int32, indices []int32, gPos []mgl64.Vec3, pos mgl64.Vec3, r2 float64) ([]int32, bool) {
	before := len(dst)
	for _, i := range indices {
		p := gPos[i]
		dx, dy, dz := p[0]-pos[0], p[1]-pos[1], p[2]-pos[2]
		if dx*dx+dy*dy+dz*dz <= r2 {
			dst = append(dst, i)
		}
	}
	return dst, len(dst) > before
}

// PlayersNear returns the players within r blocks of pos, and maybe some
// further, in the order Players returns them in.
func (tx *Tx) PlayersNear(pos mgl64.Vec3, r float64) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		tx.playersNear(pos, r, yield)
	}
}

func (w *World) yieldAllPlayers(tx *Tx, yield func(Entity) bool) {
	for p := range w.allPlayers(tx) {
		if !yield(p) {
			return
		}
	}
}

func (tx *Tx) playersNear(pos mgl64.Vec3, r float64, yield func(Entity) bool) {
	if r < 0 {
		return
	}
	w := tx.World()
	g := w.playersByCell(tx)
	if g == nil || r >= playerGridReach {
		w.yieldAllPlayers(tx, yield)
		return
	}
	if g.nCells == 0 {
		return
	}
	// Pad r slightly so float64 rounding never drops a player on the r boundary.
	rPad := r + 1e-6
	r2 := rPad * rPad
	var buf [128]int32
	near := buf[:0]
	gPos := g.pos
	list := g.list
	lo, hi := playerGridCellOf(pos[0]-r, pos[2]-r), playerGridCellOf(pos[0]+r, pos[2]+r)
	span := (int64(hi[0]) - int64(lo[0]) + 1) * (int64(hi[1]) - int64(lo[1]) + 1)
	nonEmptyCells := 0
	if span > int64(g.nCells) {
		for k := range g.nCells {
			e := &g.entries[k]
			if e.cell[0] < lo[0] || e.cell[0] > hi[0] || e.cell[1] < lo[1] || e.cell[1] > hi[1] {
				continue
			}
			var added bool
			if near, added = collectNearPlayers(near, e.indices, gPos, pos, r2); added {
				nonEmptyCells++
			}
		}
	} else {
		for x := lo[0]; x <= hi[0]; x++ {
			for z := lo[1]; z <= hi[1]; z++ {
				idx, ok := g.cells[[2]int32{x, z}]
				if !ok {
					continue
				}
				var added bool
				if near, added = collectNearPlayers(near, g.entries[idx].indices, gPos, pos, r2); added {
					nonEmptyCells++
				}
			}
		}
	}
	if nonEmptyCells == 0 {
		return
	}
	if nonEmptyCells > 1 {
		slices.Sort(near)
	}
	for _, i := range near {
		handle := list[i]
		if reg := tx.region; reg != nil {
			tx.lockShared()
			owned := reg.ownsEntity(w, handle)
			tx.unlockShared()
			if !owned {
				continue
			}
		}
		state := handle.state
		if state == nil || handle.w != w {
			continue
		}
		if ent := state.entity(tx, handle); ent != nil {
			if !yield(ent) {
				return
			}
		}
	}
}
