package world

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	portalMinWidth  = 2
	portalMaxWidth  = 21
	portalMinHeight = 3
	portalMaxHeight = 21
)

// NetherPortalFrame describes the interior of an activated Nether portal. The frame exposes
// metadata that can be used to position entities or rebuild the portal when necessary.
type NetherPortalFrame struct {
	axis          cube.Axis
	width, height int
	corner        cube.Pos
}

// Axis returns the horizontal axis of the portal.
func (f NetherPortalFrame) Axis() cube.Axis {
	return f.axis
}

// Width returns the interior width of the portal in blocks.
func (f NetherPortalFrame) Width() int {
	return f.width
}

// Height returns the interior height of the portal in blocks.
func (f NetherPortalFrame) Height() int {
	return f.height
}

// Corner returns the position of the bottom-left interior block of the portal frame.
func (f NetherPortalFrame) Corner() cube.Pos {
	return f.corner
}

// Contains reports whether the interior of the frame contains the position passed.
func (f NetherPortalFrame) Contains(pos cube.Pos) bool {
	if f.width == 0 || f.height == 0 {
		return false
	}
	local := pos.Sub(f.corner)
	switch f.axis {
	case cube.X:
		return local[0] >= 0 && local[0] < f.width && local[1] >= 0 && local[1] < f.height
	case cube.Z:
		return local[2] >= 0 && local[2] < f.width && local[1] >= 0 && local[1] < f.height
	default:
		return false
	}
}

// Center returns the centre position of the portal interior.
func (f NetherPortalFrame) Center() mgl64.Vec3 {
	if f.width == 0 || f.height == 0 {
		return f.corner.Vec3Centre()
	}
	min := f.corner
	var max cube.Pos
	switch f.axis {
	case cube.X:
		max = f.corner.Add(axisOffset(cube.X, f.width-1)).Add(cube.Pos{0, f.height - 1, 0})
	case cube.Z:
		max = f.corner.Add(axisOffset(cube.Z, f.width-1)).Add(cube.Pos{0, f.height - 1, 0})
	default:
		return f.corner.Vec3Centre()
	}
	return mgl64.Vec3{
		float64(min[0]+max[0])/2 + 0.5,
		float64(min[1]+max[1])/2 + 0.5,
		float64(min[2]+max[2])/2 + 0.5,
	}
}

// TryCreateNetherPortal attempts to activate a Nether portal at the position passed. If a valid
// obsidian frame surrounds the position, the frame interior is filled with portal blocks and true is returned.
// Otherwise false is returned.
func TryCreateNetherPortal(tx *Tx, pos cube.Pos) bool {
	for _, axis := range []cube.Axis{cube.X, cube.Z} {
		frame, ok := detectPortalFrame(tx, pos, axis)
		if !ok {
			continue
		}
		frame.fill(tx)
		return true
	}
	return false
}

// NetherPortalFrameAt attempts to resolve the Nether portal frame that contains the position passed. The
// axis argument specifies the expected orientation of the portal. If no valid frame exists, false is returned.
func NetherPortalFrameAt(tx *Tx, pos cube.Pos, axis cube.Axis) (NetherPortalFrame, bool) {
	frame, ok := detectPortalFrame(tx, pos, axis)
	if !ok || !frame.Contains(pos) {
		return NetherPortalFrame{}, false
	}
	return frame, true
}

// FindNearestNetherPortal searches for the closest activated Nether portal within the given radius. The centre
// parameter is used as the reference point. If no portal is found, false is returned.
func FindNearestNetherPortal(tx *Tx, centre cube.Pos, radius int) (NetherPortalFrame, bool) {
	if radius < 0 {
		return NetherPortalFrame{}, false
	}
	rng := tx.Range()
	minY := intMax(centre[1]-radius, rng.Min())
	maxY := intMin(centre[1]+radius, rng.Max())
	bestDist := math.MaxFloat64
	var bestFrame NetherPortalFrame
	found := false

	for y := minY; y <= maxY; y++ {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				pos := cube.Pos{centre[0] + dx, y, centre[2] + dz}
				if pos.OutOfBounds(rng) {
					continue
				}
				if name, props := tx.Block(pos).EncodeBlock(); name != "minecraft:nether_portal" {
					continue
				} else {
					axis := axisFromProps(props)
					frame, ok := detectPortalFrame(tx, pos, axis)
					if !ok || !frame.Contains(pos) {
						continue
					}
					dist := distanceSq(centre, pos)
					if dist < bestDist {
						bestDist = dist
						bestFrame = frame
						found = true
					}
				}
			}
		}
	}
	return bestFrame, found
}

