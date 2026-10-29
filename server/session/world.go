package session

import (
	"bytes"
	"image/color"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// NetworkEncodeableEntity is a world.EntityType where the save ID and network
// ID are not the same.
type NetworkEncodeableEntity interface {
	// NetworkEncodeEntity returns the network type ID of the entity. This is
	// NOT the save ID.
	NetworkEncodeEntity() string
}

// OffsetEntity is a world.EntityType that has an additional offset when sent
// over network. This is mostly the case for older entities such as players and
// TNT.
type OffsetEntity interface {
	NetworkOffset() float64
}

// entityHidden checks if a world.Entity is being explicitly hidden from the Session.
func (s *Session) entityHidden(e world.Entity) bool {
	if !s.hidesEntities.Load() {
		return false
	}
	s.entityMutex.RLock()
	_, ok := s.hiddenEntities[e.H().UUID()]
	s.entityMutex.RUnlock()
	return ok
}

// ViewEntity ...
func (s *Session) ViewEntity(e world.Entity) {
	if e.H() == s.ent {
		s.ViewEntityState(e)
		return
	}
	if s.entityHidden(e) {
		return
	}
	var runtimeID uint64

	s.entityMutex.Lock()
	if id, ok := s.entityRuntimeIDs[e.H()]; ok {
		// Reuse existing runtime IDs to avoid duplicate entities and stale mappings.
		runtimeID = id
		if _, ok := s.entities[id]; !ok {
			s.entities[id] = e.H()
		}
	} else {
		runtimeID = s.runtimeIDOf(e.H())
		s.entityRuntimeIDs[e.H()] = runtimeID
		s.entities[runtimeID] = e.H()
	}
	delete(s.deathAnimations, runtimeID)
	if s.justShown == nil {
		s.justShown = make(map[*world.EntityHandle]struct{})
	}
	s.justShown[e.H()] = struct{}{}
	s.entityMutex.Unlock()

	yaw, pitch := e.Rotation().Elem()
	metadata := s.entityMetadata(e)
	links := s.entityLinks(e)

	id := e.H().Type().EncodeEntity()
	switch v := e.(type) {
	case Controllable:
		_, actualPlayer := sessions.Lookup(v.UUID())
		if !actualPlayer {
			s.writePacket(&v2193.PlayerListPacket{Entries: []v2193.PlayerListEntry{{
				ActionType:     v2193.PlayerListActionAdd,
				UUID:           v.UUID(),
				EntityUniqueID: s.entityUniqueID(e),
				Username:       v.Name(),
				BuildPlatform:  int32(v2193.DeviceUnknown),
				Skin:           skinToProtocol(v.Skin()),
			}}})
		}

		s.writePacket(&v2193.AddPlayerPacket{
			EntityMetadata:  toMetadata(metadata),
			EntityRuntimeID: runtimeID,
			GameType:        GameTypeFromMode(v.GameMode()),
			HeadYaw:         float32(yaw),
			Pitch:           float32(pitch),
			Position:        vec64To32(entityNetworkPosition(e, e.Position())),
			UUID:            v.UUID(),
			Username:        v.Name(),
			Yaw:             float32(yaw),
			EntityLinks:     links,
			BuildPlatform:   int32(v2193.DeviceUnknown),
			AbilityData:     s.abilityData(v, s.entityUniqueID(e)),
		})
		if !actualPlayer {
			s.writePacket(&v2193.PlayerListPacket{Entries: []v2193.PlayerListEntry{{
				ActionType: v2193.PlayerListActionRemove,
				UUID:       v.UUID(),
			}}})
		}
		// The skin of a player of a session is in the player list already, as
		// BDS has it: AddPlayer goes without a PlayerSkin.
		return
	case *entity.Painting:
		s.viewPainting(v)
		return
	case *entity.Ent:
		switch e.H().Type() {
		case entity.ItemType:
			s.writePacket(&v2193.AddItemActorPacket{
				EntityUniqueID:  s.entityUniqueID(e),
				EntityRuntimeID: runtimeID,
				Item:            instanceFromItem(s.br, v.Behaviour().(*entity.ItemBehaviour).Item()),
				Position:        vec64To32(entityNetworkPosition(e, v.Position())),
				Velocity:        vec64To32(v.Velocity()),
				EntityMetadata:  toMetadata(metadata),
			})
			return
		case entity.TextType:
			metadata[v2193.EntityDataKeyVariant] = int32(networkBlockID(s.br, block.Air{}))
		case entity.FallingBlockType:
			metadata[v2193.EntityDataKeyVariant] = int32(networkBlockID(s.br, v.Behaviour().(*entity.FallingBlockBehaviour).Block()))
		}
	}
	id = networkID(e.H().Type())

	var vel mgl64.Vec3
	if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		vel = v.Velocity()
	}

	s.writePacket(&v2193.AddActorPacket{
		EntityUniqueID:   s.entityUniqueID(e),
		EntityRuntimeID:  runtimeID,
		EntityType:       id,
		Attributes:       mobAttributes(e),
		EntityMetadata:   toMetadata(metadata),
		EntityProperties: entityProperties(e),
		EntityLinks:      links,
		Position:         vec64To32(entityNetworkPosition(e, e.Position())),
		Velocity:         vec64To32(vel),
		Pitch:            float32(pitch),
		Yaw:              float32(yaw),
		HeadYaw:          float32(entityHeadYaw(e, yaw)),
		BodyYaw:          float32(yaw),
	})
}

// entityHeadYaw returns the yaw of the head of an entity, which mobs turn
// apart from their body.
func entityHeadYaw(e world.Entity, yaw float64) float64 {
	if h, ok := e.(interface{ HeadYaw() float64 }); ok {
		return h.HeadYaw()
	}
	return yaw
}

// mobHealth is a mob whose health clients show, as riders do of their mount.
type mobHealth interface {
	Health() float64
	MaxHealth() float64
}

// mobAttributes returns the attributes the dedicated server sends of a mob:
// its health, its movement speed and the jump strength of horses.
func mobAttributes(e world.Entity) []v2193.AttributeValue {
	var attributes []v2193.AttributeValue
	if h, ok := e.(mobHealth); ok {
		attributes = append(attributes, v2193.AttributeValue{Name: "minecraft:health", Value: float32(h.Health()), Max: float32(h.MaxHealth())})
	}
	if s, ok := e.(interface{ Speed() float64 }); ok {
		attributes = append(attributes, v2193.AttributeValue{Name: "minecraft:movement", Value: float32(s.Speed()), Max: math.MaxFloat32})
	}
	if j, ok := e.(interface{ JumpStrength() float64 }); ok {
		attributes = append(attributes, v2193.AttributeValue{Name: "minecraft:horse.jump_strength", Value: float32(j.JumpStrength()), Max: math.MaxFloat32})
	}
	return attributes
}

// viewMobHealth sends the current health of a mob other than a player.
func (s *Session) viewMobHealth(e world.Entity) {
	h, ok := e.(mobHealth)
	if !ok || e.H().Type().EncodeEntity() == "minecraft:player" {
		return
	}
	s.writePacket(&v2193.UpdateAttributesPacket{
		EntityRuntimeID: s.entityRuntimeID(e),
		Attributes: []v2193.Attribute{{
			Name: "minecraft:health", Value: float32(h.Health()), Max: float32(h.MaxHealth()),
			DefaultMax: float32(h.MaxHealth()),
			Default:    float32(h.MaxHealth()),
		}},
	})
}

// ViewEntityLink sends a link update between the ridden entity and the rider.
func (s *Session) ViewEntityLink(ridden, rider *world.EntityHandle, mounted bool) {
	s.ViewEntityLinkType(ridden, rider, entity.LinkRider, mounted)
}

// ViewEntityLinkType sends a link update between the ridden entity and the rider with an explicit link type.
func (s *Session) ViewEntityLinkType(ridden, rider *world.EntityHandle, linkType entity.LinkType, mounted bool) {
	if ridden == nil || rider == nil {
		return
	}
	riddenID := s.handleRuntimeID(ridden)
	riderID := s.handleRuntimeID(rider)
	if riddenID == 0 || riderID == 0 {
		return
	}
	pkType := byte(v2193.EntityLinkRemove)
	if mounted {
		switch linkType {
		case entity.LinkPassenger:
			pkType = byte(v2193.EntityLinkPassenger)
		default:
			pkType = byte(v2193.EntityLinkRider)
		}
	}
	s.writePacket(&v2193.SetActorLinkPacket{EntityLink: v2193.EntityLink{
		RiddenEntityUniqueID: s.handleUniqueID(ridden),
		RiderEntityUniqueID:  s.handleUniqueID(rider),
		Type:                 pkType,
		RiderInitiated:       true,
	}})
}

func (s *Session) entityUniqueID(e world.Entity) int64 {
	return s.handleUniqueID(e.H())
}

func (s *Session) handleUniqueID(h *world.EntityHandle) int64 {
	if h == s.ent {
		return int64(selfEntityRuntimeID)
	}
	return h.UniqueID()
}

// ViewElderGuardianCurse shows the ghost of an elder guardian, as BDS does
// for the mining fatigue it gives.
func (s *Session) ViewElderGuardianCurse(pos mgl64.Vec3) {
	s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventParticleSoundGuardianGhost, Position: vec64To32(pos)})
}

// ViewSpectatedEntity attaches the camera of the client to an entity, as BDS
// does for /camera attach_to_entity, or detaches it again if e is nil.
func (s *Session) ViewSpectatedEntity(e world.Entity) {
	if e == nil {
		s.writePacket(&v2193.CameraInstructionPacket{DetachFromEntity: proto.Some(true)})
		return
	}
	s.writePacket(&v2193.CameraInstructionPacket{AttachToEntity: proto.Some(s.entityUniqueID(e))})
}

// ViewEntityGameMode ...
func (s *Session) ViewEntityGameMode(e world.Entity) {
	if s.entityHidden(e) {
		return
	}
	c, ok := e.(Controllable)
	if !ok {
		return
	}
	s.writePacket(&v2193.UpdatePlayerGameTypePacket{
		GameType:       GameTypeFromMode(c.GameMode()),
		PlayerUniqueID: s.entityUniqueID(c),
	})
}

