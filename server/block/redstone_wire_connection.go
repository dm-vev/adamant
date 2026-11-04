package block

// WireConnection is how redstone wire connects toward one side: not at all,
// along the ground, or along the ground and up the side of the block there,
// to wire on top of it.
type WireConnection struct {
	wireConnection
}

// NoWireConnection returns the connection of wire that does not connect
// toward a side.
func NoWireConnection() WireConnection {
	return WireConnection{0}
}

// SideWireConnection returns the connection of wire that runs along the
// ground toward a side.
func SideWireConnection() WireConnection {
	return WireConnection{1}
}

// UpWireConnection returns the connection of wire that runs along the
// ground toward a side and up the side of the block there.
func UpWireConnection() WireConnection {
	return WireConnection{2}
}

// WireConnections returns every connection of redstone wire.
func WireConnections() []WireConnection {
	return []WireConnection{NoWireConnection(), SideWireConnection(), UpWireConnection()}
}

type wireConnection uint8

// Uint8 returns the connection as a uint8.
func (w wireConnection) Uint8() uint8 {
	return uint8(w)
}

// String returns the connection as Java and Bedrock name it: none, side or
// up.
func (w wireConnection) String() string {
	switch w {
	case 0:
		return "none"
	case 1:
		return "side"
	case 2:
		return "up"
	}
	panic("unknown redstone wire connection")
}