// BuildNetherPortal constructs a minimal Nether portal centred around the position passed. The axis specifies the
// orientation of the portal. The created frame is returned.
func BuildNetherPortal(tx *Tx, centre cube.Pos, axis cube.Axis) NetherPortalFrame {
	if axis != cube.X && axis != cube.Z {
		axis = cube.Z
	}
	rng := tx.Range()
	width, height := portalMinWidth, portalMinHeight
	highest := tx.HighestBlock(centre[0], centre[2])
	baseY := highest + 1
	minBase := rng.Min() + 1
	maxBase := rng.Max() - height - 1
	if maxBase < minBase {
		maxBase = minBase
	}
	if baseY < minBase {
		baseY = minBase
	}
	if baseY > maxBase {
		baseY = maxBase
	}
	base := cube.Pos{centre[0], baseY, centre[2]}
	shift := width / 2
	corner := base.Add(axisOffset(axis, -shift))

	frame := NetherPortalFrame{axis: axis, width: width, height: height, corner: corner}
	frame.build(tx)
	frame.fill(tx)
	return frame
}

func detectPortalFrame(tx *Tx, origin cube.Pos, axis cube.Axis) (NetherPortalFrame, bool) {
	if axis != cube.X && axis != cube.Z {
		return NetherPortalFrame{}, false
	}
	rng := tx.Range()
	if origin.OutOfBounds(rng) {
		return NetherPortalFrame{}, false
	}

	// Find the lowest interior block for the frame.
	current := origin
	for current[1] > rng.Min() {
		below := current.Add(cube.Pos{0, -1, 0})
		if !isPortalInteriorBlock(tx.Block(below), axis) {
			break
		}
		current = below
	}
	below := current.Add(cube.Pos{0, -1, 0})
	if below.OutOfBounds(rng) || !isPortalFrameBlock(tx.Block(below)) {
		return NetherPortalFrame{}, false
	}

	left := 0
	for left < portalMaxWidth {
		candidate := current.Add(axisOffset(axis, -(left + 1)))
		if candidate.OutOfBounds(rng) {
			return NetherPortalFrame{}, false
		}
		if isPortalFrameBlock(tx.Block(candidate)) {
			break
		}
		if !isPortalInteriorBlock(tx.Block(candidate), axis) {
			return NetherPortalFrame{}, false
		}
		left++
	}
	if left >= portalMaxWidth {
		return NetherPortalFrame{}, false
	}

	right := 0
	for right < portalMaxWidth {
		candidate := current.Add(axisOffset(axis, right+1))
		if candidate.OutOfBounds(rng) {
			return NetherPortalFrame{}, false
		}
		if isPortalFrameBlock(tx.Block(candidate)) {
			break
		}
		if !isPortalInteriorBlock(tx.Block(candidate), axis) {
			return NetherPortalFrame{}, false
		}
		right++
	}
	width := left + right + 1
	if width < portalMinWidth || width > portalMaxWidth {
		return NetherPortalFrame{}, false
	}

	corner := current.Add(axisOffset(axis, -left))
	height := 0
	for height < portalMaxHeight {
		row := corner.Add(cube.Pos{0, height, 0})
		if row.OutOfBounds(rng) {
			return NetherPortalFrame{}, false
		}
		valid := true
		for offset := 0; offset < width; offset++ {
			pos := row.Add(axisOffset(axis, offset))
			if !isPortalInteriorBlock(tx.Block(pos), axis) {
				valid = false
				break
			}
		}
		if !valid {
			break
		}
		height++
	}
	if height < portalMinHeight || height > portalMaxHeight {
		return NetherPortalFrame{}, false
	}

	// Validate vertical columns and cap stones.
	for y := 0; y < height; y++ {
		leftPos := corner.Add(axisOffset(axis, -1)).Add(cube.Pos{0, y, 0})
		rightPos := corner.Add(axisOffset(axis, width)).Add(cube.Pos{0, y, 0})
		if leftPos.OutOfBounds(rng) || rightPos.OutOfBounds(rng) {
			return NetherPortalFrame{}, false
		}
		if !isPortalFrameBlock(tx.Block(leftPos)) || !isPortalFrameBlock(tx.Block(rightPos)) {
			return NetherPortalFrame{}, false
		}
	}
	for x := -1; x <= width; x++ {
		bottom := corner.Add(axisOffset(axis, x)).Add(cube.Pos{0, -1, 0})
		top := corner.Add(axisOffset(axis, x)).Add(cube.Pos{0, height, 0})
		if bottom.OutOfBounds(rng) || top.OutOfBounds(rng) {
			return NetherPortalFrame{}, false
		}
		if !isPortalFrameBlock(tx.Block(bottom)) || !isPortalFrameBlock(tx.Block(top)) {
			return NetherPortalFrame{}, false
		}
	}

	return NetherPortalFrame{axis: axis, width: width, height: height, corner: corner}, true
}

