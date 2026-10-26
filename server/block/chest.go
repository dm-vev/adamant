package block

import (
	"fmt"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Chest is a container block which may be used to store items. Chests may also be paired to create a bigger
// single container.
// The empty value of Chest is not valid. It must be created using block.NewChest().
type Chest struct {
	chest
	transparent
	bass
	sourceWaterDisplacer

	// Facing is the direction that the chest is facing.
	Facing cube.Direction
	// CustomName is the custom name of the chest. This name is displayed when the chest is opened, and may
	// include colour codes.
	CustomName string
	lootTable  string
	lootSeed   int64

	paired       bool
	pairX, pairZ int
	pairInv      *inventory.Inventory

	inventory *inventory.Inventory
	viewerMu  *sync.RWMutex
	viewers   map[ContainerViewer]struct{}
}

// NewChest creates a new initialised chest. The inventory is properly initialised.
func NewChest() Chest {
	c := Chest{
		viewerMu: new(sync.RWMutex),
		viewers:  make(map[ContainerViewer]struct{}, 1),
	}

	c.inventory = inventory.New(27, func(slot int, _, after item.Stack) {
		c.viewerMu.RLock()
		defer c.viewerMu.RUnlock()
		for viewer := range c.viewers {
			viewer.ViewSlotChange(slot, after)
		}
	})
	return c
}

func (c Chest) ContainerSize() int {
	if c.paired {
		return 54
	}
	return 27
}

// Inventory returns the inventory of the chest. The size of the inventory will be 27 or 54, depending on
// whether the chest is single or double.
func (c Chest) Inventory(tx *world.Tx, pos cube.Pos) *inventory.Inventory {
	if c.lootTable != "" {
		if err := c.GenerateLoot(tx, pos); err != nil {
			panic(err)
		}
	}
	inv, _ := c.tryPair(tx, pos)
	return inv
}

// chestBlock keeps the concrete container type when shared chest logic updates
// its actor. Copper variants may pair with each other, but not with wood chests.
type chestBlock interface {
	world.Block
	chestData() Chest
	withChestData(Chest) chestBlock
	canPairChest(world.Block) bool
}

// Opened reports if a viewer has the chest open, as
// ChestBlockEntity.getOpenCount is above zero.
func (c Chest) Opened() bool { return len(c.viewers) > 0 }

// OwnInventory returns the inventory of this half of the chest alone, which
// identifies its block entity.
func (c Chest) OwnInventory() *inventory.Inventory { return c.inventory }

// PairedWith returns the position of the chest this chest is paired with,
// and whether it is paired.
func (c Chest) PairedWith(pos cube.Pos) (cube.Pos, bool) { return c.pairPos(pos), c.paired }

func (c Chest) chestData() Chest                    { return c }
func (c Chest) withChestData(data Chest) chestBlock { return data }
func (Chest) canPairChest(other world.Block) bool   { _, ok := other.(Chest); return ok }

// WithJavaChestState retains the actor while projecting source pairing into the
// native block-actor fields, which are not part of the Bedrock block palette.
func (c Chest) WithJavaChestState(previous world.Block, pos cube.Pos, kind string) world.Block {
	return c.withJavaChestState(previous, pos, kind)
}

func (c Chest) withJavaChestState(previous world.Block, pos cube.Pos, kind string) Chest {
	facing := c.Facing
	if old, ok := previous.(chestBlock); ok {
		c = old.chestData()
	} else if c.inventory == nil {
		c = NewChest()
	}
	c.Facing = facing
	paired := kind != "single"
	pairPos := pos
	if kind == "left" {
		pairPos = pos.Side(facing.RotateRight().Face())
	} else if kind == "right" {
		pairPos = pos.Side(facing.RotateLeft().Face())
	}
	if c.paired != paired || c.pairX != pairPos[0] || c.pairZ != pairPos[2] {
		c.pairInv = nil
	}
	c.paired = paired
	c.pairX, c.pairZ = pairPos[0], pairPos[2]
	return c
}

// GenerateLoot resolves an unopened source loot table exactly once and stores
// the resulting inventory on the chest actor.
func (c Chest) GenerateLoot(tx *world.Tx, pos cube.Pos) error {
	if c.lootTable == "" {
		return nil
	}
	if err := fillContainerLoot(tx, pos, c.inventory, c.lootTable, c.lootSeed, "minecraft:chest"); err != nil {
		return err
	}
	c.lootTable, c.lootSeed = "", 0
	if owner, ok := tx.Block(pos).(chestBlock); ok {
		tx.SetBlockEntity(pos, owner.withChestData(c))
	}
	return nil
}

// tryPair attempts to pair the inventories of this chest with a potential
// paired chest next to it. The (shared) inventory is returned and a bool is
// returned indicating if the chest changed its pairing state.
func (c Chest) tryPair(tx *world.Tx, pos cube.Pos) (*inventory.Inventory, bool) {
	if c.paired {
		if c.pairInv == nil {
			owner := tx.Block(pos).(chestBlock)
			if ch, pair, ok := c.pair(tx, pos, c.pairPos(pos), owner); ok {
				tx.SetBlockEntity(pos, ch)
				tx.SetBlockEntity(c.pairPos(pos), pair)
				return ch.chestData().pairInv, true
			}
			c.paired = false
			tx.SetBlock(pos, owner.withChestData(c), nil)
			return c.inventory, true
		}
		return c.pairInv, false
	}
	return c.inventory, false
}

// WithName returns the chest after applying a specific name to the block.
func (c Chest) WithName(a ...any) world.Item {
	c.CustomName = strings.TrimSuffix(fmt.Sprintln(a...), "\n")
	return c
}

// SideClosed ...
func (Chest) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// open opens the chest, displaying the animation and playing a sound.
func (c Chest) open(tx *world.Tx, pos cube.Pos) {
	viewers := tx.Viewers(pos.Vec3())
	for _, v := range viewers {
		if c.paired {
			v.ViewBlockAction(c.pairPos(pos), OpenAction{})
		}
		v.ViewBlockAction(pos, OpenAction{})
	}
	tx.ReleaseViewers(viewers)
	tx.PlaySound(pos.Vec3Centre(), sound.ChestOpen{})
}

// close closes the chest, displaying the animation and playing a sound.
func (c Chest) close(tx *world.Tx, pos cube.Pos) {
	viewers := tx.Viewers(pos.Vec3())
	for _, v := range viewers {
		if c.paired {
			v.ViewBlockAction(c.pairPos(pos), CloseAction{})
		}
		v.ViewBlockAction(pos, CloseAction{})
	}
	tx.ReleaseViewers(viewers)
	tx.PlaySound(pos.Vec3Centre(), sound.ChestClose{})
}

// StartOpen displays the chest opening animation and sound for a non-player container user.
func (c Chest) StartOpen(tx *world.Tx, pos cube.Pos) { c.open(tx, pos) }

// StopOpen displays the chest closing animation and sound for a non-player container user.
func (c Chest) StopOpen(tx *world.Tx, pos cube.Pos) { c.close(tx, pos) }

// AddViewer adds a viewer to the chest, so that it is updated whenever the inventory of the chest is changed.
func (c Chest) AddViewer(v ContainerViewer, tx *world.Tx, pos cube.Pos) {
	if _, changed := c.tryPair(tx, pos); changed {
		c = tx.Block(pos).(chestBlock).chestData()
	}
	c.viewerMu.Lock()
	defer c.viewerMu.Unlock()
	if len(c.viewers) == 0 {
		c.open(tx, pos)
	}
	c.viewers[v] = struct{}{}
}

// RemoveViewer removes a viewer from the chest, so that slot updates in the inventory are no longer sent to
// it.
func (c Chest) RemoveViewer(v ContainerViewer, tx *world.Tx, pos cube.Pos) {
	if _, changed := c.tryPair(tx, pos); changed {
		c = tx.Block(pos).(chestBlock).chestData()
	}
	c.viewerMu.Lock()
	defer c.viewerMu.Unlock()
	if len(c.viewers) == 0 {
		return
	}
	delete(c.viewers, v)
	if len(c.viewers) == 0 {
		c.close(tx, pos)
	}
}

// Activate ...
func (c Chest) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok {
		if c.paired {
			if d, ok := tx.Block(c.pairPos(pos).Side(cube.FaceUp)).(LightDiffuser); !ok || d.LightDiffusionLevel() > 2 {
				return false
			}
		}
		if d, ok := tx.Block(pos.Side(cube.FaceUp)).(LightDiffuser); ok && d.LightDiffusionLevel() <= 2 {
			opener.OpenBlockContainer(pos, tx)
		}
		return true
	}
	return false
}

