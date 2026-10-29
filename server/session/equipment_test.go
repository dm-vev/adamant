package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// equippedEntity is an entity holding and wearing what a test gives it.
type equippedEntity struct {
	movingEntity
	main, off, helmet item.Stack
}

func (e equippedEntity) HeldItems() (item.Stack, item.Stack) { return e.main, e.off }
func (e equippedEntity) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	return e.helmet, item.Stack{}, item.Stack{}, item.Stack{}
}

// drain returns the packets queued for a session.
func drain(s *Session) (pks []proto.Packet) {
	for {
		select {
		case pk := <-s.packets:
			pks = append(pks, pk)
		default:
			return pks
		}
	}
}

// TestEquipmentOfShownEntities shows entities as BDS does: one just shown
// is sent only the equipment it has, and a change after that is sent
// whole, so that an item taken away is cleared.
func TestEquipmentOfShownEntities(t *testing.T) {
	s := testSession("viewer")
	s.br = world.DefaultBlockRegistry
	bare := equippedEntity{movingEntity: movingEntity{h: entity.NewText("cow", mgl64.Vec3{})}}
	show := func(e equippedEntity) []proto.Packet {
		s.entityMutex.Lock()
		if s.justShown == nil {
			s.justShown = map[*world.EntityHandle]struct{}{}
		}
		s.justShown[e.h] = struct{}{}
		s.entityMutex.Unlock()
		s.ViewEntityItems(e)
		s.ViewEntityArmour(e)
		return drain(s)
	}
	if pks := show(bare); len(pks) != 0 {
		t.Fatalf("an entity without equipment was sent %d packets", len(pks))
	}
	s.ViewEntityItems(bare)
	s.ViewEntityArmour(bare)
	if pks := drain(s); len(pks) != 3 {
		t.Fatalf("a change of equipment sent %d packets, want both hands and the armour", len(pks))
	}
	armed := equippedEntity{movingEntity: movingEntity{h: entity.NewText("zombie", mgl64.Vec3{})}, main: item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), helmet: item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1)}
	pks := show(armed)
	if len(pks) != 2 {
		t.Fatalf("an entity with a sword and a helmet was sent %d packets, want 2", len(pks))
	}
	if eq, ok := pks[0].(*v2193.MobEquipmentPacket); !ok || eq.WindowID != 0 {
		t.Fatalf("first packet %+v, want the main hand", pks[0])
	}
	if _, ok := pks[1].(*v2193.MobArmourEquipmentPacket); !ok {
		t.Fatalf("second packet %T, want the armour", pks[1])
	}
}
