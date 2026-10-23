package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type strongholdDoor struct {
	x, y, z int32
	dir     string
}
type strongholdGraph struct {
	r        *vanilla.Random
	pieces   []strongholdPiece
	pending  []strongholdPiece
	weights  [11]int
	removed  [11]bool
	previous int
	portal   bool
	imposed  bool
	origin   [3]int32
}

var strongholdWeight = [11]int{40, 5, 20, 20, 10, 5, 5, 5, 5, 10, 20}
var strongholdMaximum = [11]int{0, 5, 0, 0, 6, 5, 5, 4, 4, 2, 1}

func (g *strongholdGraph) add(p strongholdPiece) { g.pieces = append(g.pieces, p) }
func (g *strongholdGraph) make(kind, dir string, x, y, z int32, depth int) (strongholdPiece, bool) {
	w, h, l, offsetX, offsetY := int32(5), int32(5), int32(5), int32(-1), int32(-1)
	switch kind {
	case "stairs":
		w, h, l, offsetY = 5, 11, 5, -7
	case "start":
		w, h, l, offsetX, offsetY = 5, 11, 5, 0, 0
	case "straight", "chest":
		l = 7
	case "prison":
		w, h, l = 9, 5, 11
	case "turn_left", "turn_right":
		w, h, l = 5, 5, 5
	case "crossing":
		w, h, l, offsetX = 11, 7, 11, -4
	case "five":
		w, h, l, offsetX, offsetY = 10, 9, 11, -4, -3
	case "straight_stairs":
		l, h, offsetY = 8, 11, -7
	case "library":
		w, h, l, offsetX = 14, 11, 15, -4
	case "portal":
		w, h, l, offsetX = 11, 8, 16, -4
	case "filler":
		w, h, l = 5, 5, 4
	}
	boxFor := func(height int32) structure.Box {
		switch dir {
		case "north":
			return mbox(x+offsetX, y+offsetY, z-l+1, x+offsetX+w-1, y+offsetY+height-1, z)
		case "south":
			return mbox(x+offsetX, y+offsetY, z, x+offsetX+w-1, y+offsetY+height-1, z+l-1)
		case "west":
			return mbox(x-l+1, y+offsetY, z+offsetX, x, y+offsetY+height-1, z+offsetX+w-1)
		default:
			return mbox(x, y+offsetY, z+offsetX, x+l-1, y+offsetY+height-1, z+offsetX+w-1)
		}
	}
	b := boxFor(h)
	if kind == "start" {
		b = mbox(x, y, z, x+4, y+10, z+4)
	}
	fillerSteps := 0
	if kind == "library" && g.collides(b) {
		h = 6
		b = boxFor(h)
	}
	if kind == "filler" {
		collision, found := structure.Box{}, false
		for _, other := range g.pieces {
			if boxesOverlap(other.box, b) {
				collision, found = other.box, true
				break
			}
		}
		if !found || collision.Min[1] != b.Min[1] {
			return strongholdPiece{}, false
		}
		for length := int32(2); length >= 1; length-- {
			l = length
			candidate := boxFor(h)
			if !boxesOverlap(collision, candidate) {
				l = length + 1
				b = boxFor(h)
				fillerSteps = int(l)
				break
			}
		}
		if fillerSteps == 0 {
			return strongholdPiece{}, false
		}
	}
	if (kind != "filler" && g.collides(b)) || (kind != "filler" && b.Min[1] <= 10) || (kind == "filler" && b.Min[1] <= 1) {
		return strongholdPiece{}, false
	}
	p := strongholdPiece{box: b, kind: kind, dir: dir, depth: depth, seed: int64(uint64(x)*0x9e3779b97f4a7c15 ^ uint64(y)*0xbf58476d1ce4e5b9 ^ uint64(z)), variant: fillerSteps}
	if kind != "start" && kind != "portal" && kind != "filler" && kind != "five" {
		door := g.r.NextInt(5)
		p.door = []string{"opening", "opening", "wood", "grates", "iron"}[door]
	}
	if kind == "straight" {
		p.leftLow = g.r.NextInt(2) == 0
		p.rightLow = g.r.NextInt(2) == 0
	}
	if kind == "crossing" {
		p.variant = int(g.r.NextInt(5))
	}
	if kind == "five" {
		p.door = []string{"opening", "opening", "wood", "grates", "iron"}[g.r.NextInt(5)]
		p.leftLow = g.r.NextBoolean()
		p.leftHigh = g.r.NextBoolean()
		p.rightLow = g.r.NextBoolean()
		p.rightHigh = g.r.NextInt(3) > 0
	}
	return p, true
}
func (g *strongholdGraph) collides(box structure.Box) bool {
	for _, other := range g.pieces {
		if boxesOverlap(other.box, box) {
			return true
		}
	}
	return false
}
func strongholdForward(p strongholdPiece, xoff, yoff int32) strongholdDoor {
	b := p.box
	switch p.dir {
	case "north":
		return strongholdDoor{b.Min[0] + xoff, b.Min[1] + yoff, b.Min[2] - 1, "north"}
	case "south":
		return strongholdDoor{b.Min[0] + xoff, b.Min[1] + yoff, b.Max[2] + 1, "south"}
	case "west":
		return strongholdDoor{b.Min[0] - 1, b.Min[1] + yoff, b.Min[2] + xoff, "west"}
	default:
		return strongholdDoor{b.Max[0] + 1, b.Min[1] + yoff, b.Min[2] + xoff, "east"}
	}
}
func (g *strongholdGraph) grow(p strongholdPiece) {
	if p.depth > 50 {
		return
	}
	forward := strongholdForward(p, 1, 1)
	if p.kind == "start" || p.kind == "stairs" || p.kind == "straight_stairs" || p.kind == "straight" || p.kind == "prison" || p.kind == "chest" {
		g.attach(forward, p.depth)
	}
	if p.kind == "crossing" {
		g.attach(strongholdForward(p, 4, 1), p.depth)
		g.side(p, p.depth, "left", 1, 4)
		g.side(p, p.depth, "right", 1, 4)
	}
	if p.kind == "straight" {
		if p.leftLow {
			g.side(p, p.depth, "left", 1, 2)
		}
		if p.rightLow {
			g.side(p, p.depth, "right", 1, 2)
		}
	}
	if p.kind == "turn_left" {
		side := "right"
		if p.dir == "north" || p.dir == "east" {
			side = "left"
		}
		g.side(p, p.depth, side, 1, 1)
	}
	if p.kind == "turn_right" {
		side := "left"
		if p.dir == "north" || p.dir == "east" {
			side = "right"
		}
		g.side(p, p.depth, side, 1, 1)
	}
	if p.kind == "five" {
		g.attach(strongholdForward(p, 5, 1), p.depth)
		offA, offB := int32(3), int32(5)
		if p.dir == "west" || p.dir == "north" {
			offA, offB = 8-offA, 8-offB
		}
		if p.leftLow {
			g.side(p, p.depth, "left", offA, 1)
		}
		if p.leftHigh {
			g.side(p, p.depth, "left", offB, 7)
		}
		if p.rightLow {
			g.side(p, p.depth, "right", offA, 1)
		}
		if p.rightHigh {
			g.side(p, p.depth, "right", offB, 7)
		}
	}
}
func (g *strongholdGraph) side(p strongholdPiece, depth int, side string, yoff, zoff int32) {
	b := p.box
	d := strongholdDoor{b.Min[0] - 1, b.Min[1] + yoff, b.Min[2] + zoff, "west"}
	switch p.dir {
	case "south", "north":
		if side == "right" {
			d.x, d.dir = b.Max[0]+1, "east"
		}
	case "west", "east":
		d.x, d.z, d.dir = b.Min[0]+zoff, b.Min[2]-1, "north"
		if side == "right" {
			d.z, d.dir = b.Max[2]+1, "south"
		}
	}
	g.attach(d, depth)
}
func (g *strongholdGraph) attach(d strongholdDoor, parentDepth int) {
	depth := parentDepth + 1
	if parentDepth > 50 || abs32(d.x-g.origin[0]) > 112 || abs32(d.z-g.origin[2]) > 112 {
		return
	}
	if g.imposed {
		g.imposed = false
		if p, ok := g.make("five", d.dir, d.x, d.y, d.z, depth); ok {
			g.add(p)
			g.pending = append(g.pending, p)
			return
		}
	}
	total, hasAny := 0, false
	for i, w := range strongholdWeight {
		if g.removed[i] {
			continue
		}
		total += w
		if strongholdMaximum[i] > 0 && g.weights[i] < strongholdMaximum[i] {
			hasAny = true
		}
	}
	if !hasAny {
		return
	}
	names := []string{"straight", "prison", "turn_left", "turn_right", "crossing", "straight_stairs", "stairs", "five", "chest", "library", "portal"}
	for range 5 {
		sel := g.r.NextInt(int32(total))
		for idx, w := range strongholdWeight {
			if g.removed[idx] {
				continue
			}
			sel -= int32(w)
			if sel >= 0 {
				continue
			}
			if idx == g.previous || idx == 9 && depth <= 4 || idx == 10 && depth <= 5 || strongholdMaximum[idx] > 0 && g.weights[idx] >= strongholdMaximum[idx] {
				break
			}
			p, ok := g.make(names[idx], d.dir, d.x, d.y, d.z, depth)
			if !ok {
				continue
			}
			g.weights[idx]++
			g.previous = idx
			if idx == 10 {
				g.portal = true
			}
			if strongholdMaximum[idx] > 0 && g.weights[idx] >= strongholdMaximum[idx] {
				g.removed[idx] = true
			}
			g.add(p)
			g.pending = append(g.pending, p)
			return
		}
	}
	p, ok := g.make("filler", d.dir, d.x, d.y, d.z, depth)
	if ok && p.box.Min[1] > 1 {
		g.add(p)
		g.pending = append(g.pending, p)
	}
}
func buildModernStronghold(in modernStructureBuildInput) (modernStructureStart, bool, error) {
	if in.random == nil {
		return modernStructureStart{}, false, fmt.Errorf("stronghold requires structure random")
	}
	x, z := in.chunkX*16+2, in.chunkZ*16+2
	r := in.random
	for attempt := int64(0); ; attempt++ {
		*r = largeFeatureRandom(in.seed+attempt, in.chunkX, in.chunkZ)
		g := strongholdGraph{r: r, previous: -1, imposed: true}
		dir := []string{"north", "east", "south", "west"}[r.NextInt(4)]
		start, ok := g.make("start", dir, x, 64, z, 0)
		if !ok {
			return modernStructureStart{}, false, nil
		}
		g.origin = [3]int32{start.box.Min[0], start.box.Min[1], start.box.Min[2]}
		g.add(start)
		g.grow(start)
		for len(g.pending) > 0 {
			at := int(r.NextInt(int32(len(g.pending))))
			p := g.pending[at]
			g.pending = append(g.pending[:at], g.pending[at+1:]...)
			g.grow(p)
		}
		if !g.portal {
			continue
		}
		box := emptyStructureBox()
		for _, p := range g.pieces {
			box = unionStructureBox(box, p.box)
		}
		span := box.Max[1] - box.Min[1] + 1
		minY, seaLevel := int32(-64), int32(63)
		target := span + minY + 1
		if maxY := seaLevel - 10; target < maxY {
			target += r.NextInt(maxY - target)
		}
		shift := target - box.Max[1]
		pieces := make([]modernStructurePiece, len(g.pieces))
		rigid := make([]structure.RigidPiece, len(g.pieces))
		box = emptyStructureBox()
		for i, p := range g.pieces {
			p.box.Min[1] += shift
			p.box.Max[1] += shift
			box = unionStructureBox(box, p.box)
			pieces[i] = p
			rigid[i] = structure.RigidPiece{Bounds: p.box, Adjustment: structure.TerrainNone}
		}
		return modernStructureStart{name: in.name, pieces: pieces, rigid: rigid, box: box}, true, nil
	}
}
