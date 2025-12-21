package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
)

// Allow is a block that permits edits for players.
type Allow struct {
	solid
	transparent
	sourceWaterDisplacer
}

// SideClosed ...
func (Allow) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// EncodeItem ...
func (Allow) EncodeItem() (name string, meta int16) {
	return "minecraft:allow", 0
}

// EncodeBlock ...
func (Allow) EncodeBlock() (string, map[string]any) {
	return "minecraft:allow", nil
}

// Deny is a block that prevents edits for players.
type Deny struct {
	solid
	transparent
	sourceWaterDisplacer
}

// SideClosed ...
func (Deny) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// EncodeItem ...
func (Deny) EncodeItem() (name string, meta int16) {
	return "minecraft:deny", 0
}

// EncodeBlock ...
func (Deny) EncodeBlock() (string, map[string]any) {
	return "minecraft:deny", nil
}

// BorderBlock is a wall-like border block.
type BorderBlock struct {
	transparent
	sourceWaterDisplacer

	// NorthConnection is the type of connection in the north direction of the post.
	NorthConnection WallConnectionType
	// EastConnection is the type of connection in the east direction of the post.
	EastConnection WallConnectionType
	// SouthConnection is the type of connection in the south direction of the post.
	SouthConnection WallConnectionType
	// WestConnection is the type of connection in the west direction of the post.
	WestConnection WallConnectionType
	// Post is if the wall is extended to the full height of a block or not.
	Post bool
}

// EncodeItem ...
func (BorderBlock) EncodeItem() (name string, meta int16) {
	return "minecraft:border_block", 0
}

// EncodeBlock ...
func (b BorderBlock) EncodeBlock() (string, map[string]any) {
	properties := map[string]any{
		"wall_connection_type_north": b.NorthConnection.String(),
		"wall_connection_type_east":  b.EastConnection.String(),
		"wall_connection_type_south": b.SouthConnection.String(),
		"wall_connection_type_west":  b.WestConnection.String(),
		"wall_post_bit":              boolByte(b.Post),
	}
	return "minecraft:border_block", properties
}

// Model ...
func (b BorderBlock) Model() world.BlockModel {
	return model.Wall{
		NorthConnection: b.NorthConnection.Height(),
		EastConnection:  b.EastConnection.Height(),
		SouthConnection: b.SouthConnection.Height(),
		WestConnection:  b.WestConnection.Height(),
		Post:            b.Post,
	}
}

// BreakInfo ...
func (b BorderBlock) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, pickaxeHarvestable, pickaxeEffective, oneOf(b))
}

// NeighbourUpdateTick ...
func (b BorderBlock) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	b, connectionsUpdated := b.calculateConnections(tx, pos)
	b, postUpdated := b.calculatePost(tx, pos)
	if connectionsUpdated || postUpdated {
		tx.SetBlock(pos, b, nil)
	}
}

// UseOnBlock ...
func (b BorderBlock) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, b)
	if !used {
		return
	}
	b, _ = b.calculateConnections(tx, pos)
	b, _ = b.calculatePost(tx, pos)
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// SideClosed ...
func (BorderBlock) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// ConnectionType returns the connection type of the border block in the given direction.
func (b BorderBlock) ConnectionType(direction cube.Direction) WallConnectionType {
	switch direction {
	case cube.North:
		return b.NorthConnection
	case cube.East:
		return b.EastConnection
	case cube.South:
		return b.SouthConnection
	case cube.West:
		return b.WestConnection
	}
	panic("unknown direction")
}

// WithConnectionType returns the border block with the given connection type in the given direction.
func (b BorderBlock) WithConnectionType(direction cube.Direction, connection WallConnectionType) BorderBlock {
	switch direction {
	case cube.North:
		b.NorthConnection = connection
	case cube.East:
		b.EastConnection = connection
	case cube.South:
		b.SouthConnection = connection
	case cube.West:
		b.WestConnection = connection
	}
	return b
}

