package session

import (
	"bytes"
	"image/color"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/go-gl/mathgl/mgl64"
)

// particlePackets hands out the packets that show a particle at a position:
// those of the particles of particle_table.go by the generated switch, and
// those of the rest here.
func (s *Session) particlePackets(pos mgl64.Vec3, p world.Particle, out func(proto.Packet)) {
	if s.literalParticlePacket(pos, p, out) {
		return
	}
	switch pa := p.(type) {
	case particle.CreakingTrail:
		out(genericLevelEvent(mc.LevelEventParticleCreakingHeartTrail, map[string]any{
			"CreakingAmount": pa.CreakingAmount,
			"CreakingX":      float32(pa.Creaking.X()),
			"CreakingY":      float32(pa.Creaking.Y()),
			"CreakingZ":      float32(pa.Creaking.Z()),
			"HeartAmount":    pa.HeartAmount,
			"HeartX":         float32(pa.Heart.X()),
			"HeartY":         float32(pa.Heart.Y()),
			"HeartZ":         float32(pa.Heart.Z()),
		}))
	case particle.VibrationSignal:
		out(genericLevelEvent(mc.LevelEventParticlesVibrationSignal, vibrationSignalPayload(pos, pa)))
	case particle.DragonEggTeleport:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesDragonEgg,
			Position:  vec64To32(pos),
			EventData: dragonEggTeleportData(pa.Diff),
		})
	case particle.Note:
		out(&mc.BlockEventPacket{
			EventType: bedrockInstrument(pa.Instrument),
			EventData: int32(pa.Pitch),
			Position:  mc.BlockPos{int32(pos.X()), int32(pos.Y()), int32(pos.Z())},
		})
	case particle.DispenserSmoke:
		direction := cube.Pos{}.Side(pa.Face)
		s.writePacket(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesShoot,
			Position:  vec64To32(pos),
			EventData: int32(direction[0] + 1 + (direction[2]+1)*3),
		})
	case particle.PunchBlock:
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticlesCrackBlock,
			Position:  vec64To32(pos),
			EventData: int32(s.blockHash(pa.Block)) | (int32(pa.Face) << 24),
		})
	case particle.Flame:
		out(flamePacket(pos, pa))
	case particle.EggSmash:
		rid, meta, _ := s.tables().itemRuntimeID(item.Egg{})
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 14,
			EventData: (rid << 16) | int32(meta),
			Position:  vec64To32(pos),
		})
	case particle.Splash:
		out(splashPacket(pos, pa))
	case particle.MobGrowth:
		t := mc.ParticleTypeResetMobGrowth
		if pa.Paused {
			t = mc.ParticleTypePauseMobGrowth
		}
		out(&mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | int32(t),
			Position:  vec64To32(pos),
		})
	case particle.SculkCharge, particle.SculkChargePop:
		out(sculkChargePacket(pos, pa))
	case particle.CauldronEffect:
		out(&mc.LevelEventPacket{
			EventType: cauldronLevelEvents[pa.Kind],
			Position:  vec64To32(pos),
			EventData: int32FromRGBA(pa.Colour),
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
	}
}

// genericLevelEvent returns the LevelEventGenericPacket of an event with the
// payload passed, which it encodes as network little endian NBT without the
// header and the end tag of the compound.
func genericLevelEvent(event int32, payload any) *mc.LevelEventGenericPacket {
	buf := bytes.NewBuffer(nil)
	_ = nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian).Encode(payload)
	data := buf.Bytes()
	return &mc.LevelEventGenericPacket{EventID: event, SerialisedEventData: data[2 : len(data)-1]}
}

// vibrationVector is a position of the payload of a vibration signal.
type vibrationVector struct {
	Type string  `nbt:"type"`
	X    float32 `nbt:"x"`
	Y    float32 `nbt:"y"`
	Z    float32 `nbt:"z"`
}

// vibrationPayload is the payload of the event of a vibration signal.
type vibrationPayload struct {
	Origin     vibrationVector `nbt:"origin"`
	Speed      float32         `nbt:"speed"`
	Target     vibrationVector `nbt:"target"`
	TimeToLive float32         `nbt:"timeToLive"`
}

