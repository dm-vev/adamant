package builtin

import (
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Java LootCommand targets: give <players> | spawn <pos> | insert <pos> |
// replace block <pos> <slot> | replace entity <targets> <slot>, each followed
// by the `loot <table>` source.
type lootGiveCommand struct {
	opGate
	Give    cmd.SubCommand `cmd:"give"`
	Targets []cmd.Target   `cmd:"players"`
	Loot    cmd.SubCommand `cmd:"loot"`
	Table   string         `cmd:"loot_table"`
}

type lootSpawnCommand struct {
	opGate
	Spawn    cmd.SubCommand `cmd:"spawn"`
	Position mgl64.Vec3     `cmd:"position"`
	Loot     cmd.SubCommand `cmd:"loot"`
	Table    string         `cmd:"loot_table"`
}

type lootInsertCommand struct {
	opGate
	Insert   cmd.SubCommand `cmd:"insert"`
	Position mgl64.Vec3     `cmd:"position"`
	Loot     cmd.SubCommand `cmd:"loot"`
	Table    string         `cmd:"loot_table"`
}

type lootReplaceBlockCommand struct {
	opGate
	Replace  cmd.SubCommand `cmd:"replace"`
	Block    cmd.SubCommand `cmd:"block"`
	Position mgl64.Vec3     `cmd:"position"`
	Slot     string         `cmd:"slot"`
	Loot     cmd.SubCommand `cmd:"loot"`
	Table    string         `cmd:"loot_table"`
}

type lootReplaceEntityCommand struct {
	opGate
	Replace cmd.SubCommand `cmd:"replace"`
	Entity  cmd.SubCommand `cmd:"entity"`
	Targets []cmd.Target   `cmd:"targets"`
	Slot    string         `cmd:"slot"`
	Loot    cmd.SubCommand `cmd:"loot"`
	Table   string         `cmd:"loot_table"`
}

func newLootCommand() cmd.Command {
	return newCommand("loot", nil, lootGiveCommand{}, lootSpawnCommand{}, lootInsertCommand{}, lootReplaceBlockCommand{}, lootReplaceEntityCommand{})
}

// generateCommandLoot rolls a table with a fresh seed, as Java's command loot
// context draws from the level random.
func generateCommandLoot(table string, tx *world.Tx, origin mgl64.Vec3) ([]item.Stack, error) {
	return loot.Generate(table, rand.Int64(), loot.Context{Tx: tx, Origin: origin})
}

func (c lootGiveCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	ps := playersFromTargets(c.Targets)
	if len(ps) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	for _, p := range ps {
		stacks, err := generateCommandLoot(c.Table, tx, p.Position())
		if err != nil {
			o.Errort(cmd.MessageParameterInvalid, c.Table)
			return
		}
		for _, stack := range stacks {
			if added, _ := p.Inventory().AddItem(stack); added < stack.Count() {
				tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: p.Position()}, stack.Grow(-added)))
			}
		}
	}
	o.Printf("Generated loot from %s for %s.", c.Table, joinNames(ps))
}

func (c lootSpawnCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	if tx == nil {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	stacks, err := generateCommandLoot(c.Table, tx, c.Position)
	if err != nil {
		o.Errort(cmd.MessageParameterInvalid, c.Table)
		return
	}
	for _, stack := range stacks {
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: c.Position}, stack))
	}
	o.Printf("Generated loot from %s.", c.Table)
}

func (c lootInsertCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	if tx == nil {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	pos := cube.PosFromVec3(c.Position)
	container, ok := tx.Block(pos).(block.Container)
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, c.Position)
		return
	}
	stacks, err := generateCommandLoot(c.Table, tx, c.Position)
	if err != nil {
		o.Errort(cmd.MessageParameterInvalid, c.Table)
		return
	}
	inv := container.Inventory(tx, pos)
	if inv == nil {
		o.Errort(cmd.MessageParameterInvalid, c.Position)
		return
	}
	for _, stack := range stacks {
		added, _ := inv.AddItem(stack)
		if added < stack.Count() {
			tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: c.Position}, stack.Grow(-added)))
		}
	}
	o.Printf("Generated loot from %s in the container at %v.", c.Table, pos)
}