// UseOnBlock ...
func (c Chest) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, c)
	if !used {
		return
	}
	//noinspection GoAssignmentToReceiver
	c = NewChest()
	c.Facing = user.Rotation().Direction().Opposite()

	// Check both sides of the chest to see if it is possible to pair with another chest.
	for _, dir := range []cube.Direction{c.Facing.RotateLeft(), c.Facing.RotateRight()} {
		pairPos := pos.Side(dir.Face())
		if ch, pair, ok := c.pair(tx, pos, pairPos, c); ok {
			place(tx, pos, ch, user, ctx)
			tx.SetBlock(pairPos, pair, nil)
			return placed(ctx)
		}
	}

	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (c Chest) BreakInfo() BreakInfo {
	return newBreakInfo(2.5, alwaysHarvestable, axeEffective, oneOf(c)).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		c.dropContents(pos, tx, c)
	})
}

func (c Chest) dropContents(pos cube.Pos, tx *world.Tx, owner chestBlock) {
	if c.paired {
		pairPos := c.pairPos(pos)
		if ch, pair, ok := c.unpair(tx, pos, owner); ok {
			c = ch.chestData()
			tx.SetBlock(pairPos, pair, nil)
		}
		c.paired = false
	}
	for _, stack := range c.Inventory(tx, pos).Clear() {
		dropItem(tx, stack, pos.Vec3Centre())
	}
}

