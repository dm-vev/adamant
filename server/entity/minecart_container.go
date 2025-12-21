package entity

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

const (
	minecartContainerChest  int32 = 10
	minecartContainerHopper int32 = 11
)

// MinecartContainerBehaviourConfig configures a container minecart.
type MinecartContainerBehaviourConfig struct {
	Minecart      MinecartBehaviourConfig
	Size          int
	ContainerType int32
}

// Apply applies the configuration to the entity data.
func (conf MinecartContainerBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new container minecart behaviour.
func (conf MinecartContainerBehaviourConfig) New() *MinecartContainerBehaviour {
	base := conf.Minecart.New()
	b := &MinecartContainerBehaviour{
		MinecartBehaviour: base,
		containerType:     conf.ContainerType,
		containerSize:     int32(conf.Size),
		viewerMu:          new(sync.RWMutex),
		viewers:           make(map[block.ContainerViewer]struct{}, 1),
	}
	b.inv = inventory.New(conf.Size, func(slot int, _, after item.Stack) {
		b.viewerMu.RLock()
		defer b.viewerMu.RUnlock()
		for viewer := range b.viewers {
			viewer.ViewSlotChange(slot, after)
		}
	})
	return b
}

// MinecartContainerBehaviour implements the behaviour for minecart containers.
type MinecartContainerBehaviour struct {
	*MinecartBehaviour

	inv *inventory.Inventory

	viewerMu *sync.RWMutex
	viewers  map[block.ContainerViewer]struct{}

	containerType int32
	containerSize int32
}

// Base returns the base minecart behaviour.
func (b *MinecartContainerBehaviour) Base() *MinecartBehaviour {
	return b.MinecartBehaviour
}

// ContainerType returns the container type for metadata.
func (b *MinecartContainerBehaviour) ContainerType() int32 {
	return b.containerType
}

// ContainerSize returns the container size for metadata.
func (b *MinecartContainerBehaviour) ContainerSize() int32 {
	return b.containerSize
}

// ContainerStrengthModifier returns the strength modifier for metadata.
func (b *MinecartContainerBehaviour) ContainerStrengthModifier() int32 {
	return 0
}

// InteractText returns the interaction prompt for the container.
func (b *MinecartContainerBehaviour) InteractText() string {
	return "action.interact.opencontainer"
}

// Inventory returns the inventory of the minecart.
func (b *MinecartContainerBehaviour) Inventory() *inventory.Inventory {
	return b.inv
}

// AddViewer adds a viewer to the minecart container.
func (b *MinecartContainerBehaviour) AddViewer(v block.ContainerViewer) {
	b.viewerMu.Lock()
	defer b.viewerMu.Unlock()
	b.viewers[v] = struct{}{}
}

// RemoveViewer removes a viewer from the minecart container.
func (b *MinecartContainerBehaviour) RemoveViewer(v block.ContainerViewer) {
	b.viewerMu.Lock()
	defer b.viewerMu.Unlock()
	delete(b.viewers, v)
}

// MinecartContainer is a minecart with a container inventory.
type MinecartContainer struct {
	*Minecart
}

func (m *MinecartContainer) container() *MinecartContainerBehaviour {
	if b, ok := m.Behaviour().(*MinecartContainerBehaviour); ok {
		return b
	}
	return nil
}

// Interact opens the container inventory.
func (m *MinecartContainer) Interact(tx *world.Tx, user item.User, _ *item.UseContext) bool {
	b := m.container()
	if b == nil {
		return false
	}
	if opener, ok := user.(ContainerOpener); ok {
		opener.OpenEntityContainer(m, b.Inventory(), byte(b.containerType), tx)
		return true
	}
	return false
}

// Destroy destroys the minecart and drops its contents.
func (m *MinecartContainer) Destroy(tx *world.Tx, src world.DamageSource, causer world.Entity) bool {
	b := m.container()
	if b == nil {
		_ = m.CloseIn(tx)
		return true
	}
	destroy := b.Hurt(minecartDamageFromSource(src, causer))
	if destroy {
		for _, stack := range b.inv.Clear() {
			if stack.Empty() {
				continue
			}
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, stack))
		}
		if canDropMinecart(causer, tx.World()) {
			drop := item.Stack{}
			switch b.containerType {
			case minecartContainerChest:
				drop = item.NewStack(item.MinecartChest{}, 1)
			case minecartContainerHopper:
				drop = item.NewStack(item.MinecartHopper{}, 1)
			}
			if !drop.Empty() {
				tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, drop))
			}
		}
		b.DismountAll(m.Ent, tx)
		_ = m.CloseIn(tx)
		return true
	}
	b.broadcastState(m.Ent, tx)
	return true
}

