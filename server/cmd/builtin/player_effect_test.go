package builtin

import (
	"strings"
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

// /enchant puts Sweeping Edge, which Bedrock has no ID of, on a sword by its
// Java ID, with the namespace or without, up to its maximum level, and not
// on an axe.
func TestEnchantCommandSweepingEdge(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "SweepTarget"})).(*player.Player)
		enchant := cmd.New("enchant", "", nil, enchantCommand{})
		for _, name := range []string{"minecraft:sweeping_edge", "sweeping_edge"} {
			p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.Stack{})
			if out := runTestCommand(enchant, "@a "+name+" 3", tx); out.ErrorCount() != 0 {
				t.Fatalf("%s: enchant errors: %v", name, out.Errors())
			}
			main, _ := p.HeldItems()
			if got, ok := main.Enchantment(enchantment.SweepingEdge); !ok || got.Level() != 3 {
				t.Fatalf("%s: held Sweeping Edge=%v present=%v", name, got, ok)
			}
		}
		if out := runTestCommand(enchant, "@a minecraft:sweeping_edge 4", tx); out.ErrorCount() == 0 {
			t.Fatal("level above the Sweeping Edge maximum was accepted")
		}
		p.SetHeldItems(item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1), item.Stack{})
		if out := runTestCommand(enchant, "@a minecraft:sweeping_edge", tx); out.ErrorCount() == 0 {
			t.Fatal("an axe accepted Sweeping Edge")
		}
	})
}

// /enchant ports EnchantCommand: it enchants the main-hand item of each living
// target and prints Java's texts. A lone target that cannot take the
// enchantment fails the command with the reason, several are skipped
// silently, and the command fails with Java's commands.enchant.failed if none took it.
func TestEnchantCommandMessagesForOneTargetAndSeveral(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		add := func(name string) *player.Player {
			return tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: name, Position: mgl64.Vec3{0, 64, 0}})).(*player.Player)
		}
		alice, bob := add("Alice"), add("Bob")
		sword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
		zombie := tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{2, 64, 0}}))
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{4, 64, 0}}))
		reset := func() {
			alice.SetHeldItems(item.Stack{}, item.Stack{})
			bob.SetHeldItems(item.Stack{}, item.Stack{})
			zombie.(interface{ SetHeldItems(a, b item.Stack) }).SetHeldItems(item.Stack{}, item.Stack{})
		}
		enchant := cmd.New("enchant", "", nil, enchantCommand{})
		for _, c := range []struct {
			name, args string
			setup      func()
			message    string
			err        string
		}{
			{name: "single without item", args: "@a[name=Alice] sharpness 3", err: "Alice is not holding any item"},
			{name: "single incompatible", args: "@a[name=Alice] sharpness 3", setup: func() {
				alice.SetHeldItems(item.NewStack(item.Apple{}, 1), item.Stack{})
			}, err: "Apple cannot support that enchantment"},
			{name: "single not living", args: "@e[type=egg] sharpness 3", setup: func() {
				tx.AddEntity(entity.NewEgg(world.EntitySpawnOpts{Position: mgl64.Vec3{6, 64, 0}}, nil))
			}, err: "Egg is not a valid entity for this command"},
			{name: "single mob without item", args: "@e[type=cow] sharpness 3", err: "Cow is not holding any item"},
			{name: "single level too high", args: "@a[name=Alice] sharpness 9", setup: func() {
				alice.SetHeldItems(sword, item.Stack{})
			}, err: "9 is higher than the maximum level of 5 supported by that enchantment"},
			{name: "single enchanted", args: "@a[name=Alice] sharpness 3", setup: func() {
				alice.SetHeldItems(sword, item.Stack{})
			}, message: "Applied enchantment Sharpness III to Alice's item"},
			{name: "single with only level", args: "@a[name=Alice] aqua_affinity", setup: func() {
				alice.SetHeldItems(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1), item.Stack{})
			}, message: "Applied enchantment Aqua Affinity to Alice's item"},
			{name: "single already has it", args: "@a[name=Alice] sharpness 4", setup: func() {
				alice.SetHeldItems(sword.WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 3)), item.Stack{})
			}, err: "Iron Sword cannot support that enchantment"},
			{name: "single mob enchanted", args: "@e[type=zombie] sharpness 2", setup: func() {
				zombie.(interface{ SetHeldItems(a, b item.Stack) }).SetHeldItems(sword, item.Stack{})
			}, message: "Applied enchantment Sharpness II to Zombie's item"},
			{name: "several enchanted", args: "@a sharpness 3", setup: func() {
				alice.SetHeldItems(sword, item.Stack{})
				bob.SetHeldItems(sword, item.Stack{})
			}, message: "Applied enchantment Sharpness III to 2 entities"},
			{name: "several skip those that cannot", args: "@e sharpness 1", setup: func() {
				alice.SetHeldItems(sword, item.Stack{})
				bob.SetHeldItems(item.NewStack(item.Apple{}, 1), item.Stack{})
			}, message: "Applied enchantment Sharpness I to Alice's item"},
			{name: "several none", args: "@e sharpness 1", err: "Nothing changed. Targets either have no item in their hands or the enchantment could not be applied"},
		} {
			reset()
			if c.setup != nil {
				c.setup()
			}
			out := runTestCommand(enchant, c.args, tx)
			var got string
			switch {
			case out.ErrorCount() > 0:
				got = out.Errors()[0].Error()
			case out.MessageCount() > 0:
				got = out.Messages()[0].String()
			}
			want := c.message + c.err
			if !strings.Contains(got, want) {
				t.Errorf("%s: /enchant %s printed %q, want %q", c.name, c.args, got, want)
			}
		}
		_ = cow
	})
}
