package session

// particleRow is a row of particleRows: a particle of server/world/particle
// whose packet is a LevelEventPacket of the same event whatever the value of
// the particle is, bar the data a row takes from a field.
//
// The names of the constants are those of the protocol (package v2225, the
// protocol the session is compiled against): the generated code names them,
// so that a misspelt one does not compile.
type particleRow struct {
	// Particle is the name of the type in server/world/particle.
	Particle string

	// Level is the name of the constant of LevelEvent that is the event.
	// Legacy sets the event to that of a legacy particle instead: the
	// LevelEventParticleLegacyEvent flag with the particle type, a number or
	// the name of a constant of ParticleType.
	Level  string
	Legacy string

	// Bool, Hash, RGBA, ARGB and Field, if one is set, set the event data to
	// that of the field of the particle it names: Bool is 1 for a true bool
	// and 0, Hash the network hash of a block, RGBA a colour as the bytes
	// red, green, blue, alpha, ARGB a colour as alpha, red, green, blue, and
	// Field an integer field as it is.
	Bool  string
	Hash  string
	RGBA  string
	ARGB  string
	Field string
}

// particleRows holds the particles whose packets particle_switch_gen.go
// writes. The rest of the particles, those whose packets depend on more than
// a field read, are the cases of particlePackets. Run go generate to write
// the switch from this table: it fails for a row of a type that is no
// world.Particle.
//
//go:generate go run ./internal/gentables -kind particle
var particleRows = []particleRow{
	{Particle: "HugeExplosion", Level: "LevelEventParticlesExplosion"},
	{Particle: "EndermanTeleport", Level: "LevelEventParticlesTeleport"},
	{Particle: "EyeOfEnderDeath", Level: "LevelEventParticlesEyeOfEnderDeath"},
	{Particle: "BoneMeal", Level: "LevelEventParticleCropGrowth", Bool: "Area"},
	{Particle: "BlockForceField", Level: "LevelEventParticleDenyBlock"},
	{Particle: "BlockBreak", Level: "LevelEventParticlesDestroyBlock", Hash: "Block"},
	{Particle: "DragonDestroyBlock", Legacy: "ParticleTypeDragonDestroyBlock"},
	{Particle: "Evaporate", Level: "LevelEventParticlesEvaporateWater"},
	{Particle: "SnowballPoof", Legacy: "15"},
	{Particle: "SonicExplosion", Level: "LevelEventSonicExplosion"},
	{Particle: "WindBurst", Level: "LevelEventParticlesWindExplosion"},
	{Particle: "Effect", Legacy: "34", ARGB: "Colour"},
	{Particle: "EntityFlame", Legacy: "19"},
	{Particle: "Dust", Legacy: "33", RGBA: "Colour"},
	{Particle: "WaterDrip", Legacy: "28"},
	{Particle: "WaterSplash", Legacy: "ParticleTypeWaterSplash"},
	{Particle: "LavaDrip", Legacy: "29"},
	{Particle: "Lava", Legacy: "10"},
	{Particle: "DustPlume", Legacy: "88"},
	{Particle: "SculkSoul", Legacy: "ParticleTypeSculkSoul"},
	{Particle: "DripstoneDrip", Level: "LevelEventParticlesDripstoneDrip"},
	{Particle: "PointedDripstoneLand", Level: "LevelEventSoundPointedDripstoneLand"},
	{Particle: "EggCrack", Level: "LevelEventParticleTurtleEgg"},
	{Particle: "ElectricSpark", Level: "LevelEventParticlesElectricSpark", Field: "Data"},
	{Particle: "TrialSpawnerSpawning", Level: "LevelEventParticlesTrialSpawnerSpawning", Bool: "Ominous"},
	{Particle: "TrialSpawnerEjecting", Level: "LevelEventParticlesTrialSpawnerEjecting"},
	{Particle: "TrialSpawnerBecomeOminous", Level: "LevelEventParticlesTrialSpawnerBecomeCharged"},
	{Particle: "VaultActivate", Level: "LevelEventAnimationVaultActivate", Bool: "Ominous"},
	{Particle: "VaultDeactivate", Level: "LevelEventAnimationVaultDeactivate", Bool: "Ominous"},
	{Particle: "VaultEjectItem", Level: "LevelEventAnimationVaultEjectItem"},
}
