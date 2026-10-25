package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

const maxBeehiveOccupants = 3

var (
	hashBeehive = NextHash()
	hashBeeNest = NextHash()
)

// Beehive is a crafted hive. Occupants use the native Bedrock block-actor
// schema and preserve each bee's actor payload while it is inside the hive.
type Beehive struct {
	solid
	Facing     cube.Direction
	HoneyLevel int
	Occupants  []any
	FlowerPos  map[string]any
}

// BeeNest is the naturally generated counterpart of Beehive.
type BeeNest struct{ Beehive }

func (h Beehive) Model() world.BlockModel     { return model.Solid{} }
func (h Beehive) EncodeItem() (string, int16) { return "minecraft:beehive", 0 }
func (h Beehive) EncodeBlock() (string, map[string]any) {
	return "minecraft:beehive", map[string]any{"direction": int32(horizontalDirection(h.Facing)), "honey_level": int32(h.HoneyLevel)}
}
func (h Beehive) Hash() (uint64, uint64) {
	return hashBeehive, uint64(h.Facing) | uint64(h.HoneyLevel)<<2
}
func (h BeeNest) EncodeItem() (string, int16) { return "minecraft:bee_nest", 0 }
func (h BeeNest) EncodeBlock() (string, map[string]any) {
	return "minecraft:bee_nest", map[string]any{"direction": int32(horizontalDirection(h.Facing)), "honey_level": int32(h.HoneyLevel)}
}
func (h BeeNest) Hash() (uint64, uint64) {
	return hashBeeNest, uint64(h.Facing) | uint64(h.HoneyLevel)<<2
}
func (h Beehive) EncodeNBT() map[string]any { return encodeBeehiveNBT("Beehive", h) }
func (h BeeNest) EncodeNBT() map[string]any { return encodeBeehiveNBT("Beehive", h.Beehive) }
func encodeBeehiveNBT(id string, h Beehive) map[string]any {
	// The honey level is the honey_level block state, as in Bedrock. Bees are
	// spawned when the hive is placed or generated, so BDS loading the world
	// must not spawn more.
	n := map[string]any{"id": id, "BlockEntityVersion": int32(0), "ShouldSpawnBees": uint8(0)}
	if len(h.Occupants) != 0 {
		n["Occupants"] = h.Occupants
	}
	if h.FlowerPos != nil {
		n["FlowerPos"] = h.FlowerPos
	}
	return n
}
func (h Beehive) DecodeNBT(n map[string]any) any { return decodeBeehiveNBT(h, n) }
func (h BeeNest) DecodeNBT(n map[string]any) any {
	h.Beehive = decodeBeehiveNBT(h.Beehive, n)
	return h
}
func decodeBeehiveNBT(h Beehive, n map[string]any) Beehive {
	// Earlier versions saved the honey level here too; BDS only keeps the
	// block state, which the hive already holds.
	if level, ok := nbtconv.Integer(n, "HoneyLevel"); ok {
		h.HoneyLevel = int(level)
	}
	if h.HoneyLevel < 0 {
		h.HoneyLevel = 0
	}
	if h.HoneyLevel > 5 {
		h.HoneyLevel = 5
	}
	h.Occupants = nbtconv.Slice(n, "Occupants")
	if p, ok := n["FlowerPos"].(map[string]any); ok {
		h.FlowerPos = p
	}
	return h
}
func allBeehives() []world.Block {
	blocks := make([]world.Block, 0, 48)
	for _, face := range []cube.Direction{cube.North, cube.East, cube.South, cube.West} {
		for honey := range 6 {
			blocks = append(blocks, Beehive{Facing: face, HoneyLevel: honey}, BeeNest{Beehive: Beehive{Facing: face, HoneyLevel: honey}})
		}
	}
	return blocks
}

// BreakInfo ports the loot of a beehive: it keeps its bees and honey only
// when mined with silk touch.
func (h Beehive) BreakInfo() BreakInfo {
	return newBreakInfo(0.6, alwaysHarvestable, axeEffective, func(_ item.Tool, enchantments []item.Enchantment) []item.Stack {
		if hasSilkTouch(enchantments) {
			return []item.Stack{item.NewStack(h.withContents(), 1)}
		}
		return []item.Stack{item.NewStack(Beehive{}, 1)}
	}).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) { breakBeehive(pos, h.Occupants, tx, u) })
}

