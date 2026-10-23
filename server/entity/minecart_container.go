package entity

import (
	"log"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
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
	lootTable     string
	lootSeed      int64
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
	switch b := m.Behaviour().(type) {
	case *MinecartContainerBehaviour:
		return b
	case *HopperMinecartBehaviour:
		return b.MinecartContainerBehaviour
	}
	return nil
}

// Interact opens the container inventory.
func (m *MinecartContainer) Interact(tx *world.Tx, user item.User, _ *item.UseContext) bool {
	b := m.container()
	if b == nil {
		return false
	}
	if b.lootTable != "" {
		if mode, ok := user.(interface{ GameMode() world.GameMode }); ok && mode.GameMode() == world.GameModeSpectator {
			return false
		}
		if _, ok := user.(ContainerOpener); !ok {
			return false
		}
		m.generateLoot(tx, user)
	}
	if opener, ok := user.(ContainerOpener); ok {
		opener.OpenEntityContainer(m, b.Inventory(), byte(b.containerType), tx)
		return true
	}
	return false
}

// Inventory returns the minecart inventory, resolving pending loot on access.
func (m *MinecartContainer) Inventory() *inventory.Inventory {
	b := m.container()
	if b == nil {
		return nil
	}
	m.generateLoot(m.Ent.tx, nil)
	return b.inv
}

func (m *MinecartContainer) generateLoot(tx *world.Tx, user item.User) {
	b := m.container()
	if b == nil || b.lootTable == "" {
		return
	}
	sequence, err := loot.RandomSequence(b.lootTable)
	if err != nil {
		panic(err)
	}
	random := tx.LootRandomSequence(sequence, b.lootSeed)
	ctx := loot.Context{Random: random, RandomSequence: tx.LootRandomSequence, Origin: m.Position(), Biome: tx.Biome(cube.PosFromVec3(m.Position())).String()}
	if entity, ok := user.(world.Entity); ok {
		ctx.ThisEntity, ctx.Player = entity, entity
		if luck, ok := user.(interface{ Luck() float64 }); ok {
			ctx.Luck = luck.Luck()
		}
	}
	stacks, err := loot.Generate(b.lootTable, b.lootSeed, ctx)
	if err != nil {
		panic(err)
	}
	fillMinecartInventory(b.inv, stacks, random)
	b.lootTable, b.lootSeed = "", 0
	delete(m.Ent.data.NativeNBT, "LootTable")
	delete(m.Ent.data.NativeNBT, "LootTableSeed")
	delete(m.Ent.data.NativeNBT, "Items")
}

func fillMinecartInventory(inv *inventory.Inventory, stacks []item.Stack, random *vanilla.Random) {
	free := make([]int, 0, inv.Size())
	for slot, stack := range inv.Slots() {
		if stack.Empty() {
			free = append(free, slot)
		}
	}
	shuffleMinecartLoot(free, random)
	var items, split []item.Stack
	for _, stack := range stacks {
		for !stack.Empty() {
			count := min(stack.Count(), stack.MaxCount())
			part := stack.Grow(count - stack.Count())
			stack = stack.Grow(-count)
			if part.Count() > 1 {
				split = append(split, part)
			} else {
				items = append(items, part)
			}
		}
	}
	for len(free)-len(items)-len(split) > 0 && len(split) > 0 {
		i := 0
		if len(split) > 1 {
			i = int(random.NextInt(int32(len(split))))
		}
		stack := split[i]
		split = append(split[:i], split[i+1:]...)
		count := 1
		if half := stack.Count() / 2; half > 1 {
			count += int(random.NextInt(int32(half)))
		}
		part := stack.Grow(-stack.Count() + count)
		stack = stack.Grow(-count)
		if stack.Count() > 1 && random.NextBoolean() {
			split = append(split, stack)
		} else {
			items = append(items, stack)
		}
		if part.Count() > 1 && random.NextBoolean() {
			split = append(split, part)
		} else {
			items = append(items, part)
		}
	}
	items = append(items, split...)
	shuffleMinecartLoot(items, random)
	for i, stack := range items {
		if i >= len(free) {
			log.Printf("loot table tried to over-fill a minecart container")
			return
		}
		_ = inv.SetItem(free[len(free)-i-1], stack)
	}
}

func shuffleMinecartLoot[T any](items []T, random *vanilla.Random) {
	for i := len(items) - 1; i > 0; i-- {
		j := int(random.NextInt(int32(i + 1)))
		items[i], items[j] = items[j], items[i]
	}
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
		m.generateLoot(tx, nil)
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
	beh.lootTable = nbtconv.String(m, "LootTable")
	beh.lootSeed = nbtconv.Int64(m, "LootTableSeed")
	if beh.lootTable == "" {
		nbtconv.InvFromNBT(beh.inv, nbtconv.Slice(m, "Items"), m)
	} else {
		delete(data.NativeNBT, "Items")
	}
	data.Data = beh
}

func (chestMinecartType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*MinecartContainerBehaviour)
	m := make(map[string]any)
	if b.lootTable != "" {
		m["LootTable"] = b.lootTable
		if b.lootSeed != 0 {
			m["LootTableSeed"] = b.lootSeed
		}
	} else if items := nbtconv.InvToNBT(b.inv); items != nil {
		m["Items"] = items
	} else {
		m["Items"] = []map[string]any{}
	}
	writeMinecartDisplayNBT(b.MinecartBehaviour, m)
	return m
}

