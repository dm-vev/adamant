package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
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