// HideEntity ...
func (s *Session) HideEntity(e world.Entity) {
	s.entityMutex.Lock()
	id, ok := s.entityRuntimeIDs[e.H()]
	_, finishingDeath := s.deathAnimations[id]
	_, forceHide := s.hiddenEntities[e.H().UUID()]
	if ok && e.H() == s.ent && id == selfEntityRuntimeID {
		s.entityMutex.Unlock()
		return
	}
	if ok {
		delete(s.entityRuntimeIDs, e.H())
		delete(s.entities, id)
		delete(s.deathAnimations, id)
		delete(s.justShown, e.H())
	}
	s.entityMutex.Unlock()
	if !ok {
		// The entity was already removed some other way. We don't need to send a packet.
		return
	}
	if finishingDeath && !forceHide {
		// Bedrock handles ordinary mob death locally, including removing the
		// actor and spawning death particles. RemoveActor here would erase the
		// model before its death animation had a chance to render.
		return
	}
	s.writePacket(&v2193.RemoveActorPacket{EntityUniqueID: s.handleUniqueID(e.H())})
}

// ViewEntityMovement ...
func (s *Session) ViewEntityMovement(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	id := s.entityRuntimeID(e)
	if (id == selfEntityRuntimeID && s.moving.Load()) || s.entityHidden(e) || s.keepBack(e) {
		return
	}
	s.viewEntityAbsoluteMovement(id, e, pos, rot, onGround, false)
}

// ViewEntityDisplacement ...
func (s *Session) ViewEntityDisplacement(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	if s.entityHidden(e) {
		return
	}
	id := s.entityRuntimeID(e)
	s.viewEntityAbsoluteMovement(id, e, pos, rot, onGround, true)
}

func (s *Session) viewEntityAbsoluteMovement(id uint64, e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround, authoritative bool) {
	s.writePacket(absoluteMove(id, e, pos, rot, onGround, authoritative))
}

// absoluteMove returns the MoveActorAbsolute of an entity at a position.
func absoluteMove(id uint64, e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround, authoritative bool) *v2193.MoveActorAbsolutePacket {
	flags := byte(0)
	if onGround {
		flags |= v2193.MoveFlagOnGround
	}
	if authoritative {
		flags |= v2193.MoveFlagTeleport
	}
	return &v2193.MoveActorAbsolutePacket{
		EntityRuntimeID: id,
		Position:        vec64To32(entityNetworkPosition(e, pos)),
		Rotation:        vec64To32(mgl64.Vec3{rot.Pitch(), rot.Yaw(), entityHeadYaw(e, rot.Yaw())}),
		Flags:           flags,
	}
}

// ViewEntityVelocity ...
func (s *Session) ViewEntityVelocity(e world.Entity, velocity mgl64.Vec3) {
	if s.entityHidden(e) || s.keepBack(e) {
		return
	}
	s.writePacket(&v2193.SetActorMotionPacket{
		EntityRuntimeID: s.entityRuntimeID(e),
		Velocity:        vec64To32(velocity),
	})
}

// entityOffset returns the offset that entities have client-side.
func entityOffset(e world.Entity) mgl64.Vec3 {
	if e.H().Type().EncodeEntity() == "minecraft:player" {
		if sl, ok := e.(sleeper); ok {
			if _, sleeping := sl.Sleeping(); sleeping {
				return mgl64.Vec3{0, 0.2}
			}
		}
	}
	if offset, ok := e.H().Type().(OffsetEntity); ok {
		return mgl64.Vec3{0, offset.NetworkOffset()}
	}
	return mgl64.Vec3{}
}

func entityNetworkPosition(e world.Entity, pos mgl64.Vec3) mgl64.Vec3 {
	return pos.Add(entityOffset(e))
}

func entityBasePosition(e world.Entity, pos mgl32.Vec3) mgl32.Vec3 {
	return pos.Sub(vec64To32(entityOffset(e)))
}

// ViewTime ...
func (s *Session) ViewTime(time int) {
	s.writePacket(&v2193.SetTimePacket{Time: int32(time)})
}

// ViewTimeCycle ...
func (s *Session) ViewTimeCycle(doDayLightCycle bool) {
	s.sendGameRules([]v2193.GameRule{{Name: "dodaylightcycle", Value: doDayLightCycle}})
}

// ViewFallDamage controls whether fall damage is enabled for this viewer.
func (s *Session) ViewFallDamage(enabled bool) {
	s.sendGameRules([]v2193.GameRule{{Name: "falldamage", Value: enabled}})
}

// ViewEntityTeleport ...
func (s *Session) ViewEntityTeleport(e world.Entity, position mgl64.Vec3) {
	id := s.entityRuntimeID(e)
	if s.entityHidden(e) {
		return
	}

	yaw, pitch := e.Rotation().Elem()
	if id == selfEntityRuntimeID {
		s.teleportPos.Store(&position)
	}

	s.writePacket(&v2193.SetActorMotionPacket{EntityRuntimeID: id})
	if _, ok := e.(Controllable); ok {
		s.writePacket(&v2193.MovePlayerPacket{
			EntityRuntimeID: id,
			Position:        vec64To32(entityNetworkPosition(e, position)),
			Pitch:           float32(pitch),
			Yaw:             float32(yaw),
			HeadYaw:         float32(yaw),
			Mode:            v2193.MoveModeTeleport,
			TeleportData: proto.Some(v2193.TeleportData{
				TeleportCause: v2193.TeleportCauseUnknown,
			}),
		})
		return
	}
	s.writePacket(&v2193.MoveActorAbsolutePacket{
		EntityRuntimeID: id,
		Position:        vec64To32(entityNetworkPosition(e, position)),
		Rotation:        vec64To32(mgl64.Vec3{pitch, yaw, yaw}),
		Flags:           v2193.MoveFlagTeleport,
	})
}

// ViewEntityItems ...
func (s *Session) ViewEntityItems(e world.Entity) {
	runtimeID := s.entityRuntimeID(e)
	if runtimeID == selfEntityRuntimeID || s.entityHidden(e) {
		// Don't view the items of the entity if the entity is the Controllable entity of the session.
		return
	}
	c, ok := e.(interface {
		HeldItems() (mainHand, offHand item.Stack)
	})
	if !ok {
		if b, ok2 := e.(*entity.Ent); ok2 {
			if c, ok = b.Behaviour().(interface {
				HeldItems() (mainHand, offHand item.Stack)
			}); !ok {
				return
			}
		} else {
			return
		}
	}

	mainHand, offHand := c.HeldItems()
	// An entity just shown holds nothing until told otherwise.
	shown := s.justShownEquipment(e.H(), false)

	// Show the main hand item.
	if !shown || !mainHand.Empty() {
		s.writePacket(&v2193.MobEquipmentPacket{
			EntityRuntimeID: runtimeID,
			NewItem:         instanceFromItem(s.br, mainHand),
		})
	}
	// Show the off-hand item.
	if !shown || !offHand.Empty() {
		s.writePacket(&v2193.MobEquipmentPacket{
			EntityRuntimeID: runtimeID,
			NewItem:         instanceFromItem(s.br, offHand),
			WindowID:        v2193.WindowIDOffHand,
		})
	}
}

// justShownEquipment reports whether the entity of a handle was shown and
// its equipment not yet, forgetting it once done is set: the armour goes
// last.
func (s *Session) justShownEquipment(h *world.EntityHandle, done bool) bool {
	s.entityMutex.Lock()
	defer s.entityMutex.Unlock()
	_, shown := s.justShown[h]
	if done {
		delete(s.justShown, h)
	}
	return shown
}

// ViewEntityArmour ...
func (s *Session) ViewEntityArmour(e world.Entity) {
	runtimeID := s.entityRuntimeID(e)
	if runtimeID == selfEntityRuntimeID || s.entityHidden(e) {
		// Don't view the items of the entity if the entity is the Controllable entity of the session.
		return
	}
	var helm, chestplate, leggings, boots, body item.Stack
	var equipment any = e
	bodyItem, hasBody := e.(interface{ BodyItem() item.Stack })
	if b, ok := e.(*entity.Ent); ok {
		equipment = b.Behaviour()
		if !hasBody {
			bodyItem, hasBody = equipment.(interface{ BodyItem() item.Stack })
		}
	}
	if armoured, ok := equipment.(interface {
		EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack)
	}); ok {
		helm, chestplate, leggings, boots = armoured.EquipmentArmour()
	} else if armoured, ok := equipment.(interface {
		Armour() *inventory.Armour
	}); ok {
		if inv := armoured.Armour(); inv != nil {
			helm, chestplate, leggings, boots = inv.Helmet(), inv.Chestplate(), inv.Leggings(), inv.Boots()
		}
	} else if !hasBody {
		return
	}
	if hasBody {
		body = bodyItem.BodyItem()
	}
	if s.viewLayer != nil && s.viewLayer.Visibility(e) == world.EnforceInvisible() {
		helm, chestplate, leggings, boots, body = item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{}
	}
	if s.justShownEquipment(e.H(), true) && helm.Empty() && chestplate.Empty() && leggings.Empty() && boots.Empty() && body.Empty() {
		// An entity just shown wears nothing until told otherwise.
		return
	}

	// Show the entity's armour and body equipment.
	s.writePacket(&v2193.MobArmourEquipmentPacket{
		EntityRuntimeID: runtimeID,
		Helmet:          instanceFromItem(s.br, helm),
		Chestplate:      instanceFromItem(s.br, chestplate),
		Leggings:        instanceFromItem(s.br, leggings),
		Boots:           instanceFromItem(s.br, boots),
		Body:            instanceFromItem(s.br, body),
	})
}

// ViewItemCooldown ...
func (s *Session) ViewItemCooldown(item world.Item, duration time.Duration) {
	name, _ := item.EncodeItem()
	category := name
	if _, suffix, ok := strings.Cut(name, ":"); ok && suffix != "" {
		category = suffix
	}
	s.writePacket(&v2193.ClientStartItemCooldownPacket{
		Category: category,
		Duration: int32(duration.Milliseconds() / 50),
	})
}

// ViewSleepingPlayers ...
func (s *Session) ViewSleepingPlayers(sleeping, max int) {
	buf := bytes.NewBuffer(nil)
	_ = nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian).Encode(map[string]any{
		"ableToSleep":          int32(max),
		"overworldPlayerCount": int32(max),
		"sleepingPlayerCount":  int32(sleeping),
	})

	eventData := buf.Bytes()

	s.writePacket(&v2193.LevelEventGenericPacket{
		EventID:             v2193.LevelEventSleepingPlayers,
		SerialisedEventData: eventData[2 : len(eventData)-1],
	})
}

