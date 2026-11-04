package session

import (
	"bytes"
	"image/color"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// NetworkEncodeableEntity is a world.EntityType where the save ID and network
// ID are not the same. Plugins implement it for their own types; the session
// names the types of the server Bedrock has no type of itself.
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

// entityHidden checks if a world.Entity is being explicitly hidden from the
// Session, or kept unseen for being farther from its player than its range.
func (s *Session) entityHidden(e world.Entity) bool {
	hidden, far := s.hidesEntities.Load(), s.hidesFar.Load()
	if !hidden && !far {
		return false
	}
	s.entityMutex.RLock()
	_, ok := s.hiddenEntities[e.H().UUID()]
	if !ok && far {
		_, ok = s.farEntities[e.H()]
	}
	s.entityMutex.RUnlock()
	return ok
}

// temporaryListEntry returns the PlayerList packet that lists a player of
// no session, such as an NPC, for the AddPlayer after it to show its skin.
func (s *Session) temporaryListEntry(v Controllable, e world.Entity) *mc.PlayerListPacket {
	return &mc.PlayerListPacket{Entries: []mc.PlayerListEntry{{
		ActionType:     mc.PlayerListActionAdd,
		UUID:           v.UUID(),
		EntityUniqueID: s.entityUniqueID(e),
		Username:       v.Name(),
		PlayFabID:      v.Skin().PlayFabID,
		BuildPlatform:  int32(mc.DeviceUnknown),
		Skin:           skinToProtocol(v.Skin(), s.primary().Version),
	}}}
}

// ViewEntity ...
func (s *Session) ViewEntity(e world.Entity) {
	if e.H() == s.ent {
		s.ViewEntityState(e)
		return
	}
	if s.entityHidden(e) || s.outOfRange(e) {
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
		listed, ok := s.roster().Player(v.UUID())
		_, ofSession := listed.Client.(*Session)
		actualPlayer := ok && ofSession
		if !actualPlayer {
			s.writePacket(s.temporaryListEntry(v, e))
		}

		s.writePacket(&mc.AddPlayerPacket{
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
			BuildPlatform:   int32(mc.DeviceUnknown),
			AbilityData:     s.abilityData(v, s.entityUniqueID(e)),
		})
		if !actualPlayer {
			s.writePacket(&mc.PlayerListPacket{Entries: []mc.PlayerListEntry{{
				ActionType: mc.PlayerListActionRemove,
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
			s.writePacket(&mc.AddItemActorPacket{
				EntityUniqueID:  s.entityUniqueID(e),
				EntityRuntimeID: runtimeID,
				Item:            instanceFromItem(s.tables(), v.Behaviour().(*entity.ItemBehaviour).Item()),
				Position:        vec64To32(entityNetworkPosition(e, v.Position())),
				Velocity:        vec64To32(v.Velocity()),
				EntityMetadata:  toMetadata(metadata),
			})
			return
		case entity.TextType:
			metadata[mc.EntityDataKeyVariant] = int32(s.blockHash(block.Air{}))
		case entity.FallingBlockType:
			metadata[mc.EntityDataKeyVariant] = int32(s.blockHash(v.Behaviour().(*entity.FallingBlockBehaviour).Block()))
		}
	}
	id = networkID(e.H().Type())

	var vel mgl64.Vec3
	if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		vel = v.Velocity()
	}

	s.writePacket(&mc.AddActorPacket{
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
func mobAttributes(e world.Entity) []mc.AttributeValue {
	var attributes []mc.AttributeValue
	if h, ok := e.(mobHealth); ok {
		attributes = append(attributes, mc.AttributeValue{Name: "minecraft:health", Value: float32(h.Health()), Max: float32(h.MaxHealth())})
	}
	if s, ok := e.(interface{ Speed() float64 }); ok {
		attributes = append(attributes, mc.AttributeValue{Name: "minecraft:movement", Value: float32(s.Speed()), Max: math.MaxFloat32})
	}
	if j, ok := e.(interface{ JumpStrength() float64 }); ok {
		attributes = append(attributes, mc.AttributeValue{Name: "minecraft:horse.jump_strength", Value: float32(j.JumpStrength()), Max: math.MaxFloat32})
	}
	return attributes
}

// viewMobHealth sends the current health of a mob other than a player.
func (s *Session) viewMobHealth(e world.Entity) {
	h, ok := e.(mobHealth)
	if !ok || e.H().Type().EncodeEntity() == "minecraft:player" {
		return
	}
	s.writePacket(&mc.UpdateAttributesPacket{
		EntityRuntimeID: s.entityRuntimeID(e),
		Attributes: []mc.Attribute{{
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
	pkType := byte(mc.EntityLinkRemove)
	if mounted {
		switch linkType {
		case entity.LinkPassenger:
			pkType = byte(mc.EntityLinkPassenger)
		default:
			pkType = byte(mc.EntityLinkRider)
		}
	}
	s.writePacket(&mc.SetActorLinkPacket{EntityLink: mc.EntityLink{
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
	s.writePacket(&mc.LevelEventPacket{EventType: mc.LevelEventParticleSoundGuardianGhost, Position: vec64To32(pos)})
}

// ViewSpectatedEntity attaches the camera of the client to an entity, as BDS
// does for /camera attach_to_entity, or detaches it again if e is nil.
func (s *Session) ViewSpectatedEntity(e world.Entity) {
	if e == nil {
		s.writePacket(&mc.CameraInstructionPacket{DetachFromEntity: proto.Some(true)})
		return
	}
	s.writePacket(&mc.CameraInstructionPacket{AttachToEntity: proto.Some(s.entityUniqueID(e))})
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
	s.writePacket(&mc.UpdatePlayerGameTypePacket{
		GameType:       GameTypeFromMode(c.GameMode()),
		PlayerUniqueID: s.entityUniqueID(c),
	})
}

// HideEntity ...
func (s *Session) HideEntity(e world.Entity) {
	s.forgetFar(e.H())
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
	s.writePacket(&mc.RemoveActorPacket{EntityUniqueID: s.handleUniqueID(e.H())})
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
func absoluteMove(id uint64, e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround, authoritative bool) *mc.MoveActorAbsolutePacket {
	flags := byte(0)
	if onGround {
		flags |= mc.MoveFlagOnGround
	}
	if authoritative {
		flags |= mc.MoveFlagTeleport
	}
	return &mc.MoveActorAbsolutePacket{
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
	s.writePacket(&mc.SetActorMotionPacket{
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
	s.writePacket(&mc.SetTimePacket{Time: int32(time)})
}

// ViewTimeCycle ...
func (s *Session) ViewTimeCycle(doDayLightCycle bool) {
	s.sendGameRules([]mc.GameRule{{Name: "dodaylightcycle", Value: doDayLightCycle}})
}

// ViewFallDamage controls whether fall damage is enabled for this viewer.
func (s *Session) ViewFallDamage(enabled bool) {
	s.sendGameRules([]mc.GameRule{{Name: "falldamage", Value: enabled}})
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

	s.writePacket(&mc.SetActorMotionPacket{EntityRuntimeID: id})
	if _, ok := e.(Controllable); ok {
		s.writePacket(&mc.MovePlayerPacket{
			EntityRuntimeID: id,
			Position:        vec64To32(entityNetworkPosition(e, position)),
			Pitch:           float32(pitch),
			Yaw:             float32(yaw),
			HeadYaw:         float32(yaw),
			Mode:            mc.MoveModeTeleport,
			TeleportData: proto.Some(mc.TeleportData{
				TeleportCause: mc.TeleportCauseUnknown,
			}),
		})
		return
	}
	s.writePacket(&mc.MoveActorAbsolutePacket{
		EntityRuntimeID: id,
		Position:        vec64To32(entityNetworkPosition(e, position)),
		Rotation:        vec64To32(mgl64.Vec3{pitch, yaw, yaw}),
		Flags:           mc.MoveFlagTeleport,
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
		s.writePacket(&mc.MobEquipmentPacket{
			EntityRuntimeID: runtimeID,
			NewItem:         instanceFromItem(s.tables(), mainHand),
		})
	}
	// Show the off-hand item.
	if !shown || !offHand.Empty() {
		s.writePacket(&mc.MobEquipmentPacket{
			EntityRuntimeID: runtimeID,
			NewItem:         instanceFromItem(s.tables(), offHand),
			WindowID:        mc.WindowIDOffHand,
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
	s.writePacket(&mc.MobArmourEquipmentPacket{
		EntityRuntimeID: runtimeID,
		Helmet:          instanceFromItem(s.tables(), helm),
		Chestplate:      instanceFromItem(s.tables(), chestplate),
		Leggings:        instanceFromItem(s.tables(), leggings),
		Boots:           instanceFromItem(s.tables(), boots),
		Body:            instanceFromItem(s.tables(), body),
	})
}

// ViewItemCooldown ...
func (s *Session) ViewItemCooldown(item world.Item, duration time.Duration) {
	name, _ := item.EncodeItem()
	category := name
	if _, suffix, ok := strings.Cut(name, ":"); ok && suffix != "" {
		category = suffix
	}
	s.writePacket(&mc.ClientStartItemCooldownPacket{
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

	s.writePacket(&mc.LevelEventGenericPacket{
		EventID:             mc.LevelEventSleepingPlayers,
		SerialisedEventData: eventData[2 : len(eventData)-1],
	})
}

// ViewParticle ...
func (s *Session) ViewParticle(pos mgl64.Vec3, p world.Particle) {
	if s.dropCosmetic() {
		return
	}
	s.particlePackets(pos, p, s.writePacket)
}

// ViewParticleShared ...
func (s *Session) ViewParticleShared(pos mgl64.Vec3, p world.Particle, b *world.Broadcast) {
	if s.dropCosmetic() {
		return
	}
	// The packets of a particle name blocks by the registry of the session,
	// that of the world every session of it shares.
	s.writeBroadcast(b, func(out func(proto.Packet)) { s.particlePackets(pos, p, out) })
}

// particlePackets hands out the packets that show a particle at a position.
func (s *Session) particlePackets(pos mgl64.Vec3, p world.Particle, out func(proto.Packet)) {
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
		out(&mc.LevelEventGenericPacket{
			EventID:             mc.LevelEventParticleCreakingHeartTrail,
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
		out(&mc.LevelEventGenericPacket{
			EventID:             mc.LevelEventParticlesVibrationSignal,
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

		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesDragonEgg,
			Position:  vec64To32(pos),
			EventData: int32((((((abs(pa.Diff.X()) << 16) | (abs(pa.Diff.Y()) << 8)) | abs(pa.Diff.Z())) | xSign) | ySign) | zSign),
		})
	case particle.Note:
		out(&mc.BlockEventPacket{
			EventType: bedrockInstrument(pa.Instrument),
			EventData: int32(pa.Pitch),
			Position:  mc.BlockPos{int32(pos.X()), int32(pos.Y()), int32(pos.Z())},
		})
	case particle.HugeExplosion:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesExplosion,
			Position:  vec64To32(pos),
		})
	case particle.BoneMeal:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleCropGrowth,
			Position:  vec64To32(pos),
			EventData: int32(boolByte(pa.Area)),
		})
	case particle.BlockForceField:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleDenyBlock,
			Position:  vec64To32(pos),
		})
	case particle.DispenserSmoke:
		direction := cube.Pos{}.Side(pa.Face)
		s.writePacket(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesShoot,
			Position:  vec64To32(pos),
			EventData: int32(direction[0] + 1 + (direction[2]+1)*3),
		})
	case particle.BlockBreak:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesDestroyBlock,
			Position:  vec64To32(pos),
			EventData: int32(s.blockHash(pa.Block)),
		})
	case particle.PunchBlock:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesCrackBlock,
			Position:  vec64To32(pos),
			EventData: int32(s.blockHash(pa.Block)) | (int32(pa.Face) << 24),
		})
	case particle.DragonDestroyBlock:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | mc.ParticleTypeDragonDestroyBlock,
			Position:  vec64To32(pos),
		})
	case particle.EndermanTeleport:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesTeleport,
			Position:  vec64To32(pos),
		})
	case particle.Flame:
		if pa.Colour != (color.RGBA{}) {
			out(&mc.LevelEventPacket{
				EventType: mc.LevelEventParticleLegacyEvent | 57,
				Position:  vec64To32(pos),
				EventData: int32FromRGBA(pa.Colour),
			})
			return
		}
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 8,
			Position:  vec64To32(pos),
		})
	case particle.Evaporate:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesEvaporateWater,
			Position:  vec64To32(pos),
		})
	case particle.SnowballPoof:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 15,
			Position:  vec64To32(pos),
		})
	case particle.EggSmash:
		rid, meta, _ := s.tables().itemRuntimeID(item.Egg{})
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 14,
			EventData: (rid << 16) | int32(meta),
			Position:  vec64To32(pos),
		})
	case particle.Splash:
		if (pa.Colour == color.RGBA{}) {
			pa.Colour, _ = effect.ResultingColour(nil)
		}
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesPotionSplash,
			EventData: (int32(pa.Colour.A) << 24) | (int32(pa.Colour.R) << 16) | (int32(pa.Colour.G) << 8) | int32(pa.Colour.B),
			Position:  vec64To32(pos),
		})
	case particle.MobGrowth:
		t := mc.ParticleTypeResetMobGrowth
		if pa.Paused {
			t = mc.ParticleTypePauseMobGrowth
		}
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | int32(t),
			Position:  vec64To32(pos),
		})
	case particle.SonicExplosion:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSonicExplosion,
			Position:  vec64To32(pos),
		})
	case particle.WindBurst:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesWindExplosion,
			Position:  vec64To32(pos),
		})
	case particle.Effect:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 34,
			EventData: (int32(pa.Colour.A) << 24) | (int32(pa.Colour.R) << 16) | (int32(pa.Colour.G) << 8) | int32(pa.Colour.B),
			Position:  vec64To32(pos),
		})
	case particle.EntityFlame:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 19,
			Position:  vec64To32(pos),
		})
	case particle.Dust:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 33,
			Position:  vec64To32(pos),
			EventData: int32FromRGBA(pa.Colour),
		})
	case particle.WaterDrip:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 28,
			Position:  vec64To32(pos),
		})
	case particle.LavaDrip:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 29,
			Position:  vec64To32(pos),
		})
	case particle.Lava:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 10,
			Position:  vec64To32(pos),
		})
	case particle.DustPlume:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 88,
			Position:  vec64To32(pos),
		})
	case particle.SculkCharge, particle.SculkChargePop:
		block := cube.PosFromVec3(pos)
		data := map[string]any{"x": int32(block.X()), "y": int32(block.Y()), "z": int32(block.Z())}
		event := int32(mc.LevelEventSculkChargePop)
		if charge, ok := pa.(particle.SculkCharge); ok {
			event = mc.LevelEventSculkCharge
			data["charge"], data["facing"] = int16(charge.Charge), int16(charge.Faces)
		}
		buf := bytes.NewBuffer(nil)
		_ = nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian).Encode(data)
		encoded := buf.Bytes()
		out(&mc.LevelEventGenericPacket{EventID: event, SerialisedEventData: encoded[2 : len(encoded)-1]})
	case particle.SculkSoul:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | mc.ParticleTypeSculkSoul,
			Position:  vec64To32(pos),
		})
	case particle.CauldronEffect:
		out(&mc.LevelEventPacket{
			EventType: cauldronLevelEvents[pa.Kind],
			Position:  vec64To32(pos),
			EventData: int32FromRGBA(pa.Colour),
		})
	case particle.DripstoneDrip:
		out(&mc.LevelEventPacket{EventType: mc.LevelEventParticlesDripstoneDrip, Position: vec64To32(pos)})
	case particle.PointedDripstoneLand:
		out(&mc.LevelEventPacket{EventType: mc.LevelEventSoundPointedDripstoneLand, Position: vec64To32(pos)})
	case particle.EggCrack:
		out(&mc.LevelEventPacket{EventType: mc.LevelEventParticleTurtleEgg, Position: vec64To32(pos)})
	case particle.ElectricSpark:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesElectricSpark,
			Position:  vec64To32(pos),
			EventData: pa.Data,
		})
	case particle.EyeblossomTransform:
		kind := int32(mc.ParticleTypeEyeblossomClose)
		if pa.Open {
			kind = mc.ParticleTypeEyeblossomOpen
		}
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | kind,
			Position:  vec64To32(pos),
		})
	case particle.TrialSpawnerDetection:
		event := int32(mc.LevelEventParticlesTrialSpawnerDetection)
		if pa.Ominous {
			event = mc.LevelEventParticlesTrialSpawnerDetectionCharged
		}
		out(&mc.LevelEventPacket{EventType: event, Position: vec64To32(pos), EventData: int32(pa.Players)})
	case particle.TrialSpawnerSpawning:
		out(&mc.LevelEventPacket{EventType: mc.LevelEventParticlesTrialSpawnerSpawning, Position: vec64To32(pos), EventData: int32(boolByte(pa.Ominous))})
	case particle.TrialSpawnerEjecting:
		out(&mc.LevelEventPacket{EventType: mc.LevelEventParticlesTrialSpawnerEjecting, Position: vec64To32(pos)})
	case particle.TrialSpawnerBecomeOminous:
		out(&mc.LevelEventPacket{EventType: mc.LevelEventParticlesTrialSpawnerBecomeCharged, Position: vec64To32(pos)})
	case particle.VaultActivate:
		out(&mc.LevelEventPacket{EventType: mc.LevelEventAnimationVaultActivate, Position: vec64To32(pos), EventData: int32(boolByte(pa.Ominous))})
	case particle.VaultDeactivate:
		out(&mc.LevelEventPacket{EventType: mc.LevelEventAnimationVaultDeactivate, Position: vec64To32(pos), EventData: int32(boolByte(pa.Ominous))})
	case particle.VaultEjectItem:
		out(&mc.LevelEventPacket{EventType: mc.LevelEventAnimationVaultEjectItem, Position: vec64To32(pos)})
	}
}