// withContents returns the hive as an item keeping its bees and honey.
func (h Beehive) withContents() Beehive {
	return Beehive{HoneyLevel: h.HoneyLevel, Occupants: h.Occupants}
}

// CreativeDrops ports BeehiveBlock.playerWillDestroy: a hive with bees or
// honey a creative player breaks drops with them.
func (h Beehive) CreativeDrops() []item.Stack {
	if len(h.Occupants) == 0 && h.HoneyLevel == 0 {
		return nil
	}
	return []item.Stack{item.NewStack(h.withContents(), 1)}
}

// breakBeehive ports BeehiveBlock.playerDestroy: the bees of a hive broken
// without silk touch burst out at the player, and the bees around attack. A
// creative player keeps them in the hive it drops.
func breakBeehive(pos cube.Pos, occupants []any, tx *world.Tx, u item.User) {
	if u != nil {
		if mode, ok := u.(interface{ GameMode() world.GameMode }); ok && mode.GameMode().CreativeInventory() {
			return
		}
		if held, _ := u.HeldItems(); hasSilkTouch(held.Enchantments()) {
			return
		}
	}
	releaseHiveOccupantsAt(pos, occupants, tx, u, true)
	angerNearbyBees(pos, tx)
}

// BreakInfo ports the loot of a bee nest: it drops only to silk touch,
// keeping its bees and honey.
func (h BeeNest) BreakInfo() BreakInfo {
	return newBreakInfo(0.6, alwaysHarvestable, axeEffective, func(_ item.Tool, enchantments []item.Enchantment) []item.Stack {
		if hasSilkTouch(enchantments) {
			return []item.Stack{item.NewStack(BeeNest{h.withContents()}, 1)}
		}
		return nil
	}).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		breakBeehive(pos, h.Occupants, tx, u)
	})
}

// CreativeDrops ...
func (h BeeNest) CreativeDrops() []item.Stack {
	if len(h.Occupants) == 0 && h.HoneyLevel == 0 {
		return nil
	}
	return []item.Stack{item.NewStack(BeeNest{h.withContents()}, 1)}
}
func (h Beehive) AddBeeOccupant(n map[string]any) (world.Block, bool) {
	if len(h.Occupants) >= maxBeehiveOccupants {
		return h, false
	}
	duration := int32(600)
	if hiveBeeHasNectar(n) {
		duration = 2400
	}
	h.Occupants = append(h.Occupants, map[string]any{"ActorIdentifier": "minecraft:bee", "SaveData": n, "TicksLeftToStay": duration})
	if state := hiveBeeJavaState(n); state != nil {
		if flower, ok := state["flower_pos"].(map[string]any); ok {
			h.FlowerPos = flower
		}
	}
	return h, true
}
func (h Beehive) IsFull() bool { return len(h.Occupants) >= maxBeehiveOccupants }
func (h BeeNest) AddBeeOccupant(n map[string]any) (world.Block, bool) {
	hive, ok := h.Beehive.AddBeeOccupant(n)
	if !ok {
		return h, false
	}
	return BeeNest{Beehive: hive.(Beehive)}, true
}
func (h BeeNest) IsFull() bool { return h.Beehive.IsFull() }
func (h Beehive) ShearDrops() item.Stack {
	if h.HoneyLevel != 5 {
		return item.Stack{}
	}
	return item.NewStack(item.Honeycomb{}, 3)
}
func (h BeeNest) ShearDrops() item.Stack { return h.Beehive.ShearDrops() }

// HoneyHarvested ports what BeehiveBlock.useItemOn does once shears or a
// glass bottle took the honey of a hive: unless smoke from a campfire below
// calms them, the bees around attack and those inside burst out at the
// player.
func (h Beehive) HoneyHarvested(pos cube.Pos, tx *world.Tx, user item.User) {
	if campfireSmokes(pos, tx) {
		return
	}
	current := tx.Block(pos)
	occupants := hiveOccupants(current)
	if len(occupants) > 0 {
		angerNearbyBees(pos, tx)
	}
	releaseHiveOccupantsAt(pos, occupants, tx, user, true)
	switch c := current.(type) {
	case Beehive:
		c.Occupants = nil
		tx.SetBlock(pos, c, nil)
	case BeeNest:
		c.Occupants = nil
		tx.SetBlock(pos, c, nil)
	}
}

// HoneyHarvested ...
func (h BeeNest) HoneyHarvested(pos cube.Pos, tx *world.Tx, user item.User) {
	h.Beehive.HoneyHarvested(pos, tx, user)
}

