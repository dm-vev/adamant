package block

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// CreakingHeartState is the source heart's state, also present in Bedrock's palette.
type CreakingHeartState uint8

const (
	CreakingHeartUprooted CreakingHeartState = iota
	CreakingHeartDormant
	CreakingHeartAwake
)

func (s CreakingHeartState) String() string {
	switch s {
	case CreakingHeartDormant:
		return "dormant"
	case CreakingHeartAwake:
		return "awake"
	default:
		return "uprooted"
	}
}

// CreakingHeart owns one protector; block states share actor data only while
// changing state at the same position. A decoded actor always has fresh data.
type CreakingHeart struct {
	solid
	Axis    cube.Axis
	State   CreakingHeartState
	Natural bool
	data    *creakingHeartData
}

type creakingHeartData struct {
	protector     *world.EntityHandle
	protectorUUID uuid.UUID
	protectorID   int64
	hasProtector  bool
	ticksExisted  int64
	ticker        int32
	emitter       int
	emitterTarget mgl64.Vec3
	output        int
}

type creakingProtector interface {
	world.Entity
	PlayerIsStuck() bool
	PersistenceRequired() bool
	RemoveProtector(world.DamageSource)
	RemoveProtectorByExplosion(world.ExplosionSource)
}

func NewCreakingHeart() CreakingHeart { return CreakingHeart{data: &creakingHeartData{}} }
func (h CreakingHeart) ensureData() CreakingHeart {
	if h.data == nil {
		h.data = &creakingHeartData{}
	}
	return h
}
func (CreakingHeart) EncodeItem() (string, int16) { return "minecraft:creaking_heart", 0 }
func (h CreakingHeart) EncodeBlock() (string, map[string]any) {
	return "minecraft:creaking_heart", map[string]any{"pillar_axis": h.Axis.String(), "creaking_heart_state": h.State.String(), "natural": boolByte(h.Natural)}
}
func (h CreakingHeart) EncodeNBT() map[string]any {
	data := map[string]any{"id": "CreakingHeart", "BlockEntityVersion": int32(0), "Cooldown": int32(0)}
	if h.data != nil {
		data["Cooldown"] = h.data.ticker
		if h.data.hasProtector {
			data["SpawnedCreakingID"] = h.data.protectorID
			if h.data.protectorUUID != uuid.Nil {
				id := h.data.protectorUUID
				data["adamant:java"] = map[string]any{"creaking": []int32{int32(binary.BigEndian.Uint32(id[0:4])), int32(binary.BigEndian.Uint32(id[4:8])), int32(binary.BigEndian.Uint32(id[8:12])), int32(binary.BigEndian.Uint32(id[12:16]))}}
			}
		}
	}
	return data
}
func (h CreakingHeart) DecodeNBT(data map[string]any) any {
	h.data = &creakingHeartData{}
	// Java persists the link, not its update/emitter counters. Cooldown is
	// emitted for native compatibility but source reload resets transient work.
	if id, ok := data["SpawnedCreakingID"].(int64); ok {
		h.data.protectorID, h.data.hasProtector = id, true
	}
	if source, ok := data["adamant:java"].(map[string]any); ok {
		if words, ok := source["creaking"].([]int32); ok && len(words) == 4 {
			for i, word := range words {
				binary.BigEndian.PutUint32(h.data.protectorUUID[i*4:], uint32(word))
			}
			h.data.protectorID = int64(binary.LittleEndian.Uint64(h.data.protectorUUID[8:]))
			h.data.hasProtector = true
		}
	}
	return h
}
func (h CreakingHeart) Placed(pos cube.Pos, tx *world.Tx, previous world.Block) {
	if h.data != nil {
		return
	}
	if old, ok := previous.(CreakingHeart); ok && old.data != nil {
		h.data = old.data
	} else {
		h = h.ensureData()
	}
	tx.SetBlockEntity(pos, h)
}
func (h CreakingHeart) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, h)
	if !used {
		return false
	}
	h = NewCreakingHeart()
	h.Axis = face.Axis()
	h = h.activate(pos, tx)
	place(tx, pos, h, user, ctx)
	return placed(ctx)
}
func (h CreakingHeart) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	tx.ScheduleBlockUpdate(pos, h, time.Second/20)
}
func (h CreakingHeart) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	updated := h.activate(pos, tx)
	if updated.State != h.State {
		tx.SetBlock(pos, updated, nil)
	}
}
func (h CreakingHeart) activate(pos cube.Pos, tx *world.Tx) CreakingHeart {
	if h.State == CreakingHeartUprooted && h.hasRequiredLogs(pos, tx) {
		h.State = CreakingHeartDormant
		if tx.World().CreakingActive() {
			h.State = CreakingHeartAwake
		}
	}
	return h
}
func paleOakLogAxis(b world.Block) (cube.Axis, bool) {
	switch b := b.(type) {
	case Log:
		return b.Axis, b.Wood == PaleOakWood()
	case Wood:
		return b.Axis, b.Wood == PaleOakWood()
	default:
		return 0, false
	}
}
func (h CreakingHeart) hasRequiredLogs(pos cube.Pos, tx *world.Tx) bool {
	var positive cube.Face
	switch h.Axis {
	case cube.X:
		positive = cube.FaceEast
	case cube.Z:
		positive = cube.FaceSouth
	default:
		positive = cube.FaceUp
	}
	for _, face := range [2]cube.Face{positive, positive.Opposite()} {
		if axis, ok := paleOakLogAxis(tx.Block(pos.Side(face))); !ok || axis != h.Axis {
			return false
		}
	}
	return true
}
func (h CreakingHeart) protector(tx *world.Tx) (creakingProtector, bool) {
	if h.data == nil || !h.data.hasProtector {
		return nil, false
	}
	d := h.data
	var entity world.Entity
	var found bool
	if d.protectorUUID != uuid.Nil {
		entity, found = tx.EntityByUUID(d.protectorUUID)
	} else {
		entity, found = tx.EntityByUniqueID(d.protectorID)
	}
	if found {
		if protector, ok := entity.(creakingProtector); ok {
			d.protector, d.protectorUUID, d.protectorID = entity.H(), entity.H().UUID(), entity.H().UniqueID()
			return protector, true
		}
	}
	if d.protector != nil {
		d.protector = nil
		d.ticksExisted = 0
	}
	if d.ticksExisted >= 30 {
		h.clearProtector()
	}
	return nil, false
}
func (h CreakingHeart) clearProtector() {
	h.data.protector = nil
	h.data.protectorUUID = uuid.Nil
	h.data.protectorID, h.data.hasProtector = 0, false
}
func (h CreakingHeart) IsCreakingProtector(pos cube.Pos, tx *world.Tx, handle *world.EntityHandle) bool {
	actual, ok := tx.Block(pos).(CreakingHeart)
	if !ok {
		return false
	}
	protector, found := actual.protector(tx)
	return found && protector.H() == handle
}
func (h CreakingHeart) ComparatorOutput(*world.Tx, cube.Pos) uint8 {
	if h.State == CreakingHeartUprooted || h.data == nil {
		return 0
	}
	return uint8(h.data.output)
}
func (h CreakingHeart) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if h.State == CreakingHeartUprooted {
		return
	}
	h = h.ensureData()
	d := h.data
	d.ticksExisted++
	protector, found := h.protector(tx)
	output := 0
	if found {
		output = 15 - int(math.Floor(min(32, protector.Position().Sub(pos.Vec3().Add(mgl64.Vec3{.5, 0, .5})).Len())/32*15))
	}
	if d.output != output {
		d.output = output
		tx.Redstone().ScheduleUpdate(pos)
	}
	if d.emitter > 0 {
		if d.emitter > 50 {
			h.emitParticles(pos, tx, 1, true)
			h.emitParticles(pos, tx, 1, false)
		}
		if d.emitter%10 == 0 {
			if found {
				d.emitterTarget = heartProtectorCentre(protector)
			}
			progress := float32(.2) + float32(.8)*float32(100-d.emitter)/100
			location := pos.Vec3Centre().Sub(d.emitterTarget).Mul(float64(progress)).Add(d.emitterTarget)
			tx.PlaySound(location, sound.NativeEvent{SoundType: "block.creaking_heart.trail", ExtraData: int32((progress*2 - 1) * 1000)})
		}
		d.emitter--
	}
	previousTicker := d.ticker
	d.ticker--
	if previousTicker >= 0 {
		tx.SetBlockEntity(pos, h)
		return
	}
	d.ticker = 20 + tx.JavaRandom().NextInt(5)
	updated := CreakingHeartDormant
	if !h.hasRequiredLogs(pos, tx) && !d.hasProtector {
		updated = CreakingHeartUprooted
	} else if tx.World().CreakingActive() {
		updated = CreakingHeartAwake
	}
	if h.State != updated {
		h.State = updated
		tx.SetBlock(pos, h, nil)
	} else {
		tx.SetBlockEntity(pos, h)
	}
	if h.State == CreakingHeartUprooted {
		return
	}
	if !d.hasProtector {
		if h.State == CreakingHeartAwake && tx.World().MonsterSpawningEnabled() && tx.World().Difficulty() != world.DifficultyPeaceful && heartPlayerNearby(pos, tx) {
			if protector := h.spawnProtector(pos, tx); protector != nil {
				d.protector, d.protectorUUID, d.protectorID, d.hasProtector = protector.H(), protector.H().UUID(), protector.H().UniqueID(), true
				tx.PlaySound(protector.Position(), sound.NativeEvent{SoundType: "creaking_heart_spawn", EntityType: "minecraft:creaking", EntityUniqueID: protector.H().UniqueID(), ExtraData: -1})
				tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: "creaking_heart_spawn", ExtraData: -1})
				tx.SetBlock(pos, h, nil)
			}
		}
	} else if protector, found = h.protector(tx); found {
		distance := protector.Position().Sub(pos.Vec3().Add(mgl64.Vec3{.5, 0, .5})).Len()
		if !tx.World().CreakingActive() && !protector.PersistenceRequired() || distance > 34 || protector.PlayerIsStuck() {
			h.removeProtector(tx, nil)
			tx.SetBlock(pos, h, nil)
		}
	}
}
func heartPlayerNearby(pos cube.Pos, tx *world.Tx) bool {
	origin := pos.Vec3()
	for entity := range tx.EntitiesWithin(cube.Box(-32, -32, -32, 32, 32, 32).Translate(origin)) {
		if entity.H().Type().EncodeEntity() != "minecraft:player" || entity.Position().Sub(origin).LenSqr() >= 1024 {
			continue
		}
		if mode, ok := entity.(interface{ GameMode() world.GameMode }); ok && mode.GameMode() == world.GameModeSpectator {
			continue
		}
		if living, ok := entity.(interface{ Dead() bool }); ok && living.Dead() {
			continue
		}
		return true
	}
	return false
}
func (h CreakingHeart) spawnProtector(pos cube.Pos, tx *world.Tx) creakingProtector {
	registry := tx.World().EntityRegistry()
	typeOf, ok := registry.Lookup("minecraft:creaking")
	if !ok || registry.Config().SpawnMobFromNBT == nil {
		panic("creaking heart requires registered minecraft:creaking factory")
	}
	random := tx.JavaRandom()
	for range 5 {
		candidate := pos.Add(cube.Pos{int(random.NextInt(33)) - 16, 8, int(random.NextInt(33)) - 16})
		above := tx.Block(candidate)
		for y := 8; y >= -8; y-- {
			candidate = candidate.Side(cube.FaceDown)
			below := tx.Block(candidate)
			_, leaves := below.(Leaves)
			if !leaves && len(above.Model().BBox(candidate.Side(cube.FaceUp), tx)) == 0 && below.Model().FaceSolid(candidate, cube.FaceUp, tx) {
				candidate = candidate.Side(cube.FaceUp)
				position := candidate.Vec3().Add(mgl64.Vec3{.5, 0, .5})
				box := typeOf.BBox(nil).Translate(position)
				if !heartSpawnSpace(box, tx) {
					break
				}
				handle := registry.Config().SpawnMobFromNBT("minecraft:creaking", world.EntitySpawnOpts{Position: position, SpawnReason: "spawner"}, map[string]any{"id": "minecraft:creaking", "home_pos": []int32{int32(pos.X()), int32(pos.Y()), int32(pos.Z())}})
				if handle == nil {
					panic("registered creaking factory rejected source spawn data")
				}
				protector, ok := tx.AddEntity(handle).(creakingProtector)
				if !ok {
					panic("registered creaking does not implement protector lifecycle")
				}
				tx.EmitGameEvent(world.GameEventEntityPlace, protector.Position(), world.GameEventContext{Source: protector})
				return protector
			}
			above = below
		}
	}
	return nil
}
func heartSpawnSpace(box cube.BBox, tx *world.Tx) bool {
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	if low.OutOfBounds(tx.Range()) || high.OutOfBounds(tx.Range()) {
		return false
	}
	for x := low.X(); x <= high.X(); x++ {
		for y := low.Y(); y <= high.Y(); y++ {
			for z := low.Z(); z <= high.Z(); z++ {
				pos := cube.Pos{x, y, z}
				if _, liquid := tx.Liquid(pos); liquid {
					return false
				}
				for _, collision := range tx.Block(pos).Model().BBox(pos, tx) {
					if box.IntersectsWith(collision.Translate(pos.Vec3())) {
						return false
					}
				}
			}
		}
	}
	for entity := range tx.EntitiesWithin(box) {
		if mode, ok := entity.(interface{ GameMode() world.GameMode }); ok && !mode.GameMode().HasCollision() {
			continue
		}
		if living, ok := entity.(interface{ Dead() bool }); ok && !living.Dead() && box.IntersectsWith(entity.H().Type().BBox(entity).Translate(entity.Position())) {
			return false
		}
	}
	return true
}
func (h CreakingHeart) CreakingHurt(pos cube.Pos, tx *world.Tx) {
	actual, ok := tx.Block(pos).(CreakingHeart)
	if !ok {
		return
	}
	h = actual
	protector, found := h.protector(tx)
	if !found || h.data.emitter > 0 {
		return
	}
	h.emitParticles(pos, tx, 20, false)
	if h.State == CreakingHeartAwake {
		clumps := 2 + tx.JavaRandom().NextInt(2)
		for range clumps {
			if placed, ok := h.spreadResin(pos, tx); ok {
				tx.PlaySound(placed.Vec3Centre(), sound.BlockPlace{Block: tx.Block(placed)})
				tx.EmitGameEvent(world.GameEventBlockPlace, placed.Vec3Centre(), world.GameEventContext{AffectedState: h})
			}
		}
	}
	h.data.emitter = 100
	h.data.emitterTarget = heartProtectorCentre(protector)
	tx.SetBlockEntity(pos, h)
}
func shuffledHeartFaces(random *java26.Random) [6]cube.Face {
	faces := [6]cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast}
	for i := len(faces); i > 1; i-- {
		j := int(random.NextInt(int32(i)))
		faces[i-1], faces[j] = faces[j], faces[i-1]
	}
	return faces
}
func (h CreakingHeart) spreadResin(origin cube.Pos, tx *world.Tx) (cube.Pos, bool) {
	type visit struct {
		pos   cube.Pos
		depth int
	}
	var queue [43]visit
	var seen [64]cube.Pos
	queue[0] = visit{pos: origin}
	head, end, visited := 0, 1, 0
	random := tx.JavaRandom()
	for head < end && visited < len(seen) {
		node := queue[head]
		head++
		duplicate := false
		for _, pos := range seen[:visited] {
			if node.pos == pos {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seen[visited] = node.pos
		visited++
		if _, log := paleOakLogAxis(tx.Block(node.pos)); log {
			for _, face := range shuffledHeartFaces(random) {
				pos := node.pos.Side(face)
				b := tx.Block(pos)
				resin, exists := b.(ResinClump)
				if !exists {
					_, air := b.(Air)
					water, sourceWater := b.(Water)
					if !air && !(sourceWater && water.LiquidDepth() == 8 && !water.LiquidFalling()) {
						continue
					}
				}
				face = face.Opposite()
				updated := resin.withFace(face, true)
				if updated == resin {
					continue
				}
				tx.SetBlock(pos, updated, nil)
				return pos, true
			}
		}
		if node.depth < 2 {
			for _, face := range shuffledHeartFaces(random) {
				pos := node.pos.Side(face)
				if _, log := paleOakLogAxis(tx.Block(pos)); log {
					queue[end] = visit{pos, node.depth + 1}
					end++
				}
			}
		}
	}
	return cube.Pos{}, false
}
func (h CreakingHeart) emitParticles(pos cube.Pos, tx *world.Tx, count int, towardsCreaking bool) {
	protector, found := h.protector(tx)
	if !found {
		return
	}
	// Bedrock's native trail event samples particle positions client-side. Java
	// samples six doubles and a duration per particle on Level.random; retaining
	// those draws is essential because resin placement uses that same stream.
	random := tx.JavaRandom()
	for range count {
		for range 6 {
			random.NextDouble()
		}
		random.NextInt(40)
	}
	trail := particle.CreakingTrail{Heart: pos.Vec3(), Creaking: heartProtectorCentre(protector)}
	if towardsCreaking {
		trail.CreakingAmount = int32(count)
	} else {
		trail.HeartAmount = int32(count)
	}
	tx.AddParticle(pos.Vec3Centre(), trail)
}
func (h CreakingHeart) removeProtector(tx *world.Tx, source world.DamageSource) {
	if protector, found := h.protector(tx); found {
		protector.RemoveProtector(source)
		h.clearProtector()
	}
}
func (h CreakingHeart) Removed(_ cube.Pos, tx *world.Tx, replacement world.Block) {
	if _, sameBlock := replacement.(CreakingHeart); !sameBlock {
		h.removeProtector(tx, nil)
	}
}
func (h CreakingHeart) PlayerWillDestroy(_ cube.Pos, tx *world.Tx, source world.DamageSource) {
	h.removeProtector(tx, source)
}
func (h CreakingHeart) SourceExperience(tx *world.Tx) int {
	if !h.Natural {
		return 0
	}
	return 20 + int(tx.JavaRandom().NextInt(5))
}
func (h CreakingHeart) BreakInfo() BreakInfo {
	return newBreakInfo(10, alwaysHarvestable, axeEffective, func(_ item.Tool, enchantments []item.Enchantment) []item.Stack {
		if hasSilkTouch(enchantments) {
			return []item.Stack{item.NewStack(CreakingHeart{}, 1)}
		}
		return []item.Stack{item.NewStack(ResinClump{}, min(9, 1+rand.IntN(3)+rand.IntN(fortuneLevel(enchantments)+1)))}
	})
}
func (h CreakingHeart) SourceDrops(pos cube.Pos, tx *world.Tx, tool item.Stack, player world.Entity, explosionRadius float64) []item.Stack {
	stacks, err := loot.Generate("minecraft:blocks/creaking_heart", 0, loot.Context{ToolStack: tool, Tool: tool.Item(), Player: player, Origin: pos.Vec3Centre(), Block: "minecraft:creaking_heart", BlockState: h, Tx: tx, ExplosionRadius: explosionRadius, RandomSequence: tx.LootRandomSequence})
	if err != nil {
		panic(err)
	}
	return stacks
}
func allCreakingHearts() []world.Block {
	blocks := make([]world.Block, 0, 18)
	for axis := cube.Axis(0); axis < 3; axis++ {
		for state := CreakingHeartUprooted; state <= CreakingHeartAwake; state++ {
			for _, natural := range [2]bool{false, true} {
				blocks = append(blocks, CreakingHeart{Axis: axis, State: state, Natural: natural})
			}
		}
	}
	return blocks
}

func heartProtectorCentre(protector world.Entity) mgl64.Vec3 {
	box := protector.H().Type().BBox(protector)
	return box.Min().Add(box.Max()).Mul(.5).Add(protector.Position())
}

func (h CreakingHeart) OnExplosionHit(src world.ExplosionSource, pos cube.Pos, tx *world.Tx) {
	if protector, found := h.protector(tx); found {
		protector.RemoveProtectorByExplosion(src)
		h.clearProtector()
	}
	if !h.Natural {
		return
	}
	responsible := world.ExplosionCausingEntity(src, tx)
	player, ok := responsible.(interface{ GameMode() world.GameMode })
	if !ok || player.GameMode().CreativeInventory() || !player.GameMode().AllowsInteraction() {
		return
	}
	for _, orb := range tx.World().EntityRegistry().Config().ExperienceOrbs(pos.Vec3Centre(), h.SourceExperience(tx)) {
		tx.AddEntity(orb)
	}
}