// calculateConnections calculates the correct connections for the border block at a given position in a world.
func (b BorderBlock) calculateConnections(tx *world.Tx, pos cube.Pos) (BorderBlock, bool) {
	var updated bool
	abovePos := pos.Add(cube.Pos{0, 1, 0})
	above := tx.Block(abovePos)
	for _, face := range cube.HorizontalFaces() {
		sidePos := pos.Side(face)
		side := tx.Block(sidePos)
		connected := side.Model().FaceSolid(sidePos, face.Opposite(), tx)
		if !connected {
			switch side.(type) {
			case Wall, BorderBlock:
				connected = true
			case WoodFenceGate:
				if gate, ok := side.(WoodFenceGate); ok {
					connected = gate.Facing.Face().Axis() != face.Axis()
				}
			default:
				if _, ok := side.Model().(model.Thin); ok {
					connected = true
				}
			}
		}
		var connectionType WallConnectionType
		if connected {
			connectionType = ShortWallConnection()
			boxes := above.Model().BBox(abovePos, tx)
			for _, bb := range boxes {
				if bb.Min().Y() == 0 {
					xOverlap := bb.Min().X() < 0.75 && bb.Max().X() > 0.25
					zOverlap := bb.Min().Z() < 0.75 && bb.Max().Z() > 0.25
					var tall bool
					switch face {
					case cube.FaceNorth:
						tall = xOverlap && bb.Max().Z() > 0.75
					case cube.FaceEast:
						tall = bb.Min().X() < 0.25 && zOverlap
					case cube.FaceSouth:
						tall = xOverlap && bb.Min().Z() < 0.25
					case cube.FaceWest:
						tall = bb.Max().X() > 0.75 && zOverlap
					}
					if tall {
						connectionType = TallWallConnection()
						break
					}
				}
			}
		}
		if b.ConnectionType(face.Direction()) != connectionType {
			updated = true
			b = b.WithConnectionType(face.Direction(), connectionType)
		}
	}
	return b, updated
}

// calculatePost calculates the correct post bit for the border block at a given position in a world.
func (b BorderBlock) calculatePost(tx *world.Tx, pos cube.Pos) (BorderBlock, bool) {
	var updated bool
	abovePos := pos.Add(cube.Pos{0, 1, 0})
	above := tx.Block(abovePos)
	connections := 0
	for _, face := range cube.HorizontalFaces() {
		if b.ConnectionType(face.Direction()) != NoWallConnection() {
			connections++
		}
	}
	var post bool
	switch above := above.(type) {
	case Lantern:
		post = !above.Hanging
	case Campfire:
		post = true
	default:
		post = connections != 2 || (b.NorthConnection != NoWallConnection()) != (b.SouthConnection != NoWallConnection())
	}
	if b.Post != post {
		updated = true
		b.Post = post
	}
	return b, updated
}

// ClientRequestPlaceholderBlock is a placeholder block.
type ClientRequestPlaceholderBlock struct {
	transparent
	replaceable
	empty
}

// EncodeItem ...
func (ClientRequestPlaceholderBlock) EncodeItem() (name string, meta int16) {
	return "minecraft:client_request_placeholder_block", 0
}

// EncodeBlock ...
func (ClientRequestPlaceholderBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:client_request_placeholder_block", nil
}

// InfoUpdate is a placeholder block used for updates.
type InfoUpdate struct {
	transparent
	replaceable
	empty
}

// EncodeItem ...
func (InfoUpdate) EncodeItem() (name string, meta int16) {
	return "minecraft:info_update", 0
}

// EncodeBlock ...
func (InfoUpdate) EncodeBlock() (string, map[string]any) {
	return "minecraft:info_update", nil
}

// InfoUpdate2 is a placeholder block used for updates.
type InfoUpdate2 struct {
	transparent
	replaceable
	empty
}

// EncodeItem ...
func (InfoUpdate2) EncodeItem() (name string, meta int16) {
	return "minecraft:info_update2", 0
}

// EncodeBlock ...
func (InfoUpdate2) EncodeBlock() (string, map[string]any) {
	return "minecraft:info_update2", nil
}

// Reserved6 is a placeholder block.
type Reserved6 struct {
	transparent
	replaceable
	empty
}

// EncodeItem ...
func (Reserved6) EncodeItem() (name string, meta int16) {
	return "minecraft:reserved6", 0
}

