package session

import (
	"github.com/df-mc/dragonfly/server/entity"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// actorContainerType returns the container type the actor data of a vehicle
// with a container of the kind passed names, as BDS 1.26.52.3 sends it:
// ChestBoat (34) for a chest boat, CartChest (10) and CartHopper (11) for
// container minecarts. These open with other types (entityContainerType).
// Mounts, whose container data is that of their MountState, are not
// vehicles of this kind.
func actorContainerType(kind entity.ContainerKind) (byte, bool) {
	switch kind {
	case entity.ContainerChestBoat:
		return mc.ContainerTypeChestBoat, true
	case entity.ContainerMinecartChest:
		return mc.ContainerTypeCartChest, true
	case entity.ContainerMinecartHopper:
		return mc.ContainerTypeCartHopper, true
	}
	return 0, false
}