// Links returns the active links for the minecart.
func (m *MinecartContainer) Links() []Link {
	return m.Minecart.Links()
}

// AddViewer adds a container viewer to the minecart inventory.
func (m *MinecartContainer) AddViewer(v block.ContainerViewer) {
	if b := m.container(); b != nil {
		b.AddViewer(v)
	}
}

// RemoveViewer removes a container viewer from the minecart inventory.
func (m *MinecartContainer) RemoveViewer(v block.ContainerViewer) {
	if b := m.container(); b != nil {
		b.RemoveViewer(v)
	}
}

// ChestMinecartType is a world.EntityType implementation for chest minecarts.
var ChestMinecartType chestMinecartType

type chestMinecartType struct{}

func (chestMinecartType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &MinecartContainer{Minecart: &Minecart{Ent: &Ent{tx: tx, handle: handle, data: data}}}
}

func (chestMinecartType) EncodeEntity() string { return "minecraft:chest_minecart" }

func (chestMinecartType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.49, 0, -0.49, 0.49, 0.7, 0.49)
}

func (chestMinecartType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := chestMinecartConf
	beh := conf.New()
	readMinecartDisplayNBT(beh.MinecartBehaviour, m)
	nbtconv.InvFromNBT(beh.inv, nbtconv.Slice(m, "Items"))
	data.Data = beh
}

func (chestMinecartType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*MinecartContainerBehaviour)
	m := map[string]any{"Items": nbtconv.InvToNBT(b.inv)}
	writeMinecartDisplayNBT(b.MinecartBehaviour, m)
	return m
}

var chestMinecartConf = MinecartContainerBehaviourConfig{
	Minecart: MinecartBehaviourConfig{
		DisplayBlock:  block.Chest{},
		DisplayOffset: minecartDisplayOffset,
	},
	Size:          27,
	ContainerType: minecartContainerChest,
}

// HopperMinecartType is a world.EntityType implementation for hopper minecarts.
var HopperMinecartType hopperMinecartType

type hopperMinecartType struct{}

func (hopperMinecartType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &MinecartContainer{Minecart: &Minecart{Ent: &Ent{tx: tx, handle: handle, data: data}}}
}

func (hopperMinecartType) EncodeEntity() string { return "minecraft:hopper_minecart" }

func (hopperMinecartType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.49, 0, -0.49, 0.49, 0.7, 0.49)
}

func (hopperMinecartType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := hopperMinecartConf
	beh := conf.New()
	readMinecartDisplayNBT(beh.MinecartBehaviour, m)
	nbtconv.InvFromNBT(beh.inv, nbtconv.Slice(m, "Items"))
	data.Data = beh
}

func (hopperMinecartType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*MinecartContainerBehaviour)
	m := map[string]any{"Items": nbtconv.InvToNBT(b.inv)}
	writeMinecartDisplayNBT(b.MinecartBehaviour, m)
	return m
}

var hopperMinecartConf = MinecartContainerBehaviourConfig{
	Minecart: MinecartBehaviourConfig{
		DisplayBlock:  block.Hopper{Facing: cube.FaceDown},
		DisplayOffset: minecartDisplayOffset,
	},
	Size:          5,
	ContainerType: minecartContainerHopper,
}
