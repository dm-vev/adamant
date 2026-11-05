package session

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
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

	pitch, yaw, headYaw := entityNetworkRotation(e, e.Rotation())
	metadata := s.entityMetadata(e)
	links := s.entityLinks(e)

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
	id := networkID(e.H().Type())

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
		HeadYaw:          float32(headYaw),
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
	pitch, yaw, headYaw := entityNetworkRotation(e, rot)
	return &mc.MoveActorAbsolutePacket{
		EntityRuntimeID: id,
		Position:        vec64To32(entityNetworkPosition(e, pos)),
		Rotation:        vec64To32(mgl64.Vec3{pitch, yaw, headYaw}),
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
	if bs, ok := e.(bedSleeper); ok {
		if _, sleeping := bs.Sleeping(); sleeping {
			return bedSleeperOffset(bs.SleepFacing())
		}
	}
	if offset, ok := e.H().Type().(OffsetEntity); ok {
		return mgl64.Vec3{0, offset.NetworkOffset()}
	}
	return mgl64.Vec3{}
}

// bedSleeperOffset returns the offset from the centre of the head of a bed, a
// block of the height of the villager, to where BDS puts a mob lying in it:
// 0.375 blocks towards the foot, and 0.6 blocks over the block (sleep_y_offset)
// in place of the 0.6875 Java uses.
func bedSleeperOffset(facing cube.Direction) mgl64.Vec3 {
	dx, dz := bedFacingVector(facing)
	return mgl64.Vec3{-dx * 0.375, 0.6 - 0.6875, -dz * 0.375}
}

// bedFacingVector returns the horizontal step of a direction.
func bedFacingVector(d cube.Direction) (dx, dz float64) {
	switch d {
	case cube.North:
		return 0, -1
	case cube.East:
		return 1, 0
	case cube.South:
		return 0, 1
	}
	return -1, 0
}

// bedFacingYaw returns the yaw that turns a mob towards a direction: south is
// 0, west 90, north 180 and east 270.
func bedFacingYaw(d cube.Direction) float64 {
	switch d {
	case cube.North:
		return 180
	case cube.East:
		return 270
	case cube.South:
		return 0
	}
	return 90
}

// entityNetworkRotation returns the pitch, yaw and head yaw clients see an
// entity turned by. A mob in a bed lies with its head to the pillow, as BDS
// turns it, whatever way it faced.
func entityNetworkRotation(e world.Entity, rot cube.Rotation) (pitch, yaw, headYaw float64) {
	if bs, ok := e.(bedSleeper); ok {
		if _, sleeping := bs.Sleeping(); sleeping {
			yaw = bedFacingYaw(bs.SleepFacing())
			return 0, yaw, yaw
		}
	}
	return rot.Pitch(), rot.Yaw(), entityHeadYaw(e, rot.Yaw())
}

func entityNetworkPosition(e world.Entity, pos mgl64.Vec3) mgl64.Vec3 {
	return pos.Add(entityOffset(e))
}

func entityBasePosition(e world.Entity, pos mgl32.Vec3) mgl32.Vec3 {
	return pos.Sub(vec64To32(entityOffset(e)))
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
	if _, ok := e.(bedSleeper); ok {
		pitch, yaw, _ = entityNetworkRotation(e, e.Rotation())
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

// ViewEntityWake ...
func (s *Session) ViewEntityWake(e world.Entity) {
	s.writePacket(&mc.AnimatePacket{
		EntityRuntimeID: s.entityRuntimeID(e),
		ActionType:      mc.AnimateActionStopSleep,
	})
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
