package block

import (
	"math/rand/v2"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// VaultState is the state of a vault, the vault_state block property.
type VaultState uint8

const (
	VaultInactive VaultState = iota
	VaultActive
	VaultUnlocking
	VaultEjecting
)

// String returns the name of the state as the vault_state property names it.
func (s VaultState) String() string {
	switch s {
	case VaultActive:
		return "active"
	case VaultUnlocking:
		return "unlocking"
	case VaultEjecting:
		return "ejecting"
	}
	return "inactive"
}

// Vault is a trial chamber block that ejects a reward once for every player
// that unlocks it with its key. Its behaviour is ported from Java's
// VaultBlockEntity and its block actor is saved as BDS saves it.
type Vault struct {
	solid
	transparent
	Facing  cube.Direction
	State   VaultState
	Ominous bool

	// data is the block actor, shared by the copies of the block.
	data *vaultData
}

// NewVault returns a vault with the default configuration: it rewards from
// the trial chamber reward loot table for a trial key.
func NewVault() Vault {
	return Vault{data: newVaultData()}
}

func (v Vault) withData() Vault {
	if v.data == nil {
		v.data = newVaultData()
	}
	return v
}

// LightEmissionLevel ...
func (v Vault) LightEmissionLevel() uint8 {
	if v.State == VaultInactive {
		return 6
	}
	return 12
}

// UseOnBlock places the vault facing the player.
func (v Vault) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, v)
	if !used {
		return false
	}
	v.Facing = user.Rotation().Direction().Opposite()
	place(tx, pos, v, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (v Vault) BreakInfo() BreakInfo {
	return newBreakInfo(50, alwaysHarvestable, pickaxeEffective, simpleDrops())
}

// EncodeItem ...
func (v Vault) EncodeItem() (string, int16) {
	return "minecraft:vault", 0
}

// EncodeBlock ...
func (v Vault) EncodeBlock() (string, map[string]any) {
	return "minecraft:vault", map[string]any{"minecraft:cardinal_direction": v.Facing.String(), "ominous": boolByte(v.Ominous), "vault_state": v.State.String()}
}

// Hash ...
func (v Vault) Hash() (uint64, uint64) {
	return hashVault, uint64(v.State) | uint64(boolByte(v.Ominous))<<3 | uint64(v.Facing)<<4
}

// EncodeNBT ...
func (v Vault) EncodeNBT() map[string]any {
	d := v.withData().data
	data := map[string]any{
		"items_to_eject":            actorItemsNBT(d.toEject),
		"rewarded_players":          uniqueIDsNBT(d.rewarded),
		"state_updating_resumes_at": d.resumesAt,
	}
	if !d.display.Empty() {
		data["display_item"] = actorItemNBT(d.display)
	}
	if d.ejectionsNeeded != 0 {
		data["total_ejections_needed"] = int32(d.ejectionsNeeded)
	}
	return map[string]any{
		"id": "Vault",
		"config": map[string]any{
			"activation_range":               float32(d.activationRange),
			"deactivation_range":             float32(d.deactivationRange),
			"key_item":                       actorItemNBT(d.key),
			"loot_table":                     bedrockLootTable(d.lootTable),
			"override_loot_table_to_display": bedrockLootTable(d.displayLootTable),
		},
		"data": data,
	}
}

// UpdateNBT returns what BDS updates vaults on clients with: the item it
// displays and the players it connects to.
func (v Vault) UpdateNBT() map[string]any {
	d := v.withData().data
	return map[string]any{
		"connected_particle_range": float32(d.particlesRange),
		"connected_players":        uniqueIDsNBT(d.connected),
		"display_item":             actorItemNBT(d.display),
	}
}

// DecodeNBT reads a vault saved by BDS, placed from a Java structure template,
// or saved in the Java layout by earlier Adamant versions.
func (v Vault) DecodeNBT(m map[string]any) any {
	d := newVaultData()
	if config, ok := m["config"].(map[string]any); ok {
		d.key = readActorItem(config["key_item"])
		if table, _ := config["loot_table"].(string); table != "" {
			d.lootTable = javaLootTable(table)
		}
		d.displayLootTable, _ = config["override_loot_table_to_display"].(string)
		d.displayLootTable = javaLootTable(d.displayLootTable)
		if r, ok := trialNumber(config["activation_range"]); ok {
			d.activationRange = r
		}
		if r, ok := trialNumber(config["deactivation_range"]); ok {
			d.deactivationRange = r
		}
		if d.activationRange > d.deactivationRange {
			d.activationRange, d.deactivationRange = 4, 4.5
		}
	}
	server, ok := m["data"].(map[string]any)
	shared := server
	if !ok {
		server, _ = m["server_data"].(map[string]any)
		shared, _ = m["shared_data"].(map[string]any)
	}
	d.toEject = readActorItems(server["items_to_eject"])
	d.rewarded = readUniqueIDs(server["rewarded_players"])
	d.resumesAt, _ = trialInteger(server["state_updating_resumes_at"])
	if n, ok := trialInteger(server["total_ejections_needed"]); ok {
		d.ejectionsNeeded = int(n)
	}
	d.display = readActorItem(shared["display_item"])
	d.connected = readUniqueIDs(shared["connected_players"])
	if r, ok := trialNumber(shared["connected_particles_range"]); ok {
		d.particlesRange = r
	}
	v.data = d
	return v
}

// vaultData is the block actor of a vault: the VaultConfig, VaultServerData
// and VaultSharedData of Java. Loot tables are Java keys and players actor
// unique IDs.
type vaultData struct {
	lootTable, displayLootTable        string
	activationRange, deactivationRange float64
	key                                item.Stack

	// rewarded are the players the vault rewarded, oldest first.
	rewarded        []int64
	resumesAt       int64
	toEject         []item.Stack
	ejectionsNeeded int
	lastInsertFail  int64

	display        item.Stack
	connected      []int64
	particlesRange float64
}

func newVaultData() *vaultData {
	return &vaultData{
		lootTable:       "minecraft:chests/trial_chambers/reward",
		activationRange: 4, deactivationRange: 4.5, particlesRange: 4.5,
		key: item.NewStack(item.TrialKey{}, 1),
	}
}

const vaultMaxRewardedPlayers = 128

// Activate inserts the key held into the vault, ported from
// VaultBlockEntity.Server.tryInsertKey. Nothing happens unless the vault is
// active and the player holds an item.
func (v Vault) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if held.Empty() || v.State != VaultActive {
		return false
	}
	t := vaultTick{v: v.withData(), pos: pos, tx: tx, tick: tx.CurrentTick(), dirty: v.data == nil}
	d := t.v.data
	switch {
	case d.key.Empty():
	case held.Count() < d.key.Count() || !held.Comparable(d.key):
		t.insertFailSound("block.vault.insert_item_fail")
	case containsID(d.rewarded, u.H().UniqueID()):
		t.insertFailSound("block.vault.reject_rewarded_player")
	default:
		random := vanilla.NewLegacyRandom(rand.Int64())
		stacks, err := loot.Generate(d.lootTable, 0, loot.Context{
			Random: &random, Origin: pos.Vec3Centre(), Player: u, ThisEntity: u, Tool: held.Item(), ToolStack: held, Tx: tx,
		})
		if err != nil || len(stacks) == 0 {
			break
		}
		ctx.SubtractFromCount(d.key.Count())
		d.toEject, d.ejectionsNeeded = stacks, len(stacks)
		t.setDisplay(d.nextToEject())
		d.resumesAt = t.tick + 14
		t.transition(VaultUnlocking)
		d.rewarded = append(d.rewarded, u.H().UniqueID())
		if len(d.rewarded) > vaultMaxRewardedPlayers {
			d.rewarded = slices.Delete(d.rewarded, 0, 1)
		}
		t.updateConnectedPlayers(d.deactivationRange)
		t.dirty = true
	}
	t.commit(v)
	return true
}

