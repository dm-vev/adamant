package session

import (
	"bytes"
	"image/color"
	"strings"
	"time"

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

// playSound plays a world.Sound at a position, disabling relative volume if set to true.
func (s *Session) playSound(pos mgl64.Vec3, t world.Sound, disableRelative bool) {
	out := s.writePacket
	if bell, ok := t.(sound.BellRing); ok && bell.AfterBlock {
		out = func(pk proto.Packet) { s.enqueuePacketAfterBlock(cube.PosFromVec3(pos), pk) }
	}
	s.soundPackets(pos, t, disableRelative, out)
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
