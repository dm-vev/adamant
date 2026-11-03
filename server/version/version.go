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

// Java is the release of Minecraft: Java Edition whose behaviour the game
// ports, as major and minor numbers separated by a dot. Where the game ports a
// mechanic from Java Edition rather than from Bedrock Dedicated Server, it is
// that of this release, and the names it uses for sounds, particles, entity
// types and effects are those of this release.
//
// Java names no content and no world format of the server: those are of
// Minecraft. It has no effect on what Bedrock clients are sent.
const Java = "26.3"

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
