package overworld

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type mansionPlacementData struct {
	pos      [3]int32
	rotation structure.Rotation
	wall     string
}
type mansionPlacer struct {
	origin    [3]int32
	rotation  structure.Rotation
	seed      int64
	random    *vanilla.Random
	templates map[string]*structure.Template
	pieces    []modernStructurePiece
}

func newMansionPlacer(origin [3]int32, rotation structure.Rotation, seed int64, random *vanilla.Random) *mansionPlacer {
	return &mansionPlacer{origin: origin, rotation: rotation, seed: seed, random: random, templates: make(map[string]*structure.Template), pieces: make([]modernStructurePiece, 0, 256)}
}
func mansionRot(a, b structure.Rotation) structure.Rotation {
	return structure.Rotation((int(a) + int(b)) % 4)
}
func mansionDirection(d mansionDir, r structure.Rotation) mansionDir {
	return mansionDir((int(d) + int(r)) % 4)
}
func mansionMove(pos [3]int32, d mansionDir, r structure.Rotation, n int32) [3]int32 {
	dx, dz := mansionDirection(d, r).delta()
	pos[0] += int32(dx) * n
	pos[2] += int32(dz) * n
	return pos
}
func mansionAbove(pos [3]int32, y int32) [3]int32 { pos[1] += y; return pos }
func mansionPieceBounds(t *structure.Template, origin [3]int32, r structure.Rotation, m structure.Mirror) structure.Box {
	b := emptyStructureBox()
	for _, x := range [2]int32{0, t.Size[0] - 1} {
		for _, y := range [2]int32{0, t.Size[1] - 1} {
			for _, z := range [2]int32{0, t.Size[2] - 1} {
				v := structure.TransformPosition([3]int32{x, y, z}, m, r)
				v[0] += origin[0]
				v[1] += origin[1]
				v[2] += origin[2]
				q := structure.Box{Min: v, Max: v}
				b = unionStructureBox(b, q)
			}
		}
	}
	return b
}
func (m *mansionPlacer) add(name string, pos [3]int32, rotation structure.Rotation, mirror structure.Mirror) error {
	t := m.templates[name]
	if t == nil {
		var err error
		t, err = structure.Load("woodland_mansion/" + name)
		if err != nil {
			return err
		}
		m.templates[name] = t
	}
	m.pieces = append(m.pieces, modernMansionPiece{template: t, origin: pos, rotation: rotation, mirror: mirror, seed: m.seed, box: mansionPieceBounds(t, pos, rotation, mirror)})
	return nil
}
func (m *mansionPlacer) entrance(data *mansionPlacementData) error {
	pos := mansionMove(data.pos, mansionWest, data.rotation, 9)
	if err := m.add("entrance", pos, data.rotation, structure.NoMirror); err != nil {
		return err
	}
	data.pos = mansionMove(data.pos, mansionSouth, data.rotation, 16)
	return nil
}
func (m *mansionPlacer) wallPiece(data *mansionPlacementData) error {
	pos := mansionMove(data.pos, mansionEast, data.rotation, 7)
	if err := m.add(data.wall, pos, data.rotation, structure.NoMirror); err != nil {
		return err
	}
	data.pos = mansionMove(data.pos, mansionSouth, data.rotation, 8)
	return nil
}
func (m *mansionPlacer) turn(data *mansionPlacementData) error {
	data.pos = mansionMove(data.pos, mansionSouth, data.rotation, -1)
	if err := m.add("wall_corner", data.pos, data.rotation, structure.NoMirror); err != nil {
		return err
	}
	data.pos = mansionMove(data.pos, mansionSouth, data.rotation, -7)
	data.pos = mansionMove(data.pos, mansionWest, data.rotation, -6)
	data.rotation = mansionRot(data.rotation, structure.Clockwise90)
	return nil
}
func (m *mansionPlacer) innerTurn(data *mansionPlacementData) {
	data.pos = mansionMove(data.pos, mansionSouth, data.rotation, 6)
	data.pos = mansionMove(data.pos, mansionEast, data.rotation, 8)
	data.rotation = mansionRot(data.rotation, structure.CounterClockwise90)
}
func (m *mansionPlacer) outerWalls(data *mansionPlacementData, g *mansionGrid, dir mansionDir, sx, sy, ex, ey int) error {
	gx, gy := sx, sy
	start := dir
	for {
		dx, dz := dir.delta()
		if !mansionHouse(g, gx+dx, gy+dz) {
			if err := m.turn(data); err != nil {
				return err
			}
			dir = dir.cw()
			if gx != ex || gy != ey || start != dir {
				if err := m.wallPiece(data); err != nil {
					return err
				}
			}
		} else {
			cx, cz := dir.delta()
			ccx, ccz := dir.ccw().delta()
			if mansionHouse(g, gx+cx, gy+cz) && mansionHouse(g, gx+cx+ccx, gy+cz+ccz) {
				m.innerTurn(data)
				gx += cx
				gy += cz
				dir = dir.ccw()
			} else {
				gx += cx
				gy += cz
				if gx != ex || gy != ey || start != dir {
					if err := m.wallPiece(data); err != nil {
						return err
					}
				}
			}
		}
		if gx == ex && gy == ey && start == dir {
			return nil
		}
	}
}
func (m *mansionPlacer) roof(origin [3]int32, r structure.Rotation, g, above *mansionGrid, startX, startY int) error {
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			pos := mansionMove(origin, mansionSouth, r, int32(8+(y-startY)*8))
			pos = mansionMove(pos, mansionEast, r, int32((x-startX)*8))
			isAbove := above != nil && mansionHouse(above, x, y)
			if mansionHouse(g, x, y) && !isAbove {
				if err := m.add("roof", mansionAbove(pos, 3), r, structure.NoMirror); err != nil {
					return err
				}
				if !mansionHouse(g, x+1, y) {
					if err := m.add("roof_front", mansionMove(pos, mansionEast, r, 6), r, structure.NoMirror); err != nil {
						return err
					}
				}
				if !mansionHouse(g, x-1, y) {
					p := mansionMove(mansionMove(pos, mansionEast, r, 0), mansionSouth, r, 7)
					if err := m.add("roof_front", p, mansionRot(r, structure.Clockwise180), structure.NoMirror); err != nil {
						return err
					}
				}
				if !mansionHouse(g, x, y-1) {
					p := mansionMove(pos, mansionWest, r, 1)
					if err := m.add("roof_front", p, mansionRot(r, structure.CounterClockwise90), structure.NoMirror); err != nil {
						return err
					}
				}
				if !mansionHouse(g, x, y+1) {
					p := mansionMove(mansionMove(pos, mansionEast, r, 6), mansionSouth, r, 6)
					if err := m.add("roof_front", p, mansionRot(r, structure.Clockwise90), structure.NoMirror); err != nil {
						return err
					}
				}
			}
		}
	}
	if above != nil {
		for y := 0; y < g.h; y++ {
			for x := 0; x < g.w; x++ {
				pos := mansionMove(origin, mansionSouth, r, int32(8+(y-startY)*8))
				pos = mansionMove(pos, mansionEast, r, int32((x-startX)*8))
				if mansionHouse(g, x, y) && mansionHouse(above, x, y) {
					if !mansionHouse(g, x+1, y) {
						if err := m.add("small_wall", mansionMove(pos, mansionEast, r, 7), r, structure.NoMirror); err != nil {
							return err
						}
					}
					if !mansionHouse(g, x-1, y) {
						p := mansionMove(mansionMove(pos, mansionWest, r, 1), mansionSouth, r, 6)
						if err := m.add("small_wall", p, mansionRot(r, structure.Clockwise180), structure.NoMirror); err != nil {
							return err
						}
					}
					if !mansionHouse(g, x, y-1) {
						p := mansionMove(mansionMove(pos, mansionWest, r, 0), mansionNorth, r, 1)
						if err := m.add("small_wall", p, mansionRot(r, structure.CounterClockwise90), structure.NoMirror); err != nil {
							return err
						}
					}
					if !mansionHouse(g, x, y+1) {
						p := mansionMove(mansionMove(pos, mansionEast, r, 6), mansionSouth, r, 7)
						if err := m.add("small_wall", p, mansionRot(r, structure.Clockwise90), structure.NoMirror); err != nil {
							return err
						}
					}
					if !mansionHouse(g, x+1, y) {
						if !mansionHouse(g, x, y-1) {
							p := mansionMove(mansionMove(pos, mansionEast, r, 7), mansionNorth, r, 2)
							if err := m.add("small_wall_corner", p, r, structure.NoMirror); err != nil {
								return err
							}
						}
						if !mansionHouse(g, x, y+1) {
							p := mansionMove(mansionMove(pos, mansionEast, r, 8), mansionSouth, r, 7)
							if err := m.add("small_wall_corner", p, mansionRot(r, structure.Clockwise90), structure.NoMirror); err != nil {
								return err
							}
						}
					}
					if !mansionHouse(g, x-1, y) {
						if !mansionHouse(g, x, y-1) {
							p := mansionMove(mansionMove(pos, mansionWest, r, 2), mansionNorth, r, 1)
							if err := m.add("small_wall_corner", p, mansionRot(r, structure.CounterClockwise90), structure.NoMirror); err != nil {
								return err
							}
						}
						if !mansionHouse(g, x, y+1) {
							p := mansionMove(mansionMove(pos, mansionWest, r, 1), mansionSouth, r, 8)
							if err := m.add("small_wall_corner", p, mansionRot(r, structure.Clockwise180), structure.NoMirror); err != nil {
								return err
							}
						}
					}
				}
			}
		}
	}
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			pos := mansionMove(origin, mansionSouth, r, int32(8+(y-startY)*8))
			pos = mansionMove(pos, mansionEast, r, int32((x-startX)*8))
			if mansionHouse(g, x, y) && (above == nil || !mansionHouse(above, x, y)) {
				if !mansionHouse(g, x+1, y) {
					p := mansionMove(pos, mansionEast, r, 6)
					if !mansionHouse(g, x, y+1) {
						p = mansionMove(p, mansionSouth, r, 6)
						if err := m.add("roof_corner", p, r, structure.NoMirror); err != nil {
							return err
						}
					} else if mansionHouse(g, x+1, y+1) {
						p = mansionMove(p, mansionSouth, r, 5)
						if err := m.add("roof_inner_corner", p, r, structure.NoMirror); err != nil {
							return err
						}
					}
					if !mansionHouse(g, x, y-1) {
						if err := m.add("roof_corner", mansionMove(pos, mansionEast, r, 6), mansionRot(r, structure.CounterClockwise90), structure.NoMirror); err != nil {
							return err
						}
					} else if mansionHouse(g, x+1, y-1) {
						p := mansionMove(mansionMove(pos, mansionEast, r, 9), mansionNorth, r, 2)
						if err := m.add("roof_inner_corner", p, mansionRot(r, structure.Clockwise90), structure.NoMirror); err != nil {
							return err
						}
					}
				}
				if !mansionHouse(g, x-1, y) {
					p := mansionMove(mansionMove(pos, mansionEast, r, 0), mansionSouth, r, 0)
					if !mansionHouse(g, x, y+1) {
						if err := m.add("roof_corner", mansionMove(p, mansionSouth, r, 6), mansionRot(r, structure.Clockwise90), structure.NoMirror); err != nil {
							return err
						}
					} else if mansionHouse(g, x-1, y+1) {
						inner := mansionMove(mansionMove(p, mansionSouth, r, 8), mansionWest, r, 3)
						if err := m.add("roof_inner_corner", inner, mansionRot(r, structure.CounterClockwise90), structure.NoMirror); err != nil {
							return err
						}
					}
					if !mansionHouse(g, x, y-1) {
						if err := m.add("roof_corner", p, mansionRot(r, structure.Clockwise180), structure.NoMirror); err != nil {
							return err
						}
					} else if mansionHouse(g, x-1, y-1) {
						if err := m.add("roof_inner_corner", mansionMove(p, mansionSouth, r, 1), mansionRot(r, structure.Clockwise180), structure.NoMirror); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}
func (m *mansionPlacer) create(plan *mansionPlan) error {
	data := mansionPlacementData{m.origin, m.rotation, "wall_flat"}
	if err := m.entrance(&data); err != nil {
		return err
	}
	second := mansionPlacementData{mansionAbove(data.pos, 8), m.rotation, "wall_window"}
	base := plan.base
	third := plan.third
	sx, sy := plan.sx, plan.sy
	ex, ey := sx, sy-1
	if err := m.outerWalls(&data, base, mansionSouth, sx, sy, ex, ey); err != nil {
		return err
	}
	if err := m.outerWalls(&second, base, mansionSouth, sx, sy, ex, ey); err != nil {
		return err
	}
	thirdData := mansionPlacementData{mansionAbove(data.pos, 19), data.rotation, "wall_window"}
	done := false
	for y := 0; y < third.h && !done; y++ {
		for x := third.w - 1; x >= 0 && !done; x-- {
			if mansionHouse(third, x, y) {
				thirdData.pos = mansionMove(thirdData.pos, mansionSouth, m.rotation, int32(8+(y-sy)*8))
				thirdData.pos = mansionMove(thirdData.pos, mansionEast, m.rotation, int32((x-sx)*8))
				if err := m.wallPiece(&thirdData); err != nil {
					return err
				}
				if err := m.outerWalls(&thirdData, third, mansionSouth, x, y, x, y); err != nil {
					return err
				}
				done = true
			}
		}
	}
	if err := m.roof(mansionAbove(m.origin, 16), m.rotation, base, third, sx, sy); err != nil {
		return err
	}
	if err := m.roof(mansionAbove(m.origin, 27), m.rotation, third, nil, sx, sy); err != nil {
		return err
	}
	return m.rooms(plan)
}

func (m *mansionPlacer) rooms(plan *mansionPlan) error {
	for floor := 0; floor < 3; floor++ {
		floorOrigin := mansionAbove(m.origin, int32(8*floor))
		if floor == 2 {
			floorOrigin = mansionAbove(floorOrigin, 3)
		}
		g := plan.base
		if floor == 2 {
			g = plan.third
		}
		rooms := plan.rooms[floor]
		southCarpet := "carpet_south_2"
		westCarpet := "carpet_west_2"
		if floor == 0 {
			southCarpet = "carpet_south_1"
			westCarpet = "carpet_west_1"
		}
		for y := 0; y < g.h; y++ {
			for x := 0; x < g.w; x++ {
				if g.get(x, y) != mansionCorridor {
					continue
				}
				pos := mansionMove(floorOrigin, mansionSouth, m.rotation, int32(8+(y-plan.sy)*8))
				pos = mansionMove(pos, mansionEast, m.rotation, int32((x-plan.sx)*8))
				if err := m.add("corridor_floor", pos, m.rotation, structure.NoMirror); err != nil {
					return err
				}
				if g.get(x, y-1) == mansionCorridor || rooms.get(x, y-1)&mansionCorridorFlag != 0 {
					if err := m.add("carpet_north", mansionAbove(mansionMove(pos, mansionEast, m.rotation, 1), 1), m.rotation, structure.NoMirror); err != nil {
						return err
					}
				}
				if g.get(x+1, y) == mansionCorridor || rooms.get(x+1, y)&mansionCorridorFlag != 0 {
					p := mansionMove(mansionMove(pos, mansionSouth, m.rotation, 1), mansionEast, m.rotation, 5)
					if err := m.add("carpet_east", mansionAbove(p, 1), m.rotation, structure.NoMirror); err != nil {
						return err
					}
				}
				if g.get(x, y+1) == mansionCorridor || rooms.get(x, y+1)&mansionCorridorFlag != 0 {
					p := mansionMove(mansionMove(pos, mansionSouth, m.rotation, 5), mansionWest, m.rotation, 1)
					if err := m.add(southCarpet, p, m.rotation, structure.NoMirror); err != nil {
						return err
					}
				}
				if g.get(x-1, y) == mansionCorridor || rooms.get(x-1, y)&mansionCorridorFlag != 0 {
					p := mansionMove(mansionMove(pos, mansionWest, m.rotation, 1), mansionNorth, m.rotation, 1)
					if err := m.add(westCarpet, p, m.rotation, structure.NoMirror); err != nil {
						return err
					}
				}
			}
		}
		wall, door := "indoors_wall_2", "indoors_door_2"
		if floor == 0 {
			wall, door = "indoors_wall_1", "indoors_door_1"
		}
		doorDirs := make([]mansionDir, 0, 4)
		for y := 0; y < g.h; y++ {
			for x := 0; x < g.w; x++ {
				start := floor == 2 && g.get(x, y) == mansionStartRoom
				if g.get(x, y) != mansionRoom && !start {
					continue
				}
				data := rooms.get(x, y)
				typ := data & mansionRoomTypeMask
				id := data & mansionRoomIDMask
				start = start && data&mansionCorridorFlag != 0
				doorDirs = doorDirs[:0]
				if data&mansionDoorFlag != 0 {
					for d := mansionNorth; d <= mansionWest; d++ {
						dx, dz := d.delta()
						if g.get(x+dx, y+dz) == mansionCorridor {
							doorDirs = append(doorDirs, d)
						}
					}
				}
				doorDir := mansionNoDoor
				if len(doorDirs) > 0 {
					doorDir = doorDirs[int(m.random.NextInt(int32(len(doorDirs))))]
				} else if data&mansionOriginFlag != 0 {
					doorDir = mansionUp
				}
				hasDoor := len(doorDirs) > 0 || data&mansionOriginFlag != 0
				roomPos := mansionMove(floorOrigin, mansionSouth, m.rotation, int32(8+(y-plan.sy)*8))
				roomPos = mansionMove(roomPos, mansionEast, m.rotation, int32(-1+(x-plan.sx)*8))
				if mansionHouse(g, x-1, y) && plan.rooms[floor].get(x-1, y)&mansionRoomIDMask != id {
					n := wall
					if hasDoor && doorDir == mansionWest {
						n = door
					}
					if err := m.add(n, roomPos, m.rotation, structure.NoMirror); err != nil {
						return err
					}
				}
				if g.get(x+1, y) == mansionCorridor && !start {
					n := wall
					if hasDoor && doorDir == mansionEast {
						n = door
					}
					if err := m.add(n, mansionMove(roomPos, mansionEast, m.rotation, 8), m.rotation, structure.NoMirror); err != nil {
						return err
					}
				}
				if mansionHouse(g, x, y+1) && plan.rooms[floor].get(x, y+1)&mansionRoomIDMask != id {
					n := wall
					if hasDoor && doorDir == mansionSouth {
						n = door
					}
					p := mansionMove(mansionMove(roomPos, mansionSouth, m.rotation, 7), mansionEast, m.rotation, 7)
					if err := m.add(n, p, mansionRot(m.rotation, structure.Clockwise90), structure.NoMirror); err != nil {
						return err
					}
				}
				if g.get(x, y-1) == mansionCorridor && !start {
					n := wall
					if hasDoor && doorDir == mansionNorth {
						n = door
					}
					p := mansionMove(mansionMove(roomPos, mansionNorth, m.rotation, 1), mansionEast, m.rotation, 7)
					if err := m.add(n, p, mansionRot(m.rotation, structure.Clockwise90), structure.NoMirror); err != nil {
						return err
					}
				}
				if typ == mansionRoom1x1 {
					if err := m.room1x1(roomPos, doorDir, hasDoor, floor); err != nil {
						return err
					}
				} else if typ == mansionRoom1x2 && hasDoor {
					rd := plan.roomDirection(g, x, y, floor, id)
					if err := m.room1x2(roomPos, rd, doorDir, data&mansionStairsFlag != 0, floor); err != nil {
						return err
					}
				} else if typ == mansionRoom2x2 && hasDoor && doorDir != mansionUp {
					rd := doorDir.cw()
					dx, dz := rd.delta()
					if plan.rooms[floor].get(x+dx, y+dz)&mansionRoomIDMask != id {
						rd = rd.opposite()
					}
					if err := m.room2x2(roomPos, rd, doorDir, floor); err != nil {
						return err
					}
				} else if typ == mansionRoom2x2 && doorDir == mansionUp {
					if err := m.room2x2Secret(roomPos, floor); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
func (m *mansionPlacer) roomName(floor int, kind string) string {
	r := m.random
	first := floor == 0
	switch kind {
	case "1x1":
		prefix := "1x1_b"
		if first {
			prefix = "1x1_a"
		}
		return fmt.Sprintf("%s%d", prefix, r.NextInt(5)+1)
	case "1x1_secret":
		return fmt.Sprintf("1x1_as%d", r.NextInt(4)+1)
	case "1x2_side":
		if first {
			return fmt.Sprintf("1x2_a%d", r.NextInt(9)+1)
		}
		return fmt.Sprintf("1x2_c%d", r.NextInt(4)+1)
	case "1x2_front":
		if first {
			return fmt.Sprintf("1x2_b%d", r.NextInt(5)+1)
		}
		return fmt.Sprintf("1x2_d%d", r.NextInt(5)+1)
	case "1x2_secret":
		if first {
			return fmt.Sprintf("1x2_s%d", r.NextInt(2)+1)
		}
		_ = r.NextInt(1)
		return "1x2_se1"
	case "2x2":
		if first {
			return fmt.Sprintf("2x2_a%d", r.NextInt(4)+1)
		}
		return fmt.Sprintf("2x2_b%d", r.NextInt(5)+1)
	case "2x2_secret":
		return "2x2_s1"
	}
	return ""
}
func (m *mansionPlacer) room1x1(roomPos [3]int32, door mansionDir, hasDoor bool, floor int) error {
	rot := structure.None
	name := m.roomName(floor, "1x1")
	if !hasDoor || door == mansionUp || door == mansionNoDoor {
		name = m.roomName(floor, "1x1_secret")
	} else if door != mansionEast {
		switch door {
		case mansionNorth:
			rot = structure.CounterClockwise90
		case mansionWest:
			rot = structure.Clockwise180
		case mansionSouth:
			rot = structure.Clockwise90
		}
	}
	zero := [3]int32{1, 0, 0}
	switch rot {
	case structure.CounterClockwise90:
		zero = [3]int32{1, 0, 6}
	case structure.Clockwise90:
		zero = [3]int32{7, 0, 0}
	case structure.Clockwise180:
		zero = [3]int32{7, 0, 6}
	}
	v := structure.TransformPosition(zero, structure.NoMirror, m.rotation)
	pos := roomPos
	pos[0] += v[0]
	pos[2] += v[2]
	return m.add(name, pos, mansionRot(m.rotation, rot), structure.NoMirror)
}
func (m *mansionPlacer) room1x2(roomPos [3]int32, roomDir, doorDir mansionDir, stairs bool, floor int) error {
	r := m.rotation
	sideName := func() string {
		if stairs && floor > 0 {
			return "1x2_c_stairs"
		}
		return m.roomName(floor, "1x2_side")
	}
	frontName := func() string {
		if stairs && floor > 0 {
			return "1x2_d_stairs"
		}
		return m.roomName(floor, "1x2_front")
	}
	pos := roomPos
	name := ""
	rot := r
	mirror := structure.NoMirror
	switch {
	case doorDir == mansionEast && roomDir == mansionSouth:
		pos = mansionMove(pos, mansionEast, r, 1)
		name = sideName()
	case doorDir == mansionEast && roomDir == mansionNorth:
		pos = mansionMove(mansionMove(pos, mansionEast, r, 1), mansionSouth, r, 6)
		name = sideName()
		mirror = structure.LeftRight
	case doorDir == mansionWest && roomDir == mansionNorth:
		pos = mansionMove(mansionMove(pos, mansionEast, r, 7), mansionSouth, r, 6)
		name = sideName()
		rot = mansionRot(r, structure.Clockwise180)
	case doorDir == mansionWest && roomDir == mansionSouth:
		pos = mansionMove(pos, mansionEast, r, 7)
		name = sideName()
		mirror = structure.FrontBack
	case doorDir == mansionSouth && roomDir == mansionEast:
		pos = mansionMove(pos, mansionEast, r, 1)
		name = sideName()
		rot = mansionRot(r, structure.Clockwise90)
		mirror = structure.LeftRight
	case doorDir == mansionSouth && roomDir == mansionWest:
		pos = mansionMove(pos, mansionEast, r, 7)
		name = sideName()
		rot = mansionRot(r, structure.Clockwise90)
	case doorDir == mansionNorth && roomDir == mansionWest:
		pos = mansionMove(mansionMove(pos, mansionEast, r, 7), mansionSouth, r, 6)
		name = sideName()
		rot = mansionRot(r, structure.Clockwise90)
		mirror = structure.FrontBack
	case doorDir == mansionNorth && roomDir == mansionEast:
		pos = mansionMove(mansionMove(pos, mansionEast, r, 1), mansionSouth, r, 6)
		name = sideName()
		rot = mansionRot(r, structure.CounterClockwise90)
	case doorDir == mansionSouth && roomDir == mansionNorth:
		pos = mansionMove(mansionMove(pos, mansionEast, r, 1), mansionNorth, r, 8)
		name = frontName()
	case doorDir == mansionNorth && roomDir == mansionSouth:
		pos = mansionMove(mansionMove(pos, mansionEast, r, 7), mansionSouth, r, 14)
		name = frontName()
		rot = mansionRot(r, structure.Clockwise180)
	case doorDir == mansionWest && roomDir == mansionEast:
		pos = mansionMove(pos, mansionEast, r, 15)
		name = frontName()
		rot = mansionRot(r, structure.Clockwise90)
	case doorDir == mansionEast && roomDir == mansionWest:
		pos = mansionMove(mansionMove(pos, mansionWest, r, 7), mansionSouth, r, 6)
		name = frontName()
		rot = mansionRot(r, structure.CounterClockwise90)
	case doorDir == mansionUp && roomDir == mansionEast:
		pos = mansionMove(pos, mansionEast, r, 15)
		name = m.roomName(floor, "1x2_secret")
		rot = mansionRot(r, structure.Clockwise90)
	case doorDir == mansionUp && roomDir == mansionSouth:
		pos = mansionMove(mansionMove(pos, mansionEast, r, 1), mansionNorth, r, 0)
		name = m.roomName(floor, "1x2_secret")
	}
	if name == "" {
		return fmt.Errorf("mansion: unsupported 1x2 room orientation door=%d room=%d", doorDir, roomDir)
	}
	return m.add(name, pos, rot, mirror)
}
func (m *mansionPlacer) room2x2(roomPos [3]int32, roomDir, doorDir mansionDir, floor int) error {
	east, south := int32(0), int32(0)
	rot := m.rotation
	mirror := structure.NoMirror
	switch {
	case doorDir == mansionEast && roomDir == mansionSouth:
		east = -7
	case doorDir == mansionEast && roomDir == mansionNorth:
		east = -7
		south = 6
		mirror = structure.LeftRight
	case doorDir == mansionNorth && roomDir == mansionEast:
		east = 1
		south = 14
		rot = mansionRot(m.rotation, structure.CounterClockwise90)
	case doorDir == mansionNorth && roomDir == mansionWest:
		east = 7
		south = 14
		rot = mansionRot(m.rotation, structure.CounterClockwise90)
		mirror = structure.LeftRight
	case doorDir == mansionSouth && roomDir == mansionWest:
		east = 7
		south = -8
		rot = mansionRot(m.rotation, structure.Clockwise90)
	case doorDir == mansionSouth && roomDir == mansionEast:
		east = 1
		south = -8
		rot = mansionRot(m.rotation, structure.Clockwise90)
		mirror = structure.LeftRight
	case doorDir == mansionWest && roomDir == mansionNorth:
		east = 15
		south = 6
		rot = mansionRot(m.rotation, structure.Clockwise180)
	case doorDir == mansionWest && roomDir == mansionSouth:
		east = 15
		mirror = structure.FrontBack
	}
	pos := mansionMove(mansionMove(roomPos, mansionEast, m.rotation, east), mansionSouth, m.rotation, south)
	return m.add(m.roomName(floor, "2x2"), pos, rot, mirror)
}
func (m *mansionPlacer) room2x2Secret(roomPos [3]int32, floor int) error {
	pos := mansionMove(roomPos, mansionEast, m.rotation, 1)
	return m.add(m.roomName(floor, "2x2_secret"), pos, m.rotation, structure.NoMirror)
}
