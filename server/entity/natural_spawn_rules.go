package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

// This file ports SpawnPlacements, the spawn rules of the mobs and the
// checks NaturalSpawner makes of the blocks a mob spawns in.

// spawnPlacementType is a SpawnPlacementType.
type spawnPlacementType uint8

const (
	placementOnGround spawnPlacementType = iota
	placementInWater
	placementInLava
	placementNoRestrictions
)

// spawnPlacementOf ports SpawnPlacements.getPlacementType and
// getHeightmapType: where a mob of a type spawns and whether it counts
// leaves as ground.
func spawnPlacementOf(id string) (placement spawnPlacementType, leaves bool) {
	switch id {
	case "minecraft:axolotl", "minecraft:cod", "minecraft:dolphin", "minecraft:drowned", "minecraft:guardian", "minecraft:pufferfish",
		"minecraft:salmon", "minecraft:squid", "minecraft:tropical_fish", "minecraft:glow_squid", "minecraft:nautilus", "minecraft:elder_guardian":
		return placementInWater, false
	case "minecraft:strider":
		return placementInLava, false
	case "minecraft:ocelot", "minecraft:parrot":
		return placementOnGround, true
	case "minecraft:evoker", "minecraft:fox", "minecraft:illusioner", "minecraft:panda", "minecraft:phantom", "minecraft:shulker",
		"minecraft:trader_llama", "minecraft:vex", "minecraft:vindicator", "minecraft:warden":
		return placementNoRestrictions, false
	}
	if _, ok := mobCategories[id]; ok || id == "minecraft:iron_golem" || id == "minecraft:snow_golem" || id == "minecraft:villager" {
		return placementOnGround, false
	}
	return placementNoRestrictions, false
}

// spawnPositionOk ports SpawnPlacementType.isSpawnPositionOk.
func spawnPositionOk(tx *world.Tx, id string, pos cube.Pos) bool {
	placement, _ := spawnPlacementOf(id)
	switch placement {
	case placementOnGround:
		if !tx.World().WithinBorder(pos) {
			return false
		}
		below := pos.Side(cube.FaceDown)
		return blockValidSpawn(tx, below, id) && validEmptySpawnBlock(tx, pos, id) && validEmptySpawnBlock(tx, pos.Side(cube.FaceUp), id)
	case placementInWater:
		if !tx.World().WithinBorder(pos) {
			return false
		}
		return waterAt(tx, pos) && !redstoneConductorAt(tx, pos.Side(cube.FaceUp))
	case placementInLava:
		l, ok := tx.Liquid(pos)
		return ok && l.LiquidType() == "lava"
	}
	return true
}

// blockShape returns the source shape metadata of the block at pos.
func blockShape(tx *world.Tx, pos cube.Pos) (gamedata.GenerationShape, bool) {
	id, err := tx.BlockStateID(pos)
	if err != nil {
		return gamedata.GenerationShape{}, false
	}
	shape, err := gamedata.BlockGenerationShapeID(id)
	return shape, err == nil
}

func redstoneConductorAt(tx *world.Tx, pos cube.Pos) bool {
	shape, ok := blockShape(tx, pos)
	return ok && shape.RedstoneConductor
}

// blockValidSpawn ports BlockState.isValidSpawn for a mob of a type standing
// on the block at pos: a block sturdy on top that is not too bright, with
// the exceptions Blocks registers.
func blockValidSpawn(tx *world.Tx, pos cube.Pos, id string) bool {
	b, loaded := tx.BlockLoaded(pos)
	if !loaded {
		return false
	}
	switch b.(type) {
	case block.Bedrock, block.Glass, block.StainedGlass, block.Barrier, block.Scaffolding, block.ChorusFlower, block.WoodTrapdoor, block.CopperTrapdoor, block.IronTrapdoor:
		return false
	case block.SoulSand, block.LitPumpkin, block.Mud:
		return true
	case block.Pumpkin:
		if b.(block.Pumpkin).Carved {
			return true
		}
	case block.RedstoneLamp:
		return true
	case block.Ice, block.FrostedIce:
		return id == "minecraft:polar_bear"
	case block.Magma:
		return fireImmuneTypes[world.BedrockEntityIdentifier(id)] || fireImmuneTypes[id]
	case block.Leaves, block.FireflyBush:
		// Blocks.ocelotOrParrot.
		return id == "minecraft:ocelot" || id == "minecraft:parrot"
	}
	if blockInTag(b, "leaves") {
		return id == "minecraft:ocelot" || id == "minecraft:parrot"
	}
	if blockInTag(b, "impermeable") || blockInTag(b, "trapdoors") {
		return false
	}
	shape, ok := blockShape(tx, pos)
	if !ok || shape.Sturdy&(1<<uint(cube.FaceUp)) == 0 {
		return false
	}
	if l, ok := b.(interface{ LightEmissionLevel() uint8 }); ok && l.LightEmissionLevel() >= 14 {
		return false
	}
	return true
}