// ViewParticle ...
func (s *Session) ViewParticle(pos mgl64.Vec3, p world.Particle) {
	if s.dropCosmetic() {
		return
	}
	switch pa := p.(type) {
	case particle.CreakingTrail:
		buf := bytes.NewBuffer(nil)
		_ = nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian).Encode(map[string]any{
			"CreakingAmount": pa.CreakingAmount,
			"CreakingX":      float32(pa.Creaking.X()),
			"CreakingY":      float32(pa.Creaking.Y()),
			"CreakingZ":      float32(pa.Creaking.Z()),
			"HeartAmount":    pa.HeartAmount,
			"HeartX":         float32(pa.Heart.X()),
			"HeartY":         float32(pa.Heart.Y()),
			"HeartZ":         float32(pa.Heart.Z()),
		})
		data := buf.Bytes()
		s.writePacket(&v2193.LevelEventGenericPacket{
			EventID:             v2193.LevelEventParticleCreakingHeartTrail,
			SerialisedEventData: data[2 : len(data)-1],
		})
	case particle.VibrationSignal:
		type vibrationVector struct {
			Type string  `nbt:"type"`
			X    float32 `nbt:"x"`
			Y    float32 `nbt:"y"`
			Z    float32 `nbt:"z"`
		}
		type vibrationPayload struct {
			Origin     vibrationVector `nbt:"origin"`
			Speed      float32         `nbt:"speed"`
			Target     vibrationVector `nbt:"target"`
			TimeToLive float32         `nbt:"timeToLive"`
		}
		vector := func(v mgl64.Vec3) vibrationVector {
			return vibrationVector{Type: "vec3", X: float32(v.X()), Y: float32(v.Y()), Z: float32(v.Z())}
		}
		buf := bytes.NewBuffer(nil)
		_ = nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian).Encode(vibrationPayload{
			Origin:     vector(pos),
			Speed:      float32(20),
			Target:     vector(pa.Destination),
			TimeToLive: float32(max(0, pa.TravelTicks)) / 20,
		})
		data := buf.Bytes()
		s.writePacket(&v2193.LevelEventGenericPacket{
			EventID:             v2193.LevelEventParticlesVibrationSignal,
			SerialisedEventData: data[2 : len(data)-1],
		})
	case particle.DragonEggTeleport:
		xSign, ySign, zSign := 0, 0, 0
		if pa.Diff.X() < 0 {
			xSign = 1 << 24
		}
		if pa.Diff.Y() < 0 {
			ySign = 1 << 25
		}
		if pa.Diff.Z() < 0 {
			zSign = 1 << 26
		}

		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticlesDragonEgg,
			Position:  vec64To32(pos),
			EventData: int32((((((abs(pa.Diff.X()) << 16) | (abs(pa.Diff.Y()) << 8)) | abs(pa.Diff.Z())) | xSign) | ySign) | zSign),
		})
	case particle.Note:
		s.writePacket(&v2193.BlockEventPacket{
			EventType: pa.Instrument.Int32(),
			EventData: int32(pa.Pitch),
			Position:  v2193.BlockPos{int32(pos.X()), int32(pos.Y()), int32(pos.Z())},
		})
	case particle.HugeExplosion:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticlesExplosion,
			Position:  vec64To32(pos),
		})
	case particle.BoneMeal:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleCropGrowth,
			Position:  vec64To32(pos),
			EventData: int32(boolByte(pa.Area)),
		})
	case particle.BlockForceField:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleDenyBlock,
			Position:  vec64To32(pos),
		})
	case particle.BlockBreak:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticlesDestroyBlock,
			Position:  vec64To32(pos),
			EventData: int32(networkBlockID(s.br, pa.Block)),
		})
	case particle.PunchBlock:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticlesCrackBlock,
			Position:  vec64To32(pos),
			EventData: int32(networkBlockID(s.br, pa.Block)) | (int32(pa.Face) << 24),
		})
	case particle.DragonDestroyBlock:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | v2193.ParticleTypeDragonDestroyBlock,
			Position:  vec64To32(pos),
		})
	case particle.EndermanTeleport:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticlesTeleport,
			Position:  vec64To32(pos),
		})
	case particle.Flame:
		if pa.Colour != (color.RGBA{}) {
			s.writePacket(&v2193.LevelEventPacket{
				EventType: v2193.LevelEventParticleLegacyEvent | 57,
				Position:  vec64To32(pos),
				EventData: nbtconv.Int32FromRGBA(pa.Colour),
			})
			return
		}
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 8,
			Position:  vec64To32(pos),
		})
	case particle.Evaporate:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticlesEvaporateWater,
			Position:  vec64To32(pos),
		})
	case particle.SnowballPoof:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 15,
			Position:  vec64To32(pos),
		})
	case particle.EggSmash:
		rid, meta, _ := world.ItemRuntimeID(item.Egg{})
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 14,
			EventData: (rid << 16) | int32(meta),
			Position:  vec64To32(pos),
		})
	case particle.Splash:
		if (pa.Colour == color.RGBA{}) {
			pa.Colour, _ = effect.ResultingColour(nil)
		}
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticlesPotionSplash,
			EventData: (int32(pa.Colour.A) << 24) | (int32(pa.Colour.R) << 16) | (int32(pa.Colour.G) << 8) | int32(pa.Colour.B),
			Position:  vec64To32(pos),
		})
	case particle.MobGrowth:
		t := v2193.ParticleTypeResetMobGrowth
		if pa.Paused {
			t = v2193.ParticleTypePauseMobGrowth
		}
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | int32(t),
			Position:  vec64To32(pos),
		})
	case particle.SonicExplosion:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSonicExplosion,
			Position:  vec64To32(pos),
		})
	case particle.WindBurst:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticlesWindExplosion,
			Position:  vec64To32(pos),
		})
	case particle.Effect:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 34,
			EventData: (int32(pa.Colour.A) << 24) | (int32(pa.Colour.R) << 16) | (int32(pa.Colour.G) << 8) | int32(pa.Colour.B),
			Position:  vec64To32(pos),
		})
	case particle.EntityFlame:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 19,
			Position:  vec64To32(pos),
		})
	case particle.Dust:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 33,
			Position:  vec64To32(pos),
			EventData: nbtconv.Int32FromRGBA(pa.Colour),
		})
	case particle.WaterDrip:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 28,
			Position:  vec64To32(pos),
		})
	case particle.LavaDrip:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 29,
			Position:  vec64To32(pos),
		})
	case particle.Lava:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 10,
			Position:  vec64To32(pos),
		})
	case particle.DustPlume:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | 88,
			Position:  vec64To32(pos),
		})
	case particle.SculkCharge, particle.SculkChargePop:
		block := cube.PosFromVec3(pos)
		data := map[string]any{"x": int32(block.X()), "y": int32(block.Y()), "z": int32(block.Z())}
		event := int32(v2193.LevelEventSculkChargePop)
		if charge, ok := pa.(particle.SculkCharge); ok {
			event = v2193.LevelEventSculkCharge
			data["charge"], data["facing"] = int16(charge.Charge), int16(charge.Faces)
		}
		buf := bytes.NewBuffer(nil)
		_ = nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian).Encode(data)
		encoded := buf.Bytes()
		s.writePacket(&v2193.LevelEventGenericPacket{EventID: event, SerialisedEventData: encoded[2 : len(encoded)-1]})
	case particle.SculkSoul:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | v2193.ParticleTypeSculkSoul,
			Position:  vec64To32(pos),
		})
	case particle.CauldronEffect:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: cauldronLevelEvents[pa.Kind],
			Position:  vec64To32(pos),
			EventData: nbtconv.Int32FromRGBA(pa.Colour),
		})
	case particle.DripstoneDrip:
		s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventParticlesDripstoneDrip, Position: vec64To32(pos)})
	case particle.PointedDripstoneLand:
		s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventSoundPointedDripstoneLand, Position: vec64To32(pos)})
	case particle.EggCrack:
		s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventParticleTurtleEgg, Position: vec64To32(pos)})
	case particle.ElectricSpark:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticlesElectricSpark,
			Position:  vec64To32(pos),
			EventData: pa.Data,
		})
	case particle.EyeblossomTransform:
		kind := int32(v2193.ParticleTypeEyeblossomClose)
		if pa.Open {
			kind = v2193.ParticleTypeEyeblossomOpen
		}
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventParticleLegacyEvent | kind,
			Position:  vec64To32(pos),
		})
	case particle.TrialSpawnerDetection:
		event := int32(v2193.LevelEventParticlesTrialSpawnerDetection)
		if pa.Ominous {
			event = v2193.LevelEventParticlesTrialSpawnerDetectionCharged
		}
		s.writePacket(&v2193.LevelEventPacket{EventType: event, Position: vec64To32(pos), EventData: int32(pa.Players)})
	case particle.TrialSpawnerSpawning:
		s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventParticlesTrialSpawnerSpawning, Position: vec64To32(pos), EventData: int32(boolByte(pa.Ominous))})
	case particle.TrialSpawnerEjecting:
		s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventParticlesTrialSpawnerEjecting, Position: vec64To32(pos)})
	case particle.TrialSpawnerBecomeOminous:
		s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventParticlesTrialSpawnerBecomeCharged, Position: vec64To32(pos)})
	case particle.VaultActivate:
		s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventAnimationVaultActivate, Position: vec64To32(pos), EventData: int32(boolByte(pa.Ominous))})
	case particle.VaultDeactivate:
		s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventAnimationVaultDeactivate, Position: vec64To32(pos), EventData: int32(boolByte(pa.Ominous))})
	case particle.VaultEjectItem:
		s.writePacket(&v2193.LevelEventPacket{EventType: v2193.LevelEventAnimationVaultEjectItem, Position: vec64To32(pos)})
	}
}

// cauldronLevelEvents maps cauldron effects to their Bedrock level events.
var cauldronLevelEvents = [...]int32{
	particle.CauldronExplode:        v2193.LevelEventCauldronExplode,
	particle.CauldronDyeArmour:      v2193.LevelEventCauldronDyeArmor,
	particle.CauldronCleanArmour:    v2193.LevelEventCauldronCleanArmor,
	particle.CauldronFillPotion:     v2193.LevelEventCauldronFillPotion,
	particle.CauldronTakePotion:     v2193.LevelEventCauldronTakePotion,
	particle.CauldronFillWater:      v2193.LevelEventCauldronFillWater,
	particle.CauldronTakeWater:      v2193.LevelEventCauldronTakeWater,
	particle.CauldronAddDye:         v2193.LevelEventCauldronAddDye,
	particle.CauldronCleanBanner:    v2193.LevelEventCauldronCleanBanner,
	particle.CauldronFillLava:       v2193.LevelEventCauldronFillLava,
	particle.CauldronTakeLava:       v2193.LevelEventCauldronTakeLava,
	particle.CauldronFillPowderSnow: v2193.LevelEventCauldronFillPowderSnow,
	particle.CauldronTakePowderSnow: v2193.LevelEventCauldronTakePowderSnow,
}