// hiveOccupants returns the bees inside a hive block.
func hiveOccupants(b world.Block) []any {
	switch h := b.(type) {
	case Beehive:
		return h.Occupants
	case BeeNest:
		return h.Occupants
	}
	return nil
}

// BeesStayInHive ports the bees_stay_in_hive environment attribute: bees stay
// in their hives at night in the Overworld and while it rains.
func BeesStayInHive(tx *world.Tx) bool {
	if tx.Raining() {
		return true
	}
	t := tx.World().Time() % 24000
	return tx.World().Dimension() == world.Overworld && t >= 12542 && t < 23460
}

// hiveBee is a bee a hive releases or angers.
type hiveBee interface {
	world.Entity
	Target() world.Entity
	SetTarget(target world.Entity)
	SetStayOutOfHive(ticks int)
}

// angerNearbyBees ports BeehiveBlock.angerNearbyBees: every bee near the
// hive without a target attacks a random player near it.
func angerNearbyBees(pos cube.Pos, tx *world.Tx) {
	box := cube.Box(float64(pos[0])-8, float64(pos[1])-6, float64(pos[2])-8, float64(pos[0])+9, float64(pos[1])+7, float64(pos[2])+9)
	var bees []hiveBee
	var players []world.Entity
	for e := range tx.EntitiesWithin(box) {
		switch e.H().Type().EncodeEntity() {
		case "minecraft:bee":
			if b, ok := e.(hiveBee); ok {
				bees = append(bees, b)
			}
		case "minecraft:player":
			players = append(players, e)
		}
	}
	if len(bees) == 0 || len(players) == 0 {
		return
	}
	for _, b := range bees {
		if b.Target() == nil {
			b.SetTarget(players[tx.RegionRandom().NextInt(int32(len(players)))])
		}
	}
}

// campfireSmokes ports CampfireBlock.isSmokeyPos: a lit campfire up to five
// blocks below, with nothing in the way of its smoke.
func campfireSmokes(pos cube.Pos, tx *world.Tx) bool {
	lit := func(p cube.Pos) bool {
		c, ok := tx.Block(p).(Campfire)
		return ok && !c.Extinguished
	}
	post := cube.Box(6.0/16, 0, 6.0/16, 10.0/16, 1, 10.0/16)
	for i := 1; i <= 5; i++ {
		check := pos.Sub(cube.Pos{0, i, 0})
		if lit(check) {
			return true
		}
		// Java tests the shape of the block below in the context of the
		// hive itself.
		for _, box := range tx.Block(check).Model().BBox(pos, tx) {
			if box.IntersectsWith(post) {
				return lit(check.Side(cube.FaceDown))
			}
		}
	}
	return false
}