// cauldronLevelEvents maps cauldron effects to their Bedrock level events.
var cauldronLevelEvents = [...]int32{
	particle.CauldronExplode:        mc.LevelEventCauldronExplode,
	particle.CauldronDyeArmour:      mc.LevelEventCauldronDyeArmor,
	particle.CauldronCleanArmour:    mc.LevelEventCauldronCleanArmor,
	particle.CauldronFillPotion:     mc.LevelEventCauldronFillPotion,
	particle.CauldronTakePotion:     mc.LevelEventCauldronTakePotion,
	particle.CauldronFillWater:      mc.LevelEventCauldronFillWater,
	particle.CauldronTakeWater:      mc.LevelEventCauldronTakeWater,
	particle.CauldronAddDye:         mc.LevelEventCauldronAddDye,
	particle.CauldronCleanBanner:    mc.LevelEventCauldronCleanBanner,
	particle.CauldronFillLava:       mc.LevelEventCauldronFillLava,
	particle.CauldronTakeLava:       mc.LevelEventCauldronTakeLava,
	particle.CauldronFillPowderSnow: mc.LevelEventCauldronFillPowderSnow,
	particle.CauldronTakePowderSnow: mc.LevelEventCauldronTakePowderSnow,
}

// tierToSoundEvent converts an item.ArmourTier to a sound event associated with equipping it.
func tierToSoundEvent(tier item.ArmourTier) string {
	switch tier.(type) {
	case item.ArmourTierLeather:
		return mc.SoundEventEquipLeather
	case item.ArmourTierCopper:
		return mc.SoundEventEquipCopper
	case item.ArmourTierGold:
		return mc.SoundEventEquipGold
	case item.ArmourTierChain:
		return mc.SoundEventEquipChain
	case item.ArmourTierIron:
		return mc.SoundEventEquipIron
	case item.ArmourTierDiamond:
		return mc.SoundEventEquipDiamond
	case item.ArmourTierNetherite:
		return mc.SoundEventEquipNetherite
	}
	return mc.SoundEventEquipGeneric
}