// tierToSoundEvent converts an item.ArmourTier to a sound event associated with equipping it.
func tierToSoundEvent(tier item.ArmourTier) string {
	switch tier.(type) {
	case item.ArmourTierLeather:
		return v2193.SoundEventEquipLeather
	case item.ArmourTierCopper:
		return v2193.SoundEventEquipCopper
	case item.ArmourTierGold:
		return v2193.SoundEventEquipGold
	case item.ArmourTierChain:
		return v2193.SoundEventEquipChain
	case item.ArmourTierIron:
		return v2193.SoundEventEquipIron
	case item.ArmourTierDiamond:
		return v2193.SoundEventEquipDiamond
	case item.ArmourTierNetherite:
		return v2193.SoundEventEquipNetherite
	}
	return v2193.SoundEventEquipGeneric
}

// playSound plays a world.Sound at a position, disabling relative volume if set to true.
func (s *Session) playSound(pos mgl64.Vec3, t world.Sound, disableRelative bool) {
	s.soundPackets(pos, t, disableRelative, s.writePacket)
}

// soundPackets hands out the packets that play a sound at a position, the
// same for every viewer.
func (s *Session) soundPackets(pos mgl64.Vec3, t world.Sound, disableRelative bool, out func(proto.Packet)) {
	pk := &v2193.LevelSoundEventPacket{
		Position:              vec64To32(pos),
		EntityType:            ":",
		ExtraData:             -1,
		EntityUniqueID:        -1,
		DisableRelativeVolume: disableRelative,
	}
	switch so := t.(type) {
	case sound.NativeEvent:
		pk.SoundType = so.SoundType
		pk.EntityType = so.EntityType
		pk.ExtraData = so.ExtraData
		if so.EntityUniqueID != 0 {
			pk.EntityUniqueID = so.EntityUniqueID
		}
	case sound.LodestoneCompassLink:
		pk.SoundType = v2193.SoundEventLinkCompassToLodestone
	case sound.EquipItem:
		switch i := so.Item.(type) {
		case item.Helmet:
			pk.SoundType = tierToSoundEvent(i.Tier)
		case item.Chestplate:
			pk.SoundType = tierToSoundEvent(i.Tier)
		case item.Leggings:
			pk.SoundType = tierToSoundEvent(i.Tier)
		case item.Boots:
			pk.SoundType = tierToSoundEvent(i.Tier)
		case item.Elytra:
			pk.SoundType = v2193.SoundEventEquipElytra
		default:
			pk.SoundType = v2193.SoundEventEquipGeneric
		}
	case sound.Note:
		pk.SoundType = v2193.SoundEventNote
		pk.ExtraData = (so.Instrument.Int32() << 8) | int32(so.Pitch)
	case sound.DoorCrash:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundZombieDoorCrash,
			Position:  vec64To32(pos),
		})
		return
	case sound.Explosion:
		pk.SoundType = v2193.SoundEventExplode
	case sound.Thunder:
		pk.SoundType, pk.EntityType = v2193.SoundEventThunder, "minecraft:lightning_bolt"
	case sound.Click:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundClick,
			Position:  vec64To32(pos),
		})
		return
	case sound.SignWaxed:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventWaxOn,
			Position:  vec64To32(pos),
		})
		return
	case sound.WaxedSignFailedInteraction:
		pk.SoundType = v2193.SoundEventWaxedSignInteractFail
	case sound.WaxRemoved:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventWaxOff,
			Position:  vec64To32(pos),
		})
		return
	case sound.CopperScraped:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventScrape,
			Position:  vec64To32(pos),
		})
		return
	case sound.Pop:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundInfinityArrowPickup,
			Position:  vec64To32(pos),
		})
		return
	case sound.Teleport:
		pk.SoundType = v2193.SoundEventTeleport
	case sound.ItemAdd:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundAddItem,
			Position:  vec64To32(pos),
		})
		return
	case sound.ItemFrameRemove:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundItemFrameRemoveItem,
			Position:  vec64To32(pos),
		})
		return
	case sound.ItemFrameRotate:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundItemFrameRotateItem,
			Position:  vec64To32(pos),
		})
		return
	case sound.HangingPlace:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundItemFramePlace,
			Position:  vec64To32(pos),
		})
		return
	case sound.HangingBreak:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundItemFrameBreak,
			Position:  vec64To32(pos),
		})
		return
	case sound.GhastWarning:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundGhastWarning,
			Position:  vec64To32(pos),
		})
		return
	case sound.GhastShoot:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundGhastFireball,
			Position:  vec64To32(pos),
		})
		return
	case sound.TNT:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundFuse,
			Position:  vec64To32(pos),
		})
		return
	case sound.FireworkLaunch:
		pk.SoundType = v2193.SoundEventLaunch
	case sound.FireworkHugeBlast:
		pk.SoundType = v2193.SoundEventLargeBlast
	case sound.FireworkBlast:
		pk.SoundType = v2193.SoundEventBlast
	case sound.FireworkTwinkle:
		pk.SoundType = v2193.SoundEventTwinkle
	case sound.FurnaceCrackle:
		pk.SoundType = v2193.SoundEventFurnaceUse
	case sound.CampfireCrackle:
		pk.SoundType = v2193.SoundEventCampfireCrackle
	case sound.BlastFurnaceCrackle:
		pk.SoundType = v2193.SoundEventBlastFurnaceUse
	case sound.SmokerCrackle:
		pk.SoundType = v2193.SoundEventSmokerUse
	case sound.PotionBrewed:
		pk.SoundType = v2193.SoundEventPotionBrewed
	case sound.UseSpyglass:
		pk.SoundType = v2193.SoundEventUseSpyglass
	case sound.StopUsingSpyglass:
		pk.SoundType = v2193.SoundEventStopUsingSpyglass
	case sound.RespawnAnchorAmbient:
		pk.SoundType = v2193.SoundEventRespawnAnchorAmbient
		pk.ExtraData = int32(so.Charge)
	case sound.RespawnAnchorCharge:
		pk.SoundType = v2193.SoundEventRespawnAnchorCharge
		pk.ExtraData = int32(so.Charge)
	case sound.RespawnAnchorDeplete:
		pk.SoundType = v2193.SoundEventRespawnAnchorDeplete
		pk.ExtraData = int32(so.Charge)
	case sound.GoatHorn:
		switch so.Horn {
		case sound.Ponder():
			pk.SoundType = v2193.SoundEventGoatCall0
		case sound.Sing():
			pk.SoundType = v2193.SoundEventGoatCall1
		case sound.Seek():
			pk.SoundType = v2193.SoundEventGoatCall2
		case sound.Feel():
			pk.SoundType = v2193.SoundEventGoatCall3
		case sound.Admire():
			pk.SoundType = v2193.SoundEventGoatCall4
		case sound.Call():
			pk.SoundType = v2193.SoundEventGoatCall5
		case sound.Yearn():
			pk.SoundType = v2193.SoundEventGoatCall6
		case sound.Dream():
			pk.SoundType = v2193.SoundEventGoatCall7
		}
	case sound.FireExtinguish:
		pk.SoundType = v2193.SoundEventExtinguishFire
	case sound.Ignite:
		pk.SoundType = v2193.SoundEventIgnite
	case sound.EnderEyePlaced:
		pk.SoundType = v2193.SoundEventEnderEyePlaced
	case sound.EndPortalCreated:
		pk.SoundType = v2193.SoundEventEndPortalCreated
	case sound.Burning:
		pk.SoundType = v2193.SoundEventPlayerHurtOnFire
	case sound.Drowning:
		pk.SoundType = v2193.SoundEventPlayerHurtDrown
	case sound.Fall:
		pk.EntityType = "minecraft:player"
		if so.Distance > 4 {
			pk.SoundType = v2193.SoundEventFallBig
			break
		}
		pk.SoundType = v2193.SoundEventFallSmall
	case sound.Burp:
		pk.SoundType = v2193.SoundEventBurp
	case sound.DoorOpen:
		pk.SoundType, pk.ExtraData = v2193.SoundEventDoorOpen, int32(networkBlockID(s.br, so.Block))
	case sound.DoorClose:
		pk.SoundType, pk.ExtraData = v2193.SoundEventDoorClose, int32(networkBlockID(s.br, so.Block))
	case sound.TrapdoorOpen:
		pk.SoundType, pk.ExtraData = v2193.SoundEventTrapdoorOpen, int32(networkBlockID(s.br, so.Block))
	case sound.TrapdoorClose:
		pk.SoundType, pk.ExtraData = v2193.SoundEventTrapdoorClose, int32(networkBlockID(s.br, so.Block))
	case sound.FenceGateOpen:
		pk.SoundType, pk.ExtraData = v2193.SoundEventFenceGateOpen, int32(networkBlockID(s.br, so.Block))
	case sound.FenceGateClose:
		pk.SoundType, pk.ExtraData = v2193.SoundEventFenceGateClose, int32(networkBlockID(s.br, so.Block))
	case sound.PistonIn:
		pk.SoundType = v2193.SoundEventPistonIn
	case sound.PistonOut:
		pk.SoundType = v2193.SoundEventPistonOut
	case sound.Deny:
		pk.SoundType = v2193.SoundEventDeny
	case sound.BlockPlace:
		pk.SoundType, pk.ExtraData = v2193.SoundEventPlace, int32(networkBlockID(s.br, so.Block))
	case sound.AnvilLand:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundAnvilLand,
			Position:  vec64To32(pos),
		})
		return
	case sound.AnvilUse:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundAnvilUsed,
			Position:  vec64To32(pos),
		})
		return
	case sound.AnvilBreak:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundAnvilBroken,
			Position:  vec64To32(pos),
		})
		return
	case sound.ChestClose:
		pk.SoundType = v2193.SoundEventChestClosed
	case sound.ChestOpen:
		pk.SoundType = v2193.SoundEventChestOpen
	case sound.EnderChestClose:
		pk.SoundType = v2193.SoundEventEnderChestClosed
	case sound.EnderChestOpen:
		pk.SoundType = v2193.SoundEventEnderChestOpen
	case sound.BarrelClose:
		pk.SoundType = v2193.SoundEventBarrelClose
	case sound.BarrelOpen:
		pk.SoundType = v2193.SoundEventBarrelOpen
	case sound.BellRing:
		pk.SoundType = v2193.SoundEventBell
	case sound.BlockBreaking:
		pk.SoundType, pk.ExtraData = v2193.SoundEventHit, int32(networkBlockID(s.br, so.Block))
	case sound.ItemBreak:
		pk.SoundType = v2193.SoundEventBreak
	case sound.ItemUseOn:
		pk.SoundType, pk.ExtraData = v2193.SoundEventItemUseOn, int32(networkBlockID(s.br, so.Block))
	case sound.BlockBounce:
		pk.SoundType, pk.ExtraData = v2193.SoundEventBounce, int32(networkBlockID(s.br, so.Block))
	case sound.GeyserEruptionStart:
		pk.SoundType = v2193.SoundEventGeyserEruptionStart
		if so.Continuous {
			pk.SoundType = v2193.SoundEventGeyserContinuousEruptionStart
		}
	case sound.MobGrowth:
		pk.SoundType = v2193.SoundEventResetGrowth
		if so.Paused {
			pk.SoundType = v2193.SoundEventPauseGrowth
		}
	case sound.FrogSpawnHatch:
		pk.SoundType = v2193.SoundEventFrogspawnHatched
	case sound.WindChargeImpact:
		pk.SoundType = v2193.SoundEventWindChargeBurst
	case sound.BreezeWindChargeBurst:
		pk.SoundType = v2193.SoundEventBreezeWindChargeBurst
	case sound.WindChargeShoot:
		pk.SoundType, pk.EntityType = v2193.SoundEventThrow, "minecraft:player"
	case sound.Fizz:
		pk.SoundType = v2193.SoundEventFizz
	case sound.PointedDripstoneCauldronDrip:
		pk.SoundType = v2193.SoundEventPointedDripstoneCauldronDripWater
		if so.Lava {
			pk.SoundType = v2193.SoundEventPointedDripstoneCauldronDripLava
		}
	case sound.BigDripleafTilt:
		pk.SoundType = v2193.SoundEventBigDripleafTiltDown
		if so.Up {
			pk.SoundType = v2193.SoundEventBigDripleafTiltUp
		}
	case sound.GlassBreak:
		pk.SoundType = v2193.SoundEventGlass
	case sound.Attack:
		pk.SoundType, pk.EntityType = v2193.SoundEventAttackStrong, "minecraft:player"
		if !so.Damage {
			pk.SoundType = v2193.SoundEventAttackNoDamage
		}
	case sound.BucketFill:
		if _, water := so.Liquid.(block.Water); water {
			pk.SoundType = v2193.SoundEventBucketFillWater
			break
		}
		pk.SoundType = v2193.SoundEventBucketFillLava
	case sound.BucketEmpty:
		if _, water := so.Liquid.(block.Water); water {
			pk.SoundType = v2193.SoundEventBucketEmptyWater
			break
		}
		pk.SoundType = v2193.SoundEventBucketEmptyLava
	case sound.BowShoot:
		pk.SoundType = v2193.SoundEventBow
	case sound.ShieldBlock:
		pk.SoundType = v2193.SoundEventShieldBlock
	case sound.CrossbowLoad:
		switch so.Stage {
		case sound.CrossbowLoadingStart:
			pk.SoundType = v2193.SoundEventCrossbowLoadingStart
			if so.QuickCharge {
				pk.SoundType = v2193.SoundEventCrossbowQuickChargeStart
			}
		case sound.CrossbowLoadingMiddle:
			pk.SoundType = v2193.SoundEventCrossbowLoadingMiddle
			if so.QuickCharge {
				pk.SoundType = v2193.SoundEventCrossbowQuickChargeMiddle
			}
		case sound.CrossbowLoadingEnd:
			pk.SoundType = v2193.SoundEventCrossbowLoadingEnd
			if so.QuickCharge {
				pk.SoundType = v2193.SoundEventCrossbowQuickChargeEnd
			}
		default:
			// Invalid stages should not crash the server; skip the sound instead.
			return
		}
	case sound.CrossbowShoot:
		pk.SoundType = v2193.SoundEventCrossbowShoot
	case sound.ArrowHit:
		pk.SoundType = v2193.SoundEventBowHit
	case sound.TridentThrow:
		pk.SoundType = v2193.SoundEventTridentThrow
	case sound.TridentHit:
		pk.SoundType = v2193.SoundEventTridentHit
	case sound.TridentReturn:
		pk.SoundType = v2193.SoundEventTridentReturn
	case sound.TridentChanneling:
		pk.SoundType = v2193.SoundEventTridentThunder
	case sound.TridentRiptide:
		switch so.Level {
		case 3:
			pk.SoundType = v2193.SoundEventTridentRiptide3
		case 2:
			pk.SoundType = v2193.SoundEventTridentRiptide2
		default:
			pk.SoundType = v2193.SoundEventTridentRiptide1
		}
	case sound.ItemThrow:
		pk.SoundType, pk.EntityType = v2193.SoundEventThrow, "minecraft:player"
	case sound.LevelUp:
		pk.SoundType, pk.ExtraData = v2193.SoundEventLevelUp, 0x10000000
	case sound.Experience:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundExperienceOrbPickup,
			Position:  vec64To32(pos),
		})
		return
	case sound.MusicDiscPlay:
		switch so.DiscType {
		case sound.Disc13():
			pk.SoundType = v2193.SoundEventRecord13
		case sound.DiscCat():
			pk.SoundType = v2193.SoundEventRecordCat
		case sound.DiscBlocks():
			pk.SoundType = v2193.SoundEventRecordBlocks
		case sound.DiscChirp():
			pk.SoundType = v2193.SoundEventRecordChirp
		case sound.DiscFar():
			pk.SoundType = v2193.SoundEventRecordFar
		case sound.DiscMall():
			pk.SoundType = v2193.SoundEventRecordMall
		case sound.DiscMellohi():
			pk.SoundType = v2193.SoundEventRecordMellohi
		case sound.DiscStal():
			pk.SoundType = v2193.SoundEventRecordStal
		case sound.DiscStrad():
			pk.SoundType = v2193.SoundEventRecordStrad
		case sound.DiscWard():
			pk.SoundType = v2193.SoundEventRecordWard
		case sound.Disc11():
			pk.SoundType = v2193.SoundEventRecord11
		case sound.DiscWait():
			pk.SoundType = v2193.SoundEventRecordWait
		case sound.DiscOtherside():
			pk.SoundType = v2193.SoundEventRecordOtherside
		case sound.DiscPigstep():
			pk.SoundType = v2193.SoundEventRecordPigstep
		case sound.Disc5():
			pk.SoundType = v2193.SoundEventRecord5
		case sound.DiscRelic():
			pk.SoundType = v2193.SoundEventRecordRelic
		case sound.DiscCreator():
			pk.SoundType = v2193.SoundEventRecordCreator
		case sound.DiscCreatorMusicBox():
			pk.SoundType = v2193.SoundEventRecordCreatorMusicBox
		case sound.DiscPrecipice():
			pk.SoundType = v2193.SoundEventRecordPrecipice
		case sound.DiscTears():
			pk.SoundType = v2193.SoundEventRecordTears
		case sound.DiscLavaChicken():
			pk.SoundType = v2193.SoundEventRecordLavaChicken
		default:
			// Unknown disc types should not crash the server; skip the sound instead.
			return
		}
	case sound.MusicDiscEnd:
		pk.SoundType = v2193.SoundEventRecordNull
	case sound.FireCharge:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundBlazeFireball,
			Position:  vec64To32(pos),
		})
		return
	case sound.ConduitAttack:
		pk.SoundType = v2193.SoundEventConduitAttack
	case sound.ComposterEmpty:
		pk.SoundType = v2193.SoundEventComposterEmpty
	case sound.BottleFill:
		pk.SoundType = v2193.SoundEventBottleFill
	case sound.ComposterFill:
		pk.SoundType = v2193.SoundEventComposterFill
	case sound.ComposterFillLayer:
		pk.SoundType = v2193.SoundEventComposterFillLayer
	case sound.ComposterReady:
		pk.SoundType = v2193.SoundEventComposterReady
	case sound.PowerOn:
		pk.SoundType = v2193.SoundEventPowerOn
	case sound.PowerOff:
		pk.SoundType = v2193.SoundEventPowerOff
	case sound.LecternBookPlace:
		pk.SoundType = v2193.SoundEventLecternBookPlace
	case sound.Totem:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundTotemUsed,
			Position:  vec64To32(pos),
		})
		return
	case sound.ShulkerBoxClose:
		pk.SoundType = v2193.SoundEventShulkerBoxClosed
	case sound.ShulkerBoxOpen:
		pk.SoundType = v2193.SoundEventShulkerBoxOpen
	case sound.DecoratedPotInserted:
		out(&v2193.PlaySoundPacket{
			SoundName: "block.decorated_pot.insert",
			Position:  vec64To32(pos),
			Volume:    1,
			Pitch:     0.7 + 0.5*float32(so.Progress),
		})
		return
	case sound.DecoratedPotInsertFailed:
		pk.SoundType = v2193.SoundEventDecoratedPotInsertFail
	case sound.Custom:
		name, ok := bedrockSoundName(so.Name)
		if !ok {
			return
		}
		out(&v2193.PlaySoundPacket{
			SoundName: name,
			Position:  vec64To32(pos),
			Volume:    float32(so.Volume),
			Pitch:     float32(so.Pitch),
		})
		return
	case sound.LightningExplode:
		out(&v2193.PlaySoundPacket{
			SoundName: "ambient.weather.lightning.impact",
			Position:  vec64To32(pos),
			Volume:    1,
			Pitch:     0.7,
		})
		return
	case sound.LightningThunder:
		out(&v2193.PlaySoundPacket{
			SoundName: "ambient.weather.thunder",
			Position:  vec64To32(pos),
			Volume:    1,
			Pitch:     1.0,
		})
		return
	case sound.ArmourStandHit:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundArmorStandHit,
			Position:  vec64To32(pos),
		})
		return
	case sound.ArmourStandBreak:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundArmorStandBreak,
			Position:  vec64To32(pos),
		})
		return
	case sound.ArmourStandPlace:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundArmorStandPlace,
			Position:  vec64To32(pos),
		})
		return
	case sound.ArmourStandLand:
		out(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventSoundArmorStandLand,
			Position:  vec64To32(pos),
		})
		return
	}
	out(pk)
}