// EncodeBlock ...
func (Reserved6) EncodeBlock() (string, map[string]any) {
	return "minecraft:reserved6", nil
}

// UnknownBlock is a placeholder for unknown blocks.
type UnknownBlock struct {
	transparent
	replaceable
	empty
}

// EncodeItem ...
func (UnknownBlock) EncodeItem() (name string, meta int16) {
	return "minecraft:unknown", 0
}

// EncodeBlock ...
func (UnknownBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:unknown", nil
}

// StructureVoid is an invisible, non-colliding block.
type StructureVoid struct {
	transparent
	replaceable
	empty
}

// EncodeItem ...
func (StructureVoid) EncodeItem() (name string, meta int16) {
	return "minecraft:structure_void", 0
}

// EncodeBlock ...
func (StructureVoid) EncodeBlock() (string, map[string]any) {
	return "minecraft:structure_void", nil
}

// DeprecatedAnvil is a placeholder anvil block.
type DeprecatedAnvil struct {
	solid
}

// BreakInfo ...
func (d DeprecatedAnvil) BreakInfo() BreakInfo {
	return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, oneOf(d))
}

// EncodeItem ...
func (DeprecatedAnvil) EncodeItem() (name string, meta int16) {
	return "minecraft:deprecated_anvil", 0
}

// EncodeBlock ...
func (DeprecatedAnvil) EncodeBlock() (string, map[string]any) {
	return "minecraft:anvil", map[string]any{"minecraft:cardinal_direction": "north"}
}

// Hash returns a max hash to force runtime ID lookup by state.
func (DeprecatedAnvil) Hash() (uint64, uint64) {
	return 0, math.MaxUint64
}

// DeprecatedPurpurBlock1 is a placeholder purpur block.
type DeprecatedPurpurBlock1 struct {
	solid
}

// BreakInfo ...
func (d DeprecatedPurpurBlock1) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, pickaxeHarvestable, pickaxeEffective, oneOf(d))
}

// EncodeItem ...
func (DeprecatedPurpurBlock1) EncodeItem() (name string, meta int16) {
	return "minecraft:deprecated_purpur_block_1", 0
}

// EncodeBlock ...
func (DeprecatedPurpurBlock1) EncodeBlock() (string, map[string]any) {
	return "minecraft:purpur_block", map[string]any{"pillar_axis": "y"}
}

// Hash returns a max hash to force runtime ID lookup by state.
func (DeprecatedPurpurBlock1) Hash() (uint64, uint64) {
	return 0, math.MaxUint64
}

// DeprecatedPurpurBlock2 is a placeholder purpur block.
type DeprecatedPurpurBlock2 struct {
	solid
}

// BreakInfo ...
func (d DeprecatedPurpurBlock2) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, pickaxeHarvestable, pickaxeEffective, oneOf(d))
}

// EncodeItem ...
func (DeprecatedPurpurBlock2) EncodeItem() (name string, meta int16) {
	return "minecraft:deprecated_purpur_block_2", 0
}

// EncodeBlock ...
func (DeprecatedPurpurBlock2) EncodeBlock() (string, map[string]any) {
	return "minecraft:purpur_block", map[string]any{"pillar_axis": "y"}
}

// Hash returns a max hash to force runtime ID lookup by state.
func (DeprecatedPurpurBlock2) Hash() (uint64, uint64) {
	return 0, math.MaxUint64
}

// allBorderBlocks returns all border block states.
func allBorderBlocks() (blocks []world.Block) {
	for _, north := range WallConnectionTypes() {
		for _, east := range WallConnectionTypes() {
			for _, south := range WallConnectionTypes() {
				for _, west := range WallConnectionTypes() {
					blocks = append(blocks, BorderBlock{
						NorthConnection: north,
						EastConnection:  east,
						SouthConnection: south,
						WestConnection:  west,
						Post:            false,
					})
					blocks = append(blocks, BorderBlock{
						NorthConnection: north,
						EastConnection:  east,
						SouthConnection: south,
						WestConnection:  west,
						Post:            true,
					})
				}
			}
		}
	}
	return
}