// Tick runs the vault, ported from VaultBlockEntity.Server.tick.
func (v Vault) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	t := vaultTick{v: v.withData(), pos: pos, tx: tx, tick: currentTick, dirty: v.data == nil}
	d := t.v.data
	if currentTick%20 == 0 && t.v.State == VaultActive {
		t.cycleDisplayItem()
	}
	if currentTick >= d.resumesAt {
		if next := t.nextState(); next != t.v.State {
			t.transition(next)
		}
	}
	t.commit(v)
}

// vaultTick holds a vault through a tick or a key insertion. shared is set
// where the data clients are sent changed, and dirty where only the saved
// data did.
type vaultTick struct {
	v             Vault
	pos           cube.Pos
	tx            *world.Tx
	tick          int64
	dirty, shared bool
}

// commit sets the vault in the world if it changed since before.
func (t *vaultTick) commit(before Vault) {
	switch {
	case t.v.State != before.State || t.shared:
		t.tx.SetBlock(t.pos, t.v, nil)
	case t.dirty:
		t.tx.SetBlockEntity(t.pos, t.v)
	}
}

// nextState ports VaultState.tickAndGetNext.
func (t *vaultTick) nextState() VaultState {
	d := t.v.data
	switch t.v.State {
	case VaultInactive:
		return t.updateStateForConnectedPlayers(d.activationRange)
	case VaultActive:
		return t.updateStateForConnectedPlayers(d.deactivationRange)
	case VaultUnlocking:
		d.resumesAt = t.tick + 20
		t.dirty = true
		return VaultEjecting
	}
	if len(d.toEject) == 0 {
		d.ejectionsNeeded = 0
		t.dirty = true
		return t.updateStateForConnectedPlayers(d.deactivationRange)
	}
	progress := d.ejectionProgress()
	ejected := d.toEject[len(d.toEject)-1]
	d.toEject = d.toEject[:len(d.toEject)-1]
	ejectTrialItem(t.tx, ejected, t.pos)
	t.tx.AddParticle(t.pos.Vec3(), particle.VaultEjectItem{})
	t.tx.PlaySound(t.pos.Vec3Centre(), sound.Custom{Name: "block.vault.eject_item", Volume: 1, Pitch: 0.8 + 0.4*progress})
	t.setDisplay(d.nextToEject())
	d.resumesAt = t.tick + 20
	t.dirty = true
	return VaultEjecting
}