// PlaySound plays a world.Sound to the client. The volume is not dependent on the distance to the source if it is a
// sound of the LevelSoundEvent packet.
func (s *Session) PlaySound(t world.Sound, pos mgl64.Vec3) {
	if s == Nop {
		return
	}
	s.playSound(pos, t, true)
}

// ViewSound ...
func (s *Session) ViewSound(pos mgl64.Vec3, soundType world.Sound) {
	if s.dropCosmetic() {
		return
	}
	s.playSound(pos, soundType, false)
}

// OpenSign ...
func (s *Session) OpenSign(pos cube.Pos, frontSide bool) {
	blockPos := v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&v2193.OpenSignPacket{
		Position:  blockPos,
		FrontSide: frontSide,
	})
}

// ViewFurnaceUpdate updates a furnace for the associated session based on previous times.
// Fuel values are UI-scaled and use the per-item cook requirement as the duration baseline.
func (s *Session) ViewFurnaceUpdate(prevCookTime, cookTime, prevRemainingFuelTime, remainingFuelTime, prevMaxFuelTime, maxFuelTime time.Duration) {
	if prevCookTime != cookTime {
		s.writePacket(&v2193.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      v2193.ContainerDataFurnaceTickCount,
			Value:    int32(cookTime.Milliseconds() / 50),
		})
	}

	if prevRemainingFuelTime != remainingFuelTime {
		s.writePacket(&v2193.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      v2193.ContainerDataFurnaceLitTime,
			Value:    int32(remainingFuelTime.Milliseconds() / 50),
		})
	}

	if prevMaxFuelTime != maxFuelTime {
		s.writePacket(&v2193.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      v2193.ContainerDataFurnaceLitDuration,
			Value:    int32(maxFuelTime.Milliseconds() / 50),
		})
	}
}