var chestMinecartConf = MinecartContainerBehaviourConfig{
	Minecart: MinecartBehaviourConfig{
		DisplayBlock:  block.Chest{},
		DisplayOffset: minecartDisplayOffset,
		Rideable:      false,
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
	beh := hopperMinecartConf.New()
	readMinecartDisplayNBT(beh.MinecartBehaviour, m)
	nbtconv.InvFromNBT(beh.inv, nbtconv.Slice(m, "Items"), m)
	beh.transferCooldown = readNBTInt(m["TransferCooldown"])
	if enabled, ok := m["Enabled"]; ok {
		beh.enabled = readNBTBool(enabled)
	}
	data.Data = beh
}

func (hopperMinecartType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*HopperMinecartBehaviour)
	items := nbtconv.InvToNBT(b.inv)
	if items == nil {
		items = []map[string]any{}
	}
	m := map[string]any{"Items": items, "TransferCooldown": b.transferCooldown, "Enabled": b.enabled}
	writeMinecartDisplayNBT(b.MinecartBehaviour, m)
	return m
}

type hopperMinecartBehaviourConfig struct {
	MinecartContainerBehaviourConfig
}

func (conf hopperMinecartBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

func (conf hopperMinecartBehaviourConfig) New() *HopperMinecartBehaviour {
	return &HopperMinecartBehaviour{MinecartContainerBehaviour: conf.MinecartContainerBehaviourConfig.New(), enabled: true}
}

// HopperMinecartBehaviour adds item transfer behaviour to a container minecart.
type HopperMinecartBehaviour struct {
	*MinecartContainerBehaviour
	transferCooldown int32
	enabled          bool
}

// Tick moves the minecart and transfers items when its cooldown permits.
func (b *HopperMinecartBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	movement := b.MinecartBehaviour.Tick(e, tx)
	b.updateEnabled(e, tx)
	if b.transferCooldown > 0 {
		b.transferCooldown--
		return movement
	}
	if !b.enabled {
		return movement
	}
	pos := cube.PosFromVec3(e.Position())
	if b.push(pos.Side(cube.FaceDown), tx) || b.pull(pos.Side(cube.FaceUp), tx) || b.collectItem(e, tx) {
		b.transferCooldown = 8
	}
	return movement
}

func (b *HopperMinecartBehaviour) updateEnabled(e *Ent, tx *world.Tx) {
	pos := cube.PosFromVec3(e.Position())
	for _, railPos := range [...]cube.Pos{pos, pos.Side(cube.FaceDown)} {
		if rail, ok := tx.Block(railPos).(block.ActivatorRail); ok {
			b.enabled = !rail.Powered
			return
		}
	}
}

func (b *HopperMinecartBehaviour) push(pos cube.Pos, tx *world.Tx) bool {
	destination, ok := tx.Block(pos).(block.Container)
	if !ok {
		return false
	}
	for slot, stack := range b.inv.Slots() {
		if stack.Empty() {
			continue
		}
		one := stack.Grow(1 - stack.Count())
		if added, _ := destination.Inventory(tx, pos).AddItem(one); added != 1 {
			continue
		}
		_ = b.inv.SetItem(slot, stack.Grow(-1))
		block.NotifyComparatorUpdate(pos, tx)
		return true
	}
	return false
}

func (b *HopperMinecartBehaviour) pull(pos cube.Pos, tx *world.Tx) bool {
	source, ok := tx.Block(pos).(block.Container)
	if !ok {
		return false
	}
	for slot, stack := range source.Inventory(tx, pos).Slots() {
		if stack.Empty() {
			continue
		}
		one := stack.Grow(1 - stack.Count())
		if added, _ := b.inv.AddItem(one); added != 1 {
			continue
		}
		_ = source.Inventory(tx, pos).SetItem(slot, stack.Grow(-1))
		block.NotifyComparatorUpdate(pos, tx)
		return true
	}
	return false
}

func (b *HopperMinecartBehaviour) collectItem(e *Ent, tx *world.Tx) bool {
	box := e.H().Type().BBox(e).Translate(e.Position()).GrowVec3(mgl64.Vec3{0.25, 0.25, 0.25})
	for candidate := range tx.EntitiesWithin(box) {
		itemEntity, ok := candidate.(*Ent)
		if !ok || itemEntity.H().Type() != ItemType {
			continue
		}
		stack := itemEntity.Behaviour().(*ItemBehaviour).Item()
		one := stack.Grow(1 - stack.Count())
		if added, _ := b.inv.AddItem(one); added != 1 {
			continue
		}
		if stack.Count() > 1 {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: itemEntity.Position(), Velocity: itemEntity.Velocity()}, stack.Grow(-1)))
		}
		_ = itemEntity.CloseIn(tx)
		return true
	}
	return false
}

var hopperMinecartConf = hopperMinecartBehaviourConfig{MinecartContainerBehaviourConfig{
	Minecart: MinecartBehaviourConfig{
		DisplayBlock:  block.Hopper{Facing: cube.FaceDown},
		DisplayOffset: minecartDisplayOffset,
		Rideable:      false,
	},
	Size:          5,
	ContainerType: minecartContainerHopper,
}}