func (c lootReplaceBlockCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	if tx == nil {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	const prefix = "container."
	if !strings.HasPrefix(c.Slot, prefix) {
		o.Errort(cmd.MessageParameterInvalid, c.Slot)
		return
	}
	slot, err := strconv.Atoi(strings.TrimPrefix(c.Slot, prefix))
	if err != nil || slot < 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Slot)
		return
	}
	pos := cube.PosFromVec3(c.Position)
	container, ok := tx.Block(pos).(block.Container)
	if !ok || slot >= container.ContainerSize() {
		o.Errort(cmd.MessageParameterInvalid, c.Slot)
		return
	}
	stacks, err := generateCommandLoot(c.Table, tx, c.Position)
	if err != nil {
		o.Errort(cmd.MessageParameterInvalid, c.Table)
		return
	}
	inv := container.Inventory(tx, pos)
	for _, stack := range stacks {
		if slot >= container.ContainerSize() {
			break
		}
		if err := inv.SetItem(slot, stack); err != nil {
			break
		}
		slot++
	}
	o.Printf("Generated loot from %s in the container at %v.", c.Table, pos)
}

func (c lootReplaceEntityCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	if tx == nil {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	if c.Slot != "weapon.mainhand" && c.Slot != "weapon.offhand" && c.Slot != "armor.head" && c.Slot != "armor.chest" && c.Slot != "armor.legs" && c.Slot != "armor.feet" && !strings.HasPrefix(c.Slot, "container.") {
		o.Errort(cmd.MessageParameterInvalid, c.Slot)
		return
	}
	containerSlot := -1
	if strings.HasPrefix(c.Slot, "container.") {
		var err error
		containerSlot, err = strconv.Atoi(strings.TrimPrefix(c.Slot, "container."))
		if err != nil || containerSlot < 0 {
			o.Errort(cmd.MessageParameterInvalid, c.Slot)
			return
		}
	}
	changed := 0
	for _, target := range c.Targets {
		stacks, err := generateCommandLoot(c.Table, tx, target.Position())
		if err != nil {
			o.Errort(cmd.MessageParameterInvalid, c.Table)
			return
		}
		targetChanged := false
		for i, stack := range stacks {
			slot := c.Slot
			if containerSlot >= 0 {
				slot = "container." + strconv.Itoa(containerSlot+i)
			} else if i > 0 {
				break
			}
			if setLootEntitySlot(target, slot, stack) {
				targetChanged = true
			}
		}
		if targetChanged {
			changed++
		}
	}
	if changed == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	o.Printf("Generated loot from %s for %d target(s).", c.Table, changed)
}

func setLootEntitySlot(target cmd.Target, slot string, stack item.Stack) bool {
	switch slot {
	case "weapon.mainhand", "weapon.offhand":
		held, ok := target.(interface {
			HeldItems() (item.Stack, item.Stack)
			SetHeldItems(item.Stack, item.Stack)
		})
		if !ok {
			return false
		}
		main, off := held.HeldItems()
		if slot == "weapon.mainhand" {
			main = stack
		} else {
			off = stack
		}
		held.SetHeldItems(main, off)
		return true
	case "armor.head", "armor.chest", "armor.legs", "armor.feet":
		wearer, ok := target.(interface{ Armour() *inventory.Armour })
		if !ok {
			return false
		}
		armour := wearer.Armour()
		switch slot {
		case "armor.head":
			armour.SetHelmet(stack)
		case "armor.chest":
			armour.SetChestplate(stack)
		case "armor.legs":
			armour.SetLeggings(stack)
		case "armor.feet":
			armour.SetBoots(stack)
		}
		return true
	default:
		index, err := strconv.Atoi(strings.TrimPrefix(slot, "container."))
		if err != nil || index < 0 {
			return false
		}
		container, ok := target.(interface{ Inventory() *inventory.Inventory })
		if !ok || container.Inventory() == nil {
			return false
		}
		return container.Inventory().SetItem(index, stack) == nil
	}
}