// ViewBrewingUpdate updates a brewing stand for the associated session based on previous times.
func (s *Session) ViewBrewingUpdate(prevBrewTime, brewTime time.Duration, prevFuelAmount, fuelAmount, prevFuelTotal, fuelTotal int32) {
	if prevBrewTime != brewTime {
		s.writePacket(&v2193.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      v2193.ContainerDataBrewingStandBrewTime,
			Value:    int32(brewTime.Milliseconds() / 50),
		})
	}

	if prevFuelAmount != fuelAmount {
		s.writePacket(&v2193.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      v2193.ContainerDataBrewingStandFuelAmount,
			Value:    fuelAmount,
		})
	}

	if prevFuelTotal != fuelTotal {
		s.writePacket(&v2193.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      v2193.ContainerDataBrewingStandFuelTotal,
			Value:    fuelTotal,
		})
	}
}

// ViewEntityAction ...
func (s *Session) ViewEntityAction(e world.Entity, a world.EntityAction) {
	switch act := a.(type) {
	case entity.SwingArmAction:
		if _, ok := e.(Controllable); ok {
			if s.entityRuntimeID(e) == selfEntityRuntimeID && s.swingingArm.Load() {
				return
			}
			s.writePacket(&v2193.AnimatePacket{
				ActionType:      v2193.AnimateActionSwingArm,
				EntityRuntimeID: s.entityRuntimeID(e),
			})
			return
		}
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventStartAttacking,
		})
	case entity.HurtAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventHurt,
		})
		s.viewMobHealth(e)
	case entity.CriticalHitAction:
		if act.Count <= 0 {
			act.Count = 55
		}
		s.writePacket(&v2193.AnimatePacket{
			ActionType:      v2193.AnimateActionCriticalHit,
			EntityRuntimeID: s.entityRuntimeID(e),
			Data:            float32(act.Count),
		})
	case entity.EnchantedHitAction:
		if act.Count <= 0 {
			act.Count = 15
		}
		s.writePacket(&v2193.AnimatePacket{
			ActionType:      v2193.AnimateActionMagicCriticalHit,
			EntityRuntimeID: s.entityRuntimeID(e),
			Data:            float32(act.Count),
		})
	case entity.DragonDeathAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventDragonStartDeathAnim,
		})
	case entity.DeathAction:
		s.entityMutex.Lock()
		id, visible := s.entityRuntimeIDs[e.H()]
		if visible && clientRemovesDyingMob(e) {
			if s.deathAnimations == nil {
				s.deathAnimations = make(map[uint64]struct{})
			}
			s.deathAnimations[id] = struct{}{}
		}
		s.entityMutex.Unlock()
		if !visible {
			return
		}
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: id,
			EventType:       v2193.ActorEventDeath,
		})
	case entity.WitchMagicAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventWitchHatMagic,
		})
	case entity.SilverfishMergeAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventSilverfishMergeAnimation,
		})
	case entity.PickedUpAction:
		s.writePacket(&v2193.TakeItemActorPacket{
			ItemEntityRuntimeID:  s.entityRuntimeID(e),
			TakerEntityRuntimeID: s.entityRuntimeID(act.Collector),
		})
	case entity.ItemStackSizeAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventUpdateStackSize,
			EventData:       int32(act.Count),
		})
	case entity.WolfShakeAction:
		s.writePacket(&v2193.ActorEventPacket{EntityRuntimeID: s.entityRuntimeID(e), EventType: v2193.ActorEventShakeWetness})
	case entity.VibrationDetectedAction:
		s.writePacket(&v2193.ActorEventPacket{EntityRuntimeID: s.entityRuntimeID(e), EventType: v2193.ActorEventVibrationDetected})
	case entity.SquidInkAction:
		s.writePacket(&v2193.ActorEventPacket{EntityRuntimeID: s.entityRuntimeID(e), EventType: v2193.ActorEventSquidFleeing})
	case entity.ArrowShakeAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventShake,
			EventData:       int32(act.Duration.Milliseconds() / 50),
		})
	case entity.FireworkExplosionAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventFireworksExplode,
		})
	case entity.EatAction:
		if user, ok := e.(item.User); ok {
			held, _ := user.HeldItems()
			it := held.Item()
			if held.Empty() {
				// This can happen sometimes if the user switches between items very quickly, so just ignore the action.
				return
			}
			if _, ok := it.(item.Consumable); !ok {
				// Not consumable, refer to the comment above.
				return
			}
			rid, meta, _ := world.ItemRuntimeID(it)
			s.writePacket(&v2193.ActorEventPacket{
				EntityRuntimeID: s.entityRuntimeID(e),
				EventType:       v2193.ActorEventFeed,
				// It's a little weird how the runtime ID is still shifted 16 bits to the left here, given the
				// runtime ID already includes the meta, but it seems to work.
				EventData: (rid << 16) | int32(meta),
			})
		}
	case entity.TotemUseAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventTalismanActivate,
		})
	case entity.FishHookBubbleAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventFishhookBubble,
		})
	case entity.FishHookBiteAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventFishhookHookTime,
		})
	case entity.FishHookTeaseAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventFishhookTease,
		})
	case entity.TameAction:
		event := byte(v2193.ActorEventTamingFailed)
		if act.Succeeded {
			event = v2193.ActorEventTamingSucceeded
		}
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       event,
		})
	case entity.DrinkAction:
		event := byte(v2193.ActorEventDrinkPotion)
		if act.Milk {
			event = v2193.ActorEventDrinkMilk
		}
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       event,
		})
	case entity.EatGrassAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventEatGrass,
		})
	case entity.LoveHeartsAction:
		s.writePacket(&v2193.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       v2193.ActorEventInLoveHearts,
		})
	}
}

// clientRemovesDyingMob excludes players and entities with special death
// sequences. Ordinary Bedrock mobs remove themselves at the end of tickDeath.
func clientRemovesDyingMob(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:player", "minecraft:ender_dragon", "minecraft:wither", "minecraft:creaking":
		return false
	}
	_, living := e.(entity.Living)
	return living
}

// ViewEntityState ...
func (s *Session) ViewEntityState(e world.Entity) {
	s.writePacket(&v2193.SetActorDataPacket{
		EntityRuntimeID:  s.entityRuntimeID(e),
		EntityMetadata:   toMetadata(s.entityMetadata(e)),
		EntityProperties: entityProperties(e),
	})
	if _, mount := e.(interface{ MountState() entity.MountState }); mount {
		// Riders see the health of their mount, which feeding restores.
		s.viewMobHealth(e)
	}
}

// entityMetadata returns the metadata of an entity as viewed by the session, including any overrides
// applied through its ViewLayer.
func (s *Session) entityMetadata(e world.Entity) v2193.Metadata {
	metadata := s.parseEntityMetadata(e)
	if s.viewLayer == nil {
		return metadata
	}
	nt, ntSet := s.viewLayer.NameTag(e)
	as, asSet := s.viewLayer.AlwaysShowNameTag(e)
	if ntSet || asSet {
		nameTag, alwaysShow, _ := nameTagState(e)
		if ntSet {
			nameTag = nt
		}
		if asSet {
			alwaysShow = as
		}
		writeNameTagMetadata(metadata, nameTag, alwaysShow)
	}
	if st, ok := s.viewLayer.ScoreTag(e); ok {
		metadata[v2193.EntityDataKeyScore] = st
	}
	if visibility := s.viewLayer.Visibility(e); visibility.EnforceVisibility() {
		if visibility == world.EnforceInvisible() {
			metadata.SetFlag(v2193.EntityDataKeyFlags, v2193.EntityDataFlagInvisible)
		} else {
			metadata.UnsetFlag(v2193.EntityDataKeyFlags, v2193.EntityDataFlagInvisible)
		}
	}
	return metadata
}

// ViewEntityAnimation ...
func (s *Session) ViewEntityAnimation(e world.Entity, a world.EntityAnimation) {
	if s.dropCosmetic() {
		return
	}
	s.writePacket(animationPacket(s.entityRuntimeID(e), a))
}

// animationPacket returns the packet that plays an animation on the entity
// of a runtime ID.
func animationPacket(id uint64, a world.EntityAnimation) *v2193.AnimateEntityPacket {
	return &v2193.AnimateEntityPacket{
		Animation:        a.Name(),
		NextState:        a.NextState(),
		StopCondition:    a.StopCondition(),
		Controller:       a.Controller(),
		EntityRuntimeIDs: []uint64{id},
	}
}

// ViewSoundShared ...
func (s *Session) ViewSoundShared(pos mgl64.Vec3, t world.Sound, b *world.Broadcast) {
	if s.dropCosmetic() {
		return
	}
	s.writeBroadcast(b, func(out func(proto.Packet)) { s.soundPackets(pos, t, false, out) })
}

// ViewEntityAnimationShared ...
func (s *Session) ViewEntityAnimationShared(e world.Entity, a world.EntityAnimation, b *world.Broadcast) {
	if s.dropCosmetic() {
		return
	}
	if e.H() == s.ent {
		// The player of the session knows itself by another runtime ID.
		s.ViewEntityAnimation(e, a)
		return
	}
	s.writeBroadcast(b, func(out func(proto.Packet)) { out(animationPacket(e.H().RuntimeID(), a)) })
}

