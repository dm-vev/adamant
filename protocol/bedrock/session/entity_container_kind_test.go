package session

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestEntityContainerTypes holds every entity.ContainerKind to the container
// type its inventory opened with before kinds existed, the type BDS 1.26.52.3
// opens it with (TestEntityContainersAgainstBDS): 12 (Horse) for mounts, which
// open with UpdateEquip, 0 for chest boats and chest minecarts and 8 for
// hopper minecarts.
func TestEntityContainerTypes(t *testing.T) {
	for _, c := range []struct {
		kind entity.ContainerKind
		want byte
	}{
		{entity.ContainerMount, 12},
		{entity.ContainerChestBoat, 0},
		{entity.ContainerMinecartChest, 0},
		{entity.ContainerMinecartHopper, 8},
	} {
		if got, ok := entityContainerType(c.kind); !ok || got != c.want {
			t.Errorf("entityContainerType(%d) = %d, %v, want %d, true", c.kind, got, ok, c.want)
		}
	}
	if _, ok := entityContainerType(entity.ContainerMinecartHopper + 1); ok {
		t.Errorf("entityContainerType of an unknown kind is known")
	}
}

// TestActorContainerTypesMatchProtocol holds the container type the actor
// data of a vehicle names for each entity.ContainerKind to the protocol's
// constant, and to the number its ContainerType method gave before kinds
// replaced it: ChestBoat 34, CartChest 10 and CartHopper 11. Mounts name
// Horse from their MountState and have no actor container type of a kind.
func TestActorContainerTypesMatchProtocol(t *testing.T) {
	var values []gameValue
	for _, c := range []struct {
		kind      entity.ContainerKind
		want, was int64
	}{
		{entity.ContainerChestBoat, v2193.ContainerTypeChestBoat, 34},
		{entity.ContainerMinecartChest, v2193.ContainerTypeCartChest, 10},
		{entity.ContainerMinecartHopper, v2193.ContainerTypeCartHopper, 11},
	} {
		typ, ok := actorContainerType(c.kind)
		if !ok {
			t.Errorf("actorContainerType(%d) is unknown", c.kind)
		}
		name := fmt.Sprintf("actorContainerType(%d)", c.kind)
		values = append(values, gameValue{name, int64(typ), c.want}, gameValue{name + " (before kinds)", int64(typ), c.was})
	}
	checkGameValues(t, values)
	for _, kind := range []entity.ContainerKind{entity.ContainerMount, entity.ContainerMinecartHopper + 1} {
		if _, ok := actorContainerType(kind); ok {
			t.Errorf("actorContainerType(%d) is known", kind)
		}
	}
}
