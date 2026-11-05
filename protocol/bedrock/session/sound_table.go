package session

// soundRow is a row of soundRows: a sound of server/world/sound whose packets
// are the same whatever the value of the sound is, bar the field a row names,
// and the packets they are.
//
// A row is a Level, an Event or a Play row, by the field it sets. The names
// of the constants are those of the protocol (package v2225, the protocol the
// session is compiled against): the generated code names them, so that a
// misspelt one does not compile.
type soundRow struct {
	// Sound is the name of the type in server/world/sound.
	Sound string

	// Level is the level event that plays the sound, as a LevelEventPacket:
	// the name of a constant of LevelEvent. Data, if set, is the Go expression
	// of the event data.
	Level string
	Data  string

	// Event is the sound event that plays the sound, as a
	// LevelSoundEventPacket: the name of a constant of SoundEvent. Of the
	// packet, the entity type is ":" unless Entity names another, or NoEntity
	// is set for "", and the extra data is -1 unless one of Hash, ExtraField
	// and Extra sets it: Hash is the name of the field of the sound holding
	// the block whose network hash it is, ExtraField the name of an integer
	// field it is the value of, and Extra an integer constant.
	Event      string
	Entity     string
	NoEntity   bool
	Hash       string
	ExtraField string
	Extra      string

	// Play is the name of the sound a PlaySoundPacket plays at Volume and
	// Pitch.
	Play   string
	Volume float32
	Pitch  float32
}