// writeBroadcast writes the packets of a broadcast: those the first viewer
// made, encoded once for all, or makes them with build.
func (s *Session) writeBroadcast(b *world.Broadcast, build func(out func(proto.Packet))) {
	pks, ok := b.Load().([]*sharedPacket)
	if !ok {
		build(func(pk proto.Packet) { pks = append(pks, &sharedPacket{Packet: pk}) })
		b.Store(pks)
	}
	for _, pk := range pks {
		s.writePacket(pk)
	}
}

// OpenBlockContainer ...
func (s *Session) OpenBlockContainer(pos cube.Pos, tx *world.Tx) {
	if s.containerOpened.Load() && *s.openedPos.Load() == pos {
		return
	}
	s.closeCurrentContainer(tx, false)

	b := tx.Block(pos)
	if container, ok := b.(block.Container); ok {
		s.openNormalContainer(container, pos, tx)
		return
	}
	// We hit a special kind of window like beacons, which are not actually opened server-side.
	nextID := s.nextWindowID()
	// The client does not send a ContainerClose packet for lecterns.
	_, lectern := b.(block.Lectern)
	if !lectern {
		s.containerOpened.Store(true)
		inv := inventory.New(1, nil)
		s.openedWindow.Store(inv)
		s.openedPos.Store(&pos)
	}

	var containerType byte
	switch b := b.(type) {
	case block.CraftingTable:
		containerType = v2193.ContainerTypeWorkbench
	case block.EnchantingTable:
		containerType = v2193.ContainerTypeEnchantment
	case block.Anvil:
		containerType = v2193.ContainerTypeAnvil
	case block.Beacon:
		containerType = v2193.ContainerTypeBeacon
	case block.Lectern:
		containerType = v2193.ContainerTypeLectern
	case block.Loom:
		containerType = v2193.ContainerTypeLoom
	case block.Grindstone:
		containerType = v2193.ContainerTypeGrindstone
	case block.CartographyTable:
		containerType = v2193.ContainerTypeCartography
	case block.Stonecutter:
		containerType = v2193.ContainerTypeStonecutter
	case block.SmithingTable:
		containerType = v2193.ContainerTypeSmithingTable
	case block.EnderChest:
		b.AddViewer(tx, pos)

		s.openedWindow.Store(s.enderChest)
		defer s.sendInv(s.enderChest, uint32(nextID))
	}

	s.openedContainerID.Store(uint32(containerType))
	s.writePacket(&v2193.ContainerOpenPacket{
		WindowID:                nextID,
		ContainerType:           containerType,
		ContainerPosition:       v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		ContainerEntityUniqueID: -1,
	})
}

// OpenEntityContainer opens an entity-backed container inventory.
func (s *Session) OpenEntityContainer(e world.Entity, inv *inventory.Inventory, containerType byte, tx *world.Tx) {
	if s.containerOpened.Load() {
		if opened := s.openedEntity.Load(); opened != nil && opened == e.H() {
			return
		}
	}
	s.closeCurrentContainer(tx, false)

	if viewer, ok := e.(interface {
		AddViewer(v block.ContainerViewer)
	}); ok {
		viewer.AddViewer(s)
	}

	nextID := s.nextWindowID()
	s.containerOpened.Store(true)
	s.openedWindow.Store(inv)
	s.openedEntity.Store(e.H())
	pos := e.Position()
	openPos := cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}
	s.openedPos.Store(&openPos)
	s.openedContainerID.Store(uint32(containerType))

	s.writePacket(&v2193.ContainerOpenPacket{
		WindowID:                nextID,
		ContainerType:           containerType,
		ContainerPosition:       v2193.BlockPos{int32(openPos[0]), int32(openPos[1]), int32(openPos[2])},
		ContainerEntityUniqueID: s.entityUniqueID(e),
	})
	s.sendInv(inv, uint32(nextID))
}

// OpenMerchant opens the Bedrock merchant UI for an entity and sends its current offers.
func (s *Session) OpenMerchant(e world.Entity, merchant entity.Merchant, tx *world.Tx) {
	if s.containerOpened.Load() {
		if opened := s.openedEntity.Load(); opened != nil && opened == e.H() {
			return
		}
	}
	user, ok := s.ent.Entity(tx)
	if !ok {
		return
	}
	offers, err := merchantOffersFor(merchant, user)
	if err != nil {
		s.notifyMerchantError(user, "failed to load merchant offers", err)
		return
	}
	serializedOffers, err := serializeMerchantOffers(offers)
	if err != nil {
		s.notifyMerchantError(user, "failed to serialize merchant offers", err)
		return
	}
	s.closeCurrentContainer(tx, false)
	if trader, ok := merchant.(entity.TradingMerchant); ok && !trader.StartTrading(s.ent) {
		return
	}
	nextID := s.nextWindowID()
	pos := e.Position()
	openPos := cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}
	s.containerOpened.Store(true)
	s.openedWindow.Store(inventory.New(1, nil))
	s.openedEntity.Store(e.H())
	s.openedPos.Store(&openPos)
	s.openedContainerID.Store(uint32(v2193.ContainerTypeTrade))
	s.writePacket(&v2193.ContainerOpenPacket{
		WindowID:                nextID,
		ContainerType:           v2193.ContainerTypeTrade,
		ContainerPosition:       v2193.BlockPos{int32(openPos[0]), int32(openPos[1]), int32(openPos[2])},
		ContainerEntityUniqueID: s.entityUniqueID(e),
	})
	s.sendMerchantOffers(e, merchant, user, offers, serializedOffers)
}

func (s *Session) sendMerchantOffers(e world.Entity, merchant entity.Merchant, user world.Entity, offers []entity.MerchantOffer, serializedOffers []byte) {
	level := merchant.MerchantLevel() - 1
	if level < 0 {
		level = 0
	}
	s.writePacket(&v2193.UpdateTradePacket{
		WindowID:          byte(s.openedWindowID.Load()),
		WindowType:        15,
		Size:              int32(len(offers)),
		TradeTier:         int32(level),
		VillagerUniqueID:  s.entityUniqueID(e),
		EntityUniqueID:    s.entityUniqueID(user),
		DisplayName:       merchant.MerchantName(),
		NewTradeUI:        true,
		DemandBasedPrices: true,
		SerialisedOffers:  serializedOffers,
	})
}

func merchantOffersFor(merchant entity.Merchant, user world.Entity) ([]entity.MerchantOffer, error) {
	return merchant.MerchantOffersFor(user)
}

func (s *Session) notifyMerchantError(user world.Entity, message string, err error) {
	if s.conf.Log != nil {
		s.conf.Log.Error(message, "err", err)
	}
	if messenger, ok := user.(interface{ Message(...any) }); ok {
		messenger.Message("Merchant offers are temporarily unavailable.")
	}
}

func serializeMerchantOffers(offers []entity.MerchantOffer) ([]byte, error) {
	recipes := make([]any, len(offers))
	for i, offer := range offers {
		rewardXP := byte(0)
		if offer.RewardXP {
			rewardXP = 1
		}
		// The client shows buyA as priced and names the offer by netId, the
		// recipe network ID its trade request carries.
		recipes[i] = map[string]any{
			"netId":            int32(i + 1),
			"buyA":             item.WriteNBT(merchantCostA(offer), true),
			"buyB":             item.WriteNBT(offer.SecondaryInput, true),
			"buyCountA":        int32(offer.Input.Count()),
			"buyCountB":        int32(offer.SecondaryInput.Count()),
			"sell":             item.WriteNBT(offer.Output, true),
			"uses":             int32(offer.Uses),
			"maxUses":          int32(offer.MaxUses),
			"rewardExp":        rewardXP,
			"tier":             int32(offer.Tier),
			"traderExp":        int32(offer.XP),
			"priceMultiplierA": offer.PriceMultiplier,
			"priceMultiplierB": float32(0),
			"demand":           int32(offer.Demand),
		}
	}
	// The experience a villager needs for each level, as
	// VillagerData.NEXT_LEVEL_XP_THRESHOLDS.
	tiers := []any{
		map[string]any{"0": int32(0)}, map[string]any{"1": int32(10)}, map[string]any{"2": int32(70)},
		map[string]any{"3": int32(150)}, map[string]any{"4": int32(250)},
	}
	var buf bytes.Buffer
	if err := nbt.NewEncoderWithEncoding(&buf, nbt.NetworkLittleEndian).Encode(map[string]any{"Recipes": recipes, "TierExpRequirements": tiers}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// openNormalContainer opens a normal container that can hold items in it server-side.
func (s *Session) openNormalContainer(b block.Container, pos cube.Pos, tx *world.Tx) {
	b.AddViewer(s, tx, pos) // Paired chests might update the block here.
	b = tx.Block(pos).(block.Container)

	nextID := s.nextWindowID()
	s.containerOpened.Store(true)
	inv := b.Inventory(tx, pos)
	s.openedWindow.Store(inv)
	s.openedPos.Store(&pos)

	var containerType byte
	switch b.(type) {
	case block.Furnace:
		containerType = v2193.ContainerTypeFurnace
	case block.BrewingStand:
		containerType = v2193.ContainerTypeBrewingStand
	case block.BlastFurnace:
		containerType = v2193.ContainerTypeBlastFurnace
	case block.Smoker:
		containerType = v2193.ContainerTypeSmoker
	case block.Hopper:
		containerType = v2193.ContainerTypeHopper
	case block.Crafter:
		containerType = v2193.ContainerTypeCrafter
	}
	s.openedContainerID.Store(uint32(containerType))

	s.writePacket(&v2193.ContainerOpenPacket{
		WindowID:                nextID,
		ContainerType:           containerType,
		ContainerPosition:       v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		ContainerEntityUniqueID: -1,
	})
	s.sendInv(b.Inventory(tx, pos), uint32(nextID))
	if crafter, ok := b.(block.Crafter); ok {
		for slot := range 9 {
			s.ViewCrafterSlotState(slot, crafter.DisabledSlots&(1<<slot) != 0)
		}
		s.ViewCrafterTriggered(crafter.Triggered)
	}
}

// ViewSlotChange ...
func (s *Session) ViewSlotChange(slot int, newItem item.Stack) {
	if !s.containerOpened.Load() {
		return
	}
	if s.inTransaction.Load() {
		// Don't send slot changes to the player itself.
		return
	}
	s.writePacket(&v2193.InventorySlotPacket{
		WindowID: s.openedWindowID.Load(),
		Slot:     uint32(slot),
		NewItem:  instanceFromItem(s.br, newItem),
	})
}

// ViewCrafterSlotState sends the disabled/enabled state for one native Crafter slot.
func (s *Session) ViewCrafterSlotState(slot int, disabled bool) {
	if !s.containerOpened.Load() || s.openedContainerID.Load() != uint32(v2193.ContainerTypeCrafter) {
		return
	}
	value := int32(0)
	if disabled {
		value = 1
	}
	s.writePacket(&v2193.ContainerSetDataPacket{WindowID: byte(s.openedWindowID.Load()), Key: int32(slot), Value: value})
}

// ViewCrafterTriggered sends the native Crafter's redstone data property.
func (s *Session) ViewCrafterTriggered(triggered bool) {
	if !s.containerOpened.Load() || s.openedContainerID.Load() != uint32(v2193.ContainerTypeCrafter) {
		return
	}
	value := int32(0)
	if triggered {
		value = 1
	}
	s.writePacket(&v2193.ContainerSetDataPacket{WindowID: byte(s.openedWindowID.Load()), Key: 9, Value: value})
}

// ViewBlockAction ...
func (s *Session) ViewBlockAction(pos cube.Pos, a world.BlockAction) {
	blockPos := v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	switch t := a.(type) {
	case world.PositionTrackingUpdateAction:
		s.writePacket(&v2193.PositionTrackingDBServerBroadcastPacket{
			BroadcastAction: v2193.PositionTrackingDBBroadcastActionUpdate,
			TrackingID:      t.Handle,
			Payload:         positionTrackingPayload(t.Handle, t.Position, t.Dimension, positionTrackingStatusTracked),
		})
	case world.PositionTrackingDestroyAction:
		s.writePacket(&v2193.PositionTrackingDBServerBroadcastPacket{
			BroadcastAction: v2193.PositionTrackingDBBroadcastActionDestroy,
			TrackingID:      t.Handle,
			Payload:         positionTrackingPayload(t.Handle, cube.Pos{}, 0, positionTrackingStatusMissing),
		})
	case block.OpenAction:
		s.writePacket(&v2193.BlockEventPacket{
			Position:  blockPos,
			EventType: v2193.BlockEventChangeChestState,
			EventData: 1,
		})
	case block.CloseAction:
		s.writePacket(&v2193.BlockEventPacket{
			Position:  blockPos,
			EventType: v2193.BlockEventChangeChestState,
		})
	case block.StartCrackAction:
		if t.BreakTime <= 0 {
			// An instant break has no cracking to animate, and encoding the crack speed would divide by zero.
			break
		}
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventStartBlockCracking,
			Position:  vec64To32(pos.Vec3()),
			EventData: int32(65535 / (t.BreakTime.Seconds() * 20)),
		})
	case block.StopCrackAction:
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventStopBlockCracking,
			Position:  vec64To32(pos.Vec3()),
			EventData: 0,
		})
	case block.ContinueCrackAction:
		if t.BreakTime <= 0 {
			break
		}
		s.writePacket(&v2193.LevelEventPacket{
			EventType: v2193.LevelEventUpdateBlockCracking,
			Position:  vec64To32(pos.Vec3()),
			EventData: int32(65535 / (t.BreakTime.Seconds() * 20)),
		})
	case block.DecoratedPotWobbleAction:
		nbt := clientActorNBT(t.DecoratedPot.EncodeNBT(), pos)
		nbt["animation"] = boolByte(t.Success) + 1
		s.writePacket(&v2193.BlockActorDataPacket{
			Position: blockPos,
			NBTData:  nbt,
		})
	}
}

