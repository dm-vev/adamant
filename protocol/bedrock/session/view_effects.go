package session

import (
	"bytes"
	"image/color"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/go-gl/mathgl/mgl64"
)

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

// plainParticleEvents are the level events of particles that carry nothing
// but where they are shown.
var plainParticleEvents = map[world.Particle]int32{
	particle.HugeExplosion{}:    mc.LevelEventParticlesExplosion,
	particle.EndermanTeleport{}: mc.LevelEventParticlesTeleport,
	particle.EyeOfEnderDeath{}:  mc.LevelEventParticlesEyeOfEnderDeath,
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
	case particle.HugeExplosion, particle.EndermanTeleport, particle.EyeOfEnderDeath:
		out(&mc.LevelEventPacket{EventType: plainParticleEvents[p], Position: vec64To32(pos)})
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

// dispenserLaunchData is the EventData BDS 1.26.61.1 sends with the launch and
// fireball events of a dispenser: a pitch of 1.2 as 16.16 fixed point.
const dispenserLaunchData = 78642

// levelEventSound returns the level event, and its data, that plays a sound
// of the world that Bedrock Edition plays with a level event rather than a
// level sound event.
func levelEventSound(t world.Sound) (event, data int32, ok bool) {
	switch t.(type) {
	case sound.Click:
		return mc.LevelEventSoundClick, 0, true
	case sound.FireCharge:
		return mc.LevelEventSoundBlazeFireball, 0, true
	case sound.GhastWarning:
		return mc.LevelEventSoundGhastWarning, 0, true
	case sound.GhastShoot:
		return mc.LevelEventSoundGhastFireball, 0, true
	case sound.DispenserLaunch:
		return mc.LevelEventSoundLaunch, dispenserLaunchData, true
	case sound.DispenserFireball:
		return mc.LevelEventSoundGhastFireball, dispenserLaunchData, true
	}
	return 0, 0, false
}

// soundPackets hands out the packets that play a sound at a position, the
// same for every viewer.
func (s *Session) soundPackets(pos mgl64.Vec3, t world.Sound, disableRelative bool, out func(proto.Packet)) {
	if event, data, ok := levelEventSound(t); ok {
		out(&mc.LevelEventPacket{EventType: event, Position: vec64To32(pos), EventData: data})
		return
	}
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
