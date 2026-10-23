package block

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	vibrationparticle "github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// SculkShrieker owns Java's vibration listener and its block-entity warning
// state. Warning cooldowns belong to each responsible player's tracker.
type SculkShrieker struct {
	solid
	WarningLevel            int
	CanSummon               bool
	Shrieking               bool
	listener                *world.VibrationData
	javaState               map[string]any
	nativeVibrationListener map[string]any
}

func (s SculkShrieker) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	s.CanSummon, s.Shrieking, s.WarningLevel = false, false, 0
	s.listener = &world.VibrationData{}
	place(tx, pos, s, user, ctx)
	return placed(ctx)
}
func (s SculkShrieker) Hash() (uint64, uint64) {
	return hashSculkShrieker, uint64(boolByte(s.CanSummon)) | uint64(boolByte(s.Shrieking))<<1
}
func (SculkShrieker) EncodeItem() (string, int16) { return "minecraft:sculk_shrieker", 0 }
func (s SculkShrieker) EncodeBlock() (string, map[string]any) {
	return "minecraft:sculk_shrieker", map[string]any{"active": boolByte(s.Shrieking), "can_summon": boolByte(s.CanSummon)}
}
func (s SculkShrieker) BreakInfo() BreakInfo {
	return newBreakInfo(3, alwaysHarvestable, pickaxeEffective, silkTouchOnlyDrop(s))
}

func (s SculkShrieker) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if e == nil || e.H().Type().EncodeEntity() != "minecraft:player" {
		return
	}
	s.tryShriek(pos, tx, e)
}

func (s SculkShrieker) Removed(pos cube.Pos, tx *world.Tx, replacement world.Block) {
	if s.Shrieking {
		if _, remains := replacement.(SculkShrieker); !remains {
			s.respond(pos, tx)
		}
	}
}

func (s SculkShrieker) VibrationUser() world.VibrationUser  { return s }
func (s SculkShrieker) VibrationData() *world.VibrationData { return s.listener }
func (SculkShrieker) VibrationRadius() int                  { return 8 }
func (SculkShrieker) VibrationParticle(destination mgl64.Vec3, travelTicks int) world.Particle {
	return vibrationparticle.VibrationSignal{Destination: destination, TravelTicks: travelTicks}
}
func (s SculkShrieker) InitialiseVibrationListener() world.Block {
	if s.listener == nil {
		s.listener = &world.VibrationData{}
	}
	return s
}

func (SculkShrieker) ListensToGameEvent(event world.GameEvent) bool {
	return event == world.GameEventSculkSensorTendrilsClicking
}
func (SculkShrieker) RequiresAdjacentChunksToBeTicking() bool { return true }
func (s SculkShrieker) CanReceiveVibration(_ *world.Tx, _ cube.Pos, _ cube.Pos, _ world.GameEvent, context world.GameEventContext) bool {
	return !s.Shrieking && responsiblePlayer(context) != nil
}
func (s SculkShrieker) ReceiveVibration(tx *world.Tx, listenerPos, _ cube.Pos, info world.VibrationInfo, _ float32) {
	player, ok := info.ProjectileOwner(tx)
	if !ok {
		player, ok = info.SourceEntity(tx)
	}
	if ok {
		s.tryShriek(listenerPos, tx, player)
	}
}

func responsiblePlayer(context world.GameEventContext) world.Entity {
	source := context.Source
	if context.ProjectileOwner != nil {
		source = context.ProjectileOwner
	}
	if source != nil && source.H().Type().EncodeEntity() == "minecraft:player" {
		return source
	}
	return nil
}