// entitylessEventTypes holds the entity type a sound.Event of no entity is
// sent with where it is not "". The break of a straw bed has always been
// sent with ":", the entity type of the typed sounds, and every other event
// with "": the difference is kept as it was until a capture of BDS shows
// which is right.
var entitylessEventTypes = map[string]string{
	"straw_bed.break_leave": ":",
}

// playSound plays a world.Sound at a position, disabling relative volume if set to true.
func (s *Session) playSound(pos mgl64.Vec3, t world.Sound, disableRelative bool) {
	out := s.writePacket
	if bell, ok := t.(sound.BellRing); ok && bell.AfterBlock {
		out = func(pk proto.Packet) { s.enqueuePacketAfterBlock(cube.PosFromVec3(pos), pk) }
	}
	s.soundPackets(pos, t, disableRelative, out)
}

// soundPackets hands out the packets that play a sound at a position, the
// same for every viewer.
func (s *Session) soundPackets(pos mgl64.Vec3, t world.Sound, disableRelative bool, out func(proto.Packet)) {
	pk := &mc.LevelSoundEventPacket{
		Position:              vec64To32(pos),
		EntityType:            ":",
		ExtraData:             -1,
		EntityUniqueID:        -1,
		DisableRelativeVolume: disableRelative,
	}
	switch so := t.(type) {
	case sound.Event:
		pk.SoundType = so.Name
		pk.EntityType = entitylessEventTypes[so.Name]
		if so.Entity != "" {
			pk.EntityType = world.BedrockEntityIdentifier(so.Entity)
		}
		if so.Block != nil {
			pk.ExtraData = int32(s.blockHash(so.Block))
		}
		if so.Source != nil {
			pk.EntityUniqueID = so.Source.UniqueID()
		}
	case sound.CreakingHeartTrail:
		pk.SoundType, pk.EntityType = "block.creaking_heart.trail", ""
		pk.ExtraData = int32((float32(so.Progress)*2 - 1) * 1000)
	case sound.LodestoneCompassLink:
		pk.SoundType = mc.SoundEventLinkCompassToLodestone
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
			pk.SoundType = mc.SoundEventEquipElytra
		default:
			pk.SoundType = mc.SoundEventEquipGeneric
		}
	case sound.Note:
		pk.SoundType = mc.SoundEventNote
		pk.ExtraData = (bedrockInstrument(so.Instrument) << 8) | int32(so.Pitch)
		pk.EntityType = ""
	case sound.DoorCrash:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundZombieDoorCrash,
			Position:  vec64To32(pos),
		})
		return
	case sound.Explosion:
		pk.SoundType = mc.SoundEventExplode
	case sound.Thunder:
		pk.SoundType, pk.EntityType = mc.SoundEventThunder, "minecraft:lightning_bolt"
	case sound.Click:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundClick,
			Position:  vec64To32(pos),
		})
		return
	case sound.SignWaxed:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventWaxOn,
			Position:  vec64To32(pos),
		})
		return
	case sound.WaxedSignFailedInteraction:
		pk.SoundType = mc.SoundEventWaxedSignInteractFail
	case sound.WaxRemoved:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventWaxOff,
			Position:  vec64To32(pos),
		})
		return
	case sound.CopperScraped:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventScrape,
			Position:  vec64To32(pos),
		})
		return
	case sound.Pop:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundInfinityArrowPickup,
			Position:  vec64To32(pos),
		})
		return
	case sound.Teleport:
		pk.SoundType = mc.SoundEventTeleport
	case sound.ItemAdd:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundAddItem,
			Position:  vec64To32(pos),
		})
		return
	case sound.ItemFrameRemove:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundItemFrameRemoveItem,
			Position:  vec64To32(pos),
		})
		return
	case sound.ItemFrameRotate:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundItemFrameRotateItem,
			Position:  vec64To32(pos),
		})
		return
	case sound.HangingPlace:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundItemFramePlace,
			Position:  vec64To32(pos),
		})
		return
	case sound.HangingBreak:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundItemFrameBreak,
			Position:  vec64To32(pos),
		})
		return
	case sound.GhastWarning:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundGhastWarning,
			Position:  vec64To32(pos),
		})
		return
	case sound.GhastShoot:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundGhastFireball,
			Position:  vec64To32(pos),
		})
		return
	case sound.TNT:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundFuse,
			Position:  vec64To32(pos),
		})
		return
	case sound.FireworkLaunch:
		pk.SoundType = mc.SoundEventLaunch
	case sound.FireworkHugeBlast:
		pk.SoundType = mc.SoundEventLargeBlast
	case sound.FireworkBlast:
		pk.SoundType = mc.SoundEventBlast
	case sound.FireworkTwinkle:
		pk.SoundType = mc.SoundEventTwinkle
	case sound.FurnaceCrackle:
		pk.SoundType = mc.SoundEventFurnaceUse
	case sound.CampfireCrackle:
		pk.SoundType = mc.SoundEventCampfireCrackle
	case sound.BlastFurnaceCrackle:
		pk.SoundType = mc.SoundEventBlastFurnaceUse
	case sound.SmokerCrackle:
		pk.SoundType = mc.SoundEventSmokerUse
	case sound.PotionBrewed:
		pk.SoundType = mc.SoundEventPotionBrewed
	case sound.UseSpyglass:
		pk.SoundType = mc.SoundEventUseSpyglass
	case sound.StopUsingSpyglass:
		pk.SoundType = mc.SoundEventStopUsingSpyglass
	case sound.RespawnAnchorAmbient:
		pk.SoundType = mc.SoundEventRespawnAnchorAmbient
		pk.ExtraData = int32(so.Charge)
	case sound.RespawnAnchorCharge:
		pk.SoundType = mc.SoundEventRespawnAnchorCharge
		pk.ExtraData = int32(so.Charge)
	case sound.RespawnAnchorDeplete:
		pk.SoundType = mc.SoundEventRespawnAnchorDeplete
		pk.ExtraData = int32(so.Charge)
	case sound.GoatHorn:
		switch so.Horn {
		case sound.Ponder():
			pk.SoundType = mc.SoundEventGoatCall0
		case sound.Sing():
			pk.SoundType = mc.SoundEventGoatCall1
		case sound.Seek():
			pk.SoundType = mc.SoundEventGoatCall2
		case sound.Feel():
			pk.SoundType = mc.SoundEventGoatCall3
		case sound.Admire():
			pk.SoundType = mc.SoundEventGoatCall4
		case sound.Call():
			pk.SoundType = mc.SoundEventGoatCall5
		case sound.Yearn():
			pk.SoundType = mc.SoundEventGoatCall6
		case sound.Dream():
			pk.SoundType = mc.SoundEventGoatCall7
		}
	case sound.FireExtinguish:
		pk.SoundType = mc.SoundEventExtinguishFire
	case sound.Ignite:
		pk.SoundType = mc.SoundEventIgnite
	case sound.EnderEyePlaced:
		pk.SoundType = mc.SoundEventEnderEyePlaced
	case sound.EndPortalCreated:
		pk.SoundType = mc.SoundEventEndPortalCreated
	case sound.Burning:
		pk.SoundType = mc.SoundEventPlayerHurtOnFire
	case sound.Drowning:
		pk.SoundType = mc.SoundEventPlayerHurtDrown
	case sound.Fall:
		pk.EntityType = "minecraft:player"
		if so.Distance > 4 {
			pk.SoundType = mc.SoundEventFallBig
			break
		}
		pk.SoundType = mc.SoundEventFallSmall
	case sound.Burp:
		pk.SoundType = mc.SoundEventBurp
	case sound.DoorOpen:
		pk.SoundType, pk.ExtraData = mc.SoundEventDoorOpen, int32(s.blockHash(so.Block))
	case sound.DoorClose:
		pk.SoundType, pk.ExtraData = mc.SoundEventDoorClose, int32(s.blockHash(so.Block))
	case sound.TrapdoorOpen:
		pk.SoundType, pk.ExtraData = mc.SoundEventTrapdoorOpen, int32(s.blockHash(so.Block))
	case sound.TrapdoorClose:
		pk.SoundType, pk.ExtraData = mc.SoundEventTrapdoorClose, int32(s.blockHash(so.Block))
	case sound.FenceGateOpen:
		pk.SoundType, pk.ExtraData = mc.SoundEventFenceGateOpen, int32(s.blockHash(so.Block))
	case sound.FenceGateClose:
		pk.SoundType, pk.ExtraData = mc.SoundEventFenceGateClose, int32(s.blockHash(so.Block))
	case sound.PistonIn:
		pk.SoundType, pk.EntityType = mc.SoundEventPistonIn, ""
	case sound.PistonOut:
		pk.SoundType, pk.EntityType = mc.SoundEventPistonOut, ""
	case sound.Deny:
		pk.SoundType = mc.SoundEventDeny
	case sound.BlockPlace:
		pk.SoundType, pk.ExtraData = mc.SoundEventPlace, int32(s.blockHash(so.Block))
	case sound.AnvilLand:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundAnvilLand,
			Position:  vec64To32(pos),
		})
		return
	case sound.AnvilUse:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundAnvilUsed,
			Position:  vec64To32(pos),
		})
		return
	case sound.AnvilBreak:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundAnvilBroken,
			Position:  vec64To32(pos),
		})
		return
	case sound.ChestClose:
		pk.SoundType = mc.SoundEventChestClosed
	case sound.ChestOpen:
		pk.SoundType = mc.SoundEventChestOpen
	case sound.EnderChestClose:
		pk.SoundType = mc.SoundEventEnderChestClosed
	case sound.EnderChestOpen:
		pk.SoundType = mc.SoundEventEnderChestOpen
	case sound.BarrelClose:
		pk.SoundType = mc.SoundEventBarrelClose
	case sound.BarrelOpen:
		pk.SoundType = mc.SoundEventBarrelOpen
	case sound.BellRing:
		pk.SoundType = mc.SoundEventBell
		pk.EntityType = ""
	case sound.BlockBreaking:
		pk.SoundType, pk.ExtraData = mc.SoundEventHit, int32(s.blockHash(so.Block))
	case sound.ItemBreak:
		pk.SoundType = mc.SoundEventBreak
	case sound.ItemUseOn:
		pk.SoundType, pk.ExtraData = mc.SoundEventItemUseOn, int32(s.blockHash(so.Block))
	case sound.BlockBounce:
		pk.SoundType, pk.ExtraData = mc.SoundEventBounce, int32(s.blockHash(so.Block))
	case sound.GeyserEruptionStart:
		pk.SoundType = mc.SoundEventGeyserEruptionStart
		if so.Continuous {
			pk.SoundType = mc.SoundEventGeyserContinuousEruptionStart
		}
	case sound.MobGrowth:
		pk.SoundType = mc.SoundEventResetGrowth
		if so.Paused {
			pk.SoundType = mc.SoundEventPauseGrowth
		}
	case sound.FrogSpawnHatch:
		pk.SoundType = mc.SoundEventFrogspawnHatched
	case sound.WindChargeImpact:
		pk.SoundType = mc.SoundEventWindChargeBurst
	case sound.BreezeWindChargeBurst:
		pk.SoundType = mc.SoundEventBreezeWindChargeBurst
	case sound.WindChargeShoot:
		pk.SoundType, pk.EntityType = mc.SoundEventThrow, "minecraft:player"
	case sound.Fizz:
		pk.SoundType = mc.SoundEventFizz
	case sound.PointedDripstoneCauldronDrip:
		pk.SoundType = mc.SoundEventPointedDripstoneCauldronDripWater
		if so.Lava {
			pk.SoundType = mc.SoundEventPointedDripstoneCauldronDripLava
		}
	case sound.BigDripleafTilt:
		pk.SoundType = mc.SoundEventBigDripleafTiltDown
		if so.Up {
			pk.SoundType = mc.SoundEventBigDripleafTiltUp
		}
	case sound.GlassBreak:
		pk.SoundType = mc.SoundEventGlass
	case sound.Attack:
		pk.SoundType, pk.EntityType = mc.SoundEventAttackStrong, "minecraft:player"
		if !so.Damage {
			pk.SoundType = mc.SoundEventAttackNoDamage
		}
	case sound.BucketFill:
		if _, water := so.Liquid.(block.Water); water {
			pk.SoundType = mc.SoundEventBucketFillWater
			break
		}
		pk.SoundType = mc.SoundEventBucketFillLava
	case sound.BucketEmpty:
		if _, water := so.Liquid.(block.Water); water {
			pk.SoundType = mc.SoundEventBucketEmptyWater
			break
		}
		pk.SoundType = mc.SoundEventBucketEmptyLava
	case sound.BowShoot:
		pk.SoundType = mc.SoundEventBow
	case sound.ShieldBlock:
		pk.SoundType = mc.SoundEventShieldBlock
	case sound.CrossbowLoad:
		switch so.Stage {
		case sound.CrossbowLoadingStart:
			pk.SoundType = mc.SoundEventCrossbowLoadingStart
			if so.QuickCharge {
				pk.SoundType = mc.SoundEventCrossbowQuickChargeStart
			}
		case sound.CrossbowLoadingMiddle:
			pk.SoundType = mc.SoundEventCrossbowLoadingMiddle
			if so.QuickCharge {
				pk.SoundType = mc.SoundEventCrossbowQuickChargeMiddle
			}
		case sound.CrossbowLoadingEnd:
			pk.SoundType = mc.SoundEventCrossbowLoadingEnd
			if so.QuickCharge {
				pk.SoundType = mc.SoundEventCrossbowQuickChargeEnd
			}
		default:
			// Invalid stages should not crash the server; skip the sound instead.
			return
		}
	case sound.CrossbowShoot:
		pk.SoundType = mc.SoundEventCrossbowShoot
	case sound.ArrowHit:
		pk.SoundType = mc.SoundEventBowHit
	case sound.TridentThrow:
		pk.SoundType = mc.SoundEventTridentThrow
	case sound.TridentHit:
		pk.SoundType = mc.SoundEventTridentHit
	case sound.TridentReturn:
		pk.SoundType = mc.SoundEventTridentReturn
	case sound.TridentChanneling:
		pk.SoundType = mc.SoundEventTridentThunder
	case sound.TridentRiptide:
		switch so.Level {
		case 3:
			pk.SoundType = mc.SoundEventTridentRiptide3
		case 2:
			pk.SoundType = mc.SoundEventTridentRiptide2
		default:
			pk.SoundType = mc.SoundEventTridentRiptide1
		}
	case sound.ItemThrow:
		pk.SoundType, pk.EntityType = mc.SoundEventThrow, "minecraft:player"
	case sound.LevelUp:
		pk.SoundType, pk.ExtraData = mc.SoundEventLevelUp, 0x10000000
	case sound.Experience:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundExperienceOrbPickup,
			Position:  vec64To32(pos),
		})
		return
	case sound.MusicDiscPlay:
		switch so.DiscType {
		case sound.Disc13():
			pk.SoundType = mc.SoundEventRecord13
		case sound.DiscCat():
			pk.SoundType = mc.SoundEventRecordCat
		case sound.DiscBlocks():
			pk.SoundType = mc.SoundEventRecordBlocks
		case sound.DiscChirp():
			pk.SoundType = mc.SoundEventRecordChirp
		case sound.DiscFar():
			pk.SoundType = mc.SoundEventRecordFar
		case sound.DiscMall():
			pk.SoundType = mc.SoundEventRecordMall
		case sound.DiscMellohi():
			pk.SoundType = mc.SoundEventRecordMellohi
		case sound.DiscStal():
			pk.SoundType = mc.SoundEventRecordStal
		case sound.DiscStrad():
			pk.SoundType = mc.SoundEventRecordStrad
		case sound.DiscWard():
			pk.SoundType = mc.SoundEventRecordWard
		case sound.Disc11():
			pk.SoundType = mc.SoundEventRecord11
		case sound.DiscWait():
			pk.SoundType = mc.SoundEventRecordWait
		case sound.DiscOtherside():
			pk.SoundType = mc.SoundEventRecordOtherside
		case sound.DiscPigstep():
			pk.SoundType = mc.SoundEventRecordPigstep
		case sound.Disc5():
			pk.SoundType = mc.SoundEventRecord5
		case sound.DiscRelic():
			pk.SoundType = mc.SoundEventRecordRelic
		case sound.DiscCreator():
			pk.SoundType = mc.SoundEventRecordCreator
		case sound.DiscCreatorMusicBox():
			pk.SoundType = mc.SoundEventRecordCreatorMusicBox
		case sound.DiscPrecipice():
			pk.SoundType = mc.SoundEventRecordPrecipice
		case sound.DiscTears():
			pk.SoundType = mc.SoundEventRecordTears
		case sound.DiscLavaChicken():
			pk.SoundType = mc.SoundEventRecordLavaChicken
		default:
			// Unknown disc types should not crash the server; skip the sound instead.
			return
		}
	case sound.MusicDiscEnd:
		pk.SoundType = mc.SoundEventRecordNull
	case sound.FireCharge:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundBlazeFireball,
			Position:  vec64To32(pos),
		})
		return
	case sound.ConduitAttack:
		pk.SoundType = mc.SoundEventConduitAttack
	case sound.ComposterEmpty:
		pk.SoundType = mc.SoundEventComposterEmpty
	case sound.BottleFill:
		pk.SoundType = mc.SoundEventBottleFill
	case sound.ComposterFill:
		pk.SoundType = mc.SoundEventComposterFill
	case sound.ComposterFillLayer:
		pk.SoundType = mc.SoundEventComposterFillLayer
	case sound.ComposterReady:
		pk.SoundType = mc.SoundEventComposterReady
	case sound.PowerOn:
		pk.SoundType = mc.SoundEventPowerOn
	case sound.PowerOff:
		pk.SoundType = mc.SoundEventPowerOff
	case sound.LecternBookPlace:
		pk.SoundType = mc.SoundEventLecternBookPlace
	case sound.Totem:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundTotemUsed,
			Position:  vec64To32(pos),
		})
		return
	case sound.ShulkerBoxClose:
		pk.SoundType = mc.SoundEventShulkerBoxClosed
	case sound.ShulkerBoxOpen:
		pk.SoundType = mc.SoundEventShulkerBoxOpen
	case sound.DecoratedPotInserted:
		out(&mc.PlaySoundPacket{
			SoundName: "block.decorated_pot.insert",
			Position:  vec64To32(pos),
			Volume:    1,
			Pitch:     0.7 + 0.5*float32(so.Progress),
		})
		return
	case sound.DecoratedPotInsertFailed:
		pk.SoundType = mc.SoundEventDecoratedPotInsertFail
	case sound.Custom:
		name, ok := bedrockSoundName(so.Name)
		if !ok {
			return
		}
		out(&mc.PlaySoundPacket{
			SoundName: name,
			Position:  vec64To32(pos),
			Volume:    float32(so.Volume),
			Pitch:     float32(so.Pitch),
		})
		return
	case sound.LightningExplode:
		out(&mc.PlaySoundPacket{
			SoundName: "ambient.weather.lightning.impact",
			Position:  vec64To32(pos),
			Volume:    1,
			Pitch:     0.7,
		})
		return
	case sound.LightningThunder:
		out(&mc.PlaySoundPacket{
			SoundName: "ambient.weather.thunder",
			Position:  vec64To32(pos),
			Volume:    1,
			Pitch:     1.0,
		})
		return
	case sound.ArmourStandHit:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundArmorStandHit,
			Position:  vec64To32(pos),
		})
		return
	case sound.ArmourStandBreak:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundArmorStandBreak,
			Position:  vec64To32(pos),
		})
		return
	case sound.ArmourStandPlace:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundArmorStandPlace,
			Position:  vec64To32(pos),
		})
		return
	case sound.ArmourStandLand:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventSoundArmorStandLand,
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
	blockPos := mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&mc.OpenSignPacket{
		Position:  blockPos,
		FrontSide: frontSide,
	})
}

