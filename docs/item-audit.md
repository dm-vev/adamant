# Item audit against Java 26.3

Every Java 26.3 item is checked against Adamant by hand: its components (dumped from the mapped Java runtime by the local item audit) and, for items with a class of their own, the Java class's behaviour. Combat stays as Bedrock has it and is not compared. Block items take their behaviour from their blocks, which are a separate audit.

Java has 1,658 items: 1,038 plain `BlockItem`s, 257 plain `Item`s whose behaviour lies in their components, and the item classes below.

## Components

Compared for all items: `max_stack_size`, `max_damage`, `enchantable`, `compostable`, `use_cooldown`, `damage_resistant`, `consumable` and its `onConsumeEffects`, `cooking_fuel` and the cooking recipes.

| Finding | Fixed |
|---|---|
| Cakes stacked to 64 and name tags to 1 | Cakes stack to 1, name tags to 64 |
| Bow 385, crossbow 464, elytra 433, fishing rod and flint and steel 65, turtle shell 276 durability | Java's 384, 465, 432, 64, 64 and 275 |
| Netherite ingots, scrap and blocks and ancient debris burned as items | Fire-resistant, as their `damage_resistant` for `is_fire` |
| 41 composting chances wrong or missing (saplings, sugar cane, vines, mushroom blocks, moss, glow lichen, cake and others could not be composted; chorus plants and grass blocks could) | Generated from the `compostable` components; an empty composter always takes a layer, as `ContextIntProviders.compostable` |
| Golden apples stacked absorption up to 16 | Absorption is raised to `max(current, 4 × level)` whenever the effect is added, as `AbsorptionMobEffect.onEffectStarted` |
| Enchanted golden apples gave 30 s of Regeneration II, pufferfish Nausea II | 20 s of Regeneration II, Nausea I |
| Chorus fruit teleported to block centres within ±8 by its own rules | `TeleportRandomlyConsumeEffect`: 16 attempts at a random point within 8 blocks, clamped to the logical height (128 in the Nether), dropping onto `entities_can_teleport_to` blocks clear of collisions and liquid, dismounting first |
| Suspicious sand and gravel and command blocks were no registered items; Java `light`, `spawner` and `dirt_path` did not resolve | Registered; resolved to `light_block_15`, `mob_spawner` and `grass_path` |
| Cooking recipes and fuels | See [Furnaces and cooking recipes](verification.md#furnaces-and-cooking-recipes) |

Items Java has and Bedrock does not: `debug_stick`, `furnace_minecart`, `test_block`, `test_instance_block`.

## Engine fixes found on the way

- A pitch of exactly 90° (looking straight down) wrapped to −90°, so view vectors pointed straight up; only pitches out of range now wrap.
- `Tx.DestroyBlock` ports `Level.destroyBlock` and `Tx.PlaceFire` ports `BaseFireBlock.canBePlacedAt` for items, which cannot reach the block package.
- A usable item whose use does nothing, such as a milk bucket, is consumed instead of doing nothing.

## Item classes

| Class | Items | Status |
|---|---:|---|
| ArmorStandItem | 1 | Ported `useOn`: no placement on the bottom face, the stand stands on the floor of the placed block (it floated half a block up), the box must be clear of blocks and entities, yaw rounds to 45° facing the player (it was negated), and the stack's custom name is applied as `createDefaultStackConfig` |
| ArrowItem, TippedArrowItem, SpectralArrowItem | 3 | combat: skipped |
| BannerItem | 16 | block placement: covered by the block audit |
| BoatItem | 22 | Ported `use`: the boat goes where the player's view first meets a block outline or liquid surface (it went to a block centre beside the clicked face), faces as the player does (the yaw was negated), is refused when the player is inside an entity or the boat would collide, and also places from a click into the air, as onto water. Custom names apply |
| BoneMealItem | 1 | Ported `useOn` and `growWaterPlant`: on a sturdy face beside water, seagrass, and in warm oceans corals, grow around. The blocks follow `BonemealableBlock`: weeping, twisting and cave vines, kelp (it stopped at age 25 and grew from the clicked block), pale hanging moss and moss carpet (whose placement now computes its sides and topper as `MossyCarpetBlock`), mangrove leaves, netherrack, rooted dirt, seagrass and dry grass took no bone meal; flowers and sugar cane, which are no bonemealables in Java, did |
| BottleItem | 1 | Ported `use`: past dragon's breath, the bottle fills from the water source the player's view meets (`Fluid.SOURCE_ONLY`), also from a click into the air or through water onto the floor, with the bottle fill sound and a `fluid_pickup` game event; flowing water no longer fills it. Dispensers fill from a water source in front |
| BowItem, CrossbowItem, TridentItem, MaceItem, ShieldItem | 5 | combat: skipped |
| BrushItem | 1 | Matches: 200-tick use, a stroke every 10 ticks from the fifth, durability only on completed brushing |
| BucketItem, SolidBucketItem, MobBucketItem | 11 | Ported `use` and `emptyContents`: the view ray picks the target (sources only for an empty bucket, none for a filled one), water goes into a block that takes it or beside the block looked at, never into a replaced plant, which now breaks with its drops; sneaking places beside; water evaporates in the Nether; `fluid_place`/`fluid_pickup` game events. Mob buckets empty their water (they never did: the water lookup failed) and spawn the mob standing on the placed block's floor with the stack's name, playing only their own sound; fish buckets can no longer be eaten. Dispensers empty and fill as `DispensibleContainerItem` |
| BundleItem | 17 | Ported `BundleContents` weights: shulker boxes no longer fit (`canFitInsideContainerItems`), a hive with bees fills the bundle; using a bundle throws out the whole selected stack (`removeOne`), not one item. The Bedrock bundle screen stays client-driven |
| CompassItem | 1 | Matches through Bedrock's position tracking: lodestones link compasses, a missing lodestone reports its target as not found |
| CushionItem | 16 | Ported `useOn` and `Cushion.canBePlacedAt`/`wouldSurviveAt`: an anchor within 1/64 below, covered only when every block in the box suffocates (any overlap refused before), not buried; cauldrons, hoppers and composters are clicked on their collision shape; the cushion faces the nearest cardinal direction and takes the stack's name |
| DiscFragmentItem | 1 | Tooltip only |
| DoubleHighBlockItem | 29 | block placement: covered by the block audit |
| DyeItem | 16 | Ported `interactLivingEntity` and `tryApplyToSign`: sheep take a dye only alive, unsheared and of another colour, with the dye sound and an update to viewers; signs and hanging signs only when not waxed and the facing side has text, with the dye sound and a `block_change` game event |
| EggItem, SnowballItem | 4 | Ported `spawnProjectileFromRotation` for every thrown item (also ender pearls, bottles o' enchanting and splash and lingering potions): 0.1 below the eyes, Java's uncertainty spread and the thrower's movement; bottles o' enchanting fly at 0.7 (0.6) and potions use Java's −20° pitch offset instead of an approximation. Snowballs deal 3 to blazes. Eggs hatch as `ThrownEgg` |
| EmptyMapItem, MapItem | 18 | Matches: a map centred on the player at scale 0 with the cartography sound; maps keep Java's colours and native persistence |
| EndCrystalItem | 1 | Ported `useOn`: the crystal stands on the floor of the block above (it floated half a block up), with an `entity_place` game event; obsidian or bedrock, an air block above and no entity in the two blocks |
| EnderEyeItem | 1 | Matches: frames take eyes and complete portals, eyes fly to the nearest `eye_of_ender_located` structure within 100 chunks from mid-height; now with a `projectile_shoot` game event. Bedrock has no launch sound and plays the throw |
| EnderpearlItem | 1 | Ported `ThrownEnderpearl.onHit`: the owner arrives where the pearl was when its tick began, not at the hit point inside the block, only when alive and not asleep; only player owners take the 5 fall damage and may leave an endermite, 5% of the time where monsters spawn outside Peaceful, at their own position |
| ExperienceBottleItem | 1 | Experience is `3 + nextInt(5) + nextInt(5)`, not uniform over 3–11 |
| FireChargeItem | 1 | Ported with flint and steel: lights what it lights, otherwise places fire beside the clicked face only where `BaseFireBlock.canBePlacedAt` allows (a sturdy floor, a neighbour that burns or a Nether portal frame; it placed fire in any air), soul fire on soul sand and soil, with a `block_place` game event |
| FireworkRocketItem | 1 | Ported `useOn`/`use`: a gliding player clicking a block boosts instead of launching; the rocket starts 0.15 off the clicked face rising at 0.05 with Java's drift; its lifetime is `10 × (flight + 1) + nextInt(6) + nextInt(7)` ticks. Boosting stays client-driven, as Bedrock does it |
| FishingRodItem | 1 | Ported with `FishingHook`: the hook leaves 0.3 beside the eyes at Java's launch speed, flies until it hooks a mob or item or lands in water, then bobs on the surface; fish are lured for 100–600 ticks less 100 a Lure level (rain speeds, covered sky slows), approach for 20–80 and nibble for 20–40; open water is checked over Java's 5×5×4 layers; the hook goes when the owner stops holding a rod or is 32 blocks away. Retrieving pulls hooked entities, lands the catch towards the player with 1–6 experience at the player and wears the rod by 5, 3, 1 or 2; casting no longer costs durability. Catches never landed before: the loot context lacked the hook, so the open-water predicate failed |
| FlintAndSteelItem | 1 | As fire charges, and a use that does nothing costs no durability |
| FoodOnAStickItem | 2 | Matches: boosts the pig or strider ridden for 7 or 1 durability and leaves a fishing rod when broken |
| GameMasterBlockItem | 7 | Command, structure and jigsaw blocks are placed only by operators in creative mode, as `canUseGameMasterBlocks`; command blocks face the placer as BDS places them (they always faced down). Structure blocks keep BDS's Save default rather than Java's Load |
| GlowInkSacItem, InkSacItem | 2 | As dye: not on waxed signs or empty sides, with their sounds and a `block_change` game event |
| HangingEntityItem, ItemFrameItem | 3 | Paintings pick a fitting variant on a horizontal face, now with an `entity_place` game event. Item frames are Bedrock blocks, not entities |
| HangingSignItem | 13 | block placement: covered by the block audit |
| HoneycombItem | 1 | Waxes copper and signs with the wax-on event and a `block_change` game event |
| InstrumentItem | 1 | Goat horns play with their 7 s cooldown and now emit `instrument_play`, so sculk sensors and wardens hear them |
| KnowledgeBookItem | 1 | Unlocks its recipes through Bedrock's recipe unlocking |
| LeadItem | 1 | Ported `bindPlayerMobs`: gathers the mobs the player leads within a 32 block cube around the fence (±7 before), reuses the fence's knot instead of adding another, ties only mobs within the 12 block snap distance, with the knot sound and a `block_attach` game event |
| MinecartItem | 6 | Carts start 1/16 above the rail, half a block higher on slopes (they started half a block too high), with the stack's name and an `entity_place` game event |
| NameTagItem | 1 | Matches |
| PlaceOnWaterBlockItem | 2 | Ported: lily pads and frogspawn go above the water source the player looks at, also from a click into the air; they need a source below (flowing water counted), and lily pads also sit on ice and frosted ice |
| PlayerHeadItem | 1 | block placement: covered by the block audit |
| PotionItem, SplashPotionItem, LingeringPotionItem | 3 | done: water on convertible_to_mud makes mud; throws at -20 pitch, 0.5 power; splash scales by box distance and drops effects of 20 ticks or less; clouds (only of potions with effects) form at the hit entity's feet, wait 10 ticks, reach mobs whose box meets them, apply every 5 ticks with a 1 s reapplication delay, a quarter of lasting durations and half-strength instant effects; exploding creepers leave a cloud of their effects |
| ScaffoldingBlockItem | 1 | done: placement context ported with the block |
| ShearsItem | 1 | done: trims growing plant tips to max age first; wears on instant-break blocks except fire |
| SmithingTemplateItem | 19 | done: plain material; behaviour lives in the smithing recipes |
| SpawnEggItem | 88 | done: spawns in a clicked block without collision or beside the face, dropped by getYOffset; air use spawns in a liquid source; peaceful refuses notInPeaceful types; own-type clicks spawn offspring (breeding offspring, Age -24000 or IsBaby), zombie loot pickup and fox trust; dispensers spawn in front; the mob plays its ambient sound on spawn |
| SpyglassItem | 1 | Bedrock: the client zooms and plays the sounds |
| StandingAndWallBlockItem | 33 | block placement: covered by the block audit |
| WindChargeItem | 1 | combat projectile: behaviour only |
| WritableBookItem, WrittenBookItem | 2 | Bedrock: the client edits books; the server keeps the protocol's 50 pages of 256 bytes |
