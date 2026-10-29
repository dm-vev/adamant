package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestVaultUpdateIsSentAsBDS checks the BlockActorData a vault update sends:
// BDS sends the item displayed and the players connected, with the client's
// own player under its runtime ID, not the saved block actor.
func TestVaultUpdateIsSentAsBDS(t *testing.T) {
	s := testSession("vault-update")
	s.br = world.DefaultBlockRegistry
	own := s.ent.UniqueID()
	v := block.Vault{State: block.VaultActive}.DecodeNBT(map[string]any{"data": map[string]any{
		"connected_players": []int64{own, 77},
		"display_item":      map[string]any{"Name": "minecraft:emerald", "Count": byte(8), "Damage": int16(0)},
	}}).(block.Vault)
	pos := cube.Pos{4, 100, 6}
	data := s.blockActorUpdateNBT(pos, v)
	if _, saved := data["config"]; saved {
		t.Fatalf("vault update carries its saved block actor: %v", data)
	}
	if ids, _ := data["connected_players"].([]int64); len(ids) != 2 || ids[0] != selfEntityRuntimeID || ids[1] != 77 {
		t.Fatalf("connected_players = %#v, want [%d 77]", data["connected_players"], selfEntityRuntimeID)
	}
	if display, _ := data["display_item"].(map[string]any); display["Name"] != "minecraft:emerald" || display["Count"] != byte(8) {
		t.Fatalf("display_item = %#v", data["display_item"])
	}

	// Other block actors are still sent whole.
	if chest := s.blockActorUpdateNBT(pos.Side(cube.FaceUp), block.NewChest()); chest["id"] != "Chest" || chest["y"] != int32(101) {
		t.Fatalf("chest update %v", chest)
	}
}