func (s SculkShrieker) tryShriek(pos cube.Pos, tx *world.Tx, player world.Entity) {
	if player == nil || player.H().Type().EncodeEntity() != "minecraft:player" || s.Shrieking {
		return
	}
	s.WarningLevel = 0
	if s.canRespond(tx) {
		warning, warned := tx.TryWarnWarden(pos, player)
		if !warned {
			return
		}
		s.WarningLevel = warning
	}
	s.shriek(pos, tx, player)
}
func (s SculkShrieker) canRespond(tx *world.Tx) bool {
	return s.CanSummon && tx.World().Difficulty() != world.DifficultyPeaceful && tx.World().SpawnWardensEnabled()
}
func (s SculkShrieker) shriek(pos cube.Pos, tx *world.Tx, source world.Entity) {
	s.Shrieking = true
	tx.SetBlock(pos, s, nil)
	tx.ScheduleBlockUpdate(pos, s, 90*time.Second/20)
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.sculk_shrieker.shriek", Volume: 1, Pitch: 1})
	tx.EmitGameEvent(world.GameEventShriek, pos.Vec3Centre(), world.GameEventContext{Source: source})
}
func (s SculkShrieker) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !s.Shrieking {
		return
	}
	s.Shrieking = false
	tx.SetBlock(pos, s, nil)
	s.respond(pos, tx)
}
func (s SculkShrieker) respond(pos cube.Pos, tx *world.Tx) {
	if !s.canRespond(tx) || s.WarningLevel == 0 {
		return
	}
	if !s.trySummonWarden(pos, tx) {
		s.playReplySound(pos, tx)
	}
	s.applyDarkness(pos, tx)
}

// trySummonWarden mirrors SpawnUtil.trySpawnMob with ON_TOP_OF_COLLIDER and
// the source 20-attempt, 5-block horizontal, 6-block vertical search.
func (s SculkShrieker) trySummonWarden(pos cube.Pos, tx *world.Tx) bool {
	if s.WarningLevel < 4 {
		return false
	}
	spawn := tx.World().EntityRegistry().Config().SpawnMob
	if spawn == nil {
		return false
	}
	random := tx.RegionRandom()
	for range 20 {
		x, z := int(random.NextInt(11))-5, int(random.NextInt(11))-5
		candidate := pos.Add(cube.Pos{x, 6, z})
		above := tx.Block(candidate)
		found := false
		for range 13 {
			candidate = candidate.Side(cube.FaceDown)
			below := tx.Block(candidate)
			if below.Model().FaceSolid(candidate, cube.FaceUp, tx) && len(above.Model().BBox(candidate.Side(cube.FaceUp), tx)) == 0 {
				candidate = candidate.Side(cube.FaceUp)
				found = true
				break
			}
			above = below
		}
		if !found || !s.canWardenOccupy(candidate, tx) {
			continue
		}
		position := candidate.Vec3().Add(mgl64.Vec3{.5, 0, .5})
		handle := spawn("minecraft:warden", world.EntitySpawnOpts{Position: position, SpawnReason: "triggered", SpawnSeed: random.NextLong()})
		if handle == nil {
			continue
		}
		tx.AddEntity(handle)
		return true
	}
	return false
}
func (s SculkShrieker) canWardenOccupy(pos cube.Pos, tx *world.Tx) bool {
	box := cube.Box(-.45, 0, -.45, .45, 2.9, .45).Translate(pos.Vec3().Add(mgl64.Vec3{.5, 0, .5}))
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])); y <= int(math.Floor(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
				p := cube.Pos{x, y, z}
				if _, liquid := tx.Liquid(p); liquid {
					return false
				}
				for _, collision := range tx.Block(p).Model().BBox(p, tx) {
					if collision.IntersectsWith(box) {
						return false
					}
				}
			}
		}
	}
	for e := range tx.EntitiesWithin(box) {
		if mode, ok := e.(interface{ GameMode() world.GameMode }); ok && !mode.GameMode().HasCollision() {
			continue
		}
		if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			return false
		}
	}
	return true
}
func (s SculkShrieker) playReplySound(pos cube.Pos, tx *world.Tx) {
	sounds := [...]string{"", "entity.warden.nearby_close", "entity.warden.nearby_closer", "entity.warden.nearby_closest", "entity.warden.listening_angry"}
	if s.WarningLevel < 1 || s.WarningLevel > 4 {
		return
	}
	random := tx.RegionRandom()
	offset := func() int { return int(random.NextInt(21)) - 10 }
	at := pos.Vec3().Add(mgl64.Vec3{float64(offset()), float64(offset()), float64(offset())})
	tx.PlaySound(at, sound.Custom{Name: sounds[s.WarningLevel], Volume: 5, Pitch: 1})
}
func (s SculkShrieker) applyDarkness(pos cube.Pos, tx *world.Tx) {
	origin := pos.Vec3Centre()
	for e := range tx.EntitiesWithin(cube.Box(-40, -40, -40, 40, 40, 40).Translate(origin)) {
		if e.H().Type().EncodeEntity() != "minecraft:player" || e.Position().Sub(origin).Len() >= 40 {
			continue
		}
		mode, ok := e.(interface{ GameMode() world.GameMode })
		if !ok || (mode.GameMode() != world.GameModeSurvival && mode.GameMode() != world.GameModeAdventure) {
			continue
		}
		if existing, ok := e.(interface {
			Effect(effect.Type) (effect.Effect, bool)
		}); ok {
			if current, has := existing.Effect(effect.Darkness); has && (current.Level() > 1 || current.Infinite() || current.Duration() > 199*time.Second/20) {
				continue
			}
		}
		if living, ok := e.(interface{ AddEffect(effect.Effect) }); ok {
			living.AddEffect(effect.New(effect.Darkness, 1, 260*time.Second/20).WithoutParticles())
		}
	}
}

