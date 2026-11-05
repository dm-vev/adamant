package session

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// levelSoundPacket returns the LevelSoundEventPacket that plays a sound at a
// position, with what a sound that sets no more of it has.
func levelSoundPacket(pos mgl64.Vec3, disableRelative bool) *mc.LevelSoundEventPacket {
	return &mc.LevelSoundEventPacket{
		Position:              vec64To32(pos),
		EntityType:            ":",
		ExtraData:             -1,
		EntityUniqueID:        -1,
		DisableRelativeVolume: disableRelative,
	}
}

// soundPackets hands out the packets that play a sound at a position, the
// same for every viewer: those of the sounds of sound_table.go by the
// generated switch, and those of the rest here.
func (s *Session) soundPackets(pos mgl64.Vec3, t world.Sound, disableRelative bool, out func(proto.Packet)) {
	if s.literalSoundPacket(pos, t, disableRelative, out) {
		return
	}
	pk := levelSoundPacket(pos, disableRelative)
	switch so := t.(type) {
	case sound.Event:
		s.eventSound(pk, so)
	case sound.CreakingHeartTrail:
		pk.SoundType, pk.EntityType = "block.creaking_heart.trail", ""
		pk.ExtraData = int32((float32(so.Progress)*2 - 1) * 1000)
	case sound.EquipItem:
		pk.SoundType = equipSoundEvent(so.Item)
	case sound.Note:
		pk.SoundType = mc.SoundEventNote
		pk.ExtraData = (bedrockInstrument(so.Instrument) << 8) | int32(so.Pitch)
		pk.EntityType = ""
	case sound.GoatHorn:
		pk.SoundType = goatHornSoundEvent(so.Horn)
	case sound.Fall:
		pk.EntityType = "minecraft:player"
		if so.Distance > 4 {
			pk.SoundType = mc.SoundEventFallBig
			break
		}
		pk.SoundType = mc.SoundEventFallSmall
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
	case sound.Attack:
		pk.SoundType, pk.EntityType = mc.SoundEventAttackStrong, "minecraft:player"
		if !so.Damage {
			pk.SoundType = mc.SoundEventAttackNoDamage
		}
	case sound.BucketFill:
		pk.SoundType = bucketSoundEvent(so.Liquid, mc.SoundEventBucketFillWater, mc.SoundEventBucketFillLava)
	case sound.BucketEmpty:
		pk.SoundType = bucketSoundEvent(so.Liquid, mc.SoundEventBucketEmptyWater, mc.SoundEventBucketEmptyLava)
	case sound.CrossbowLoad:
		var ok bool
		if pk.SoundType, ok = crossbowLoadSoundEvent(so.Stage, so.QuickCharge); !ok {
			// Invalid stages should not crash the server; skip the sound instead.
			return
		}
	case sound.TridentRiptide:
		switch so.Level {
		case 3:
			pk.SoundType = mc.SoundEventTridentRiptide3
		case 2:
			pk.SoundType = mc.SoundEventTridentRiptide2
		default:
			pk.SoundType = mc.SoundEventTridentRiptide1
		}
	case sound.MusicDiscPlay:
		var ok bool
		if pk.SoundType, ok = discSoundEvent(so.DiscType); !ok {
			// Unknown disc types should not crash the server; skip the sound instead.
			return
		}
	case sound.DecoratedPotInserted:
		out(&mc.PlaySoundPacket{
			SoundName: "block.decorated_pot.insert",
			Position:  vec64To32(pos),
			Volume:    1,
			Pitch:     0.7 + 0.5*float32(so.Progress),
		})
		return
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
	}
	out(pk)
}

// eventSound sets the fields of the packet that plays a sound.Event: its
// name, the entity type it is made by, the block whose variant plays and the
// source.
func (s *Session) eventSound(pk *mc.LevelSoundEventPacket, so sound.Event) {
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
}

// equipSoundEvent returns the sound event of equipping an item: that of the
// tier of a piece of armour, of an elytra, or the generic one.
func equipSoundEvent(it world.Item) string {
	switch i := it.(type) {
	case item.Helmet:
		return tierToSoundEvent(i.Tier)
	case item.Chestplate:
		return tierToSoundEvent(i.Tier)
	case item.Leggings:
		return tierToSoundEvent(i.Tier)
	case item.Boots:
		return tierToSoundEvent(i.Tier)
	case item.Elytra:
		return mc.SoundEventEquipElytra
	}
	return mc.SoundEventEquipGeneric
}