func (h Beehive) FillBottle() (world.Block, item.Stack, bool) {
	if h.HoneyLevel < 5 {
		return h, item.Stack{}, false
	}
	// BeehiveBlock.resetHoneyLevel.
	h.HoneyLevel = 0
	return h, item.NewStack(item.HoneyBottle{}, 1), true
}
func (h BeeNest) FillBottle() (world.Block, item.Stack, bool) {
	b, s, ok := h.Beehive.FillBottle()
	if !ok {
		return h, s, false
	}
	return BeeNest{Beehive: b.(Beehive)}, s, true
}
func (h Beehive) Carve(cube.Face) (world.Block, bool) {
	if h.HoneyLevel < 5 {
		return h, false
	}
	h.HoneyLevel = 0
	return h, true
}
func (h BeeNest) Carve(f cube.Face) (world.Block, bool) {
	b, ok := h.Beehive.Carve(f)
	if !ok {
		return h, false
	}
	return BeeNest{Beehive: b.(Beehive)}, true
}
func (h Beehive) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	tickBeehive(pos, h, tx, r)
}
func (h BeeNest) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	tickBeehive(pos, h.Beehive, tx, r)
}
func (h Beehive) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if len(h.Occupants) != 0 {
		tx.ScheduleBlockUpdate(pos, h, time.Second/20)
	}
}
func (h BeeNest) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if len(h.Occupants) != 0 {
		tx.ScheduleBlockUpdate(pos, h, time.Second/20)
	}
}
func tickBeehive(pos cube.Pos, h Beehive, tx *world.Tx, r *rand.Rand) {
	current := tx.Block(pos)
	if h.hasNearbyFire(pos, tx) {
		releaseHiveOccupants(pos, h.Occupants, tx, true)
		h.Occupants = nil
		tx.SetBlock(pos, currentWithBeehive(current, h), nil)
		return
	}
	for i := 0; i < len(h.Occupants); {
		o, ok := h.Occupants[i].(map[string]any)
		if !ok {
			i++
			continue
		}
		left := int(nbtconv.Int32(o, "TicksLeftToStay")) - 1
		ready := left <= 0
		java, source := o["adamant:java"].(map[string]any)
		if source {
			ticks, minimum := nbtconv.Int32(java, "ticks_in_hive"), nbtconv.Int32(java, "min_ticks_in_hive")
			ready = ticks > minimum
			ticks++
			java["ticks_in_hive"] = ticks
			left = int(max(int64(0), min(int64(2147483647), int64(minimum)-int64(ticks)+2)))
		}
		if ready {
			hadNectar := hiveBeeHasNectarFromOccupant(o)
			if releaseHiveOccupant(pos, o, tx, false, false) != nil {
				if hadNectar && h.HoneyLevel < 5 {
					increase := 1
					if r.IntN(100) == 0 {
						increase = 2
					}
					if h.HoneyLevel+increase > 5 {
						increase--
					}
					h.HoneyLevel += increase
				}
				h.Occupants = append(h.Occupants[:i], h.Occupants[i+1:]...)
				continue
			}
			if !source {
				left = 20
			}
		}
		o["TicksLeftToStay"] = int32(left)
		i++
	}
	tx.SetBlock(pos, currentWithBeehive(current, h), nil)
	if len(h.Occupants) > 0 {
		tx.ScheduleBlockUpdate(pos, currentWithBeehive(current, h), time.Second/20)
	}
}
func currentWithBeehive(current world.Block, h Beehive) world.Block {
	if _, ok := current.(BeeNest); ok {
		return BeeNest{Beehive: h}
	}
	return h
}

// JavaEmergencyRelease releases occupants using the emergency release rules used
// when fire changes a Java beehive's neighbouring shape.
func (h Beehive) JavaEmergencyRelease(pos cube.Pos, tx *world.Tx) {
	remaining := h.Occupants[:0]
	for _, raw := range h.Occupants {
		occupant, ok := raw.(map[string]any)
		if !ok || releaseHiveOccupant(pos, occupant, tx, true, true) == nil {
			remaining = append(remaining, raw)
		}
	}
	h.Occupants = remaining
	tx.SetBlock(pos, currentWithBeehive(tx.Block(pos), h), nil)
}

// JavaEmergencyRelease releases occupants using the emergency release rules used
// when fire changes a Java beehive's neighbouring shape.
func (h BeeNest) JavaEmergencyRelease(pos cube.Pos, tx *world.Tx) {
	h.Beehive.JavaEmergencyRelease(pos, tx)
}

func (h Beehive) hasNearbyFire(pos cube.Pos, tx *world.Tx) bool {
	for x := -1; x <= 1; x++ {
		for y := -1; y <= 1; y++ {
			for z := -1; z <= 1; z++ {
				name, _ := tx.Block(pos.Add(cube.Pos{x, y, z})).EncodeBlock()
				if name == "minecraft:fire" || name == "minecraft:soul_fire" {
					return true
				}
			}
		}
	}
	return false
}
func hiveBeeJavaState(data map[string]any) map[string]any {
	state, _ := data["adamant:java"].(map[string]any)
	return state
}
func hiveFlowerPos(block world.Block) map[string]any {
	switch h := block.(type) {
	case Beehive:
		return h.FlowerPos
	case BeeNest:
		return h.FlowerPos
	}
	return nil
}
func hiveBeeHasNectar(data map[string]any) bool {
	return nbtconv.Bool(hiveBeeJavaState(data), "HasNectar")
}
func hiveBeeClearNectar(data map[string]any) {
	if state := hiveBeeJavaState(data); state != nil {
		state["HasNectar"] = byte(0)
		state["CropsGrownSincePollination"] = int32(0)
	}
}
func hiveBeeHasNectarFromOccupant(occupant map[string]any) bool {
	data, _ := occupant["SaveData"].(map[string]any)
	return hiveBeeHasNectar(data)
}
func releaseHiveOccupants(pos cube.Pos, occupants []any, tx *world.Tx, emergency bool) {
	releaseHiveOccupantsAt(pos, occupants, tx, nil, emergency)
}