// validEmptySpawnBlock ports NaturalSpawner.isValidEmptySpawnBlock: a mob may
// stand in a block that does not fill it, gives no redstone power, holds no
// fluid and does not hurt it.
func validEmptySpawnBlock(tx *world.Tx, pos cube.Pos, id string) bool {
	b, loaded := tx.BlockLoaded(pos)
	if !loaded {
		return false
	}
	if shape, ok := blockShape(tx, pos); ok && (shape.CollisionFull || shape.SignalSource) {
		return false
	}
	if _, ok := tx.Liquid(pos); ok {
		return false
	}
	if blockInTag(b, "prevent_mob_spawning_inside") {
		return false
	}
	return !blockDangerous(b, id)
}

// blockDangerous ports EntityType.isBlockDangerous.
func blockDangerous(b world.Block, id string) bool {
	switch id {
	case "minecraft:fox":
		if blockInTag(b, "fox_immune_to") {
			return false
		}
	case "minecraft:polar_bear":
		if blockInTag(b, "polar_bear_immune_to") {
			return false
		}
	case "minecraft:snow_golem":
		if blockInTag(b, "snow_golem_immune_to") {
			return false
		}
	case "minecraft:stray":
		if blockInTag(b, "stray_immune_to") {
			return false
		}
	case "minecraft:wither":
		if blockInTag(b, "wither_immune_to") {
			return false
		}
	case "minecraft:wither_skeleton":
		if blockInTag(b, "wither_skeleton_immune_to") {
			return false
		}
	default:
		if blockInTag(b, "default_immune_to") {
			return false
		}
	}
	if !fireImmuneTypes[world.BedrockEntityIdentifier(id)] && !fireImmuneTypes[id] && burningBlock(b) {
		return true
	}
	switch v := b.(type) {
	case block.Cactus, block.SweetBerries, block.PowderSnow:
		return true
	case block.Flower:
		return v.Type == block.WitherRose()
	}
	return false
}

// burningBlock ports NodeEvaluator.isBurningBlock.
func burningBlock(b world.Block) bool {
	switch v := b.(type) {
	case block.Lava, block.Magma:
		return true
	case block.Campfire:
		return !v.Extinguished
	case block.Cauldron:
		return v.FillLevel > 0 && v.Liquid == block.CauldronLava
	}
	return blockInTag(b, "fire")
}

func waterAt(tx *world.Tx, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	return ok && l.LiquidType() == "water"
}

func lavaAt(tx *world.Tx, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	return ok && l.LiquidType() == "lava"
}

// canSeeSkyFromBelowWater ports LevelReader.canSeeSkyFromBelowWater.
func canSeeSkyFromBelowWater(tx *world.Tx, pos cube.Pos) bool {
	sea := seaLevel(tx)
	if pos[1] >= sea {
		return canSeeSky(tx, pos)
	}
	scan := cube.Pos{pos[0], sea, pos[2]}
	if !canSeeSky(tx, scan) {
		return false
	}
	for y := sea - 1; y > pos[1]; y-- {
		p := cube.Pos{pos[0], y, pos[2]}
		if _, liquid := tx.Liquid(p); liquid {
			continue
		}
		if b := tx.Block(p); !blockLetsLightThrough(b) {
			return false
		}
	}
	return canSeeSky(tx, pos)
}

// blockLetsLightThrough reports whether a block dampens no light.
func blockLetsLightThrough(b world.Block) bool {
	if l, ok := b.(interface{ LightDiffusionLevel() uint8 }); ok {
		return l.LightDiffusionLevel() == 0
	}
	return true
}

