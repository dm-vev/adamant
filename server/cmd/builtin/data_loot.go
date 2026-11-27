package builtin

import (
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/inventory"
	"github.com/dm-vev/adamant/server/loot"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Java LootCommand targets: give <players> | spawn <pos> | insert <pos> |
// replace block <pos> <slot> | replace entity <targets> <slot>, each followed
// by the `loot <table>` source.
type lootGiveCommand struct {
	gameMaster
	Give    cmd.SubCommand `cmd:"give"`
	Targets cmd.Players    `cmd:"players"`
	Loot    cmd.SubCommand `cmd:"loot"`
	Table   string         `cmd:"loot_table"`
}

type lootSpawnCommand struct {
	gameMaster
	Spawn    cmd.SubCommand `cmd:"spawn"`
	Position mgl64.Vec3     `cmd:"position"`
	Loot     cmd.SubCommand `cmd:"loot"`
	Table    string         `cmd:"loot_table"`
}

type lootInsertCommand struct {
	gameMaster
	Insert   cmd.SubCommand `cmd:"insert"`
	Position mgl64.Vec3     `cmd:"position"`
	Loot     cmd.SubCommand `cmd:"loot"`
	Table    string         `cmd:"loot_table"`
}

type lootReplaceBlockCommand struct {
	gameMaster
	Replace  cmd.SubCommand `cmd:"replace"`
	Block    cmd.SubCommand `cmd:"block"`
	Position mgl64.Vec3     `cmd:"position"`
	Slot     string         `cmd:"slot"`
	Loot     cmd.SubCommand `cmd:"loot"`
	Table    string         `cmd:"loot_table"`
}

type lootReplaceEntityCommand struct {
	gameMaster
	Replace cmd.SubCommand `cmd:"replace"`
	Entity  cmd.SubCommand `cmd:"entity"`
	Targets cmd.Entities   `cmd:"targets"`
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

type lootTargetResult struct {
	ok  bool
	err error
}

func (c lootGiveCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	table := c.Table
	cmd.ForEach(src, o, tx, c.Targets,
		func(rtx *world.Tx, t cmd.Target) lootTargetResult {
			p, ok := t.(*player.Player)
			if !ok {
				return lootTargetResult{}
			}
			stacks, err := generateCommandLoot(table, rtx, p.Position())
			if err != nil {
				return lootTargetResult{ok: true, err: err}
			}
			for _, stack := range stacks {
				if added, _ := p.Inventory().AddItem(stack); added < stack.Count() && rtx != nil {
					rtx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: p.Position()}, stack.Grow(-added)))
				}
			}
			return lootTargetResult{ok: true}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[lootTargetResult]) {
			var names []string
			for _, r := range rs {
				if !r.Value.ok {
					continue
				}
				if r.Value.err != nil {
					o.Errort(cmd.MessageParameterInvalid, table)
					return
				}
				names = append(names, r.Name)
			}
			if len(names) == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(len(names))
			o.Printf("Generated loot from %s for %s.", table, joinSortedStrings(names))
		},
	)
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

func (c lootReplaceEntityCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
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
	table := c.Table
	baseSlot := c.Slot
	cmd.ForEach(src, o, tx, c.Targets,
		func(rtx *world.Tx, target cmd.Target) lootTargetResult {
			stacks, err := generateCommandLoot(table, rtx, target.Position())
			if err != nil {
				return lootTargetResult{err: err}
			}
			targetChanged := false
			for i, stack := range stacks {
				slot := baseSlot
				if containerSlot >= 0 {
					slot = "container." + strconv.Itoa(containerSlot+i)
				} else if i > 0 {
					break
				}
				if setLootEntitySlot(target, slot, stack) {
					targetChanged = true
				}
			}
			return lootTargetResult{ok: targetChanged}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[lootTargetResult]) {
			changed := 0
			for _, r := range rs {
				if r.Value.err != nil {
					o.Errort(cmd.MessageParameterInvalid, table)
					return
				}
				if r.Value.ok {
					changed++
				}
			}
			if changed == 0 {
				o.Errort(cmd.MessageNoTargets)
				return
			}
			o.SetResult(changed)
			o.Printf("Generated loot from %s for %d target(s).", table, changed)
		},
	)
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
