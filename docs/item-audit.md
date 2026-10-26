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
- `Tx.DestroyBlock` ports `Level.destroyBlock` for items, which cannot reach the block package.
- A usable item whose use does nothing, such as a milk bucket, is consumed instead of doing nothing.

## Item classes

| Class | Items | Status |
|---|---:|---|
| ArmorStandItem | 1 | Ported `useOn`: no placement on the bottom face, the stand stands on the floor of the placed block (it floated half a block up), the box must be clear of blocks and entities, yaw rounds to 45° facing the player (it was negated), and the stack's custom name is applied as `createDefaultStackConfig` |
| ArrowItem, TippedArrowItem, SpectralArrowItem | 3 | combat: skipped |
| BannerItem | 16 | |
| BoatItem | 22 | Ported `use`: the boat goes where the player's view first meets a block outline or liquid surface (it went to a block centre beside the clicked face), faces as the player does (the yaw was negated), is refused when the player is inside an entity or the boat would collide, and also places from a click into the air, as onto water. Custom names apply |
| BoneMealItem | 1 | Ported `useOn` and `growWaterPlant`: on a sturdy face beside water, seagrass, and in warm oceans corals, grow around. The blocks follow `BonemealableBlock`: weeping, twisting and cave vines, kelp (it stopped at age 25 and grew from the clicked block), pale hanging moss and moss carpet (whose placement now computes its sides and topper as `MossyCarpetBlock`), mangrove leaves, netherrack, rooted dirt, seagrass and dry grass took no bone meal; flowers and sugar cane, which are no bonemealables in Java, did |
| BottleItem | 1 | Ported `use`: past dragon's breath, the bottle fills from the water source the player's view meets (`Fluid.SOURCE_ONLY`), also from a click into the air or through water onto the floor, with the bottle fill sound and a `fluid_pickup` game event; flowing water no longer fills it. Dispensers fill from a water source in front |
| BowItem, CrossbowItem, TridentItem, MaceItem, ShieldItem | 5 | combat: skipped |
| BrushItem | 1 | Matches: 200-tick use, a stroke every 10 ticks from the fifth, durability only on completed brushing |
| BucketItem, SolidBucketItem, MobBucketItem | 11 | Ported `use` and `emptyContents`: the view ray picks the target (sources only for an empty bucket, none for a filled one), water goes into a block that takes it or beside the block looked at, never into a replaced plant, which now breaks with its drops; sneaking places beside; water evaporates in the Nether; `fluid_place`/`fluid_pickup` game events. Mob buckets empty their water (they never did: the water lookup failed) and spawn the mob standing on the placed block's floor with the stack's name, playing only their own sound; fish buckets can no longer be eaten. Dispensers empty and fill as `DispensibleContainerItem` |
| BundleItem | 17 | Ported `BundleContents` weights: shulker boxes no longer fit (`canFitInsideContainerItems`), a hive with bees fills the bundle; using a bundle throws out the whole selected stack (`removeOne`), not one item. The Bedrock bundle screen stays client-driven |
| CompassItem | 1 | Matches through Bedrock's position tracking: lodestones link compasses, a missing lodestone reports its target as not found |
| CushionItem | 16 | Ported `useOn` and `Cushion.canBePlacedAt`/`wouldSurviveAt`: an anchor within 1/64 below, covered only when every block in the box suffocates (any overlap refused before), not buried; cauldrons, hoppers and composters are clicked on their collision shape; the cushion faces the nearest cardinal direction and takes the stack's name |
| DiscFragmentItem | 1 | |
| DoubleHighBlockItem | 29 | |
| DyeItem | 16 | |
| EggItem, SnowballItem | 4 | |
| EmptyMapItem, MapItem | 18 | |
| EndCrystalItem | 1 | |
| EnderEyeItem | 1 | |
| EnderpearlItem | 1 | |
| ExperienceBottleItem | 1 | |
| FireChargeItem | 1 | |
| FireworkRocketItem | 1 | |
| FishingRodItem | 1 | |
| FlintAndSteelItem | 1 | |
| FoodOnAStickItem | 2 | |
| GameMasterBlockItem | 7 | |
| GlowInkSacItem, InkSacItem | 2 | |
| HangingEntityItem, ItemFrameItem | 3 | |
| HangingSignItem | 13 | |
| HoneycombItem | 1 | |
| InstrumentItem | 1 | |
| KnowledgeBookItem | 1 | |
| LeadItem | 1 | |
| MinecartItem | 6 | |
| NameTagItem | 1 | |
| PlaceOnWaterBlockItem | 2 | |
| PlayerHeadItem | 1 | |
| PotionItem, SplashPotionItem, LingeringPotionItem | 3 | |
| ScaffoldingBlockItem | 1 | |
| ShearsItem | 1 | |
| SmithingTemplateItem | 19 | |
| SpawnEggItem | 88 | |
| SpyglassItem | 1 | |
| StandingAndWallBlockItem | 33 | |
| WindChargeItem | 1 | combat projectile: behaviour only |
| WritableBookItem, WrittenBookItem | 2 | |
