package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// ContainerViewer represents a viewer that is able to view a container and its inventory.
type ContainerViewer interface {
	world.Viewer
	// ViewSlotChange views a change of a single slot in the inventory, in which the item was changed to the
	// new item passed.
	ViewSlotChange(slot int, newItem item.Stack)
}

// MobContainerOpener is a mob that has a container open without viewing it,
// as a copper golem sorting items does. Added as a viewer, it counts towards
// the openers of the container, opening its lid and powering a trapped chest.
// Mobs with the same handle are the same opener.
type MobContainerOpener struct {
	// Viewer is nil: the mob views nothing through the container.
	world.Viewer
	Handle *world.EntityHandle
}

// ViewSlotChange does nothing: the mob sees no window.
func (MobContainerOpener) ViewSlotChange(int, item.Stack) {}

// ContainerOpener represents an entity that is able to open a container.
type ContainerOpener interface {
	// OpenBlockContainer opens a block container at the position passed.
	OpenBlockContainer(pos cube.Pos, tx *world.Tx)
}

// Container represents a container of items, typically a block such as a chest. Containers may have their
// inventory opened by viewers.
type Container interface {
	AddViewer(v ContainerViewer, tx *world.Tx, pos cube.Pos)
	RemoveViewer(v ContainerViewer, tx *world.Tx, pos cube.Pos)
	Inventory(tx *world.Tx, pos cube.Pos) *inventory.Inventory
	ContainerSize() int
}