// ViewEmote ...
func (s *Session) ViewEmote(player world.Entity, emote uuid.UUID) {
	flags := byte(v2193.EmoteFlagServerSide)
	if s.emoteChatMuted {
		flags |= v2193.EmoteFlagMuteChat
	}
	s.writePacket(&v2193.EmotePacket{
		EntityRuntimeID: s.entityRuntimeID(player),
		EmoteID:         emote.String(),
		Flags:           flags,
	})
}

// ViewSkin ...
func (s *Session) ViewSkin(e world.Entity) {
	if v, ok := e.(Controllable); ok {
		s.writePacket(&v2193.PlayerSkinPacket{
			UUID: v.UUID(),
			Skin: skinToProtocol(v.Skin()),
		})
	}
}

// ViewWorldSpawn ...
func (s *Session) ViewWorldSpawn(pos cube.Pos) {
	blockPos := v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&v2193.SetSpawnPositionPacket{
		SpawnType:     v2193.SpawnTypeWorld,
		Position:      blockPos,
		Dimension:     v2193.DimensionOverworld,
		SpawnPosition: blockPos,
	})
}

// SetPlayerSpawn updates the player's personal respawn location.
func (s *Session) SetPlayerSpawn(pos cube.Pos, dim world.Dimension) {
	if s == nil {
		return
	}
	id, _ := world.DimensionID(dim)
	blockPos := v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&v2193.SetSpawnPositionPacket{
		SpawnType:     v2193.SpawnTypePlayer,
		Position:      blockPos,
		Dimension:     int32(id),
		SpawnPosition: blockPos,
	})
}

// ViewWeather ...
func (s *Session) ViewWeather(raining, thunder bool) {
	pk := &v2193.LevelEventPacket{
		EventType: v2193.LevelEventStopRaining,
	}
	if raining {
		pk.EventType, pk.EventData = v2193.LevelEventStartRaining, int32(rand.IntN(50000)+10000)
	}
	s.writePacket(pk)

	pk = &v2193.LevelEventPacket{
		EventType: v2193.LevelEventStopThunderstorm,
	}
	if thunder {
		pk.EventType, pk.EventData = v2193.LevelEventStartThunderstorm, int32(rand.IntN(50000)+10000)
	}
	s.writePacket(pk)
}

// ViewEntityWake ...
func (s *Session) ViewEntityWake(e world.Entity) {
	s.writePacket(&v2193.AnimatePacket{
		EntityRuntimeID: s.entityRuntimeID(e),
		ActionType:      v2193.AnimateActionStopSleep,
	})
}

// nextWindowID produces the next window ID for a new window. It is an int of 1-99.
func (s *Session) nextWindowID() byte {
	if s.openedWindowID.CompareAndSwap(99, 1) {
		return 1
	}
	return byte(s.openedWindowID.Add(1))
}

// closeWindow closes the container window currently opened. If no window is open, closeWindow will do
// nothing.
func (s *Session) closeWindow(clientRequested bool) bool {
	if !s.containerOpened.CompareAndSwap(true, false) {
		return false
	}
	containerType := byte(s.openedContainerID.Load())
	windowID := byte(s.openedWindowID.Load())
	s.openedContainerID.Store(0)
	s.openedWindow.Store(inventory.New(1, nil))
	s.openedEntity.Store(nil)
	if !clientRequested {
		s.writePacket(&v2193.ContainerClosePacket{
			WindowID:      windowID,
			ContainerType: containerType,
			ServerSide:    true,
		})
	}
	return true
}

func (s *Session) entityLinks(e world.Entity) []v2193.EntityLink {
	var links []entity.Link
	if linker, ok := e.(entity.Linker); ok {
		links = append(links, linker.Links()...)
	}
	if rider, ok := e.(entity.Rider); ok {
		if riding := rider.Riding(); riding != nil {
			links = append(links, entity.Link{
				Ridden:         riding,
				Rider:          e.H(),
				Type:           entity.LinkRider,
				RiderInitiated: true,
			})
		}
	}
	if len(links) == 0 {
		return nil
	}
	out := make([]v2193.EntityLink, 0, len(links))
	for _, link := range links {
		if link.Ridden == nil || link.Rider == nil {
			continue
		}
		riddenID := s.handleRuntimeID(link.Ridden)
		riderID := s.handleRuntimeID(link.Rider)
		if riddenID == 0 || riderID == 0 {
			continue
		}
		linkType := byte(v2193.EntityLinkRemove)
		switch link.Type {
		case entity.LinkRider:
			linkType = byte(v2193.EntityLinkRider)
		case entity.LinkPassenger:
			linkType = byte(v2193.EntityLinkPassenger)
		}
		out = append(out, v2193.EntityLink{
			RiddenEntityUniqueID:   s.handleUniqueID(link.Ridden),
			RiderEntityUniqueID:    s.handleUniqueID(link.Rider),
			Type:                   linkType,
			Immediate:              link.Immediate,
			RiderInitiated:         link.RiderInitiated,
			VehicleAngularVelocity: link.VehicleAngularVelocity,
		})
	}
	return out
}

// entityRuntimeID returns the runtime ID of the entity passed, whether or not
// it is shown to the session: the client leaves out what packets say of an
// actor it does not know.
func (s *Session) entityRuntimeID(e world.Entity) uint64 {
	return s.runtimeIDOf(e.H())
}

// runtimeIDOf returns the runtime ID the session knows the entity of the
// handle passed by: selfEntityRuntimeID for its own player, the runtime ID of
// the handle for any other.
func (s *Session) runtimeIDOf(h *world.EntityHandle) uint64 {
	if h == s.ent {
		return selfEntityRuntimeID
	}
	return h.RuntimeID()
}

// handleRuntimeID returns the runtime ID of the entity of the handle passed
// if it is shown to the session, or 0 if it is not.
func (s *Session) handleRuntimeID(e *world.EntityHandle) uint64 {
	s.entityMutex.RLock()
	defer s.entityMutex.RUnlock()

	if id, ok := s.entityRuntimeIDs[e]; ok {
		return id
	}
	s.conf.Log.Debug("entity runtime ID not found", "UUID", e.UUID().String())
	return 0
}

// entityFromRuntimeID attempts to return an entity by its runtime ID. False is returned if no entity with the
// ID could be found.
func (s *Session) entityFromRuntimeID(id uint64) (*world.EntityHandle, bool) {
	s.entityMutex.RLock()
	e, ok := s.entities[id]
	s.entityMutex.RUnlock()
	return e, ok
}

// vec32To64 converts a mgl32.Vec3 to a mgl64.Vec3.
func vec32To64(vec3 mgl32.Vec3) mgl64.Vec3 {
	return mgl64.Vec3{float64(vec3[0]), float64(vec3[1]), float64(vec3[2])}
}

// vec64To32 converts a mgl64.Vec3 to a mgl32.Vec3.
func vec64To32(vec3 mgl64.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{float32(vec3[0]), float32(vec3[1]), float32(vec3[2])}
}

// vec2To32 converts a mgl64.Vec2 to a mgl32.Vec2.
func vec2To32(vec2 mgl64.Vec2) mgl32.Vec2 {
	return mgl32.Vec2{float32(vec2[0]), float32(vec2[1])}
}

// blockPosFromProtocol ...
func blockPosFromProtocol(pos v2193.BlockPos) cube.Pos {
	return cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}
}

// boolByte returns 1 if the bool passed is true, or 0 if it is false.
func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// abs ...
func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
