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

// playerGrid holds the players of a world by cells of playerGridCell blocks,
// so that the players near a position are found among few rather than among
// every player of the world, as mobs ask several times a tick.
type playerGrid struct {
	// moves is the playerMoves of the world the grid was built at, plus one.
	moves uint64
	// list is the playerList of the world the grid was built from. cells
	// holds indices in it.
	list  []*EntityHandle
	cells map[[2]int32][]int32
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
	if g.cells == nil {
		g.cells = make(map[[2]int32][]int32)
	}
	clear(g.cells)
	for i, handle := range g.list {
		c := playerGridCellOf(handle.data.Pos[0], handle.data.Pos[2])
		g.cells[c] = append(g.cells[c], int32(i))
	}
	return g
}

// PlayersNear returns the players within r blocks of pos, and maybe some
// further, in the order Players returns them in.
func (tx *Tx) PlayersNear(pos mgl64.Vec3, r float64) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		w := tx.World()
		g := w.playersByCell(tx)
		if g == nil || r >= playerGridReach {
			for p := range w.allPlayers(tx) {
				if !yield(p) {
					return
				}
			}
			return
		}
		var near []int32
		lo, hi := playerGridCellOf(pos[0]-r, pos[2]-r), playerGridCellOf(pos[0]+r, pos[2]+r)
		for x := lo[0]; x <= hi[0]; x++ {
			for z := lo[1]; z <= hi[1]; z++ {
				near = append(near, g.cells[[2]int32{x, z}]...)
			}
		}
		slices.Sort(near)
		for _, i := range near {
			handle := g.list[i]
			if r := tx.region; r != nil {
				tx.lockShared()
				owned := r.ownsEntity(w, handle)
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
}
