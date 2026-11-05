package leveldat

import (
	"github.com/df-mc/dragonfly/server/version"
)

// Version is the current version stored in level.dat files.
const Version = 10

// NetworkVersion is the value of the NetworkVersion field of level.dat files
// the server writes. It is a value of the world format: the network protocol
// number of the release of Minecraft the world was last written by, which for
// the server is that of version.Minecraft: BDS 1.26.61.1 writes 2225.
const NetworkVersion = 2225

// minimumCompatibleClientVersion is the minimum compatible client version,
// required by the latest Minecraft data provider.
var minimumCompatibleClientVersion = version.MinecraftNumbers()
