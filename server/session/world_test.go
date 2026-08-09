package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestViewFallDamageSendsGameRule(t *testing.T) {
	s := testSession("fall-damage")
	s.ViewFallDamage(false)

	pk, ok := (<-s.packets).(*packet.GameRulesChanged)
	if !ok {
		t.Fatalf("packet type = %T, want *packet.GameRulesChanged", pk)
	}
	want := []protocol.GameRule{{Name: "falldamage", Value: false}}
	if len(pk.GameRules) != 1 || pk.GameRules[0] != want[0] {
		t.Fatalf("game rules = %#v, want %#v", pk.GameRules, want)
	}
}

func TestViewVisibilityHidesArmourPerViewer(t *testing.T) {
	handle := &world.EntityHandle{}
	armour := inventory.NewArmour(nil)
	helm := item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1)
	armour.SetHelmet(helm)
	e := testArmouredEntity{handle: handle, armour: armour, mainHand: item.NewStack(item.Diamond{}, 1)}

	hiddenViewer, publicViewer := testSession("hidden"), testSession("public")
	for id, viewer := range map[uint64]*Session{2: hiddenViewer, 3: publicViewer} {
		viewer.br = world.DefaultBlockRegistry
		viewer.viewLayer = world.NewViewLayer(nil)
		viewer.entityRuntimeIDs[handle] = id
		viewer.entities[id] = handle
	}

	hiddenViewer.ViewVisibility(e, world.EnforceInvisible())
	hiddenArmour := (<-hiddenViewer.packets).(*packet.MobArmourEquipment)
	if hiddenArmour.Helmet.Stack.Count != 0 {
		t.Fatal("invisible viewer received equipped armour")
	}
	if len(publicViewer.packets) != 0 {
		t.Fatal("visibility override leaked packets to public viewer")
	}

	publicViewer.ViewEntityArmour(e)
	if got := (<-publicViewer.packets).(*packet.MobArmourEquipment); got.Helmet.Stack.Count != 1 {
		t.Fatal("public viewer did not receive equipped armour")
	}
	if armour.Helmet().Count() != 1 {
		t.Fatal("per-viewer hiding changed the entity's armour")
	}
	hiddenViewer.ViewEntityItems(e)
	if got := (<-hiddenViewer.packets).(*packet.MobEquipment); got.NewItem.Stack.Count != 1 {
		t.Fatal("armour hiding suppressed held-item updates")
	}
	<-hiddenViewer.packets // Empty off-hand update.

	for _, level := range []world.VisibilityLevel{world.EnforceVisible(), world.EnforceInvisible(), world.PublicVisibility()} {
		hiddenViewer.ViewVisibility(e, level)
		got := (<-hiddenViewer.packets).(*packet.MobArmourEquipment)
		want := uint16(1)
		if level == world.EnforceInvisible() {
			want = 0
		}
		if got.Helmet.Stack.Count != want {
			t.Fatalf("helmet count after visibility change = %d, want %d", got.Helmet.Stack.Count, want)
		}
	}
	if hiddenViewer.entityRuntimeIDs[handle] != 2 || publicViewer.entityRuntimeIDs[handle] != 3 {
		t.Fatal("visibility changes replaced per-viewer runtime IDs")
	}

	hiddenViewer.ViewVisibility(e, world.EnforceInvisible())
	publicViewer.ViewVisibility(e, world.EnforceInvisible())
	<-hiddenViewer.packets
	<-publicViewer.packets
	hiddenViewer.RemoveViewLayer(e)
	if got := (<-hiddenViewer.packets).(*packet.MobArmourEquipment); got.Helmet.Stack.Count != 1 {
		t.Fatal("removing one viewer's invisibility layer did not restore armour")
	}
	if len(publicViewer.packets) != 0 {
		t.Fatal("removing one viewer's layer refreshed another viewer")
	}
	publicViewer.ViewEntityArmour(e)
	if got := (<-publicViewer.packets).(*packet.MobArmourEquipment); got.Helmet.Stack.Count != 0 {
		t.Fatal("remaining invisibility layer did not retain precedence")
	}
	publicViewer.RemoveViewLayer(e)
	if got := (<-publicViewer.packets).(*packet.MobArmourEquipment); got.Helmet.Stack.Count != 1 {
		t.Fatal("removing final invisibility layer did not restore armour")
	}
}

func TestHideSessionlessControllableTwice(t *testing.T) {
	s := testSession("viewer")
	handle := &world.EntityHandle{}
	entity := testControllable{Controllable: nil, handle: handle}
	const runtimeID = 2
	s.entityRuntimeIDs[handle] = runtimeID
	s.entities[runtimeID] = handle

	s.HideEntity(entity)
	s.HideEntity(entity)

	if _, ok := s.entityRuntimeIDs[handle]; ok {
		t.Fatal("runtime ID remains after hiding sessionless controllable")
	}
	if _, ok := s.entities[runtimeID]; ok {
		t.Fatal("entity remains after hiding sessionless controllable")
	}
	if got := len(s.packets); got != 1 {
		t.Fatalf("got %d remove packets, want 1", got)
	}
	removed := (<-s.packets).(*packet.RemoveActor)
	if removed.EntityUniqueID != runtimeID {
		t.Fatalf("removed runtime ID %d, want %d", removed.EntityUniqueID, runtimeID)
	}
}

type testControllable struct {
	Controllable
	handle *world.EntityHandle
}

func (c testControllable) H() *world.EntityHandle { return c.handle }

type testArmouredEntity struct {
	world.Entity
	handle   *world.EntityHandle
	armour   *inventory.Armour
	mainHand item.Stack
}

func (e testArmouredEntity) H() *world.EntityHandle { return e.handle }
func (e testArmouredEntity) Armour() *inventory.Armour {
	return e.armour
}
func (e testArmouredEntity) HeldItems() (item.Stack, item.Stack) {
	return e.mainHand, item.Stack{}
}
