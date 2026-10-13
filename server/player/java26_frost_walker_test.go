package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestFrostWalkerFreezesSourceWater(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetLiquid(pos, block.Water{Depth: 8, Still: true})
		handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 0.5}}.New(Type, Config{Position: mgl64.Vec3{0.5, 65, 0.5}})
		p := tx.AddEntity(handle).(*Player)
		p.armour.SetBoots(item.NewStack(item.Boots{Tier: item.ArmourTierLeather{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.FrostWalker, 1)))
		p.onGround = true
		p.tickFrostWalker()
		if _, ok := tx.Block(pos).(block.FrostedIce); !ok {
			t.Fatalf("block beneath frost walker = %T, want FrostedIce", tx.Block(pos))
		}
	})
}
func TestFrostWalkerDepthStriderIncompatibility(t *testing.T) {
	if enchantment.FrostWalker.CompatibleWithEnchantment(enchantment.DepthStrider) || enchantment.DepthStrider.CompatibleWithEnchantment(enchantment.FrostWalker) {
		t.Fatal("Frost Walker and Depth Strider must be incompatible")
	}
}