// isBrightEnoughToSpawn ports Animal.isBrightEnoughToSpawn.
func isBrightEnoughToSpawn(tx *world.Tx, pos cube.Pos) bool {
	return max(int(tx.BlockLight(pos)), int(tx.SkyLight(pos))) > 8
}

// isDarkEnoughToSpawn ports Monster.isDarkEnoughToSpawn with the monster
// settings of the dimension.
func isDarkEnoughToSpawn(tx *world.Tx, pos cube.Pos, r *vanilla.Random) bool {
	if int(tx.SkyLight(pos)) > int(r.NextInt(32)) {
		return false
	}
	blockLimit, lightTest := 0, func() int { return int(r.NextInt(8)) }
	switch tx.World().Dimension() {
	case world.Nether:
		blockLimit, lightTest = 15, func() int { return 7 }
	case world.End:
		lightTest = func() int { return 15 }
	}
	if blockLimit < 15 && int(tx.BlockLight(pos)) > blockLimit {
		return false
	}
	brightness := tx.RawBrightness(pos)
	if tx.Thundering() {
		brightness = max(int(tx.BlockLight(pos)), int(tx.SkyLight(pos))-10)
	}
	return brightness <= lightTest()
}

// checkMobSpawnRules ports Mob.checkMobSpawnRules.
func checkMobSpawnRules(tx *world.Tx, id string, pos cube.Pos) bool {
	return blockValidSpawn(tx, pos.Side(cube.FaceDown), id)
}

// checkMonsterSpawnRules ports Monster.checkMonsterSpawnRules.
func checkMonsterSpawnRules(tx *world.Tx, id string, pos cube.Pos, r *vanilla.Random) bool {
	return isDarkEnoughToSpawn(tx, pos, r) && checkMobSpawnRules(tx, id, pos)
}

// checkAnimalSpawnRules ports Animal.checkAnimalSpawnRules for natural
// spawns.
func checkAnimalSpawnRules(tx *world.Tx, pos cube.Pos, tag string) bool {
	return blockInTag(tx.Block(pos.Side(cube.FaceDown)), tag) && isBrightEnoughToSpawn(tx, pos)
}

// checkSurfaceWaterSpawnRules ports WaterAnimal.checkSurfaceWaterAnimalSpawnRules.
func checkSurfaceWaterSpawnRules(tx *world.Tx, pos cube.Pos, depth, ceiling int) bool {
	sea := seaLevel(tx)
	return pos[1] >= sea-depth && pos[1] <= sea-ceiling && waterAt(tx, pos.Side(cube.FaceDown)) && waterBlockAt(tx, pos.Side(cube.FaceUp))
}

// waterBlockAt reports whether the block at pos is water itself, as
// BlockState.is(Blocks.WATER).
func waterBlockAt(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(block.Water)
	return ok
}

// biomeHasTag reports whether the biome at pos is in a biome tag.
func biomeHasTag(tx *world.Tx, pos cube.Pos, tag string) bool {
	for _, t := range tx.Biome(pos).Tags() {
		if t == tag {
			return true
		}
	}
	return false
}

// nearestPlayerWithin reports whether a player in survival or adventure is
// within dist of pos, as Level.getNearestPlayer with creative players
// filtered out.
func nearestPlayerWithin(tx *world.Tx, pos cube.Pos, dist float64) bool {
	centre := pos.Vec3Centre()
	for p := range tx.Players() {
		if m, ok := p.(interface{ GameMode() world.GameMode }); ok && (m.GameMode() == world.GameModeCreative || m.GameMode() == world.GameModeSpectator) {
			continue
		}
		if p.Position().Sub(centre).Len() < dist {
			return true
		}
	}
	return false
}

