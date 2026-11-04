package session_test

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"reflect"
)

// grindstonePos is where the cases of the grindstone place it.
var grindstonePos = cube.Pos{0, -60, 2}

// grind returns the actions of a request that takes the result of the
// grindstone and puts it in a slot of the inventory.
func (s *scenario) grind(to int) []mc.StackRequestAction {
	return []mc.StackRequestAction{
		&mc.CraftGrindstoneRecipeStackRequestAction{NumberOfCrafts: 1},
		&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerGrindstoneInput, uiGrindstoneFirst)},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(mc.ContainerCombinedHotBarAndInventory, to)},
	}
}

// grindInputs puts the stacks of slots of the inventory in the slots of the
// grindstone, either left out if its slot is -1.
func (s *scenario) grindInputs(heading string, first, second int) {
	s.t.Helper()
	const inv = mc.ContainerCombinedHotBarAndInventory
	var actions []mc.StackRequestAction
	if first >= 0 {
		actions = append(actions, &mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, first), Destination: s.slot(mc.ContainerGrindstoneInput, uiGrindstoneFirst)})
	}
	if second >= 0 {
		actions = append(actions, &mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, second), Destination: s.slot(mc.ContainerGrindstoneAdditional, uiGrindstoneSecond)})
	}
	s.request(heading, nil, actions...)
}

// grindBack takes the stacks of the slots of the grindstone back to slots
// of the inventory, either left out if its slot is -1.
func (s *scenario) grindBack(heading string, first, second int) {
	s.t.Helper()
	const inv = mc.ContainerCombinedHotBarAndInventory
	var actions []mc.StackRequestAction
	if first >= 0 {
		actions = append(actions, &mc.TakeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerGrindstoneInput, uiGrindstoneFirst), Destination: s.slot(inv, first)})
	}
	if second >= 0 {
		actions = append(actions, &mc.TakeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerGrindstoneAdditional, uiGrindstoneSecond), Destination: s.slot(inv, second)})
	}
	s.request(heading, nil, actions...)
}

// takeOrbs removes the experience orbs of the world, which fly off at
// random, and fails the test unless they held the experience passed.
func (s *scenario) takeOrbs(heading string, experience int) {
	s.t.Helper()
	s.step(heading, func(tx *world.Tx, _ *player.Player) {
		total := 0
		for e := range tx.Entities() {
			if orb, ok := e.(*entity.Ent); ok && e.H().Type() == entity.ExperienceOrbType {
				total += orb.Behaviour().(*entity.ExperienceOrbBehaviour).Experience()
				tx.RemoveEntity(e)
			}
		}
		if total != experience {
			s.t.Errorf("%s: the orbs held %d experience, want %d", heading, total, experience)
		}
	})
}

// stationOrbMotion returns a normaliser of the motion of experience orbs,
// which are spawned with a velocity drawn at random: the velocity they are
// added with becomes 0, and the motion and moves they are sent after, which
// depend on it, are left out.
func stationOrbMotion() func(pk proto.Packet) (changed, omit bool) {
	orbs := map[uint64]bool{}
	return func(pk proto.Packet) (changed, omit bool) {
		v := reflect.ValueOf(pk).Elem()
		switch v.Type().Name() {
		case "AddActorPacket":
			if v.FieldByName("EntityType").String() == "minecraft:xp_orb" {
				orbs[v.FieldByName("EntityRuntimeID").Uint()] = true
				vel := v.FieldByName("Velocity")
				vel.Set(reflect.Zero(vel.Type()))
				return true, false
			}
		case "SetActorMotionPacket", "MoveActorDeltaPacket":
			return false, orbs[v.FieldByName("EntityRuntimeID").Uint()]
		}
		return false, false
	}
}

