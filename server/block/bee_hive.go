package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/javadata"
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
	n := map[string]any{"id": id, "BlockEntityVersion": int32(0), "HoneyLevel": int32(h.HoneyLevel)}
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
	h.HoneyLevel = int(nbtconv.Int32(n, "HoneyLevel"))
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
func (h Beehive) BreakInfo() BreakInfo {
	return newBreakInfo(0.6, alwaysHarvestable, axeEffective, oneOf(h)).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) { releaseHiveOccupants(pos, h.Occupants, tx, true) })
}
func (h BeeNest) BreakInfo() BreakInfo {
	return newBreakInfo(0.6, alwaysHarvestable, axeEffective, oneOf(h)).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		releaseHiveOccupants(pos, h.Occupants, tx, true)
	})
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
func (h Beehive) ShearDropsAt(pos cube.Pos, tx *world.Tx, _ item.User) item.Stack {
	drops := h.ShearDrops()
	if !drops.Empty() && !h.hasNearbySmoke(pos, tx) {
		h = h.angerOccupants(pos, tx)
	}
	return drops
}
func (h BeeNest) ShearDropsAt(pos cube.Pos, tx *world.Tx, user item.User) item.Stack {
	return h.Beehive.ShearDropsAt(pos, tx, user)
}
func (h Beehive) FillBottleAt(pos cube.Pos, tx *world.Tx, _ item.User) (world.Block, item.Stack, bool) {
	next, stack, ok := h.FillBottle()
	if !ok {
		return h, stack, false
	}
	if !h.hasNearbySmoke(pos, tx) {
		h = h.angerOccupants(pos, tx)
	}
	return currentWithBeehive(tx.Block(pos), mergeBeehiveHoney(next, h)), stack, true
}
func (h BeeNest) FillBottleAt(pos cube.Pos, tx *world.Tx, user item.User) (world.Block, item.Stack, bool) {
	next, stack, ok := h.Beehive.FillBottleAt(pos, tx, user)
	if !ok {
		return h, stack, false
	}
	return BeeNest{Beehive: next.(Beehive)}, stack, true
}
func mergeBeehiveHoney(next, other world.Block) Beehive {
	h := next.(Beehive)
	switch current := other.(type) {
	case Beehive:
		h.Occupants = current.Occupants
		h.FlowerPos = current.FlowerPos
	case BeeNest:
		h.Occupants = current.Occupants
		h.FlowerPos = current.FlowerPos
	}
	return h
}
func (h Beehive) angerOccupants(pos cube.Pos, tx *world.Tx) Beehive {
	for _, raw := range h.Occupants {
		occupant, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		data, _ := occupant["SaveData"].(map[string]any)
		if state := hiveBeeJavaState(data); state != nil {
			state["AngerTime"] = int32(400 + rand.IntN(380))
		}
	}
	if len(h.Occupants) > 0 {
		tx.ScheduleBlockUpdate(pos, currentWithBeehive(tx.Block(pos), h), time.Second/20)
	}
	return h
}
func (h Beehive) FillBottle() (world.Block, item.Stack, bool) {
	if h.HoneyLevel < 5 {
		return h, item.Stack{}, false
	}
	h.HoneyLevel -= 1
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
			if releaseHiveOccupant(pos, o, tx, false, false) {
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
		if !ok || !releaseHiveOccupant(pos, occupant, tx, true, true) {
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
func (h Beehive) hasNearbySmoke(pos cube.Pos, tx *world.Tx) bool {
	for x := -2; x <= 2; x++ {
		for y := -2; y <= 2; y++ {
			for z := -2; z <= 2; z++ {
				fire, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(Campfire)
				if ok && !fire.Extinguished {
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
	for _, raw := range occupants {
		o, ok := raw.(map[string]any)
		if ok {
			releaseHiveOccupant(pos, o, tx, emergency, false)
		}
	}
}
func releaseHiveOccupant(pos cube.Pos, occupant map[string]any, tx *world.Tx, emergency, javaFlowerChance bool) bool {
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
	if id, err := tx.JavaBlockStateID(front); err == nil {
		shape, err := javadata.BlockGenerationShapeID(id)
		if err != nil {
			panic(err)
		}
		frontBlocked = !shape.CollisionEmpty
	}
	if !emergency && frontBlocked {
		return false
	}
	if state := hiveBeeJavaState(data); state != nil {
		if flower := hiveFlowerPos(tx.Block(pos)); flower != nil {
			if _, exists := state["flower_pos"]; !exists {
				if !javaFlowerChance || tx.JavaRandom().NextFloat() < 0.9 {
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
		return false
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
	return tx.AddEntity(handle) != nil
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
