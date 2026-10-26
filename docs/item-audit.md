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

## Item classes

| Class | Items | Status |
|---|---:|---|
| ArmorStandItem | 1 | Ported `useOn`: no placement on the bottom face, the stand stands on the floor of the placed block (it floated half a block up), the box must be clear of blocks and entities, yaw rounds to 45° facing the player (it was negated), and the stack's custom name is applied as `createDefaultStackConfig` |
| ArrowItem, TippedArrowItem, SpectralArrowItem | 3 | combat: skipped |
| BannerItem | 16 | |
| BoatItem | 22 | Ported `use`: the boat goes where the player's view first meets a block outline or liquid surface (it went to a block centre beside the clicked face), faces as the player does (the yaw was negated), is refused when the player is inside an entity or the boat would collide, and also places from a click into the air, as onto water. Custom names apply |
| BoneMealItem | 1 | Ported `useOn` and `growWaterPlant`: on a sturdy face beside water, seagrass, and in warm oceans corals, grow around. The blocks follow `BonemealableBlock`: weeping, twisting and cave vines, kelp (it stopped at age 25 and grew from the clicked block), pale hanging moss and moss carpet (whose placement now computes its sides and topper as `MossyCarpetBlock`), mangrove leaves, netherrack, rooted dirt, seagrass and dry grass took no bone meal; flowers and sugar cane, which are no bonemealables in Java, did |
| BottleItem | 1 | |
| BowItem, CrossbowItem, TridentItem, MaceItem, ShieldItem | 5 | combat: skipped |
| BrushItem | 1 | |
| BucketItem, SolidBucketItem, MobBucketItem | 11 | |
| BundleItem | 17 | |
| CompassItem | 1 | |
| CushionItem | 16 | |
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