// checkSpawnRules ports SpawnPlacements.checkSpawnRules for natural spawns.
func checkSpawnRules(tx *world.Tx, id string, pos cube.Pos, r *vanilla.Random) bool {
	peaceful := tx.World().Difficulty() == world.DifficultyPeaceful
	if peaceful && notInPeaceful[id] {
		return false
	}
	below := pos.Side(cube.FaceDown)
	switch id {
	case "minecraft:axolotl":
		return blockInTag(tx.Block(below), "axolotls_spawnable_on")
	case "minecraft:cod", "minecraft:pufferfish", "minecraft:salmon", "minecraft:dolphin", "minecraft:squid":
		return checkSurfaceWaterSpawnRules(tx, pos, 13, 0)
	case "minecraft:tropical_fish":
		return waterAt(tx, below) && waterBlockAt(tx, pos.Side(cube.FaceUp)) &&
			(biomeHasTag(tx, pos, "allows_tropical_fish_spawns_at_any_height") || checkSurfaceWaterSpawnRules(tx, pos, 13, 0))
	case "minecraft:nautilus":
		return checkSurfaceWaterSpawnRules(tx, pos, 25, 5)
	case "minecraft:glow_squid":
		return pos[1] <= seaLevel(tx)-33 && tx.RawBrightness(pos) == 0 && waterBlockAt(tx, pos)
	case "minecraft:drowned":
		return checkDrownedSpawnRules(tx, pos, r)
	case "minecraft:guardian", "minecraft:elder_guardian":
		return (r.NextInt(20) == 0 || !canSeeSkyFromBelowWater(tx, pos)) && !peaceful && waterAt(tx, pos) && waterAt(tx, below)
	case "minecraft:armadillo":
		return checkAnimalSpawnRules(tx, pos, "armadillo_spawnable_on")
	case "minecraft:bat":
		if pos[1] >= tx.HighestBlock(pos[0], pos[2])+1 || r.NextBoolean() || tx.RawBrightness(pos) > int(r.NextInt(4)) {
			return false
		}
		return blockInTag(tx.Block(below), "bats_spawnable_on") && checkMobSpawnRules(tx, id, pos)
	case "minecraft:blaze", "minecraft:breeze", "minecraft:zoglin":
		return checkMobSpawnRules(tx, id, pos)
	case "minecraft:camel":
		return checkAnimalSpawnRules(tx, pos, "camels_spawnable_on")
	case "minecraft:camel_husk", "minecraft:husk", "minecraft:parched":
		return checkMonsterSpawnRules(tx, id, pos, r) && canSeeSky(tx, pos)
	case "minecraft:chicken", "minecraft:cow", "minecraft:donkey", "minecraft:happy_ghast", "minecraft:horse", "minecraft:llama",
		"minecraft:mule", "minecraft:pig", "minecraft:sheep", "minecraft:cat", "minecraft:panda", "minecraft:trader_llama", "minecraft:skeleton_horse":
		return checkAnimalSpawnRules(tx, pos, "animals_spawnable_on")
	case "minecraft:endermite", "minecraft:silverfish":
		return checkMobSpawnRules(tx, id, pos) && !nearestPlayerWithin(tx, pos, 5)
	case "minecraft:ender_dragon", "minecraft:iron_golem", "minecraft:snow_golem", "minecraft:villager", "minecraft:phantom",
		"minecraft:shulker", "minecraft:wandering_trader":
		return checkMobSpawnRules(tx, id, pos)
	case "minecraft:frog":
		return checkAnimalSpawnRules(tx, pos, "frogs_spawnable_on")
	case "minecraft:ghast":
		return !peaceful && r.NextInt(20) == 0 && checkMobSpawnRules(tx, id, pos)
	case "minecraft:goat":
		return checkAnimalSpawnRules(tx, pos, "goats_spawnable_on")
	case "minecraft:magma_cube":
		return !peaceful
	case "minecraft:sulfur_cube":
		return true
	case "minecraft:mooshroom":
		return checkAnimalSpawnRules(tx, pos, "mooshrooms_spawnable_on")
	case "minecraft:ocelot":
		return r.NextInt(3) != 0
	case "minecraft:parrot":
		return checkAnimalSpawnRules(tx, pos, "parrots_spawnable_on")
	case "minecraft:hoglin", "minecraft:piglin":
		_, wart := tx.Block(below).(block.NetherWartBlock)
		return !wart
	case "minecraft:zombified_piglin":
		_, wart := tx.Block(below).(block.NetherWartBlock)
		return !peaceful && !wart
	case "minecraft:pillager":
		return tx.BlockLight(pos) <= 8 && checkMobSpawnRules(tx, id, pos)
	case "minecraft:polar_bear":
		if !biomeHasTag(tx, pos, "polar_bears_spawn_on_alternate_blocks") {
			return checkAnimalSpawnRules(tx, pos, "animals_spawnable_on")
		}
		return isBrightEnoughToSpawn(tx, pos) && blockInTag(tx.Block(below), "polar_bears_spawnable_on_alternate")
	case "minecraft:rabbit":
		return checkAnimalSpawnRules(tx, pos, "rabbits_spawnable_on")
	case "minecraft:slime":
		return checkSlimeSpawnRules(tx, pos, r)
	case "minecraft:stray":
		sky := pos
		for {
			sky = sky.Side(cube.FaceUp)
			if _, snow := tx.Block(sky).(block.PowderSnow); !snow {
				break
			}
		}
		return checkMonsterSpawnRules(tx, id, pos, r) && canSeeSky(tx, sky.Side(cube.FaceDown))
	case "minecraft:strider":
		check := pos
		for {
			check = check.Side(cube.FaceUp)
			if !lavaAt(tx, check) {
				break
			}
		}
		_, air := tx.Block(check).(block.Air)
		return air
	case "minecraft:turtle":
		return pos[1] < seaLevel(tx)+4 && blockInTag(tx.Block(below), "sand") && isBrightEnoughToSpawn(tx, pos)
	case "minecraft:wolf":
		return checkAnimalSpawnRules(tx, pos, "wolves_spawnable_on")
	case "minecraft:fox":
		return checkAnimalSpawnRules(tx, pos, "foxes_spawnable_on")
	}
	if mobCategories[id] == spawnMonster {
		return checkMonsterSpawnRules(tx, id, pos, r)
	}
	return true
}