// grindstoneCases are requests of the grindstone: refused for nothing in
// it, items of two kinds, two that are not durable, a locked input; a stick
// alone, the second slot alone, two pickaxes merged whose enchantment drops
// experience and whose curse stays, a book disenchanted; a result taken
// twice in a request, which fails after it dropped experience; and a
// request once the grindstone is gone from under the open menu.
func (s *scenario) grindstoneCases() {
	s.normalise = stationOrbMotion()
	pickaxe := item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)
	s.give("items given",
		slotStack{0, pickaxe.Damage(100).WithEnchantments(item.NewEnchantment(enchantment.Efficiency, 1), item.NewEnchantment(enchantment.CurseOfVanishing, 1))},
		slotStack{1, pickaxe.Damage(120)},
		slotStack{2, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)},
		slotStack{3, item.NewStack(item.Stick{}, 1)},
		slotStack{4, item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Efficiency, 1))},
		slotStack{5, item.NewStack(item.Book{}, 1)},
		slotStack{6, item.NewStack(item.Book{}, 1)},
		slotStack{7, item.NewStack(item.Shears{}, 1).Damage(5)},
		slotStack{9, item.NewStack(item.Shears{}, 1)},
	)
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.place("a grindstone is placed", grindstonePos, block.Grindstone{})
	s.clickBlock("the client opens the grindstone", grindstonePos)

	s.refuse("refused: nothing ground", s.grind(20)...)
	s.grindInputs("a pickaxe and a sword put in it", 1, 2)
	s.refuse("refused: a pickaxe ground with a sword", s.grind(20)...)
	s.grindBack("the pickaxe and the sword taken back", 1, 2)
	s.grindInputs("two books put in it", 5, 6)
	s.refuse("refused: two books ground", s.grind(20)...)
	s.grindBack("the books taken back", 5, 6)
	// The client cannot put a locked item in the grindstone.
	s.step("a locked pickaxe in the grindstone", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiGrindstoneSecond, pickaxe.WithLockMode(item.LockInInventory))
	})
	s.refuse("refused: the locked pickaxe ground", s.grind(20)...)
	s.step("the locked pickaxe taken away", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiGrindstoneSecond, item.Stack{})
	})

	s.grindInputs("a stick put in it", 3, -1)
	s.request("the stick ground", nil, s.grind(20)...)
	s.grindInputs("shears put in the second slot", -1, 7)
	s.request("the shears ground from the second slot", nil,
		&mc.CraftGrindstoneRecipeStackRequestAction{NumberOfCrafts: 1},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(mc.ContainerCombinedHotBarAndInventory, 21)})
	s.grindInputs("two damaged pickaxes put in it, the first of efficiency and vanishing", 0, 1)
	s.request("the pickaxes merged", nil, s.grind(22)...)
	s.takeOrbs("the orbs taken away", 1)
	s.grindInputs("a book of efficiency put in it", 4, -1)
	s.request("the book disenchanted", nil, s.grind(23)...)
	s.takeOrbs("the orbs taken away", 1)

	// The second takes the result of the first again: it is refused after
	// both dropped their experience, and the first is undone.
	s.grindInputs("the merged pickaxe put in it", 22, -1)
	s.refuse("refused: a result ground twice in a request",
		&mc.CraftGrindstoneRecipeStackRequestAction{NumberOfCrafts: 1},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(mc.ContainerCombinedHotBarAndInventory, 9), Destination: s.again(mc.ContainerGrindstoneInput, uiGrindstoneFirst)},
		&mc.CraftGrindstoneRecipeStackRequestAction{NumberOfCrafts: 1})
	s.takeOrbs("the orbs taken away", 0)

	s.step("the grindstone broken under the open menu", func(tx *world.Tx, _ *player.Player) {
		tx.SetBlock(grindstonePos, block.Stone{}, nil)
	})
	s.refuse("refused: the pickaxe ground with no grindstone", &mc.CraftGrindstoneRecipeStackRequestAction{NumberOfCrafts: 1})
	s.attempt("the pickaxe taken back",
		&mc.TakeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerGrindstoneInput, uiGrindstoneFirst), Destination: s.slot(mc.ContainerCombinedHotBarAndInventory, 22)})
	s.closeContainer("the client closes the grindstone")
}
