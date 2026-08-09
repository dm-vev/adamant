package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
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
			if got := GameTypeFromMode(test.mode); got != test.want {
				t.Fatalf("game type = %d, want %d", got, test.want)
			}
		})
	}
}

func TestResyncInventory(t *testing.T) {
	s := &Session{
		br:      world.DefaultBlockRegistry,
		inv:     inventory.New(36, nil),
		offHand: inventory.New(1, nil),
		packets: make(chan packet.Packet, 2),
	}
	s.ResyncInventory()

	for _, want := range []uint32{protocol.WindowIDInventory, protocol.WindowIDOffHand} {
		pk := <-s.packets
		content, ok := pk.(*packet.InventoryContent)
		if !ok {
			t.Fatalf("packet type = %T, want *packet.InventoryContent", pk)
		}
		if content.WindowID != want {
			t.Errorf("window ID = %v, want %v", content.WindowID, want)
		}
	}
}
