package world

import (
	"cmp"
	"encoding/binary"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// GameEvent identifies a Java game event delivered to vibration listeners.
type GameEvent string

const (
	GameEventStep                        GameEvent = "minecraft:step"
	GameEventSwim                        GameEvent = "minecraft:swim"
	GameEventFlap                        GameEvent = "minecraft:flap"
	GameEventProjectileLand              GameEvent = "minecraft:projectile_land"
	GameEventHitGround                   GameEvent = "minecraft:hit_ground"
	GameEventSplash                      GameEvent = "minecraft:splash"
	GameEventBounce                      GameEvent = "minecraft:bounce"
	GameEventItemInteractFinish          GameEvent = "minecraft:item_interact_finish"
	GameEventProjectileShoot             GameEvent = "minecraft:projectile_shoot"
	GameEventInstrumentPlay              GameEvent = "minecraft:instrument_play"
	GameEventEntityAction                GameEvent = "minecraft:entity_action"
	GameEventElytraGlide                 GameEvent = "minecraft:elytra_glide"
	GameEventUnequip                     GameEvent = "minecraft:unequip"
	GameEventEntityDismount              GameEvent = "minecraft:entity_dismount"
	GameEventEquip                       GameEvent = "minecraft:equip"
	GameEventEntityInteract              GameEvent = "minecraft:entity_interact"
	GameEventShear                       GameEvent = "minecraft:shear"
	GameEventEntityMount                 GameEvent = "minecraft:entity_mount"
	GameEventEntityDamage                GameEvent = "minecraft:entity_damage"
	GameEventItemInteractStart           GameEvent = "minecraft:item_interact_start"
	GameEventDrink                       GameEvent = "minecraft:drink"
	GameEventEat                         GameEvent = "minecraft:eat"
	GameEventContainerClose              GameEvent = "minecraft:container_close"
	GameEventBlockClose                  GameEvent = "minecraft:block_close"
	GameEventBlockDeactivate             GameEvent = "minecraft:block_deactivate"
	GameEventBlockDetach                 GameEvent = "minecraft:block_detach"
	GameEventContainerOpen               GameEvent = "minecraft:container_open"
	GameEventBlockOpen                   GameEvent = "minecraft:block_open"
	GameEventBlockActivate               GameEvent = "minecraft:block_activate"
	GameEventBlockAttach                 GameEvent = "minecraft:block_attach"
	GameEventPistonContract              GameEvent = "minecraft:piston_contract"
	GameEventPistonExtend                GameEvent = "minecraft:piston_extend"
	GameEventPrimeFuse                   GameEvent = "minecraft:prime_fuse"
	GameEventNoteBlockPlay               GameEvent = "minecraft:note_block_play"
	GameEventBlockChange                 GameEvent = "minecraft:block_change"
	GameEventBlockDestroy                GameEvent = "minecraft:block_destroy"
	GameEventFluidPickup                 GameEvent = "minecraft:fluid_pickup"
	GameEventBlockPlace                  GameEvent = "minecraft:block_place"
	GameEventFluidPlace                  GameEvent = "minecraft:fluid_place"
	GameEventEntityPlace                 GameEvent = "minecraft:entity_place"
	GameEventLightningStrike             GameEvent = "minecraft:lightning_strike"
	GameEventTeleport                    GameEvent = "minecraft:teleport"
	GameEventEntityDie                   GameEvent = "minecraft:entity_die"
	GameEventExplode                     GameEvent = "minecraft:explode"
	GameEventShriek                      GameEvent = "minecraft:shriek"
	GameEventSculkSensorTendrilsClicking GameEvent = "minecraft:sculk_sensor_tendrils_clicking"
)

// GameEventContext describes the source and affected block state of an event.
type GameEventContext struct {
	Source          Entity
	ProjectileOwner Entity
	AffectedState   Block
	// Explicit flags also support event producers without an entity source.
	Sneaking          bool
	Spectator         bool
	DampensVibrations bool
	// Experience is the experience of an entity_die source. A sculk catalyst
	// handling the event consumes it so the entity drops none.
	Experience *DeathExperience
}

// DeathExperience is the experience a dying entity would award, as Java's
// LivingEntity.getExperienceReward and shouldDropExperience.
type DeathExperience struct {
	Amount   int
	Eligible bool
	Consumed bool
}

// GameEventHandler is a block actor that handles game events itself, like a
// Java listener with DeliveryMode.BY_DISTANCE: every handler within its radius
// receives the event after the dispatch scan, nearest first.
type GameEventHandler interface {
	GameEventRadius() int
	HandleGameEvent(tx *Tx, pos cube.Pos, event GameEvent, position mgl64.Vec3, context GameEventContext)
}

// VibrationInfo is one selected event travelling to a listener.
type VibrationInfo struct {
	Event             GameEvent
	Distance          float32
	Position          mgl64.Vec3
	SourceID          uuid.UUID
	ProjectileOwnerID uuid.UUID
	source            *EntityHandle
}

// VibrationUser defines listener-specific filtering and reception behaviour.
type VibrationUser interface {
	VibrationRadius() int
	CanReceiveVibration(tx *Tx, listenerPos, eventPos cube.Pos, event GameEvent, context GameEventContext) bool
	ReceiveVibration(tx *Tx, listenerPos, eventPos cube.Pos, info VibrationInfo, distance float32)
	VibrationParticle(destination mgl64.Vec3, travelTicks int) Particle
}

// VibrationListener is implemented by persistent block actors that receive game events.
type VibrationListener interface {
	VibrationUser() VibrationUser
	VibrationData() *VibrationData
	// InitialiseVibrationListener returns the same block state with owned data.
	InitialiseVibrationListener() Block
}

// EntityVibrationListener is implemented by entities that receive game
// events as vibrations through an EntityPositionSource, as the allay and the
// warden do. They tick their vibrations with TickEntityVibrationSystem.
type EntityVibrationListener interface {
	Entity
	// VibrationUser returns the user of the listener, nil while it does not
	// listen.
	VibrationUser() VibrationUser
	VibrationData() *VibrationData
	// VibrationListenerPosition returns where the entity hears from: its
	// position raised by the offset of its position source.
	VibrationListenerPosition() mgl64.Vec3
}

// entityVibrationReach is the furthest an entity listener hears from, the
// radius of the warden and the allay, with room for the height of its ears.
const entityVibrationReach = 16 + 4

// VibrationSelector retains the best event received during a single game tick.
type VibrationSelector struct {
	Candidate    VibrationInfo
	HasCandidate bool
	Tick         int64
}

// AddCandidate applies Java's same-tick distance, then frequency selection precedence.
func (s *VibrationSelector) AddCandidate(info VibrationInfo, tick int64) bool {
	if s.HasCandidate {
		if s.Tick != tick {
			return false
		}
		previous := s.Candidate
		if info.Distance > previous.Distance || (info.Distance == previous.Distance && GameEventFrequency(info.Event) <= GameEventFrequency(previous.Event)) {
			return false
		}
	}
	s.Candidate, s.HasCandidate, s.Tick = info, true, tick
	return true
}

// Chosen returns a candidate only after the tick during which it was heard.
func (s *VibrationSelector) Chosen(tick int64) (VibrationInfo, bool) {
	if !s.HasCandidate || s.Tick >= tick {
		return VibrationInfo{}, false
	}
	return s.Candidate, true
}

func (s *VibrationSelector) StartOver() { *s = VibrationSelector{Tick: -1} }

// VibrationData stores Java VibrationSystem.Data state and is persisted under listener.
type VibrationData struct {
	Current        VibrationInfo
	HasCurrent     bool
	Selector       VibrationSelector
	TravelTime     int
	ReloadParticle bool
}

// EncodeNBT emits the source actor's listener data fields without dropping active selection/travel state.
func (d *VibrationData) EncodeNBT() map[string]any {
	out := map[string]any{"event_delay": int32(max(0, d.TravelTime)), "selector": encodeSelector(d.Selector)}
	if d.HasCurrent {
		out["event"] = encodeVibration(d.Current)
	}
	return out
}

// DecodeVibrationData restores a listener payload. ReloadParticle is true after load, matching Java Data.CODEC.
func DecodeVibrationData(m map[string]any) VibrationData {
	d := VibrationData{ReloadParticle: true}
	if n, ok := m["event_delay"]; ok {
		d.TravelTime = max(0, int(asInt64(n)))
	}
	if e, ok := m["event"].(map[string]any); ok {
		d.Current, d.HasCurrent = decodeVibration(e)
	}
	if selector, ok := m["selector"].(map[string]any); ok {
		d.Selector = decodeSelector(selector)
	}
	return d
}

// EncodeListenerNBT returns the actor's Java-compatible nested NBT field.
func (d *VibrationData) EncodeListenerNBT() map[string]any {
	return map[string]any{"listener": d.EncodeNBT()}
}

// GameEventFrequency returns the Java 26.3 vibration frequency, or zero for unclassified events.
func GameEventFrequency(event GameEvent) int {
	if strings.HasPrefix(string(event), "minecraft:resonate_") {
		n, _ := strconv.Atoi(strings.TrimPrefix(string(event), "minecraft:resonate_"))
		if n >= 1 && n <= 15 {
			return n
		}
	}
	switch event {
	case GameEventStep, GameEventSwim, GameEventFlap:
		return 1
	case GameEventProjectileLand, GameEventHitGround, GameEventSplash, GameEventBounce:
		return 2
	case GameEventItemInteractFinish, GameEventProjectileShoot, GameEventInstrumentPlay:
		return 3
	case GameEventEntityAction, GameEventElytraGlide, GameEventUnequip:
		return 4
	case GameEventEntityDismount, GameEventEquip:
		return 5
	case GameEventEntityInteract, GameEventShear, GameEventEntityMount:
		return 6
	case GameEventEntityDamage:
		return 7
	case GameEventDrink, GameEventEat:
		return 8
	case GameEventContainerClose, GameEventBlockClose, GameEventBlockDeactivate, GameEventBlockDetach, GameEventPistonContract:
		return 9
	case GameEventContainerOpen, GameEventBlockOpen, GameEventBlockActivate, GameEventBlockAttach, GameEventPrimeFuse, GameEventNoteBlockPlay, GameEventPistonExtend:
		return 10
	case GameEventBlockChange:
		return 11
	case GameEventBlockDestroy, GameEventFluidPickup:
		return 12
	case GameEventBlockPlace, GameEventFluidPlace:
		return 13
	case GameEventEntityPlace, GameEventLightningStrike, GameEventTeleport:
		return 14
	case GameEventEntityDie, GameEventExplode:
		return 15
	default:
		return 0
	}
}

// EmitGameEvent searches the same loaded chunk sections as Java's dispatcher.
func (tx *Tx) EmitGameEvent(event GameEvent, position mgl64.Vec3, context GameEventContext) {
	w := tx.World()
	if source := context.Source; source != nil {
		if mode, ok := source.(interface{ GameMode() GameMode }); ok {
			context.Spectator = context.Spectator || mode.GameMode() == GameModeSpectator
		}
		if sneaking, ok := source.(interface{ Sneaking() bool }); ok {
			context.Sneaking = context.Sneaking || sneaking.Sneaking()
		}
		if dampening, ok := source.(interface{ DampensVibrations() bool }); ok {
			context.DampensVibrations = context.DampensVibrations || dampening.DampensVibrations()
		}
		if context.ProjectileOwner == nil {
			if owned, ok := source.(interface{ ProjectileOwner() *EntityHandle }); ok {
				if owner := owned.ProjectileOwner(); owner != nil {
					context.ProjectileOwner, _ = owner.Entity(tx)
				}
			}
		}
	}
	vibrationsFiltered := context.Spectator || context.DampensVibrations ||
		(context.Sneaking && isSneakingIgnored(event)) ||
		w.gameEventBlockFlags(context.AffectedState)&vibrationDampening != 0
	sourcePos := vibrationCellPosition(position)
	type handlerInfo struct {
		pos      cube.Pos
		handler  GameEventHandler
		distance float64
	}
	var handlers []handlerInfo
	radius := 16
	switch event {
	case GameEventShriek:
		radius = 32
	case "minecraft:jukebox_play", "minecraft:jukebox_stop_play":
		radius = 10
	}
	minY, maxY := (sourcePos.Y()-radius)>>4, (sourcePos.Y()+radius)>>4
	for x := (sourcePos.X() - radius) >> 4; x <= (sourcePos.X()+radius)>>4; x++ {
		for z := (sourcePos.Z() - radius) >> 4; z <= (sourcePos.Z()+radius)>>4; z++ {
			col, ok := tx.readyColumn(ChunkPos{int32(x), int32(z)})
			if !ok {
				continue
			}
			for listenerPos, actor := range col.BlockEntities {
				if section := listenerPos.Y() >> 4; section < minY || section > maxY {
					continue
				}
				if handler, ok := actor.(GameEventHandler); ok {
					radius := handler.GameEventRadius()
					d := listenerPos.Sub(sourcePos)
					if d.X()*d.X()+d.Y()*d.Y()+d.Z()*d.Z() <= radius*radius {
						handlers = append(handlers, handlerInfo{pos: listenerPos, handler: handler, distance: position.Sub(listenerPos.Vec3Centre()).LenSqr()})
					}
				}
				listener, ok := actor.(VibrationListener)
				if !ok || vibrationsFiltered {
					continue
				}
				data := listener.VibrationData()
				if data == nil {
					actor = listener.InitialiseVibrationListener()
					listener = actor.(VibrationListener)
					data = listener.VibrationData()
					tx.SetBlockEntity(listenerPos, actor)
				}
				user := listener.VibrationUser()
				if user == nil || data.HasCurrent {
					continue
				}
				listens := isVibrationEvent(event)
				if filter, ok := user.(interface{ ListensToGameEvent(GameEvent) bool }); ok {
					listens = filter.ListensToGameEvent(event)
				}
				if !listens {
					continue
				}
				listenerPoint := listenerPos.Vec3Centre()
				listenerRadius := user.VibrationRadius()
				if !vibrationInRange(position, listenerPoint, listenerRadius) {
					continue
				}
				if !user.CanReceiveVibration(tx, listenerPos, sourcePos, event, context) ||
					vibrationOccluded(tx, position, listenerPoint) {
					continue
				}
				info := makeVibrationInfo(tx, event, position, vibrationDistance(position, listenerPoint), context)
				if data.Selector.AddCandidate(info, w.CurrentTick()) {
					tx.SetBlockEntity(listenerPos, actor)
				}
			}
		}
	}
	if !vibrationsFiltered {
		w.scheduleEntityVibrations(tx, event, position, sourcePos, context)
	}
	slices.SortStableFunc(handlers, func(a, b handlerInfo) int { return cmp.Compare(a.distance, b.distance) })
	for _, h := range handlers {
		h.handler.HandleGameEvent(tx, h.pos, event, position, context)
	}
}

// scheduleEntityVibrations offers a game event to the entity listeners in
// reach, as VibrationSystem.Listener.handleGameEvent does for each.
func (w *World) scheduleEntityVibrations(tx *Tx, event GameEvent, position mgl64.Vec3, sourcePos cube.Pos, context GameEventContext) {
	reach := mgl64.Vec3{entityVibrationReach, entityVibrationReach, entityVibrationReach}
	for e := range tx.EntitiesWithin(cube.Box(position[0]-reach[0], position[1]-reach[1], position[2]-reach[2], position[0]+reach[0], position[1]+reach[1], position[2]+reach[2])) {
		listener, ok := e.(EntityVibrationListener)
		if !ok {
			continue
		}
		user, data := listener.VibrationUser(), listener.VibrationData()
		if user == nil || data == nil || data.HasCurrent {
			continue
		}
		listens := isVibrationEvent(event)
		if filter, ok := user.(interface{ ListensToGameEvent(GameEvent) bool }); ok {
			listens = filter.ListensToGameEvent(event)
		}
		if !listens {
			continue
		}
		point := listener.VibrationListenerPosition()
		radius := user.VibrationRadius()
		if !vibrationInRange(position, point, radius) {
			continue
		}
		if !user.CanReceiveVibration(tx, vibrationCellPosition(point), sourcePos, event, context) || vibrationOccluded(tx, position, point) {
			continue
		}
		data.Selector.AddCandidate(makeVibrationInfo(tx, event, position, vibrationDistance(position, point), context), w.CurrentTick())
	}
}

// ForceScheduleVibration is the source sensor step-on path. Its caller checks
// receiver eligibility; this bypasses normal event filtering and wool occlusion.
// A forced event may queue while another vibration is still travelling.
func (tx *Tx) ForceScheduleVibration(listenerPos cube.Pos, event GameEvent, context GameEventContext, origin mgl64.Vec3) {
	w := tx.World()
	actor, loaded := tx.blockLoaded(listenerPos)
	if !loaded {
		return
	}
	listener, ok := actor.(VibrationListener)
	if !ok {
		return
	}
	data := listener.VibrationData()
	if data == nil {
		actor = listener.InitialiseVibrationListener()
		listener = actor.(VibrationListener)
		data = listener.VibrationData()
		tx.SetBlockEntity(listenerPos, actor)
	}
	distance := vibrationDistance(origin, listenerPos.Vec3Centre())
	if data.Selector.AddCandidate(makeVibrationInfo(tx, event, origin, distance, context), w.CurrentTick()) {
		tx.SetBlockEntity(listenerPos, actor)
	}
}

func makeVibrationInfo(tx *Tx, event GameEvent, position mgl64.Vec3, distance float32, context GameEventContext) VibrationInfo {
	info := VibrationInfo{Event: event, Distance: distance, Position: position}
	if context.Source != nil {
		info.source = context.Source.H()
		info.SourceID = info.source.UUID()
		if context.ProjectileOwner == nil {
			if owned, ok := context.Source.(interface{ ProjectileOwner() *EntityHandle }); ok {
				if owner := owned.ProjectileOwner(); owner != nil {
					context.ProjectileOwner, _ = owner.Entity(tx)
				}
			}
		}
	}
	if context.ProjectileOwner != nil {
		info.ProjectileOwnerID = context.ProjectileOwner.H().UUID()
	}
	return info
}

// SourceEntity resolves the source inside the current transaction only.
func (info VibrationInfo) SourceEntity(tx *Tx) (Entity, bool) {
	if info.source != nil {
		if source, ok := info.source.Entity(tx); ok {
			return source, true
		}
	}
	if info.SourceID == uuid.Nil {
		return nil, false
	}
	return tx.EntityByUUID(info.SourceID)
}

// ProjectileOwner prefers the projectile's current owner, then its captured UUID.
func (info VibrationInfo) ProjectileOwner(tx *Tx) (Entity, bool) {
	if source, ok := info.SourceEntity(tx); ok {
		if owned, ok := source.(interface{ ProjectileOwner() *EntityHandle }); ok {
			if owner := owned.ProjectileOwner(); owner != nil {
				if entity, ok := owner.Entity(tx); ok {
					return entity, true
				}
			}
		}
	}
	if info.ProjectileOwnerID == uuid.Nil {
		return nil, false
	}
	return tx.EntityByUUID(info.ProjectileOwnerID)
}

// TickVibrationSystem advances selection, particle reload, travel, and reception
// in source order. A blocked arrival stays pending without repeatedly dirtying NBT.
func (w *World) TickVibrationSystem(tx *Tx, pos cube.Pos, user VibrationUser, data *VibrationData) {
	w.tickVibration(tx, pos, pos.Vec3Centre(), user, data, true)
}

// TickEntityVibrationSystem ticks the vibrations of an entity listener as
// VibrationSystem.Ticker does: vibrations travel to where it hears from.
func (w *World) TickEntityVibrationSystem(tx *Tx, l EntityVibrationListener) {
	user, data := l.VibrationUser(), l.VibrationData()
	if user == nil || data == nil {
		return
	}
	point := l.VibrationListenerPosition()
	w.tickVibration(tx, vibrationCellPosition(point), point, user, data, false)
}

// tickVibration ticks the vibrations of a listener at pos that they travel
// to destination, saving the block actor at pos as they change if persist.
func (w *World) tickVibration(tx *Tx, pos cube.Pos, destination mgl64.Vec3, user VibrationUser, data *VibrationData, persist bool) {
	if !data.HasCurrent {
		if candidate, ok := data.Selector.Chosen(w.CurrentTick()); ok {
			data.Current, data.HasCurrent = candidate, true
			data.TravelTime = int(math.Floor(float64(candidate.Distance)))
			w.sendVibrationParticle(tx, user, candidate.Position, destination, data.TravelTime)
			if persist {
				persistVibrationData(tx, pos)
			}
			data.Selector.StartOver()
		}
	}
	if !data.HasCurrent {
		return
	}
	changed := data.TravelTime > 0
	if data.ReloadParticle {
		initialTravel := int(math.Floor(float64(data.Current.Distance)))
		alpha := 1 - float64(data.TravelTime)/float64(initialTravel)
		origin := data.Current.Position
		origin = origin.Add(destination.Sub(origin).Mul(alpha))
		if w.sendVibrationParticle(tx, user, origin, destination, data.TravelTime) {
			data.ReloadParticle = false
		}
	}
	data.TravelTime = max(0, data.TravelTime-1)
	if data.TravelTime == 0 {
		changed = true
		if adjacent, ok := user.(interface{ RequiresAdjacentChunksToBeTicking() bool }); ok && adjacent.RequiresAdjacentChunksToBeTicking() {
			center := chunkPosFromBlockPos(pos)
			for x := center[0] - 1; x <= center[0]+1 && changed; x++ {
				for z := center[1] - 1; z <= center[1]+1; z++ {
					chunkPos := ChunkPos{x, z}
					col := w.chunks[chunkPos]
					_, active := w.activeColumnIndex[chunkPos]
					if col == nil || !col.Ready() || (!w.conf.Synchronous && !active) {
						changed = false
						break
					}
				}
			}
		}
		if changed {
			info := data.Current
			eventPos := vibrationCellPosition(info.Position)
			user.ReceiveVibration(tx, pos, eventPos, info, info.Distance)
			data.Current, data.HasCurrent = VibrationInfo{}, false
		}
	}
	if changed && persist {
		persistVibrationData(tx, pos)
	}
}

func (w *World) sendVibrationParticle(tx *Tx, user VibrationUser, origin, destination mgl64.Vec3, travel int) bool {
	viewers := viewerSlicePool.Get().([]Viewer)[:0]
	if tx.region != nil {
		// The viewers of other regions are not the region's to ask: those
		// near enough view the columns around origin.
		lo, hi := chunkPosFromVec3(origin.Sub(mgl64.Vec3{33, 0, 33})), chunkPosFromVec3(origin.Add(mgl64.Vec3{33, 0, 33}))
		for x := lo[0]; x <= hi[0]; x++ {
			for z := lo[1]; z <= hi[1]; z++ {
				if c, ok := tx.loadedColumn(ChunkPos{x, z}); ok {
					for viewer := range c.viewers {
						if !slices.Contains(viewers, viewer) {
							viewers = append(viewers, viewer)
						}
					}
				}
			}
		}
	} else {
		w.viewerMu.Lock()
		for _, viewer := range w.viewers {
			viewers = append(viewers, viewer)
		}
		w.viewerMu.Unlock()
	}
	defer w.releaseViewers(viewers)
	var particle Particle
	for _, viewer := range viewers {
		controlled, ok := viewer.(interface{ ViewerEntity() *EntityHandle })
		if !ok {
			continue
		}
		handle := controlled.ViewerEntity()
		if handle == nil {
			continue
		}
		entity, ok := handle.Entity(tx)
		if !ok || !(cube.PosFromVec3(entity.Position()).Vec3Centre().Sub(origin).LenSqr() < 32*32) {
			continue
		}
		if particle == nil {
			particle = user.VibrationParticle(destination, travel)
			particle.Spawn(w, origin)
		}
		viewer.ViewParticle(origin, particle)
	}
	return particle != nil
}

func persistVibrationData(tx *Tx, pos cube.Pos) {
	if col, ok := tx.loadedColumn(chunkPosFromBlockPos(pos)); ok {
		if actor, ok := col.BlockEntities[pos]; ok {
			tx.SetBlockEntity(pos, actor)
		}
	}
}

func blockDistance(a, b cube.Pos) float64 {
	dx, dy, dz := int64(a.X()-b.X()), int64(a.Y()-b.Y()), int64(a.Z()-b.Z())
	return math.Sqrt(float64(dx*dx + dy*dy + dz*dz))
}

// vibrationDistance matches Bedrock's VibrationSelector::addCandidate: first
// floor the event/listener positions to cells, then truncate their Euclidean
// distance to an integer. This same stored distance controls selection, travel
// time and sensor strength; reception must not recompute an unrounded distance.
func vibrationDistance(origin, listener mgl64.Vec3) float32 {
	return float32(int64(blockDistance(vibrationCellPosition(origin), vibrationCellPosition(listener))))
}

func vibrationCellPosition(position mgl64.Vec3) cube.Pos {
	return cube.PosFromVec3(mgl64.Vec3{float64(float32(position[0])), float64(float32(position[1])), float64(float32(position[2]))})
}

// vibrationInRange uses GameEventDispatcher::post's separate range gate. Native
// positions and arithmetic are binary32, and the squared distance is truncated
// before comparison against the listener's radius and the dispatch limit 256.
func vibrationInRange(origin, listener mgl64.Vec3, radius int) bool {
	dx := float32(origin[0]) - float32(listener[0])
	dy := float32(origin[1]) - float32(listener[1])
	dz := float32(origin[2]) - float32(listener[2])
	squared := float32(dx*dx + dy*dy)
	squared += dz * dz
	if radius < 0 || !(squared < 257) {
		return false
	}
	return int(squared) <= radius*radius
}

const (
	vibrationListenable uint8 = 1 << iota
	vibrationSneakingIgnored
	vibrationShriekerListenable
	vibrationWardenListenable
)

const (
	vibrationOccluding uint8 = 1 << iota
	vibrationDampening
	movementClimbable
	movementHighSupport
)

var gameEventTags = sync.OnceValue(func() struct {
	events map[GameEvent]uint8
	blocks map[string]uint8
} {
	tags := struct {
		events map[GameEvent]uint8
		blocks map[string]uint8
	}{make(map[GameEvent]uint8), make(map[string]uint8)}
	for _, tag := range [...]struct {
		name string
		flag uint8
	}{
		{"vibrations", vibrationListenable},
		{"ignore_vibrations_sneaking", vibrationSneakingIgnored},
		{"shrieker_can_listen", vibrationShriekerListenable},
		{"warden_can_listen", vibrationWardenListenable},
	} {
		names, err := gamedata.GameEventTagMembers(tag.name)
		if err != nil {
			panic(err)
		}
		for _, name := range names {
			tags.events[GameEvent(name)] |= tag.flag
		}
	}
	// Bedrock gives piston transitions their own names. They use the
	// corresponding block-transition listener categories, absent from the
	// Java event-tag source used to initialize the common event set.
	tags.events[GameEventPistonContract] = tags.events[GameEventBlockDeactivate]
	tags.events[GameEventPistonExtend] = tags.events[GameEventBlockActivate]
	for _, tag := range [...]struct {
		name string
		flag uint8
	}{
		{"occludes_vibration_signals", vibrationOccluding},
		{"dampens_vibrations", vibrationDampening},
		{"climbable", movementClimbable},
		{"fences", movementHighSupport},
		{"walls", movementHighSupport},
		{"fence_gates", movementHighSupport},
	} {
		names, err := gamedata.BlockTagMembers(tag.name)
		if err != nil {
			panic(err)
		}
		for _, name := range names {
			tags.blocks[name] |= tag.flag
		}
	}
	tags.blocks["minecraft:powder_snow"] |= movementClimbable
	return tags
})

func isSneakingIgnored(event GameEvent) bool {
	return gameEventTags().events[event]&vibrationSneakingIgnored != 0
}

func isVibrationEvent(event GameEvent) bool {
	return gameEventTags().events[event]&vibrationListenable != 0
}

// GameEventShriekerCanListen uses the source SHRIEKER_CAN_LISTEN tag.
func GameEventShriekerCanListen(event GameEvent) bool {
	return gameEventTags().events[event]&vibrationShriekerListenable != 0
}

// GameEventWardenCanListen uses the source WARDEN_CAN_LISTEN tag.
func GameEventWardenCanListen(event GameEvent) bool {
	return gameEventTags().events[event]&vibrationWardenListenable != 0
}

// MovementBlockProperties returns the source movement-emission properties:
// climbable blocks count vertical travel, and tall supports retain their own
// position for legacy step effects rather than the block at feet minus 0.2.
func (tx *Tx) MovementBlockProperties(block Block) (climbable, highSupport bool) {
	flags := tx.World().gameEventBlockFlags(block)
	return flags&movementClimbable != 0, flags&movementHighSupport != 0
}

func (w *World) gameEventBlockFlags(block Block) uint8 {
	if block == nil {
		return 0
	}
	rid := w.conf.Blocks.BlockRuntimeID(block)
	if flags, ok := w.gameEventBlocks.load(rid); ok {
		return flags
	}
	name, _, _ := w.conf.Blocks.RuntimeIDToState(rid)
	if state, err := CanonicalState(w.conf.Blocks, NativeState{Block: rid, Liquid: w.conf.Blocks.AirRuntimeID()}); err == nil {
		name = state.Name
	}
	flags := gameEventTags().blocks[name]
	w.gameEventBlocks.store(rid, flags)
	return flags
}

func encodeSelector(s VibrationSelector) map[string]any {
	m := map[string]any{"tick": int64(-1)}
	if s.HasCandidate {
		m["event"] = encodeVibration(s.Candidate)
		m["tick"] = s.Tick
	}
	return m
}
func decodeSelector(m map[string]any) VibrationSelector {
	s := VibrationSelector{Tick: -1}
	if n, ok := m["tick"]; ok {
		s.Tick = asInt64(n)
	}
	if e, ok := m["event"].(map[string]any); ok {
		s.Candidate, s.HasCandidate = decodeVibration(e)
	}
	return s
}
func encodeVibration(info VibrationInfo) map[string]any {
	m := map[string]any{"game_event": string(info.Event), "distance": info.Distance, "pos": []float64{info.Position[0], info.Position[1], info.Position[2]}}
	if info.SourceID != uuid.Nil {
		m["source"] = vibrationUUIDNBT(info.SourceID)
	}
	if info.ProjectileOwnerID != uuid.Nil {
		m["projectile_owner"] = vibrationUUIDNBT(info.ProjectileOwnerID)
	}
	return m
}
func decodeVibration(m map[string]any) (VibrationInfo, bool) {
	event, ok := m["game_event"].(string)
	if !ok {
		return VibrationInfo{}, false
	}
	pos, ok := m["pos"].([]float64)
	if !ok || len(pos) != 3 {
		if p, ok := m["pos"].([]any); ok && len(p) == 3 {
			pos = []float64{asFloat64(p[0]), asFloat64(p[1]), asFloat64(p[2])}
		} else {
			return VibrationInfo{}, false
		}
	}
	info := VibrationInfo{Event: GameEvent(event), Distance: float32(asFloat64(m["distance"])), Position: mgl64.Vec3{pos[0], pos[1], pos[2]}}
	info.SourceID = decodeVibrationUUID(m["source"])
	info.ProjectileOwnerID = decodeVibrationUUID(m["projectile_owner"])
	return info, true
}

func vibrationUUIDNBT(id uuid.UUID) []int32 {
	return []int32{
		int32(binary.BigEndian.Uint32(id[0:4])), int32(binary.BigEndian.Uint32(id[4:8])),
		int32(binary.BigEndian.Uint32(id[8:12])), int32(binary.BigEndian.Uint32(id[12:16])),
	}
}

func decodeVibrationUUID(value any) uuid.UUID {
	words, ok := value.([]int32)
	if !ok || len(words) != 4 {
		return uuid.Nil
	}
	var id uuid.UUID
	for i, word := range words {
		binary.BigEndian.PutUint32(id[i*4:], uint32(word))
	}
	return id
}
func asInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint32:
		return int64(n)
	case uint64:
		return int64(n)
	default:
		return 0
	}
}
func asFloat64(v any) float64 {
	switch n := v.(type) {
	case float32:
		return float64(n)
	case float64:
		return n
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
