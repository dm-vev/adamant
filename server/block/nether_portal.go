package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// NetherPortal is the intangible block that forms an activated nether portal.
type NetherPortal struct {
	transparent
	empty

	// Axis specifies the horizontal axis of the portal plane.
	Axis cube.Axis
}

// LightEmissionLevel ...
func (NetherPortal) LightEmissionLevel() uint8 {
	return 11
}

// BreakInfo ...
func (NetherPortal) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, func(item.Tool, []item.Enchantment) []item.Stack { return nil })
}

// EncodeItem ...
func (NetherPortal) EncodeItem() (name string, meta int16) {
	return "minecraft:nether_portal", 0
}

// EncodeBlock ...
func (p NetherPortal) EncodeBlock() (string, map[string]any) {
	axis := "z"
	if p.Axis == cube.X {
		axis = "x"
	}
	return "minecraft:nether_portal", map[string]any{"portal_axis": axis}
}

// EntityInside ...
func (p NetherPortal) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if traveller, ok := e.(netherPortalTraveller); ok {
		traveller.EnterNetherPortal(pos, p.Axis)
	}
}

// NeighbourUpdateTick ...
func (p NetherPortal) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if _, ok := world.NetherPortalFrameAt(tx, pos, p.Axis); ok {
		return
	}
	tx.SetBlock(pos, nil, nil)
}

// netherPortalTraveller represents an entity capable of reacting to portal contact.
type netherPortalTraveller interface {
	EnterNetherPortal(pos cube.Pos, axis cube.Axis)
}

// allNetherPortals returns all permutations of the nether portal block.
func allNetherPortals() (portals []world.Block) {
	for _, axis := range []cube.Axis{cube.X, cube.Z} {
		portals = append(portals, NetherPortal{Axis: axis})
	}
	return portals
}