func (f NetherPortalFrame) fill(tx *Tx) {
	portal := netherPortalBlock(f.axis)
	for y := 0; y < f.height; y++ {
		for x := 0; x < f.width; x++ {
			pos := f.corner.Add(axisOffset(f.axis, x)).Add(cube.Pos{0, y, 0})
			tx.SetBlock(pos, portal, nil)
		}
	}
}

func (f NetherPortalFrame) build(tx *Tx) {
	obsidian := obsidianBlock()
	for x := -1; x <= f.width; x++ {
		bottom := f.corner.Add(axisOffset(f.axis, x)).Add(cube.Pos{0, -1, 0})
		top := f.corner.Add(axisOffset(f.axis, x)).Add(cube.Pos{0, f.height, 0})
		tx.SetBlock(bottom, obsidian, nil)
		tx.SetBlock(top, obsidian, nil)
	}
	for y := 0; y < f.height; y++ {
		left := f.corner.Add(axisOffset(f.axis, -1)).Add(cube.Pos{0, y, 0})
		right := f.corner.Add(axisOffset(f.axis, f.width)).Add(cube.Pos{0, y, 0})
		tx.SetBlock(left, obsidian, nil)
		tx.SetBlock(right, obsidian, nil)
	}
}

func axisOffset(axis cube.Axis, n int) cube.Pos {
	switch axis {
	case cube.X:
		return cube.Pos{n, 0, 0}
	case cube.Z:
		return cube.Pos{0, 0, n}
	default:
		return cube.Pos{}
	}
}

func isPortalFrameBlock(b Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:obsidian"
}

func isPortalInteriorBlock(b Block, axis cube.Axis) bool {
	name, props := b.EncodeBlock()
	switch name {
	case "minecraft:air", "minecraft:cave_air", "minecraft:void_air", "minecraft:fire", "minecraft:soul_fire":
		return true
	case "minecraft:nether_portal":
		return axis == axisFromProps(props)
	default:
		return false
	}
}

func axisFromProps(props map[string]any) cube.Axis {
	if s, ok := props["portal_axis"].(string); ok && s == "x" {
		return cube.X
	}
	return cube.Z
}

func netherPortalBlock(axis cube.Axis) Block {
	block, ok := BlockByName("minecraft:nether_portal", map[string]any{"portal_axis": axis.String()})
	if !ok {
		panic("minecraft:nether_portal block state not registered")
	}
	return block
}

func obsidianBlock() Block {
	block, ok := BlockByName("minecraft:obsidian", nil)
	if !ok {
		panic("minecraft:obsidian block state not registered")
	}
	return block
}

func distanceSq(a, b cube.Pos) float64 {
	dx := float64(a[0] - b[0])
	dy := float64(a[1] - b[1])
	dz := float64(a[2] - b[2])
	return dx*dx + dy*dy + dz*dz
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
