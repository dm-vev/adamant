package leveldat

import (
	"strconv"
	"strings"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// Version is the current version stored in level.dat files.
const Version = 10

// minimumCompatibleClientVersion is the minimum compatible client version,
// required by the latest Minecraft data provider.
var minimumCompatibleClientVersion []int32

// init initialises the minimum compatible client version.
func init() {
	fullVersion := append(strings.Split(v2193.CurrentVersion, "."), "0", "0")
	for _, v := range fullVersion {
		i, _ := strconv.Atoi(v)
		minimumCompatibleClientVersion = append(minimumCompatibleClientVersion, int32(i))
	}
}
