package builtin

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func effectTestCommand() cmd.Command {
	return cmd.New("effect", "", nil, effectCommand{}, clearEffectCommand{})
}

func TestEffectCommandsApplyAndClearLivingTargets(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "EffectTarget", Position: mgl64.Vec3{0, 64, 0}})).(*player.Player)
		zombie := tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{2, 64, 0}})).(entity.Living)
		if out := runTestCommand(effectTestCommand(), "give @e minecraft:speed 12 2", tx); out.ErrorCount() != 0 {
			t.Fatalf("effect give errors: %v", out.Errors())
		}
		for _, target := range []entity.Living{p, zombie} {
			effects := target.Effects()
			if len(effects) != 1 || effects[0].Level() != 3 || effects[0].Duration() != 12*time.Second {
				t.Fatalf("effects=%v, want speed III for 12s", effects)
			}
		}
		runTestCommand(effectTestCommand(), "give @e minecraft:jump_boost", tx)
		if out := runTestCommand(effectTestCommand(), "clear @e minecraft:speed", tx); out.ErrorCount() != 0 {
			t.Fatalf("effect clear errors: %v", out.Errors())
		}
		if effects := p.Effects(); len(effects) != 1 {
			t.Fatalf("specific clear left %v", effects)
		}
		runTestCommand(effectTestCommand(), "clear @e", tx)
		if len(p.Effects()) != 0 || len(zombie.Effects()) != 0 {
			t.Fatal("clear-all did not clear both targets")
		}
	})
}

func TestEnchantCommandCompatibleAndIncompatibleItems(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "EnchantTarget"})).(*player.Player)
		enchant := cmd.New("enchant", "", nil, enchantCommand{})
		p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.Stack{})
		if out := runTestCommand(enchant, "@a minecraft:sharpness 3", tx); out.ErrorCount() != 0 {
			t.Fatalf("enchant errors: %v", out.Errors())
		}
		main, off := p.HeldItems()
		if got, ok := main.Enchantment(enchantment.Sharpness); !ok || got.Level() != 3 {
			t.Fatalf("held sharpness=%v present=%v", got, ok)
		}
		if out := runTestCommand(enchant, "@a minecraft:sharpness 9", tx); out.ErrorCount() == 0 {
			t.Fatal("level above Sharpness maximum was accepted")
		}
		p.SetHeldItems(item.NewStack(item.Apple{}, 1), off)
		if out := runTestCommand(enchant, "@a minecraft:sharpness", tx); out.ErrorCount() == 0 {
			t.Fatal("apple unexpectedly accepted Sharpness")
		}
	})
}