// vibrationSignalPayload returns the payload of the event of a vibration
// signal that leaves pos.
func vibrationSignalPayload(pos mgl64.Vec3, pa particle.VibrationSignal) vibrationPayload {
	vector := func(v mgl64.Vec3) vibrationVector {
		return vibrationVector{Type: "vec3", X: float32(v.X()), Y: float32(v.Y()), Z: float32(v.Z())}
	}
	return vibrationPayload{
		Origin:     vector(pos),
		Speed:      float32(20),
		Target:     vector(pa.Destination),
		TimeToLive: float32(max(0, pa.TravelTicks)) / 20,
	}
}

// dragonEggTeleportData returns the event data of a dragon egg that teleports
// by diff: the distance on each axis in a byte, with the sign of each as a
// bit above.
func dragonEggTeleportData(diff cube.Pos) int32 {
	xSign, ySign, zSign := 0, 0, 0
	if diff.X() < 0 {
		xSign = 1 << 24
	}
	if diff.Y() < 0 {
		ySign = 1 << 25
	}
	if diff.Z() < 0 {
		zSign = 1 << 26
	}
	return int32((((((abs(diff.X()) << 16) | (abs(diff.Y()) << 8)) | abs(diff.Z())) | xSign) | ySign) | zSign)
}

// sculkChargePacket returns the packet of a sculk charge or of its pop at a
// position, which name the block the position is in.
func sculkChargePacket(pos mgl64.Vec3, p world.Particle) *mc.LevelEventGenericPacket {
	block := cube.PosFromVec3(pos)
	x, y, z := int32(block.X()), int32(block.Y()), int32(block.Z())
	if charge, ok := p.(particle.SculkCharge); ok {
		return genericLevelEvent(mc.LevelEventSculkCharge, sculkChargePayload{
			X: x, Y: y, Z: z, Charge: int16(charge.Charge), Facing: int16(charge.Faces),
		})
	}
	return genericLevelEvent(mc.LevelEventSculkChargePop, sculkPopPayload{X: x, Y: y, Z: z})
}

// sculkPopPayload is the payload of the event of a sculk charge pop: the
// block the pop is in. A struct, as the encoder writes the keys of a map in
// random order.
type sculkPopPayload struct {
	X int32 `nbt:"x"`
	Y int32 `nbt:"y"`
	Z int32 `nbt:"z"`
}

// sculkChargePayload is the payload of the event of a sculk charge: the
// block it is in with the charge and the faces it spreads to.
type sculkChargePayload struct {
	X      int32 `nbt:"x"`
	Y      int32 `nbt:"y"`
	Z      int32 `nbt:"z"`
	Charge int16 `nbt:"charge"`
	Facing int16 `nbt:"facing"`
}

// flamePacket returns the packet of a flame at a position: of the colour it
// has, or the plain one.
func flamePacket(pos mgl64.Vec3, pa particle.Flame) *mc.LevelEventPacket {
	if pa.Colour != (color.RGBA{}) {
		return &mc.LevelEventPacket{
			EventType: mc.LevelEventParticleLegacyEvent | 57,
			Position:  vec64To32(pos),
			EventData: int32FromRGBA(pa.Colour),
		}
	}
	return &mc.LevelEventPacket{
		EventType: mc.LevelEventParticleLegacyEvent | 8,
		Position:  vec64To32(pos),
	}
}

// splashPacket returns the packet of a potion splash at a position, of the
// colour of a potion of no effects where the splash has none.
func splashPacket(pos mgl64.Vec3, pa particle.Splash) *mc.LevelEventPacket {
	if (pa.Colour == color.RGBA{}) {
		pa.Colour, _ = effect.ResultingColour(nil)
	}
	return &mc.LevelEventPacket{
		EventType: mc.LevelEventParticlesPotionSplash,
		EventData: (int32(pa.Colour.A) << 24) | (int32(pa.Colour.R) << 16) | (int32(pa.Colour.G) << 8) | int32(pa.Colour.B),
		Position:  vec64To32(pos),
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
