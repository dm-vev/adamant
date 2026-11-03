package session

import "testing"

// javaSoundDefinitions pairs each sound the game named by its Java sound
// event ID with the Bedrock definition it played by before, so that Bedrock
// clients keep playing the same sound.
var javaSoundDefinitions = []struct{ java, bedrock string }{
	// LevelEventHandler.levelEvent, 3000: the spawn of an end gateway.
	{"block.end_gateway.spawn", "portal.travel"},
	// Allay.mobInteract.
	{"entity.allay.item_given", "mob.allay.item_given"},
	{"entity.allay.item_taken", "mob.allay.item_taken"},
	// AllayAi.throwItem.
	{"entity.allay.item_thrown", "mob.allay.item_thrown"},
	// Armadillo.brushOffScute.
	{"entity.armadillo.brush", "mob.armadillo.brush"},
	// ArmadilloAi.ArmadilloBallUp.tick.
	{"entity.armadillo.land", "mob.armadillo.land"},
	// Armadillo.handleEntityEvent.
	{"entity.armadillo.peek", "mob.armadillo.peek"},
	// Armadillo.rollUp and rollOut.
	{"entity.armadillo.roll", "mob.armadillo.roll"},
	{"entity.armadillo.unroll_finish", "mob.armadillo.unroll_finish"},
	// Armadillo.customServerAiStep.
	{"entity.armadillo.scute_drop", "mob.armadillo.scute_drop"},
	// ArmadilloAi.ArmadilloBallUp.tick.
	{"entity.armadillo.unroll_start", "mob.armadillo.unroll_start"},
	// Axolotl.playAttackSound.
	{"entity.axolotl.attack", "mob.axolotl.attack"},
	// EnderDragon.tickDeath, global level event 1028.
	{"entity.ender_dragon.death", "mob.enderdragon.death"},
	// LevelEventHandler.levelEvent, 3001: the roar of a dragon fight.
	{"entity.ender_dragon.growl", "mob.enderdragon.growl"},
	// EyeOfEnder.tick.
	{"entity.ender_eye.death", "random.glass"},
	// FrogAi.initTongueActivity (ShootTongue) and Frog.playEatingSound.
	{"entity.frog.eat", "mob.frog.eat"},
	{"entity.frog.tongue", "mob.frog.tongue"},
	// FrogAi.initLongJumpActivity.
	{"entity.frog.long_jump", "mob.frog.jump_to_block"},
	{"entity.frog.step", "mob.frog.step"},
	// Goat.playEatingSound, Goat.getMilkingSound and GoatAi.
	{"entity.goat.eat", "mob.goat.eat"},
	{"entity.goat.horn_break", "mob.goat.horn_break"},
	{"entity.goat.milk", "mob.cow.milk"},
	{"entity.goat.prepare_ram", "mob.goat.prepare_ram"},
	{"entity.goat.ram_impact", "mob.goat.ram_impact"},
	{"entity.goat.screaming.milk", "mob.goat.milk.screamer"},
	{"entity.goat.screaming.prepare_ram", "mob.goat.prepare_ram.screamer"},
	{"entity.goat.screaming.ram_impact", "mob.goat.ram_impact.screamer"},
	{"entity.goat.step", "mob.goat.step"},
	// HoglinAi.getSoundForActivity, Hoglin.doHurtTarget and
	// Hoglin.customServerAiStep.
	{"entity.hoglin.ambient", "mob.hoglin.ambient"},
	{"entity.hoglin.angry", "mob.hoglin.angry"},
	{"entity.hoglin.attack", "mob.hoglin.attack"},
	{"entity.hoglin.converted_to_zombified", "mob.hoglin.converted_to_zombified"},
	{"entity.hoglin.retreat", "mob.hoglin.retreat"},
	// The equip sound of horse armour, Item.Properties.horseArmor.
	{"entity.horse.armor", "mob.horse.armor"},
	// PiglinAi.getSoundForActivity, and the playConvertedSound and
	// playAngrySound of Piglin and PiglinBrute.
	{"entity.piglin.admiring_item", "mob.piglin.admiring_item"},
	{"entity.piglin.ambient", "mob.piglin.ambient"},
	{"entity.piglin.angry", "mob.piglin.angry"},
	{"entity.piglin.celebrate", "mob.piglin.celebrate"},
	{"entity.piglin.converted_to_zombified", "mob.piglin.converted_to_zombified"},
	{"entity.piglin.jealous", "mob.piglin.jealous"},
	{"entity.piglin.retreat", "mob.piglin.retreat"},
	{"entity.piglin_brute.angry", "mob.piglin_brute.angry"},
	{"entity.piglin_brute.converted_to_zombified", "mob.piglin_brute.converted_to_zombified"},
	// Sniffer.dropSeed and Sniffer.playEatingSound.
	{"entity.sniffer.drop_seed", "mob.sniffer.drop_seed"},
	{"entity.sniffer.eat", "mob.sniffer.eat"},
	// Tadpole.ageUp.
	{"entity.tadpole.grow_up", "mob.tadpole.convert_to_frog"},
	// WitherBoss.customServerAiStep (level events 1022 and 1023),
	// performRangedAttack (1024) and getDeathSound.
	{"entity.wither.break_block", "mob.wither.break_block"},
	{"entity.wither.death", "mob.wither.death"},
	{"entity.wither.shoot", "mob.wither.shoot"},
	{"entity.wither.spawn", "mob.wither.spawn"},
	// Zoglin.doHurtTarget and Zoglin.playAngrySound.
	{"entity.zoglin.angry", "mob.zoglin.angry"},
	{"entity.zoglin.attack", "mob.zoglin.attack"},
	// LeashFenceKnotEntity.playPlacementSound.
	{"item.lead.tied", "leashknot.place"},
}

// TestJavaSoundNamesKeepTheirDefinitions holds every sound the game renamed
// from its Bedrock definition to its Java sound event ID to the definition
// Bedrock clients were sent before.
func TestJavaSoundNamesKeepTheirDefinitions(t *testing.T) {
	for _, s := range javaSoundDefinitions {
		if got, ok := bedrockSoundName(s.java); got != s.bedrock || !ok {
			t.Errorf("bedrockSoundName(%q) = %q, %v, want %q, true", s.java, got, ok, s.bedrock)
		}
	}
}
