package leveldat

import (
	"github.com/df-mc/dragonfly/server/version"
)

// Version is the current version stored in level.dat files.
const Version = 10

// NetworkVersion is the value of the NetworkVersion field of level.dat files
// the server writes. It is a value of the world format: the network protocol
// number of the release of Minecraft the world was last written by, which for
// the server is version.Minecraft.
const NetworkVersion = 2193

// minimumCompatibleClientVersion is the minimum compatible client version,
// required by the latest Minecraft data provider.
var minimumCompatibleClientVersion = version.MinecraftNumbers()