// checkDrownedSpawnRules ports Drowned.checkDrownedSpawnRules for natural
// spawns.
func checkDrownedSpawnRules(tx *world.Tx, pos cube.Pos, r *vanilla.Random) bool {
	if !waterAt(tx, pos.Side(cube.FaceDown)) {
		return false
	}
	can := tx.World().Difficulty() != world.DifficultyPeaceful && isDarkEnoughToSpawn(tx, pos, r) && waterAt(tx, pos)
	if biomeHasTag(tx, pos, "more_frequent_drowned_spawns") {
		return r.NextInt(15) == 0 && can
	}
	// Drowned.isDeepEnoughToSpawn.
	return r.NextInt(40) == 0 && pos[1] < seaLevel(tx)-5 && can
}

// checkSlimeSpawnRules ports Slime.checkSlimeSpawnRules for natural spawns:
// slimes spawn in the dark at the surface of swamps, and deep down in slime
// chunks.
func checkSlimeSpawnRules(tx *world.Tx, pos cube.Pos, r *vanilla.Random) bool {
	if tx.World().Difficulty() == world.DifficultyPeaceful {
		return false
	}
	if biomeHasTag(tx, pos, "allows_surface_slime_spawns") && pos[1] > 50 && pos[1] < 70 {
		chance := world.JavaBiomeAttribute(world.JavaBiomeName(tx.Biome(pos)), "minecraft:gameplay/surface_slime_spawn_chance", 0)
		if r.NextFloat() < chance && tx.RawBrightness(pos) <= int(r.NextInt(8)) {
			return checkMobSpawnRules(tx, "minecraft:slime", pos)
		}
	}
	if r.NextInt(10) == 0 && slimeChunk(tx.World().Seed(), int32(pos[0]>>4), int32(pos[2]>>4)) && pos[1] < 40 {
		return checkMobSpawnRules(tx, "minecraft:slime", pos)
	}
	return false
}

// slimeChunk ports WorldgenRandom.seedSlimeChunk(x, z, seed, 987234911): a
// tenth of the chunks are slime chunks.
func slimeChunk(seed int64, x, z int32) bool {
	s := seed + int64(x*x*4987142) + int64(x*5947611) + int64(z*z)*4392871 + int64(z*389711) ^ 987234911
	r := vanilla.NewLegacyRandom(s)
	return r.NextInt(10) == 0
}

// mobSpawnWalkTarget ports PathfinderMob.checkSpawnRules: a pathfinding mob
// spawns only where it would like to walk.
func mobSpawnWalkTarget(m *Mob, id string, pos cube.Pos) bool {
	switch id {
	case "minecraft:slime", "minecraft:magma_cube", "minecraft:sulfur_cube", "minecraft:ghast", "minecraft:phantom", "minecraft:bat", "minecraft:ender_dragon":
		// Mob.checkSpawnRules: these are no PathfinderMob.
		return true
	}
	if w, ok := m.Behaviour().(mobWalkTarget); ok {
		return w.walkTargetValue(m, pos) >= 0
	}
	if javaAnimalClasses[id] {
		if _, grass := m.tx.Block(pos.Side(cube.FaceDown)).(block.Grass); grass {
			return true
		}
		return pathfindingLightCost(m.tx, pos) >= 0
	}
	if mobCategories[id] == spawnMonster {
		return -pathfindingLightCost(m.tx, pos) >= 0
	}
	return true
}

