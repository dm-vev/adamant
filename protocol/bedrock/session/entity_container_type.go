package session

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
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
		return v2193.ContainerTypeChestBoat, true
	case entity.ContainerMinecartChest:
		return v2193.ContainerTypeCartChest, true
	case entity.ContainerMinecartHopper:
		return v2193.ContainerTypeCartHopper, true
	}
	return 0, false
}