func (s SculkShrieker) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if s.listener == nil {
		s.listener = &world.VibrationData{}
		tx.SetBlockEntity(pos, s)
	}
	tx.World().TickVibrationSystem(tx, pos, s, s.listener)
}
func (s SculkShrieker) DecodeNBT(m map[string]any) any {
	javaState, ok := m["adamant:java"].(map[string]any)
	if !ok {
		javaState = world.CloneNativeEntityNBT(m)
		for _, key := range []string{"id", "x", "y", "z", "BlockEntityVersion", "VibrationListener"} {
			delete(javaState, key)
		}
	}
	s.javaState = world.CloneNativeEntityNBT(javaState)
	s.WarningLevel = max(0, min(4, int(nbtconv.Int32(s.javaState, "warning_level"))))
	if listener, ok := s.javaState["listener"].(map[string]any); ok {
		data := world.DecodeVibrationData(listener)
		s.listener = &data
	} else {
		s.listener = &world.VibrationData{}
	}
	if native, ok := m["VibrationListener"].(map[string]any); ok {
		s.nativeVibrationListener = world.CloneNativeEntityNBT(native)
	} else {
		s.nativeVibrationListener = map[string]any{"selector": map[string]any{}}
	}
	return s
}
func (s SculkShrieker) EncodeNBT() map[string]any {
	data := map[string]any{"id": "SculkShrieker", "BlockEntityVersion": int32(0)}
	// BDS writes {selector: {}} for a fresh listener.
	if s.nativeVibrationListener != nil {
		data["VibrationListener"] = world.CloneNativeEntityNBT(s.nativeVibrationListener)
	} else {
		data["VibrationListener"] = map[string]any{"selector": map[string]any{}}
	}
	javaState := world.CloneNativeEntityNBT(s.javaState)
	if javaState == nil {
		javaState = map[string]any{}
	}
	javaState["warning_level"] = int32(s.WarningLevel)
	if s.listener != nil {
		javaState["listener"] = s.listener.EncodeNBT()
	} else {
		javaState["listener"] = (&world.VibrationData{}).EncodeNBT()
	}
	data["adamant:java"] = javaState
	return data
}

var hashSculkShrieker = NextHash()
var _ EntityStepper = SculkShrieker{}
var _ world.TickerBlock = SculkShrieker{}
var _ world.ScheduledTicker = SculkShrieker{}
var _ world.NBTer = SculkShrieker{}
var _ world.VibrationListener = SculkShrieker{}
var _ world.BlockRemovalHandler = SculkShrieker{}