// releaseHiveOccupantsAt ports BeehiveBlockEntity.emptyAllLivingFromHive:
// the released bees within four blocks of the player attack it, or stay out
// of the hive for 20 seconds while smoke calms them.
func releaseHiveOccupantsAt(pos cube.Pos, occupants []any, tx *world.Tx, player world.Entity, emergency bool) {
	sedated := player != nil && campfireSmokes(pos, tx)
	for _, raw := range occupants {
		o, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		released := releaseHiveOccupant(pos, o, tx, emergency, false)
		if released == nil || player == nil {
			continue
		}
		bee, ok := released.Entity(tx)
		if !ok || bee.Position().Sub(player.Position()).LenSqr() > 16 {
			continue
		}
		if b, ok := bee.(hiveBee); ok {
			if sedated {
				b.SetStayOutOfHive(400)
			} else {
				b.SetTarget(player)
			}
		}
	}
}
func releaseHiveOccupant(pos cube.Pos, occupant map[string]any, tx *world.Tx, emergency, javaFlowerChance bool) *world.EntityHandle {
	data, _ := occupant["SaveData"].(map[string]any)
	id, _ := occupant["ActorIdentifier"].(string)
	if id == "" {
		id = "minecraft:bee"
	}
	face := cube.FaceNorth
	switch hive := tx.Block(pos).(type) {
	case Beehive:
		face = hive.Facing.Face()
	case BeeNest:
		face = hive.Facing.Face()
	}
	front := pos.Side(face)
	frontBlocked := len(tx.Block(front).Model().BBox(front, tx)) != 0
	if id, err := tx.BlockStateID(front); err == nil {
		shape, err := gamedata.BlockGenerationShapeID(id)
		if err != nil {
			panic(err)
		}
		frontBlocked = !shape.CollisionEmpty
	}
	if !emergency && (frontBlocked || BeesStayInHive(tx)) {
		return nil
	}
	if state := hiveBeeJavaState(data); state != nil {
		// BeehiveBlockEntity.Occupant.createEntity: the bee forgets its
		// counters, hovers and knows this hive as its home.
		for _, key := range [...]string{"CannotEnterHiveTicks", "TicksSincePollination", "CropsGrownSincePollination"} {
			delete(state, key)
		}
		state["hive_pos"] = map[string]any{"X": int32(pos[0]), "Y": int32(pos[1]), "Z": int32(pos[2])}
		state["NoGravity"] = byte(1)
		if flower := hiveFlowerPos(tx.Block(pos)); flower != nil {
			if _, exists := state["flower_pos"]; !exists {
				if !javaFlowerChance || tx.RegionRandom().NextFloat() < 0.9 {
					state["flower_pos"] = flower
				}
			}
		}
	}
	if !emergency {
		hiveBeeClearNectar(data)
	}
	opts := world.EntitySpawnOpts{Position: pos.Vec3Centre(), SpawnReason: "bee_hive"}
	handle := tx.World().EntityRegistry().Config().SpawnMobFromNBT(id, opts, data)
	if handle == nil {
		return nil
	}
	spawned := handle.WorldlessEntity(tx)
	if id == "minecraft:bee" {
		box := handle.Type().BBox(spawned)
		delta := 0.0
		if !frontBlocked {
			delta = .55 + float64(float32(box.Width())/2)
		}
		spawnPos := pos.Vec3Centre().Add(front.Sub(pos).Vec3().Mul(delta))
		spawnPos[1] -= float64(float32(box.Height()) / 2)
		handle.SetPosition(spawnPos)
	}
	tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: "block.beehive.exit", ExtraData: -1})
	tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{Source: spawned, AffectedState: tx.Block(pos)})
	if tx.AddEntity(handle) == nil {
		return nil
	}
	return handle
}

var _ world.Block = Beehive{}
var _ world.Block = BeeNest{}
var _ interface{ EncodeNBT() map[string]any } = Beehive{}
var _ interface {
	FillBottle() (world.Block, item.Stack, bool)
} = Beehive{}
var _ interface {
	Carve(cube.Face) (world.Block, bool)
} = Beehive{}
var _ interface {
	JavaEmergencyRelease(cube.Pos, *world.Tx)
} = Beehive{}
var _ interface {
	JavaEmergencyRelease(cube.Pos, *world.Tx)
} = BeeNest{}

// ComparatorOutput ports BeehiveBlock.getAnalogOutputSignal: the honey level
// of the hive.
func (h Beehive) ComparatorOutput(*world.Tx, cube.Pos) uint8 { return uint8(h.HoneyLevel) }
