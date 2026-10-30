package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"strings"
)

// MinecartCommandBlock is a minecart carrying an activator-rail command actor.
type MinecartCommandBlock struct{ *Minecart }

// CommandBlockMinecartConfig configures the command actor carried by a minecart.
type CommandBlockMinecartConfig struct {
	Command, CustomName, LastOutput                                       string
	TrackOutput                                                           bool
	SuccessCount, TickDelay                                               int
	Powered, Conditional, NeedsRedstone, ConditionMet, ExecuteOnFirstTick bool
	Mode                                                                  int
}

// Apply installs command-block minecart state on entity data.
func (c CommandBlockMinecartConfig) Apply(data *world.EntityData) {
	data.Data = &commandBlockMinecartBehaviour{
		MinecartBehaviour: minecartConf.New(), command: c.Command, customName: c.CustomName,
		inv:        inventory.New(1, nil),
		lastOutput: c.LastOutput, trackOutput: c.TrackOutput, successCount: c.SuccessCount,
		tickDelay: c.TickDelay, powered: c.Powered, conditional: c.Conditional,
		needsRedstone: c.NeedsRedstone, conditionMet: c.ConditionMet,
		executeOnFirstTick: c.ExecuteOnFirstTick, mode: c.Mode,
	}
}

// NewCommandBlockMinecart constructs a command-block minecart entity handle.
func NewCommandBlockMinecart(opts world.EntitySpawnOpts, conf CommandBlockMinecartConfig) *world.EntityHandle {
	return opts.New(CommandBlockMinecartType, conf)
}

type commandBlockMinecartBehaviour struct {
	*MinecartBehaviour
	inv                                                                                *inventory.Inventory
	command, customName, lastOutput                                                    string
	successCount, tickDelay                                                            int
	trackOutput, powered, conditional, needsRedstone, conditionMet, executeOnFirstTick bool
	mode                                                                               int
	lastActivatorRail                                                                  cube.Pos
	hasActivatorRail                                                                   bool
}

// Inventory exposes the one-slot container used by Bedrock's detector-rail
// comparator query. It is independent of command execution success count.
func (m *MinecartCommandBlock) Inventory() *inventory.Inventory {
	return m.Behaviour().(*commandBlockMinecartBehaviour).inv
}

func (b *commandBlockMinecartBehaviour) Base() *MinecartBehaviour { return b.MinecartBehaviour }

// Interact opens the command-block editor only for creative operators.
func (m *MinecartCommandBlock) Interact(_ *world.Tx, user item.User, _ *item.UseContext) bool {
	player, ok := user.(interface {
		Name() string
		GameMode() world.GameMode
	})
	return ok && player.GameMode().CreativeInventory() && cmd.IsOperator(player.Name())
}

func (m *MinecartCommandBlock) Tick(tx *world.Tx, tick int64) {
	m.Ent.Tick(tx, tick)
	b := m.Behaviour().(*commandBlockMinecartBehaviour)
	p := cube.PosFromVec3(m.Position())
	if block.IsRail(tx.Block(p.Side(cube.FaceDown))) {
		p = p.Side(cube.FaceDown)
	}
	r, ok := tx.Block(p).(block.ActivatorRail)
	if !ok || !r.Powered {
		b.hasActivatorRail = false
		return
	}
	if b.hasActivatorRail && b.lastActivatorRail == p {
		return
	}
	b.lastActivatorRail, b.hasActivatorRail = p, true
	b.execute(m.Ent, tx)
}
func (b *commandBlockMinecartBehaviour) execute(e *Ent, tx *world.Tx) {
	if !tx.World().GameRuleBool("command_blocks_work") || b.command == "" {
		return
	}
	line := strings.TrimSpace(strings.TrimPrefix(b.command, "/"))
	words := strings.Fields(line)
	if len(words) == 0 {
		return
	}
	if b.conditional && !b.conditionMet {
		b.successCount = 0
		return
	}
	c, ok := cmd.ByAlias(words[0])
	if !ok {
		b.successCount = 0
		b.conditionMet = false
		return
	}
	source := cmd.NewBlockSource(cube.PosFromVec3(e.Position()), tx.World(), b.customName)
	c.Execute(strings.TrimSpace(strings.TrimPrefix(line, words[0])), source, tx)
	out := source.Output()
	b.successCount, b.conditionMet = out.Result(), out.Success()
	if b.trackOutput && tx.World().GameRuleBool("command_block_output") {
		b.lastOutput = source.Text()
	}
}

