package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestLootGiveAddsGeneratedTableItems(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 65, 0.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Name: "Looter", Position: pos, GameMode: world.GameModeSurvival})).(*player.Player)
		out := runTestCommand(newLootCommand(), "give Looter loot minecraft:chests/simple_dungeon", tx)
		if out.ErrorCount() != 0 {
			t.Fatalf("loot give errors: %v", out.Errors())
		}
		count := 0
		for _, stack := range p.Inventory().Items() {
			count += stack.Count()
		}
		if count == 0 {
			t.Fatal("loot give did not add any generated items")
		}
	})
}