// transition sets the state of the vault, ported from
// VaultState.onTransition.
func (t *vaultTick) transition(to VaultState) {
	from := t.v.State
	t.v.State = to
	centre := t.pos.Vec3Centre()
	if from == VaultEjecting {
		t.tx.PlaySound(centre, sound.Custom{Name: "block.vault.close_shutter", Volume: 1, Pitch: 1})
	}
	switch to {
	case VaultInactive:
		t.setDisplay(item.Stack{})
		t.tx.AddParticle(t.pos.Vec3(), particle.VaultDeactivate{Ominous: t.v.Ominous})
	case VaultActive:
		if t.v.data.display.Empty() {
			t.cycleDisplayItem()
		}
		t.tx.AddParticle(t.pos.Vec3(), particle.VaultActivate{Ominous: t.v.Ominous})
	case VaultUnlocking:
		t.tx.PlaySound(centre, sound.Custom{Name: "block.vault.insert_item", Volume: 1, Pitch: 1})
	case VaultEjecting:
		t.tx.PlaySound(centre, sound.Custom{Name: "block.vault.open_shutter", Volume: 1, Pitch: 1})
	}
}

// updateStateForConnectedPlayers ports the method of VaultState of that name.
func (t *vaultTick) updateStateForConnectedPlayers(r float64) VaultState {
	t.updateConnectedPlayers(r)
	t.v.data.resumesAt = t.tick + 20
	t.dirty = true
	if len(t.v.data.connected) != 0 {
		return VaultActive
	}
	return VaultInactive
}

// updateConnectedPlayers connects the vault to the players within r of it it
// has not rewarded yet, creative ones included.
func (t *vaultTick) updateConnectedPlayers(r float64) {
	d := t.v.data
	var connected []int64
	for _, p := range trialPlayersNear(t.tx, t.pos, r, true, false) {
		if id := p.H().UniqueID(); !containsID(d.rewarded, id) {
			connected = append(connected, id)
		}
	}
	if len(connected) != len(d.connected) || slices.ContainsFunc(connected, func(id int64) bool { return !containsID(d.connected, id) }) {
		d.connected = connected
		t.shared = true
	}
}

// cycleDisplayItem shows a random item of the loot table of the vault, or
// nothing if it can't be unlocked.
func (t *vaultTick) cycleDisplayItem() {
	d := t.v.data
	if d.key.Empty() || t.v.State == VaultInactive {
		t.setDisplay(item.Stack{})
		return
	}
	table := d.lootTable
	if d.displayLootTable != "" {
		table = d.displayLootTable
	}
	random := vanilla.NewLegacyRandom(rand.Int64())
	stacks, err := loot.Generate(table, 0, loot.Context{Random: &random, Origin: t.pos.Vec3Centre(), Tx: t.tx})
	if err != nil || len(stacks) == 0 {
		t.setDisplay(item.Stack{})
		return
	}
	t.setDisplay(stacks[random.NextInt(int32(len(stacks)))])
}

// setDisplay sets the item the vault displays.
func (t *vaultTick) setDisplay(s item.Stack) {
	d := t.v.data
	if s.Empty() && d.display.Empty() || !s.Empty() && !d.display.Empty() && s.Equal(d.display) {
		return
	}
	d.display = s
	t.shared = true
}

// insertFailSound plays the sound of a key the vault refuses, at most every
// 15 ticks.
func (t *vaultTick) insertFailSound(name string) {
	d := t.v.data
	if t.tick >= d.lastInsertFail+15 {
		t.tx.PlaySound(t.pos.Vec3Centre(), sound.Custom{Name: name, Volume: 1, Pitch: 1})
		d.lastInsertFail = t.tick
	}
}

// nextToEject returns the item the vault ejects next, which it displays.
func (d *vaultData) nextToEject() item.Stack {
	if len(d.toEject) == 0 {
		return item.Stack{}
	}
	return d.toEject[len(d.toEject)-1]
}

// ejectionProgress ports VaultServerData.ejectionProgress, which raises the
// pitch of the ejections.
func (d *vaultData) ejectionProgress() float64 {
	if d.ejectionsNeeded <= 1 {
		return 1
	}
	return 1 - (float64(len(d.toEject))-1)/float64(d.ejectionsNeeded-1)
}