// CommandBlockMinecartType is the registered command-block minecart entity type.
var CommandBlockMinecartType commandBlockMinecartType

type commandBlockMinecartType struct{}

func (commandBlockMinecartType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &MinecartCommandBlock{Minecart: &Minecart{Ent: &Ent{tx: tx, handle: h, data: d}}}
}
func (commandBlockMinecartType) EncodeEntity() string          { return "minecraft:command_block_minecart" }
func (commandBlockMinecartType) BBox(e world.Entity) cube.BBox { return minecartBBox(e) }
func (commandBlockMinecartType) NetworkOffset() float64        { return minecartNetworkOffset }
func (commandBlockMinecartType) DecodeNBT(m map[string]any, d *world.EntityData) {
	b := &commandBlockMinecartBehaviour{MinecartBehaviour: minecartConf.New(), inv: inventory.New(1, nil)}
	nbtconv.InvFromNBT(b.inv, nbtconv.Slice(m, "Items"), m)
	b.command = nbtconv.String(m, "Command")
	b.customName = nbtconv.String(m, "CustomName")
	b.lastOutput = nbtconv.String(m, "LastOutput")
	b.trackOutput = nbtconv.Bool(m, "TrackOutput")
	b.successCount = int(nbtconv.Int32(m, "SuccessCount"))
	b.powered = nbtconv.Bool(m, "powered")
	b.conditionMet = nbtconv.Bool(m, "conditionMet")
	b.tickDelay = int(nbtconv.Int32(m, "TickDelay"))
	b.executeOnFirstTick = nbtconv.Bool(m, "ExecuteOnFirstTick")
	b.mode = int(nbtconv.Int32(m, "LPCommandMode"))
	b.conditional = nbtconv.Bool(m, "LPCondionalMode")
	if _, ok := m["LPRedstoneMode"]; ok {
		b.needsRedstone = !nbtconv.Bool(m, "LPRedstoneMode")
	} else {
		b.needsRedstone = !nbtconv.Bool(m, "auto")
	}
	readMinecartDisplayNBT(b.MinecartBehaviour, m)
	d.Data = b
}
func (commandBlockMinecartType) EncodeNBT(d *world.EntityData) map[string]any {
	b, ok := d.Data.(*commandBlockMinecartBehaviour)
	if !ok {
		return map[string]any{}
	}
	m := map[string]any{
		"id": "CommandBlockMinecart", "Command": b.command, "CustomName": b.customName, "LastOutput": b.lastOutput,
		"TrackOutput": boolByte(b.trackOutput), "SuccessCount": int32(b.successCount), "powered": boolByte(b.powered),
		"conditionMet": boolByte(b.conditionMet), "TickDelay": int32(b.tickDelay), "ExecuteOnFirstTick": boolByte(b.executeOnFirstTick),
		"LPCommandMode": int32(b.mode), "LPCondionalMode": boolByte(b.conditional), "LPRedstoneMode": boolByte(!b.needsRedstone),
		"auto": boolByte(!b.needsRedstone), "Version": int32(26),
	}
	writeMinecartDisplayNBT(b.MinecartBehaviour, m)
	if items := nbtconv.InvToNBT(b.inv); items != nil {
		m["Items"] = items
	} else {
		m["Items"] = []map[string]any{}
	}
	return m
}

// UpdateCommandBlock applies client editor fields to the minecart actor.
func (m *MinecartCommandBlock) UpdateCommandBlock(command, name, output string, trackOutput bool, delay uint32, firstTick bool) {
	b := m.Behaviour().(*commandBlockMinecartBehaviour)
	b.command, b.customName, b.lastOutput = command, name, output
	b.trackOutput, b.tickDelay, b.executeOnFirstTick = trackOutput, int(delay), firstTick
}
