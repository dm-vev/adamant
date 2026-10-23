package entity

import "github.com/df-mc/dragonfly/server/world"

// CarryBlock makes the enderman carry a block.
func (e *Enderman) CarryBlock(b world.Block) bool {
	if b == nil {
		return false
	}
	e.s().carried = b
	e.updateState()
	return true
}

// CarriedBlock returns the block the enderman carries, if any.
func (e *Enderman) CarriedBlock() (world.Block, bool) {
	b := e.s().carried
	return b, b != nil
}

// DropCarriedBlock clears the block the enderman carries and returns it.
func (e *Enderman) DropCarriedBlock() (world.Block, bool) {
	b, ok := e.CarriedBlock()
	e.s().carried = nil
	e.updateState()
	return b, ok
}

// Angry reports if the enderman has a target, which makes it scream and
// shake, as Enderman.isCreepy.
func (e *Enderman) Angry() bool { return e.s().creepy }

// decodeCarriedBlock reads a carried block from a block state compound with
// the name and properties under the keys passed.
func decodeCarriedBlock(v any, nameKey, propertiesKey string) world.Block {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	name, _ := m[nameKey].(string)
	properties, _ := m[propertiesKey].(map[string]any)
	if name == "" || name == "minecraft:air" {
		return nil
	}
	if b, ok := world.BlockByName(name, properties); ok {
		return b
	}
	return nil
}
