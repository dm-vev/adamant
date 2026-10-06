package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

type thinConnections struct {
	north bool
	east  bool
	south bool
	west  bool
}

func allThinConnections() []thinConnections {
	connections := make([]thinConnections, 0, 16)
	for mask := range 16 {
		connections = append(connections, thinConnections{
			north: mask&1 != 0, east: mask&2 != 0, south: mask&4 != 0, west: mask&8 != 0,
		})
	}
	return connections
}

func (c thinConnections) properties() map[string]any {
	return map[string]any{
		"minecraft:connection_north": c.north,
		"minecraft:connection_east":  c.east,
		"minecraft:connection_south": c.south,
		"minecraft:connection_west":  c.west,
	}
}

func (c thinConnections) update(pos cube.Pos, tx *world.Tx) thinConnections {
	return thinConnections{
		north: thinConnects(tx, pos, cube.FaceNorth),
		east:  thinConnects(tx, pos, cube.FaceEast),
		south: thinConnects(tx, pos, cube.FaceSouth),
		west:  thinConnects(tx, pos, cube.FaceWest),
	}
}

func withThinConnections(block world.Block, c thinConnections) world.Block {
	switch b := block.(type) {
	case GlassPane:
		b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection = c.north, c.east, c.south, c.west
		return b
	case StainedGlassPane:
		b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection = c.north, c.east, c.south, c.west
		return b
	case HardenedGlassPane:
		b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection = c.north, c.east, c.south, c.west
		return b
	case HardenedStainedGlassPane:
		b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection = c.north, c.east, c.south, c.west
		return b
	case IronBars:
		b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection = c.north, c.east, c.south, c.west
		return b
	case CopperBars:
		b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection = c.north, c.east, c.south, c.west
		return b
	default:
		panic("unsupported thin block")
	}
}

func placeThin(pos cube.Pos, face cube.Face, block world.Block, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, block)
	if !used {
		return false
	}
	block = withThinConnections(block, (thinConnections{}).update(pos, tx))
	place(tx, pos, block, user, ctx)
	return placed(ctx)
}

func updateThinBlock(pos cube.Pos, tx *world.Tx, block world.Block, c thinConnections) {
	updated := c.update(pos, tx)
	if updated != c {
		tx.SetBlock(pos, withThinConnections(block, updated), nil)
	}
}

func reconstructThinConnections(block world.Block, pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	connections := thinConnectionsOf(block)
	changed := false
	if hasMissingProperty(missing, "minecraft:connection_north") {
		connections.north = thinConnects(bs, pos, cube.FaceNorth)
		changed = true
	}
	if hasMissingProperty(missing, "minecraft:connection_east") {
		connections.east = thinConnects(bs, pos, cube.FaceEast)
		changed = true
	}
	if hasMissingProperty(missing, "minecraft:connection_south") {
		connections.south = thinConnects(bs, pos, cube.FaceSouth)
		changed = true
	}
	if hasMissingProperty(missing, "minecraft:connection_west") {
		connections.west = thinConnects(bs, pos, cube.FaceWest)
		changed = true
	}
	if !changed {
		return block
	}
	return withThinConnections(block, connections)
}

func thinConnectionsOf(block world.Block) thinConnections {
	switch b := block.(type) {
	case GlassPane:
		return thinConnections{b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection}
	case StainedGlassPane:
		return thinConnections{b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection}
	case HardenedGlassPane:
		return thinConnections{b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection}
	case HardenedStainedGlassPane:
		return thinConnections{b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection}
	case IronBars:
		return thinConnections{b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection}
	case CopperBars:
		return thinConnections{b.NorthConnection, b.EastConnection, b.SouthConnection, b.WestConnection}
	default:
		panic("unsupported thin block")
	}
}

// thinConnects connects to thin blocks, walls, or an opposing solid face.
func thinConnects(bs world.BlockSource, pos cube.Pos, face cube.Face) bool {
	sidePos := pos.Side(face)
	m := bs.Block(sidePos).Model()
	_, isThin := m.(model.Thin)
	_, isWall := m.(model.Wall)
	return isThin || isWall || m.FaceSolid(sidePos, face.Opposite(), bs)
}

func hasMissingProperty(missing []string, property string) bool {
	for _, name := range missing {
		if name == property {
			return true
		}
	}
	return false
}

func (p GlassPane) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	return reconstructThinConnections(p, pos, missing, bs)
}

func (p StainedGlassPane) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	return reconstructThinConnections(p, pos, missing, bs)
}

func (p HardenedGlassPane) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	return reconstructThinConnections(p, pos, missing, bs)
}

func (p HardenedStainedGlassPane) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	return reconstructThinConnections(p, pos, missing, bs)
}

func (i IronBars) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	return reconstructThinConnections(i, pos, missing, bs)
}

func (c CopperBars) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	return reconstructThinConnections(c, pos, missing, bs)
}