// FlammabilityInfo ...
func (c Chest) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(0, 0, true)
}

// Paired returns whether the chest is paired with another chest.
func (c Chest) Paired() bool {
	return c.paired
}

// pair pairs this chest with the given chest position.
func (c Chest) pair(tx *world.Tx, pos, pairPos cube.Pos, owner chestBlock) (ch, other chestBlock, ok bool) {
	other, ok = tx.Block(pairPos).(chestBlock)
	if !ok || !owner.canPairChest(other) {
		return owner, other, false
	}
	pair := other.chestData()
	if c.Facing != pair.Facing || pair.paired && (pair.pairX != pos[0] || pair.pairZ != pos[2]) {
		return owner, other, false
	}
	m := new(sync.RWMutex)
	v := make(map[ContainerViewer]struct{})
	left, right := c.inventory.Clone(nil), pair.inventory.Clone(nil)
	if pos.Side(c.Facing.RotateRight().Face()) == pairPos {
		left, right = right, left
	}
	double := left.Merge(right, func(slot int, _, item item.Stack) {
		if slot < 27 {
			_ = left.SetItem(slot, item)
		} else {
			_ = right.SetItem(slot-27, item)
		}
		m.RLock()
		defer m.RUnlock()
		for viewer := range v {
			viewer.ViewSlotChange(slot, item)
		}
	})

	c.inventory, pair.inventory = left, right
	if pos.Side(c.Facing.RotateRight().Face()) == pairPos {
		c.inventory, pair.inventory = right, left
	}
	c.pairX, c.pairZ, c.paired = pairPos[0], pairPos[2], true
	pair.pairX, pair.pairZ, pair.paired = pos[0], pos[2], true
	c.viewerMu, pair.viewerMu = m, m
	c.viewers, pair.viewers = v, v
	c.pairInv, pair.pairInv = double, double
	return owner.withChestData(c), other.withChestData(pair), true
}

