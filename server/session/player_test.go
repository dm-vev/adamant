package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestGameTypeFromMode(t *testing.T) {
	tests := map[string]struct {
		mode world.GameMode
		want int32
	}{
		"survival":  {world.GameModeSurvival, packet.GameTypeSurvival},
		"creative":  {world.GameModeCreative, packet.GameTypeCreative},
		"spectator": {world.GameModeSpectator, packet.GameTypeSpectator},
		"fallback":  {world.GameModeAdventure, packet.GameTypeSurvival},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := gameTypeFromMode(test.mode); got != test.want {
				t.Fatalf("game type = %d, want %d", got, test.want)
			}
		})
	}
}