// soundRows holds the sounds whose packets sound_switch_gen.go writes. The
// rest of the sounds, those whose packets depend on more than a field read,
// are the cases of soundPackets. Run go generate to write the switch from
// this table: it fails for a row of a type that is no world.Sound.
//
//go:generate go run ./internal/gentables -kind sound
var soundRows = []soundRow{
	{Sound: "Click", Level: "LevelEventSoundClick"},
	{Sound: "FireCharge", Level: "LevelEventSoundBlazeFireball"},
	{Sound: "GhastWarning", Level: "LevelEventSoundGhastWarning"},
	{Sound: "GhastShoot", Level: "LevelEventSoundGhastFireball"},
	{Sound: "DispenserLaunch", Level: "LevelEventSoundLaunch", Data: "dispenserLaunchData"},
	{Sound: "DispenserFireball", Level: "LevelEventSoundGhastFireball", Data: "dispenserLaunchData"},
	{Sound: "LodestoneCompassLink", Event: "SoundEventLinkCompassToLodestone"},
	{Sound: "DoorCrash", Level: "LevelEventSoundZombieDoorCrash"},
	{Sound: "Explosion", Event: "SoundEventExplode"},
	{Sound: "Thunder", Event: "SoundEventThunder", Entity: "minecraft:lightning_bolt"},
	{Sound: "SignWaxed", Level: "LevelEventWaxOn"},
	{Sound: "WaxedSignFailedInteraction", Event: "SoundEventWaxedSignInteractFail"},
	{Sound: "WaxRemoved", Level: "LevelEventWaxOff"},
	{Sound: "CopperScraped", Level: "LevelEventScrape"},
	{Sound: "Pop", Level: "LevelEventSoundInfinityArrowPickup"},
	{Sound: "Teleport", Event: "SoundEventTeleport"},
	{Sound: "ItemAdd", Level: "LevelEventSoundAddItem"},
	{Sound: "ItemFrameRemove", Level: "LevelEventSoundItemFrameRemoveItem"},
	{Sound: "ItemFrameRotate", Level: "LevelEventSoundItemFrameRotateItem"},
	{Sound: "HangingPlace", Level: "LevelEventSoundItemFramePlace"},
	{Sound: "HangingBreak", Level: "LevelEventSoundItemFrameBreak"},
	{Sound: "TNT", Level: "LevelEventSoundFuse"},
	{Sound: "FireworkLaunch", Event: "SoundEventLaunch"},
	{Sound: "FireworkHugeBlast", Event: "SoundEventLargeBlast"},
	{Sound: "FireworkBlast", Event: "SoundEventBlast"},
	{Sound: "FireworkTwinkle", Event: "SoundEventTwinkle"},
	{Sound: "FurnaceCrackle", Event: "SoundEventFurnaceUse"},
	{Sound: "CampfireCrackle", Event: "SoundEventCampfireCrackle"},
	{Sound: "BlastFurnaceCrackle", Event: "SoundEventBlastFurnaceUse"},
	{Sound: "SmokerCrackle", Event: "SoundEventSmokerUse"},
	{Sound: "PotionBrewed", Event: "SoundEventPotionBrewed"},
	{Sound: "UseSpyglass", Event: "SoundEventUseSpyglass"},
	{Sound: "StopUsingSpyglass", Event: "SoundEventStopUsingSpyglass"},
	{Sound: "RespawnAnchorAmbient", Event: "SoundEventRespawnAnchorAmbient", ExtraField: "Charge"},
	{Sound: "RespawnAnchorCharge", Event: "SoundEventRespawnAnchorCharge", ExtraField: "Charge"},
	{Sound: "RespawnAnchorDeplete", Event: "SoundEventRespawnAnchorDeplete", ExtraField: "Charge"},
	{Sound: "FireExtinguish", Event: "SoundEventExtinguishFire"},
	{Sound: "Ignite", Event: "SoundEventIgnite"},
	{Sound: "EnderEyePlaced", Event: "SoundEventEnderEyePlaced"},
	{Sound: "EndPortalCreated", Event: "SoundEventEndPortalCreated"},
	{Sound: "Burning", Event: "SoundEventPlayerHurtOnFire"},
	{Sound: "Drowning", Event: "SoundEventPlayerHurtDrown"},
	{Sound: "Burp", Event: "SoundEventBurp"},
	{Sound: "DoorOpen", Event: "SoundEventDoorOpen", Hash: "Block"},
	{Sound: "DoorClose", Event: "SoundEventDoorClose", Hash: "Block"},
	{Sound: "TrapdoorOpen", Event: "SoundEventTrapdoorOpen", Hash: "Block"},
	{Sound: "TrapdoorClose", Event: "SoundEventTrapdoorClose", Hash: "Block"},
	{Sound: "FenceGateOpen", Event: "SoundEventFenceGateOpen", Hash: "Block"},
	{Sound: "FenceGateClose", Event: "SoundEventFenceGateClose", Hash: "Block"},
	{Sound: "PistonIn", Event: "SoundEventPistonIn", NoEntity: true},
	{Sound: "PistonOut", Event: "SoundEventPistonOut", NoEntity: true},
	{Sound: "Deny", Event: "SoundEventDeny"},
	{Sound: "BlockPlace", Event: "SoundEventPlace", Hash: "Block"},
	{Sound: "AnvilLand", Level: "LevelEventSoundAnvilLand"},
	{Sound: "AnvilUse", Level: "LevelEventSoundAnvilUsed"},
	{Sound: "AnvilBreak", Level: "LevelEventSoundAnvilBroken"},
	{Sound: "ChestClose", Event: "SoundEventChestClosed"},
	{Sound: "ChestOpen", Event: "SoundEventChestOpen"},
	{Sound: "EnderChestClose", Event: "SoundEventEnderChestClosed"},
	{Sound: "EnderChestOpen", Event: "SoundEventEnderChestOpen"},
	{Sound: "BarrelClose", Event: "SoundEventBarrelClose"},
	{Sound: "BarrelOpen", Event: "SoundEventBarrelOpen"},
	{Sound: "BellRing", Event: "SoundEventBell", NoEntity: true},
	{Sound: "BlockBreaking", Event: "SoundEventHit", Hash: "Block"},
	{Sound: "ItemBreak", Event: "SoundEventBreak"},
	{Sound: "ItemUseOn", Event: "SoundEventItemUseOn", Hash: "Block"},
	{Sound: "BlockBounce", Event: "SoundEventBounce", Hash: "Block"},
	{Sound: "FrogSpawnHatch", Event: "SoundEventFrogspawnHatched"},
	{Sound: "WindChargeImpact", Event: "SoundEventWindChargeBurst"},
	{Sound: "BreezeWindChargeBurst", Event: "SoundEventBreezeWindChargeBurst"},
	{Sound: "WindChargeShoot", Event: "SoundEventThrow", Entity: "minecraft:player"},
	{Sound: "Fizz", Event: "SoundEventFizz"},
	{Sound: "GlassBreak", Event: "SoundEventGlass"},
	{Sound: "BowShoot", Event: "SoundEventBow"},
	{Sound: "ShieldBlock", Event: "SoundEventShieldBlock"},
	{Sound: "CrossbowShoot", Event: "SoundEventCrossbowShoot"},
	{Sound: "ArrowHit", Event: "SoundEventBowHit"},
	{Sound: "TridentThrow", Event: "SoundEventTridentThrow"},
	{Sound: "TridentHit", Event: "SoundEventTridentHit"},
	{Sound: "TridentReturn", Event: "SoundEventTridentReturn"},
	{Sound: "TridentChanneling", Event: "SoundEventTridentThunder"},
	{Sound: "ItemThrow", Event: "SoundEventThrow", Entity: "minecraft:player"},
	{Sound: "LevelUp", Event: "SoundEventLevelUp", Extra: "0x10000000"},
	{Sound: "Experience", Level: "LevelEventSoundExperienceOrbPickup"},
	{Sound: "MusicDiscEnd", Event: "SoundEventRecordNull"},
	{Sound: "ConduitAttack", Event: "SoundEventConduitAttack"},
	{Sound: "ComposterEmpty", Event: "SoundEventComposterEmpty"},
	{Sound: "BottleFill", Event: "SoundEventBottleFill"},
	{Sound: "ComposterFill", Event: "SoundEventComposterFill"},
	{Sound: "ComposterFillLayer", Event: "SoundEventComposterFillLayer"},
	{Sound: "ComposterReady", Event: "SoundEventComposterReady"},
	{Sound: "PowerOn", Event: "SoundEventPowerOn"},
	{Sound: "PowerOff", Event: "SoundEventPowerOff"},
	{Sound: "LecternBookPlace", Event: "SoundEventLecternBookPlace"},
	{Sound: "Totem", Level: "LevelEventSoundTotemUsed"},
	{Sound: "ShulkerBoxClose", Event: "SoundEventShulkerBoxClosed"},
	{Sound: "ShulkerBoxOpen", Event: "SoundEventShulkerBoxOpen"},
	{Sound: "DecoratedPotInsertFailed", Event: "SoundEventDecoratedPotInsertFail"},
	{Sound: "ArmourStandHit", Level: "LevelEventSoundArmorStandHit"},
	{Sound: "ArmourStandBreak", Level: "LevelEventSoundArmorStandBreak"},
	{Sound: "ArmourStandPlace", Level: "LevelEventSoundArmorStandPlace"},
	{Sound: "ArmourStandLand", Level: "LevelEventSoundArmorStandLand"},
	{Sound: "LightningExplode", Play: "ambient.weather.lightning.impact", Volume: 1, Pitch: 0.7},
	{Sound: "LightningThunder", Play: "ambient.weather.thunder", Volume: 1, Pitch: 1.0},
}