// unpair unpairs this chest from the chest it is currently paired with.
func (c Chest) unpair(tx *world.Tx, pos cube.Pos, owner chestBlock) (ch, other chestBlock, ok bool) {
	if !c.paired {
		return owner, nil, false
	}
	other, ok = tx.Block(c.pairPos(pos)).(chestBlock)
	if !ok || !owner.canPairChest(other) {
		return owner, other, false
	}
	pair := other.chestData()
	if c.Facing != pair.Facing || pair.paired && (pair.pairX != pos[0] || pair.pairZ != pos[2]) {
		return owner, other, false
	}

	if len(c.viewers) != 0 {
		c.close(tx, pos)
	}

	// Use fresh viewer maps so the cloned inventories notify the correct subscribers after unpairing.
	newViewerMu := new(sync.RWMutex)
	newViewers := make(map[ContainerViewer]struct{}, 1)
	c.inventory = c.inventory.Clone(func(slot int, _, after item.Stack) {
		newViewerMu.RLock()
		defer newViewerMu.RUnlock()
		for viewer := range newViewers {
			viewer.ViewSlotChange(slot, after)
		}
	})
	newPairViewerMu := new(sync.RWMutex)
	newPairViewers := make(map[ContainerViewer]struct{}, 1)
	pair.inventory = pair.inventory.Clone(func(slot int, _, after item.Stack) {
		newPairViewerMu.RLock()
		defer newPairViewerMu.RUnlock()
		for viewer := range newPairViewers {
			viewer.ViewSlotChange(slot, after)
		}
	})
	c.paired, pair.paired = false, false
	c.viewerMu, pair.viewerMu = newViewerMu, newPairViewerMu
	c.viewers, pair.viewers = newViewers, newPairViewers
	c.pairInv, pair.pairInv = nil, nil
	return owner.withChestData(c), other.withChestData(pair), true
}

// pairPos returns the position of the chest that this chest is paired with.
func (c Chest) pairPos(pos cube.Pos) cube.Pos {
	return cube.Pos{c.pairX, pos[1], c.pairZ}
}

// DecodeNBT ...
func (c Chest) DecodeNBT(data map[string]any) any {
	facing := c.Facing
	//noinspection GoAssignmentToReceiver
	c = NewChest()
	c.Facing = facing
	c.CustomName = nbtconv.String(data, "CustomName")
	c.lootTable = loot.JavaTableKey(nbtconv.String(data, "LootTable"))
	c.lootSeed = nbtconv.Int64(data, "LootTableSeed")

	pairX, ok := data["pairx"]
	pairZ, ok2 := data["pairz"]
	if ok && ok2 {
		pairX, ok := pairX.(int32)
		pairZ, ok2 := pairZ.(int32)
		if ok && ok2 {
			c.paired = true
			c.pairX, c.pairZ = int(pairX), int(pairZ)
		}
	}

	nbtconv.InvFromNBT(c.inventory, nbtconv.Slice(data, "Items"), data)
	return c
}

// EncodeNBT ...
func (c Chest) EncodeNBT() map[string]any {
	if c.inventory == nil {
		facing, customName, lootTable, lootSeed := c.Facing, c.CustomName, c.lootTable, c.lootSeed
		//noinspection GoAssignmentToReceiver
		c = NewChest()
		c.Facing, c.CustomName, c.lootTable, c.lootSeed = facing, customName, lootTable, lootSeed
	}
	m := map[string]any{
		"Items": nbtconv.InvToNBT(c.inventory),
		"id":    "Chest",
	}
	if c.CustomName != "" {
		m["CustomName"] = c.CustomName
	}

	if c.paired {
		m["pairx"] = int32(c.pairX)
		m["pairz"] = int32(c.pairZ)
	}
	if c.lootTable != "" {
		m["LootTable"] = loot.BedrockTablePath(c.lootTable)
		m["LootTableSeed"] = c.lootSeed
	} else {
		// BDS marks chests players place as not findable.
		m["Findable"] = byte(0)
	}
	return m
}

// EncodeItem ...
func (Chest) EncodeItem() (name string, meta int16) {
	return "minecraft:chest", 0
}

// EncodeBlock ...
func (c Chest) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:chest", map[string]any{"minecraft:cardinal_direction": c.Facing.String()}
}

// allChests ...
func allChests() (chests []world.Block) {
	for _, direction := range cube.Directions() {
		chests = append(chests, Chest{Facing: direction})
	}
	return
}