// ViewFurnaceUpdate updates a furnace for the associated session based on previous times.
// Fuel values are UI-scaled and use the per-item cook requirement as the duration baseline.
func (s *Session) ViewFurnaceUpdate(prevCookTime, cookTime, prevRemainingFuelTime, remainingFuelTime, prevMaxFuelTime, maxFuelTime time.Duration) {
	if prevCookTime != cookTime {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataFurnaceTickCount,
			Value:    int32(cookTime.Milliseconds() / 50),
		})
	}

	if prevRemainingFuelTime != remainingFuelTime {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataFurnaceLitTime,
			Value:    int32(remainingFuelTime.Milliseconds() / 50),
		})
	}

	if prevMaxFuelTime != maxFuelTime {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataFurnaceLitDuration,
			Value:    int32(maxFuelTime.Milliseconds() / 50),
		})
	}
}

// ViewBrewingUpdate updates a brewing stand for the associated session based on previous times.
func (s *Session) ViewBrewingUpdate(prevBrewTime, brewTime time.Duration, prevFuelAmount, fuelAmount, prevFuelTotal, fuelTotal int32) {
	if prevBrewTime != brewTime {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataBrewingStandBrewTime,
			Value:    int32(brewTime.Milliseconds() / 50),
		})
	}

	if prevFuelAmount != fuelAmount {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataBrewingStandFuelAmount,
			Value:    fuelAmount,
		})
	}

	if prevFuelTotal != fuelTotal {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataBrewingStandFuelTotal,
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
			s.writePacket(&mc.AnimatePacket{
				ActionType:      mc.AnimateActionSwingArm,
				EntityRuntimeID: s.entityRuntimeID(e),
			})
			return
		}
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventStartAttacking,
		})
	case entity.HurtAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventHurt,
		})
		s.viewMobHealth(e)
	case entity.CriticalHitAction:
		if act.Count <= 0 {
			act.Count = 55
		}
		s.writePacket(&mc.AnimatePacket{
			ActionType:      mc.AnimateActionCriticalHit,
			EntityRuntimeID: s.entityRuntimeID(e),
			Data:            float32(act.Count),
		})
	case entity.EnchantedHitAction:
		if act.Count <= 0 {
			act.Count = 15
		}
		s.writePacket(&mc.AnimatePacket{
			ActionType:      mc.AnimateActionMagicCriticalHit,
			EntityRuntimeID: s.entityRuntimeID(e),
			Data:            float32(act.Count),
		})
	case entity.DragonDeathAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventDragonStartDeathAnim,
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
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: id,
			EventType:       mc.ActorEventDeath,
		})
	case entity.WitchMagicAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventWitchHatMagic,
		})
	case entity.SilverfishMergeAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventSilverfishMergeAnimation,
		})
	case entity.PickedUpAction:
		s.writePacket(&mc.TakeItemActorPacket{
			ItemEntityRuntimeID:  s.entityRuntimeID(e),
			TakerEntityRuntimeID: s.entityRuntimeID(act.Collector),
		})
	case entity.ItemStackSizeAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventUpdateStackSize,
			EventData:       int32(act.Count),
		})
	case entity.WolfShakeAction:
		s.writePacket(&mc.ActorEventPacket{EntityRuntimeID: s.entityRuntimeID(e), EventType: mc.ActorEventShakeWetness})
	case entity.VibrationDetectedAction:
		s.writePacket(&mc.ActorEventPacket{EntityRuntimeID: s.entityRuntimeID(e), EventType: mc.ActorEventVibrationDetected})
	case entity.SquidInkAction:
		s.writePacket(&mc.ActorEventPacket{EntityRuntimeID: s.entityRuntimeID(e), EventType: mc.ActorEventSquidFleeing})
	case entity.ArrowShakeAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventShake,
			EventData:       int32(act.Duration.Milliseconds() / 50),
		})
	case entity.FireworkExplosionAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventFireworksExplode,
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
			rid, meta, _ := s.tables().itemRuntimeID(it)
			s.writePacket(&mc.ActorEventPacket{
				EntityRuntimeID: s.entityRuntimeID(e),
				EventType:       mc.ActorEventFeed,
				// It's a little weird how the runtime ID is still shifted 16 bits to the left here, given the
				// runtime ID already includes the meta, but it seems to work.
				EventData: (rid << 16) | int32(meta),
			})
		}
	case entity.TotemUseAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventTalismanActivate,
		})
	case entity.FishHookBubbleAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventFishhookBubble,
		})
	case entity.FishHookBiteAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventFishhookHookTime,
		})
	case entity.FishHookTeaseAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventFishhookTease,
		})
	case entity.TameAction:
		event := byte(mc.ActorEventTamingFailed)
		if act.Succeeded {
			event = mc.ActorEventTamingSucceeded
		}
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       event,
		})
	case entity.DrinkAction:
		event := byte(mc.ActorEventDrinkPotion)
		if act.Milk {
			event = mc.ActorEventDrinkMilk
		}
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       event,
		})
	case entity.EatGrassAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventEatGrass,
		})
	case entity.LoveHeartsAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventInLoveHearts,
		})
	case entity.OfferFlowerAction:
		// Bedrock plays the iron golem's flower as an animation of its
		// resource pack, which, like other animations, is cosmetic.
		if s.dropCosmetic() {
			return
		}
		s.writePacket(animationPacket(s.entityRuntimeID(e), offerFlowerAnimation))
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
	s.writePacket(&mc.SetActorDataPacket{
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
func (s *Session) entityMetadata(e world.Entity) mc.Metadata {
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
		metadata[mc.EntityDataKeyScore] = st
	}
	if visibility := s.viewLayer.Visibility(e); visibility.EnforceVisibility() {
		if visibility == world.EnforceInvisible() {
			metadata.SetFlag(mc.EntityDataKeyFlags, mc.EntityDataFlagInvisible)
		} else {
			metadata.UnsetFlag(mc.EntityDataKeyFlags, mc.EntityDataFlagInvisible)
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

// offerFlowerAnimation is the animation of Bedrock's resource pack in which
// an iron golem holds out a poppy.
var offerFlowerAnimation = world.NewEntityAnimation("animation.iron_golem.offer_flower")

// animationPacket returns the packet that plays an animation on the entity
// of a runtime ID.
func animationPacket(id uint64, a world.EntityAnimation) *mc.AnimateEntityPacket {
	return &mc.AnimateEntityPacket{
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
	if bell, ok := t.(sound.BellRing); ok && bell.AfterBlock {
		s.playSound(pos, t, false)
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
	pks, ok := b.Load().([]*netconn.Shared)
	if !ok {
		build(func(pk proto.Packet) { pks = append(pks, &netconn.Shared{Packet: pk}) })
		b.Store(pks)
	}
	for _, pk := range pks {
		s.writePacket(pk)
	}
}

// OpenMenu shows the client m, the menu its player opened in tx, in a new
// window: ContainerOpen, or UpdateEquip for the inventory of a mount, then
// the items of the menu, if it holds any of its own, and the slot states of
// a crafter. A lectern opens a window the session keeps no state for, as
// the client sends no close for it.
func (s *Session) OpenMenu(m *menu.Menu, tx *world.Tx) {
	nextID := s.nextWindowID()
	if m.Kind() != menu.Lectern {
		s.menu.Store(m)
	}
	containerType := windowType(m, tx)
	s.openedContainerID.Store(uint32(containerType))

	pos := m.Pos()
	switch m.Kind() {
	case menu.Entity:
		e, _ := m.Entity().Entity(tx)
		if m.ContainerKind() == entity.ContainerMount {
			// BDS opens the inventory of a mount with UpdateEquip, which names the
			// slots of its screen, and no ContainerOpen.
			s.writePacket(&mc.UpdateEquipPacket{
				WindowID:                nextID,
				WindowType:              containerType,
				EntityUniqueID:          s.handleUniqueID(m.Entity()),
				SerialisedInventoryData: mountEquipDataOf(e),
			})
		} else {
			s.writePacket(&mc.ContainerOpenPacket{
				WindowID:                nextID,
				ContainerType:           containerType,
				ContainerPosition:       mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
				ContainerEntityUniqueID: s.handleUniqueID(m.Entity()),
			})
		}
		s.sendInv(m.Inventory(), uint32(nextID))
	case menu.Merchant:
		s.writePacket(&mc.ContainerOpenPacket{
			WindowID:                nextID,
			ContainerType:           containerType,
			ContainerPosition:       mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
			ContainerEntityUniqueID: s.handleUniqueID(m.Entity()),
		})
	default:
		s.writePacket(&mc.ContainerOpenPacket{
			WindowID:                nextID,
			ContainerType:           containerType,
			ContainerPosition:       mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
			ContainerEntityUniqueID: -1,
		})
		if blockContainer(m.Kind()) || m.Kind() == menu.EnderChest {
			s.sendInv(m.Inventory(), uint32(nextID))
		}
		if crafter, ok := tx.Block(pos).(block.Crafter); ok && m.Kind() == menu.Crafter {
			for slot := range 9 {
				s.ViewCrafterSlotState(slot, crafter.DisabledSlots&(1<<slot) != 0)
			}
			s.ViewCrafterTriggered(crafter.Triggered)
		}
	}
}

// blockContainer reports whether menus of the kind passed are open for a
// block.Container.
func blockContainer(k menu.Kind) bool {
	switch k {
	case menu.Generic, menu.Furnace, menu.BlastFurnace, menu.Smoker, menu.BrewingStand, menu.Hopper, menu.Crafter:
		return true
	}
	return false
}

// window returns the inventory of the menu the player has open as requests
// name it: that of a block container, of an entity or the ender chest of
// the player, and noWindow while no menu, or one with no items of its own,
// is open.
func (s *Session) window() *inventory.Inventory {
	if m := s.menu.Load(); m != nil && (blockContainer(m.Kind()) || m.Kind() == menu.EnderChest || m.Kind() == menu.Entity) {
		return m.Inventory()
	}
	return s.noWindow
}

// windowType returns the type of the window that m opens in, as BDS
// 1.26.52.3 opens it.
func windowType(m *menu.Menu, tx *world.Tx) byte {
	switch m.Kind() {
	case menu.Generic:
		switch tx.Block(m.Pos()).(type) {
		case block.Dispenser:
			// BDS 1.26.52.3 opens a dispenser as type 6 and a dropper as 7
			// (testdata/bedrock_1_26_52_menu_requests.json).
			return mc.ContainerTypeDispenser
		case block.Dropper:
			return mc.ContainerTypeDropper
		}
	case menu.Furnace:
		return mc.ContainerTypeFurnace
	case menu.BrewingStand:
		return mc.ContainerTypeBrewingStand
	case menu.BlastFurnace:
		return mc.ContainerTypeBlastFurnace
	case menu.Smoker:
		return mc.ContainerTypeSmoker
	case menu.Hopper:
		return mc.ContainerTypeHopper
	case menu.Crafter:
		return mc.ContainerTypeCrafter
	case menu.Crafting:
		return mc.ContainerTypeWorkbench
	case menu.Enchantment:
		return mc.ContainerTypeEnchantment
	case menu.Anvil:
		return mc.ContainerTypeAnvil
	case menu.Beacon:
		return mc.ContainerTypeBeacon
	case menu.Lectern:
		return mc.ContainerTypeLectern
	case menu.Loom:
		return mc.ContainerTypeLoom
	case menu.Grindstone:
		return mc.ContainerTypeGrindstone
	case menu.Cartography:
		return mc.ContainerTypeCartography
	case menu.Stonecutter:
		return mc.ContainerTypeStonecutter
	case menu.Smithing:
		return mc.ContainerTypeSmithingTable
	case menu.Entity:
		t, _ := entityContainerType(m.ContainerKind())
		return t
	case menu.Merchant:
		return mc.ContainerTypeTrade
	}
	// A chest, a barrel, an ender chest and any other block.
	return mc.ContainerTypeContainer
}

// CloseMenu closes the window of the menu m: the client closed it itself if
// clientRequested, otherwise it is sent a close.
func (s *Session) CloseMenu(_ *menu.Menu, clientRequested bool) {
	s.closeWindow(clientRequested)
}

// entityContainerType returns the container type that the inventory of an
// entity of the kind passed opens with, as BDS 1.26.52.3 opens it: a mount
// with UpdateEquip of window type 12 (Horse), for every mount, chested or
// not; a chest boat and a chest minecart as a plain container (0); a hopper
// minecart as a hopper (8). The actor data of these entities names other
// types, which they give themselves.
func entityContainerType(kind entity.ContainerKind) (byte, bool) {
	switch kind {
	case entity.ContainerMount:
		return mc.ContainerTypeHorse, true
	case entity.ContainerChestBoat, entity.ContainerMinecartChest:
		return mc.ContainerTypeContainer, true
	case entity.ContainerMinecartHopper:
		return mc.ContainerTypeHopper, true
	}
	return 0, false
}

// ViewTrades shows the client the offers of merchant, the merchant of the
// menu m, in the window of m.
func (s *Session) ViewTrades(m *menu.Menu, merchant entity.Merchant, offers []entity.MerchantOffer) {
	serializedOffers, err := serializeMerchantOffers(offers)
	if err != nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("failed to serialize merchant offers", "err", err)
		}
		return
	}
	s.sendMerchantOffers(m.Entity(), merchant, s.ent, offers, serializedOffers)
}

func (s *Session) sendMerchantOffers(e *world.EntityHandle, merchant entity.Merchant, user *world.EntityHandle, offers []entity.MerchantOffer, serializedOffers []byte) {
	level := merchant.MerchantLevel() - 1
	if level < 0 {
		level = 0
	}
	s.writePacket(&mc.UpdateTradePacket{
		WindowID:          byte(s.openedWindowID.Load()),
		WindowType:        15,
		Size:              int32(len(offers)),
		TradeTier:         int32(level),
		VillagerUniqueID:  s.handleUniqueID(e),
		EntityUniqueID:    s.handleUniqueID(user),
		DisplayName:       merchant.MerchantName(),
		NewTradeUI:        true,
		DemandBasedPrices: true,
		SerialisedOffers:  serializedOffers,
	})
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
			"buyA":             item.WriteNBT(offer.CostA()),
			"buyB":             item.WriteNBT(offer.SecondaryInput),
			"buyCountA":        int32(offer.Input.Count()),
			"buyCountB":        int32(offer.SecondaryInput.Count()),
			"sell":             item.WriteNBT(offer.Output),
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

// ViewSlotChange ...
func (s *Session) ViewSlotChange(slot int, newItem item.Stack) {
	if s.menu.Load() == nil {
		return
	}
	if s.inTransaction.Load() {
		// Don't send slot changes to the player itself.
		return
	}
	s.writePacket(&mc.InventorySlotPacket{
		WindowID: s.openedWindowID.Load(),
		Slot:     uint32(slot),
		NewItem:  instanceFromItem(s.tables(), newItem),
	})
}

// ViewCrafterSlotState sends the disabled/enabled state for one native Crafter slot.
func (s *Session) ViewCrafterSlotState(slot int, disabled bool) {
	if s.menu.Load() == nil || s.openedContainerID.Load() != uint32(mc.ContainerTypeCrafter) {
		return
	}
	value := int32(0)
	if disabled {
		value = 1
	}
	s.writePacket(&mc.ContainerSetDataPacket{WindowID: byte(s.openedWindowID.Load()), Key: int32(slot), Value: value})
}

// ViewCrafterTriggered sends the native Crafter's redstone data property.
func (s *Session) ViewCrafterTriggered(triggered bool) {
	if s.menu.Load() == nil || s.openedContainerID.Load() != uint32(mc.ContainerTypeCrafter) {
		return
	}
	value := int32(0)
	if triggered {
		value = 1
	}
	s.writePacket(&mc.ContainerSetDataPacket{WindowID: byte(s.openedWindowID.Load()), Key: 9, Value: value})
}

// ViewBlockAction ...
func (s *Session) ViewBlockAction(pos cube.Pos, a world.BlockAction) {
	blockPos := mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	switch t := a.(type) {
	case block.BellRingAction:
		s.enqueueBellRingUpdate(pos, t.Bell)
	case block.BellUseRejectedAction:
		s.enqueueBellUseRejection(pos, t.Bell)
	case block.MovingBlockUpdateAction:
		s.enqueueBlockActorUpdate(pos, t.MovingBlock.PistonPos, clientActorNBT(t.MovingBlock.EncodeNBT(), pos))
	case world.PositionTrackingUpdateAction:
		s.writePacket(&mc.PositionTrackingDBServerBroadcastPacket{
			BroadcastAction: mc.PositionTrackingDBBroadcastActionUpdate,
			TrackingID:      t.Handle,
			Payload:         positionTrackingPayload(t.Handle, t.Position, t.Dimension, positionTrackingStatusTracked),
		})
	case world.PositionTrackingDestroyAction:
		s.writePacket(&mc.PositionTrackingDBServerBroadcastPacket{
			BroadcastAction: mc.PositionTrackingDBBroadcastActionDestroy,
			TrackingID:      t.Handle,
			Payload:         positionTrackingPayload(t.Handle, cube.Pos{}, 0, positionTrackingStatusMissing),
		})
	case block.OpenAction:
		s.writePacket(&mc.BlockEventPacket{
			Position:  blockPos,
			EventType: mc.BlockEventChangeChestState,
			EventData: 1,
		})
	case block.CloseAction:
		s.writePacket(&mc.BlockEventPacket{
			Position:  blockPos,
			EventType: mc.BlockEventChangeChestState,
		})
	case block.StartCrackAction:
		if t.BreakTime <= 0 {
			// An instant break has no cracking to animate, and encoding the crack speed would divide by zero.
			break
		}
		s.writePacket(&mc.LevelEventPacket{
			EventType: mc.LevelEventStartBlockCracking,
			Position:  vec64To32(pos.Vec3()),
			EventData: int32(65535 / (t.BreakTime.Seconds() * 20)),
		})
	case block.StopCrackAction:
		s.writePacket(&mc.LevelEventPacket{
			EventType: mc.LevelEventStopBlockCracking,
			Position:  vec64To32(pos.Vec3()),
			EventData: 0,
		})
	case block.ContinueCrackAction:
		if t.BreakTime <= 0 {
			break
		}
		s.writePacket(&mc.LevelEventPacket{
			EventType: mc.LevelEventUpdateBlockCracking,
			Position:  vec64To32(pos.Vec3()),
			EventData: int32(65535 / (t.BreakTime.Seconds() * 20)),
		})
	case block.DecoratedPotWobbleAction:
		nbt := clientActorNBT(t.DecoratedPot.EncodeNBT(), pos)
		nbt["animation"] = boolByte(t.Success) + 1
		s.writePacket(&mc.BlockActorDataPacket{
			Position: blockPos,
			NBTData:  nbt,
		})
	}
}

// ViewEmote ...
func (s *Session) ViewEmote(player world.Entity, emote uuid.UUID) {
	flags := byte(mc.EmoteFlagServerSide)
	if s.emoteChatMuted {
		flags |= mc.EmoteFlagMuteChat
	}
	s.writePacket(&mc.EmotePacket{
		EntityRuntimeID: s.entityRuntimeID(player),
		EmoteID:         emote.String(),
		Flags:           flags,
	})
}

// ViewSkin ...
func (s *Session) ViewSkin(e world.Entity) {
	if v, ok := e.(Controllable); ok {
		s.writePacket(&mc.PlayerSkinPacket{
			UUID: v.UUID(),
			Skin: skinToProtocol(v.Skin(), s.primary().Version),
		})
	}
}

// ViewWorldSpawn ...
func (s *Session) ViewWorldSpawn(pos cube.Pos) {
	blockPos := mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&mc.SetSpawnPositionPacket{
		SpawnType:     mc.SpawnTypeWorld,
		Position:      blockPos,
		Dimension:     mc.DimensionOverworld,
		SpawnPosition: blockPos,
	})
}

// SetPlayerSpawn updates the player's personal respawn location.
func (s *Session) SetPlayerSpawn(pos cube.Pos, dim world.Dimension) {
	if s == nil {
		return
	}
	id, _ := world.DimensionID(dim)
	blockPos := mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&mc.SetSpawnPositionPacket{
		SpawnType:     mc.SpawnTypePlayer,
		Position:      blockPos,
		Dimension:     int32(id),
		SpawnPosition: blockPos,
	})
}

// ViewWeather ...
func (s *Session) ViewWeather(raining, thunder bool) {
	pk := &mc.LevelEventPacket{
		EventType: mc.LevelEventStopRaining,
	}
	if raining {
		pk.EventType, pk.EventData = mc.LevelEventStartRaining, int32(rand.IntN(50000)+10000)
	}
	s.writePacket(pk)

	pk = &mc.LevelEventPacket{
		EventType: mc.LevelEventStopThunderstorm,
	}
	if thunder {
		pk.EventType, pk.EventData = mc.LevelEventStartThunderstorm, int32(rand.IntN(50000)+10000)
	}
	s.writePacket(pk)
}

// ViewEntityWake ...
func (s *Session) ViewEntityWake(e world.Entity) {
	s.writePacket(&mc.AnimatePacket{
		EntityRuntimeID: s.entityRuntimeID(e),
		ActionType:      mc.AnimateActionStopSleep,
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
	if s.menu.Swap(nil) == nil {
		return false
	}
	containerType := byte(s.openedContainerID.Load())
	windowID := byte(s.openedWindowID.Load())
	s.openedContainerID.Store(0)
	if !clientRequested {
		s.writePacket(&mc.ContainerClosePacket{
			WindowID:      windowID,
			ContainerType: containerType,
			ServerSide:    true,
		})
	}
	return true
}

func (s *Session) entityLinks(e world.Entity) []mc.EntityLink {
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
	out := make([]mc.EntityLink, 0, len(links))
	for _, link := range links {
		if link.Ridden == nil || link.Rider == nil {
			continue
		}
		riddenID := s.handleRuntimeID(link.Ridden)
		riderID := s.handleRuntimeID(link.Rider)
		if riddenID == 0 || riderID == 0 {
			continue
		}
		linkType := byte(mc.EntityLinkRemove)
		switch link.Type {
		case entity.LinkRider:
			linkType = byte(mc.EntityLinkRider)
		case entity.LinkPassenger:
			linkType = byte(mc.EntityLinkPassenger)
		}
		out = append(out, mc.EntityLink{
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
func blockPosFromProtocol(pos mc.BlockPos) cube.Pos {
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
