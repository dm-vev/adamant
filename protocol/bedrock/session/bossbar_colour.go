package session

import "github.com/df-mc/dragonfly/server/player/bossbar"

// bedrockBossBarColour returns the colour BossEvent sends for the colour of a
// boss bar. Bedrock numbers them pink 0, blue 1, red 2, green 3, yellow 4,
// purple 5, rebecca purple 6 and white 7; the protocol has no constants.
func bedrockBossBarColour(c bossbar.Colour) uint8 {
	switch c {
	case bossbar.Pink():
		return 0
	case bossbar.Blue():
		return 1
	case bossbar.Red():
		return 2
	case bossbar.Green():
		return 3
	case bossbar.Yellow():
		return 4
	case bossbar.Purple():
		return 5
	case bossbar.RebeccaPurple():
		return 6
	case bossbar.White():
		return 7
	}
	return 0
}
