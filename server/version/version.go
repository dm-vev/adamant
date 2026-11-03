// Package version names the release of Minecraft: Bedrock Edition whose
// vanilla content and world format the server implements.
//
// The release is a property of the game, not of a protocol: the vanilla items,
// blocks and behaviour, the worlds it writes and the version it reports in
// queries and commands are those of this release, whichever protocol a client
// connects with. vanilla-reference.json in the repository root records the
// build of Bedrock Dedicated Server the vanilla data was taken from, a hotfix of
// this release.
package version

import (
	"strconv"
	"strings"
)

// Minecraft is the release of Minecraft: Bedrock Edition the server implements,
// as major, minor and patch numbers separated by dots.
const Minecraft = "1.26.50"

// MinecraftNumbers returns the numbers of Minecraft, followed by two zeros for
// the revision and the beta numbers, as the world format stores versions.
func MinecraftNumbers() []int32 {
	parts := strings.Split(Minecraft, ".")
	numbers := make([]int32, 0, len(parts)+2)
	for _, part := range parts {
		n, _ := strconv.Atoi(part)
		numbers = append(numbers, int32(n))
	}
	return append(numbers, 0, 0)
}
