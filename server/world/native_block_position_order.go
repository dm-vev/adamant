package world

import "github.com/df-mc/dragonfly/server/block/cube"

// NativeBlockPositionOrder deduplicates positions in the native BlockPos hash
// collection's traversal order. Explosion's affected set uses the same hash,
// bucket growth and list insertion rules as the circuit position maps.
func NativeBlockPositionOrder(positions []cube.Pos) []cube.Pos {
	var order circuitPosOrder
	for _, pos := range positions {
		order.insert(pos)
	}
	result := make([]cube.Pos, 0, len(order.nodes))
	for node := order.head; node != nil; node = node.next {
		result = append(result, node.pos)
	}
	return result
}