// goatHornSoundEvent returns the sound event of a goat horn, or "" for a
// horn Bedrock Edition has no sound of.
func goatHornSoundEvent(h sound.Horn) string {
	switch h {
	case sound.Ponder():
		return mc.SoundEventGoatCall0
	case sound.Sing():
		return mc.SoundEventGoatCall1
	case sound.Seek():
		return mc.SoundEventGoatCall2
	case sound.Feel():
		return mc.SoundEventGoatCall3
	case sound.Admire():
		return mc.SoundEventGoatCall4
	case sound.Call():
		return mc.SoundEventGoatCall5
	case sound.Yearn():
		return mc.SoundEventGoatCall6
	case sound.Dream():
		return mc.SoundEventGoatCall7
	}
	return ""
}

// bucketSoundEvent returns the sound event of a bucket that fills or empties
// a liquid: water for water, else lava.
func bucketSoundEvent(l world.Liquid, water, lava string) string {
	if _, ok := l.(block.Water); ok {
		return water
	}
	return lava
}

// crossbowLoadSoundEvent returns the sound event of a stage of the loading
// of a crossbow, and false for a stage that has none.
func crossbowLoadSoundEvent(stage int, quickCharge bool) (string, bool) {
	switch stage {
	case sound.CrossbowLoadingStart:
		if quickCharge {
			return mc.SoundEventCrossbowQuickChargeStart, true
		}
		return mc.SoundEventCrossbowLoadingStart, true
	case sound.CrossbowLoadingMiddle:
		if quickCharge {
			return mc.SoundEventCrossbowQuickChargeMiddle, true
		}
		return mc.SoundEventCrossbowLoadingMiddle, true
	case sound.CrossbowLoadingEnd:
		if quickCharge {
			return mc.SoundEventCrossbowQuickChargeEnd, true
		}
		return mc.SoundEventCrossbowLoadingEnd, true
	}
	return "", false
}

// discSoundEvent returns the sound event that plays a music disc, and false
// for a disc that has none.
func discSoundEvent(d sound.DiscType) (string, bool) {
	switch d {
	case sound.Disc13():
		return mc.SoundEventRecord13, true
	case sound.DiscCat():
		return mc.SoundEventRecordCat, true
	case sound.DiscBlocks():
		return mc.SoundEventRecordBlocks, true
	case sound.DiscChirp():
		return mc.SoundEventRecordChirp, true
	case sound.DiscFar():
		return mc.SoundEventRecordFar, true
	case sound.DiscMall():
		return mc.SoundEventRecordMall, true
	case sound.DiscMellohi():
		return mc.SoundEventRecordMellohi, true
	case sound.DiscStal():
		return mc.SoundEventRecordStal, true
	case sound.DiscStrad():
		return mc.SoundEventRecordStrad, true
	case sound.DiscWard():
		return mc.SoundEventRecordWard, true
	case sound.Disc11():
		return mc.SoundEventRecord11, true
	case sound.DiscWait():
		return mc.SoundEventRecordWait, true
	case sound.DiscOtherside():
		return mc.SoundEventRecordOtherside, true
	case sound.DiscPigstep():
		return mc.SoundEventRecordPigstep, true
	case sound.Disc5():
		return mc.SoundEventRecord5, true
	case sound.DiscRelic():
		return mc.SoundEventRecordRelic, true
	case sound.DiscCreator():
		return mc.SoundEventRecordCreator, true
	case sound.DiscCreatorMusicBox():
		return mc.SoundEventRecordCreatorMusicBox, true
	case sound.DiscPrecipice():
		return mc.SoundEventRecordPrecipice, true
	case sound.DiscTears():
		return mc.SoundEventRecordTears, true
	case sound.DiscLavaChicken():
		return mc.SoundEventRecordLavaChicken, true
	}
	return "", false
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

// dispenserLaunchData is the EventData BDS 1.26.61.1 sends with the launch and
// fireball events of a dispenser: a pitch of 1.2 as 16.16 fixed point.
const dispenserLaunchData = 78642