// mobSpawnObstruction ports Mob.checkSpawnObstruction and its overrides: a
// mob does not spawn in a fluid, unless it lives in one, nor into another
// living entity.
func mobSpawnObstruction(tx *world.Tx, id string, box cube.BBox) bool {
	unobstructed := !livingEntityIn(tx, box)
	switch id {
	case "minecraft:guardian", "minecraft:elder_guardian", "minecraft:drowned", "minecraft:strider", "minecraft:nautilus", "minecraft:zombie_nautilus",
		"minecraft:axolotl", "minecraft:dolphin", "minecraft:squid", "minecraft:glow_squid", "minecraft:cod", "minecraft:salmon",
		"minecraft:pufferfish", "minecraft:tropical_fish", "minecraft:tadpole":
		return unobstructed
	case "minecraft:ravager":
		return !liquidIn(tx, box)
	case "minecraft:ocelot":
		if !unobstructed || liquidIn(tx, box) {
			return false
		}
		pos := cube.PosFromVec3(box.Min().Add(box.Max()).Mul(.5))
		pos[1] = int(math.Floor(box.Min()[1]))
		if pos[1] < seaLevel(tx) {
			return false
		}
		below := tx.Block(pos.Side(cube.FaceDown))
		_, grass := below.(block.Grass)
		return grass || blockInTag(below, "leaves")
	}
	return unobstructed && !liquidIn(tx, box)
}

// livingEntityIn reports whether a living entity's box overlaps box, as
// LevelReader.isUnobstructed finds entities that block building.
func livingEntityIn(tx *world.Tx, box cube.BBox) bool {
	for e := range tx.EntitiesWithin(box) {
		if l, ok := e.(Living); ok && !l.Dead() && !spectator(e) && e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			return true
		}
	}
	return false
}

// liquidIn ports LevelReader.containsAnyLiquid.
func liquidIn(tx *world.Tx, box cube.BBox) bool {
	for x := int(math.Floor(box.Min()[0])); x < int(math.Ceil(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])); y < int(math.Ceil(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z < int(math.Ceil(box.Max()[2])); z++ {
				if _, ok := tx.Liquid(cube.Pos{x, y, z}); ok {
					return true
				}
			}
		}
	}
	return false
}

// spawnNoCollision ports Level.noCollision for a spawn box: no block and no
// boat or shulker collides with it.
func spawnNoCollision(tx *world.Tx, box cube.BBox) bool {
	for x := int(math.Floor(box.Min()[0])) - 1; x <= int(math.Ceil(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])) - 1; y <= int(math.Ceil(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])) - 1; z <= int(math.Ceil(box.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				b, ok := tx.BlockLoaded(pos)
				if !ok {
					return false
				}
				for _, bb := range b.Model().BBox(pos, tx) {
					if bb.Translate(pos.Vec3()).IntersectsWith(box) {
						return false
					}
				}
			}
		}
	}
	for e := range tx.EntitiesWithin(box) {
		switch e.H().Type().EncodeEntity() {
		case "minecraft:boat", "minecraft:chest_boat", "minecraft:shulker":
			if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
				return false
			}
		}
	}
	return true
}

// maxSpawnClusterSize ports Mob.getMaxSpawnClusterSize.
func maxSpawnClusterSize(id string) int {
	switch id {
	case "minecraft:cod", "minecraft:salmon", "minecraft:tropical_fish", "minecraft:pufferfish", "minecraft:wolf":
		return 8
	case "minecraft:pillager", "minecraft:happy_ghast", "minecraft:ghast":
		return 1
	case "minecraft:horse", "minecraft:donkey", "minecraft:mule", "minecraft:skeleton_horse", "minecraft:zombie_horse", "minecraft:llama",
		"minecraft:trader_llama", "minecraft:camel", "minecraft:camel_husk":
		return 6
	}
	return 4
}
