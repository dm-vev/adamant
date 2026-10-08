<!--suppress ALL -->

# Adamant

Adamant is a Go-based Minecraft: Bedrock Edition server, forked from df-mc/dragonfly. It ports Java 26.3 generation and gameplay while using a pinned Bedrock Dedicated Server (BDS) release for protocol, registries and NBT compatibility. Full vanilla parity has **not** been reached.

Upstream: https://github.com/df-mc/dragonfly

## What’s New in Adamant
- **Modern protocol and transport:** gophertunnel v1.62.0 (protocol 2193), native NetherNet HTTP signaling/WebRTC, and a persistent server identity across restarts.
- **Fully asynchronous world generator:** generation tasks now run in parallel across worker goroutines, drastically improving chunk generation speed and reducing main-thread load.
- **Optional PM-style generator:** pmgen remains available separately; it is not the default dimension generator.
- **Java 26.3 generation kernel:** Java random streams, noise, terrain splines, climate lookup, biome zoom and aquifers are checked against the mapped Java runtime. Dimension structures, material rules and feature integration are still in progress.
- **Built‑in admin commands + server CLI**: `help`, `list`, `kick`, `gamemode`, `time`, `chat`, `gc`, `status`, `stop`, `about`, `whitelist`.
- **RakNet Query support**: server status, players, MOTD and plugins exposed to query clients when the RakNet transport and Query are enabled.
- **Whitelist system**: Nukkit-style `white-list.txt` allowlist with legacy TOML fallback; built‑in `whitelist`/`allowlist` commands and config enforcement.
- **New items and vanilla features**: early stage of implementing core Bedrock mechanics — starting with fishing rods and Nether portals.
- **Stability and concurrency hardening**: locks/atomics across sessions, registries, world counters, whitelist checks, chunk radius, and command origin tracking; snapshotting to avoid hot locks and I/O under locks.
- **Inventory and container safety**: ordered locks for merge operations, guarded slot callbacks, validated stack requests/containers, hotbar slot clamping, and safer container-close handling.
- **Data validation and panic resistance**: checks for creative item IDs, anvil rename indices, banner patterns, item NBT damage/book pages, skin/cape dimensions, transfer ports, and query HostPort bounds.
- **World/AI correctness fixes**: bounded pmgen population concurrency, placeholder chunk tracking after load errors, stable pathfinding node storage, and hardened weather/explosion randomness.
- **Gameplay correctness**: fixes for lightning height/offsets, grindstone XP ranges, crossbow quick-charge ticks, instant-break haste baseline, XP drop ranges, chest unpairing, and barrel/brewing NBT state.
- **Query and persistence robustness**: token pruning, UUID validation, corruption guards for inventories, detached query player slices, and safer resource pack building.

## Project Goal
Port Java **26.3** generation and gameplay from the pinned [MCP-Reborn](https://github.com/Hexeption/MCP-Reborn) sources. Java is the behavioural reference for terrain, features, items and survival logic; byte-for-byte reproduction of Bedrock's generation algorithms is not the goal.

Use BDS **1.26.52.3**, build **51798919**, as the reference for Bedrock protocol, registry and NBT compatibility. Archive, item-registry, block-palette and source revisions are recorded in [`vanilla-reference.json`](vanilla-reference.json). Successful connections alone do not establish gameplay parity.

Current gaps include modern world generation, mob coverage and block implementations. A registered block state can still be an `unknownBlock`; a non-placeholder implementation can also have incomplete behaviour.

### Biome registry

The registered 89 biome definitions match the pinned BDS's initial climate, water-colour, precipitation and tag metadata, including `dappled_forest` at persistent ID **195**. That ID was read from a saved BDS chunk after its biome was identified through the live Script API.

`world.Biome.HasPrecipitation()` is now required independently of `Rainfall()` humidity. The End has humidity 0.5 but permits no precipitation. Existing unqualified biome names remain unchanged in the Go registry; the network packet qualifies default names with `minecraft:`.

An offline BDS/Adamant handshake matched all 2,076 item definitions, all 89 biome definitions and the compared world settings at seed `-9223372036854775808`. Both saved `level.dat` files retained that exact seed. This verifies initial metadata and saved settings, not terrain or gameplay parity.

Java-only knowledge books use the existing custom-item/resource-pack path as `adamant:knowledge_book`; Java NBT retains `minecraft:knowledge_book`. This adds one item without replacing a native BDS ID (the pinned native maximum is 883; the custom ID is 2076). A runtime probe verified recipe preservation through encoded native NBT and back to Java NBT, and built the generated pack with the pinned Java 16×16 texture. Clients need that resource pack; visual rendering and use input on an actual Bedrock client remain unverified.

### World seed and saved settings

`World.Seed` selects a new world's seed in all three dimensions. Once a world exists, `level.dat.RandomSeed` is authoritative, including zero; changing configuration does not change future terrain. `world.Settings.SeedSet` distinguishes a stored zero from an uninitialised provider. `server.Config.Generator` now receives `(dimension, resolvedSeed)` instead of capturing an unrelated configuration seed.

Login advertises the opened world's seed, default game mode and difficulty. New persistent worlds use Survival, matching non-persistent worlds. Adventure is sent as Adventure rather than falling through to Survival.

Restart verification generated three previously absent chunks in each dimension with stored seed `0` and configuration seed `12345`: their block hashes matched fresh seed-0 worlds and differed from seed-12345 controls. A separate real offline reconnect retained the signed minimum seed and advertised saved Adventure/Hard settings without changing item or biome metadata.

**Migration warning:** earlier Adamant versions wrote the current timestamp to `RandomSeed`, independently of their generator seed. Back up such worlds and correct `level.dat.RandomSeed` to the actual original generation seed before upgrading. The timestamp cannot recover that seed, and changing configuration no longer overrides an existing saved value.

### Block states and migration

The embedded palette contains **22,079 persisted states across 1,477 block identifiers**, captured from the pinned BDS. This is registry coverage, not implementation coverage.

- Panes, bars, fences and tripwire persist their four cardinal connections. Stairs persist straight, inner-left/right and outer-left/right corners.
- Old disk states retain explicitly supplied properties. Missing derived properties are reconstructed from loaded neighbours; unresolved boundary states survive save/reload without forcing neighbouring chunk generation. Stair geometry resolves before dependent pane/fence connections.
- Generated chunks reconstruct the same derived properties before they are committed as complete states.
- Stair neighbour rules are checked against 112 BDS scenarios, including orientation, half and material-dependent corner suppression. Physical corner collision and projectile/ray shape are separate: captured BDS arrows use the straight stair outline even at corners. Explosion and firework exposure retain their existing collision-shape behaviour.
- State lookup distinguishes property names, types and string boundaries; invalid supplied properties are not silently replaced with defaults.
- Shared Java state translation handles standing and wall orientations for normal, soul and redstone torches. Runtime probes checked all 15 placements against native attachment directions and the reverse Java state.
- `Tx.JavaMotionBlockingHeight` uses shared Java block tags, with separate leaf-inclusive and no-leaves classifications cached by runtime ID. A live synchronous End world verified empty columns, solid blocks, leaves, non-blocking torches, removal and the inclusive Y=255 boundary at negative chunk coordinates. Block removal canonicalises `nil` to the registry's air block before actor and POI updates.
- Village POIs recognise native bed-head states and lit smoker/blast-furnace variants. A live-world probe reproduced missing bed POIs before the fix, then verified home/workstation classification and removal at the End's upper build limit.

Custom `MissingStateReconstructor` implementations must preserve explicit properties and assign a lower `ReconstructionOrder` to derived-state dependencies. Same-order neighbours may be read for base properties, not unresolved derived geometry. Ray callers now choose `trace.RayShape` or `trace.CollisionShape` explicitly.

### Generated entities and block data

`world.Generator.GenerateChunk` returns `world.GenerationResult` in addition to filling the supplied chunk. Return newly constructed worldless entity handles, absolute-position block-entity NBT, and `ScheduledTicks` carrying Java registry type, block/fluid kind and priority. Generated updates have zero delay, matching `ProtoChunkTicks`; `ScheduleTick` keeps the first occurrence of each kind/type/position. Live updates retain their requested delays. The world takes ownership; generation workers must not retain mutable result data or enter world transactions. Block-only generators return the zero value.

The world installs this data once on its owner goroutine before publishing or saving the column. Shutdown also consumes in-flight results, and read-only worlds close their entities without saving columns. Dimension generators, rather than the world layer, supply gateway destinations and banner data.

Earlier race-detector runs covered synchronous and background generation, an End crystal with a beam target, gateway NBT and disk reopening without a generator. The current public generator smoke inserts water before a lit lamp, then observes the lamp already dark during the first fluid-flow callback. Generated updates run on the first eligible tick; a subsequent flowing-water update survives LevelDB close/reopen and fires after its saved five-tick delay. Regression tests also cover shutdown while a generation worker is still running. Flat generation fills biomes through the dimension's inclusive upper boundary, including Overworld Y=319.

Block and fluid queues collect independently, with the block phase first and a 65,536-update limit per phase. Collection requires a loaded, ticking destination, merges chunk heads by priority/subtick order, and clears all collected identities before callbacks. Scheduling changes dirty the destination column. Java source/flowing fluid identity follows depth and falling state, not Bedrock's independent `water`/`flowing_water` block names.

Pending updates use the captured BDS record: `currentTick` (int32), `tickList`, native `blockState`, `time` (int64) and int32 coordinates. Java kind/type/priority are retained separately in each record's `adamant:java` compound; saved delays pause while unloaded. An isolated **BDS 1.26.52.3 / build 51798919** replay reopened records written by the public MCDB provider and returned both recorded active sensor variants to phase zero. This verifies native record acceptance and runtime state transitions, not Java priority semantics in BDS.

Generated beds, banners, dragon heads and copper-golem statues were checked through native chunk installation and LevelDB reopening. Rotated/mirrored placements retain bed colour and occupancy, banner pattern layers, head rotation and powered state, statue pose/facing, and the separate water layer. Four consecutive-tick bed occupancy transitions converge on both halves without stale neighbour updates reversing the head state; chained updates retain the existing per-tick budget and unloaded-chunk safeguards.

A separate capture from BDS **1.26.52.3 / build 51798919** verified powered and unpowered dragon-head NBT: the native flag is `DoingAnimation`, not `MouthMoving`. The decoder and encoder retain `MouthTickCount` as well. This checks actor persistence, not Bedrock-client animation rendering.

### Java 26.3 generation kernel

`server/internal/java26` ports Java's legacy and Xoroshiro random sources, float32 noise stacks, blended terrain noise, terrain splines, climate search, three-dimensional biome zoom, Overworld density graph, noodle caves, ore-vein fields and aquifers. The climate tree contains all 7,594 Java parameter records and uses Java's 19-child search tree; biome identities are translated separately to Bedrock registry IDs.

The kernel is wired into the modern Overworld generator, but end-to-end surface, structure, carver and placed-feature parity is not yet established. Kernel comparisons alone do not establish complete generation or gameplay parity.

Verification against executed Java 26.3 classes:

- 707 random/noise comparisons cover both random algorithms, Gaussian sampling, normal and legacy Nether noise, simplex noise, blended terrain, biome-seed hashing and ridge folding.
- 16,538 terrain-spline samples match the Java results exactly.
- 280 Overworld router fields across 35 sampled positions agree within `1e-6`; the largest observed absolute difference was `7.450580596923828e-9`.
- 35 biome-zoom probes agree, including negative coordinates.
- Fifteen complete Overworld density/aquifer chunks across seeds `0`, `1` and `-1` match all **1,474,560** material decisions and fluid-update flags. This excludes surface rules, structures, carvers and placed features.
- Eight seeds cover 80 End-spike layouts and **490,568** cylinder/cage material and connection decisions with no mismatches. The oracle stops before crystal construction, so the 160 subsequent pedestal/fire cells are excluded.

The figures above are historical oracle runs and have not been repeated after the latest integration. The latest executed Java `Mth` comparison matched all **65,536** sine-table values and 13 sine/cosine boundary probes, including double-precision indexing and Java's NaN/infinity narrowing. The former float-index expression disagreed on 29 of 100,000 sampled float arguments. Modern carvers, relevant feature consumers and dragon math now share the lookup implementation; source `Math.sin/cos` paths retain analytic trig.

Executed Java `IntProvider` sequences cover 11 provider configurations with both legacy and Xoroshiro random sources: **352 sampled values plus 22 following-stream checks**. These include nested weighted/clamped providers, Gaussian sampling, triangular/trapezoidal distributions and zero-weight entries. Zero-weight entries are valid but never selected; an all-zero weighted list remains invalid.

The `solid` placement predicate uses an immutable table exported from the executed Java 26.3 block registry, not native collision-box guesses. All **1,286 block types** were invariant across their source states, and the Go lookup matched every exported value. `bash cmd/vanilla-solid-export/generate.sh` regenerates it from the pinned mapped classes, client resources and manifest libraries; the exporter verifies the source version and rejects a future state-dependent result.

The same exporter now records typed default properties for all 1,286 Java blocks. The codec fills omitted properties without changing explicit values or mutating the input map; bare source block names no longer select arbitrary native palette defaults.

Generation shape metadata distinguishes vine attachment (a full support **or collision** face), sturdy faces, centre support, full/empty collision volumes and replaceability. All six fields matched **35,723 executed Java block states**, with zero measured allocations per lookup; **398 states** distinguish attachment from sturdy-face support. `has_sturdy_face`, block piles, vines, bonus-chest torches and sculk use the source metadata rather than native collision approximations. The export models fresh generation with no active moving pistons or open shulkers; it is not a runtime entity-dependent shape API.

Placement regressions match the executed Java `FeaturePlacer` for **14 emitted positions/feature draws and four following-stream values** across both random algorithms. Modifier expansion is depth-first, rarity filters consume Java float draws, and feature execution remains interleaved with placement sampling. Eight coordinate probes match both noise-count modifiers, including negative counts and exact threshold equality; biome-information noise discards its coordinate offset as Java does. A separate Java cuboid probe matches all six face centres with edges and interior excluded.

Layered-count placement matches **18 emitted positions and four following-stream values** from Java with constant/uniform counts and both random algorithms. Real chunk fixtures check split-layer fluids, native/source name aliases and heightmap cache invalidation. Four selector families match **16 Java selections and following-stream checks**; sequence stops at the first failed placement, while overlay evaluates every child. A nested empty rule-based provider continues to the next matching rule rather than replacing the block with an invented fallback.

Disk placement now samples an optional provider separately at each accepted target, visiting X before Z and each column top-down. Executed Java comparisons cover **56 weighted material choices and four following-stream values**, plus a nine-cell contextual provider result, an empty optional provider and a rejected-target/no-draw case. This does not establish Java's deferred chunk postprocessing, which requires a persisted queue, neighboring FULL chunks and fluid/shape updates not yet represented by `GenerationResult`.

The eight builtin `block_state_provider` resources are now embedded and resolved as provider references, separately from literal block-state strings. Six providers match **108 executed Java states and 12 following-stream values**: three flower-noise providers, both cave-vine providers and hanging mangrove propagules. The probes include the low-threshold branch, negative coordinates, float-coordinate precision boundaries and signed-coordinate boundaries. Named optional tree-soil providers use their data-driven rules instead of hard-coded names. A direct generation run at seed `-1`, chunk `(1024,1024)`, reproduced the missing `flower_plain` registry failure before the fix and completed afterward.

`stepped_column_cluster` matches **109 executed Java block states, eight following-stream values and eight height-predicate boundary outcomes**. Height anchors use each dimension's sea level and an exclusive upper generation bound. `single_block_pillar` and `projected_random_patchy_square` match **130 block states and 16 following-stream values**, including upward/downward growth, blocked origins, zero continuation chance, build-height termination, optional projected states and cap placement. Cap comparisons exposed and removed an incorrect air-only restriction in `simple_block`.

After these fixes, direct generator entrypoints completed all **27 columns** for seeds `0`, `1`, `-1`, chunks `(0,0)`, `(-1,-17)`, `(1024,1024)` and all three dimensions. This is execution coverage, not source chunk-layout parity or deferred postprocessing coverage.

The `block_blob`, `block_pile`, `fill_layer`, `replace_single_block`, `coral_tree` and `coral_claw` paths match **648 final Java block writes and 42 following-stream values** in source-oracle fixtures. These cover both RNG algorithms, placement failures, dirt-path piles, occupied fill layers, state-sensitive/random/logical replacement rules and inclusive height boundaries. Replacement height rules use absolute integers, not generation anchors.

Builtin `would_survive` predicates now use the requested plant state, Java support tags and neighboring fluids. **31 executed Java outcomes** cover all **13 builtin predicate states**, invalid soils, cactus obstructions/liquids, sugar-cane adjacency and hanging propagules. Bubble-column translation preserves both drag directions and source water in the native secondary layer; a public-codec smoke exercised both directions. Vine placement matches **13 executed Java scenarios** for occupied origins, attachment precedence, slabs, oriented trapdoors and leaf collision support, including the following RNG state. Expected vine states are compared after native palette projection.

The `the_void` biome previously panicked on its unsupported `void_start_platform` feature. Platform placement now matches **75 executed Java scenarios and 3,267 source block writes**, covering neighboring/outside chunks, negative coordinates and three origin heights. A public `ModernFeatures.Decorate` smoke checked 6,400 plane cells across 25 void columns: exactly 1,089 platform blocks with one central cobblestone. The builtin `no_op` feature succeeds without modifying blocks or consuming randomness, as the executed Java implementation does.

Bonus-chest placement matches **96 executed Java scenarios**, 264 final block writes, 72 loot actors and both RNG streams. A public decorator smoke placed a chest and four torches on centre-supporting fences, checked the following RNG value and preserved the deferred loot table/seed through native chest NBT serialization.

Sculk patches match **28 executed Java scenarios and 5,968 final block writes**, including both RNG algorithms, dry/flooded terrain, initial sculk/veins, replaceable grass, flowers, fire, zero rounds and rejected starting states. The comparison corrected cursor-neighbor ordering, spread precedence and native fluid identity: `liquid_depth`, not the Bedrock block name alone, distinguishes source from flowing water/lava. A public `ModernFeatures.Decorate` smoke checked all **229,376 target-column cells** and 28 following-stream values.

Template features are connected to the feature dispatcher. **48 executed Java scenarios and 1,548 final block writes** cover both desert-well templates, rotations, missing-template failure, dry/wet starting terrain and inline archaeology loot processors. A public decorator smoke checked **196,608 cells, 12 loot actors and 48 following-stream values**. Rule processors use a position-seeded Legacy stream, independent of the placement RNG. These cases do not establish general template fluid-fill, neighbor-shape updates or deferred chunk postprocessing parity.

Template processor block and height queries read the current feature region, including earlier feature writes, rather than the untouched terrain source. A public chained-feature smoke reproduced a stale-location failure before the correction, then matched a stone-dependent rule and placed gold at Y=80; with a following gravity processor it placed gold at Y=81 and retained the original stone.

Template processing now keeps source states unrotated until the processor chain completes, refreshes the world position after gravity, and performs whole-piece finalization before clipping. An executed Java ordering probe checks a state-sensitive stair replacement, final rotation and ordinary clipping. Two executed Java capped-processor pipelines each retain nine records and replace the sole eligible block, regardless of whether the capped processor precedes or follows the rule that makes it eligible. The public Go placement smoke reproduces the diamond result without changing its template.

Processor states now decode canonical `id`/`properties` JSON or bare identifiers, complete source defaults before matching/rotation, and compile static state predicates once rather than rebuilding property maps per block. **Ten executed Java cases** and a public placement smoke cover bare/partial states, nested logical predicates, state-mismatch RNG consumption, explicit always-true position tests, all four builtin trial-chamber bulb outcomes and the state-sensitive zombie-village pane replacement. The non-existent `always_false` rule type is rejected instead of silently disabling a rule.

Jigsaw `final_state` now uses the Java block-argument grammar, not the processor JSON codec. **22 executed Java replacement cases** cover properties, final rotation, adjacent SNBT, missing/non-string NBT, malformed states and `structure_void`: a missing compound retains the connector, whereas invalid replacement text drops only that record. **66 source parser boundaries** cover default namespaces, quoted properties, typed domains, prefix consumption and whitespace quirks. A public runtime smoke matched all **35,723 registered source states** with their complete properties. Property domains come from `StateDefinition` through `cmd/vanilla-solid-export/generate.sh`; shared static domain records avoid per-block lookup-map initialization.

The native SNBT parser matches **165 executed Java cases**, including signed/unsigned numeric suffixes and bases, typed-array widening, heterogeneous lists, nested `bool`/`uuid` operations, UTF-16 escapes and byte-offset prefix parsing. A public runtime smoke matched **262,144 Java digit checks**, whitespace classification over all **1,114,112 code points**, and **294,579 source character-name attempts** (294,575 accepted). `go run ./cmd/vanilla-snbt-export` regenerates the compact named-escape tables from the pinned JDK 25.

Template fluid-state placement matches **12 executed Java scenarios**, with neighbor-shape updates disabled: dry/source/flowing water, double slabs, ambient versus template-locked sources, second-pass propagation, overwritten containers, default-waterlogged sea pickles, lava-before-water direction precedence, campfires and candles. Flowing water does **not** directly waterlog a slab. A public decorator smoke checks all 12 outcomes and emits the extinguished campfire actor. Native waterlogging capability now comes from the exported Java property registry rather than suffix guesses; campfires, candles, chests and lanterns roundtrip, while native wet fence gates and muddy mangrove roots are rejected as unrepresentable Java states. These checks do not establish scheduled-fluid-tick, neighbor-shape or deferred-postprocessing parity.

Explicit template liquid placement now publishes the source-water update requested by `placeLiquid`, including the iterative refill pass. A separate public decorator smoke checks direct fill, pre-waterlogged placement without an implicit tick, neighboring-source refill, retention of a pending update after its container is overwritten, and destination ownership during neighboring-chunk replay. Spring replay no longer publishes updates for blocks that exist only in another target's temporary overlay. General neighbor-shape and chunk-promotion postprocessing remain separate, unfinished work.

End chorus generation matches **36 executed Java scenarios, 419 final block writes and 36 following-stream values**, including both RNG algorithms, obstructions and the upper build-height boundary. End rarity/count sampling matches ten source outcomes and following-stream values; rarity uses float draws, and island counts preserve the source weighted-list order. Natural End generation now places the fixed `(100,49,0)` platform with its obsidian foundation at Y=48: a public two-column run matches all **100 source cells** across the Z chunk boundary. A separate public outer-End run at chunk `(102,0)` produces 29 chorus-plant blocks and five flowers. This is not proof of complete End chunk layouts, portal-arrival behavior or dragon-fight lifecycle parity.

The chorus oracle now checks all six Java connection properties after target-column publication, not only native runtime IDs. Growth samples connections at each source write and explicitly refreshes the preceding vertical or branching segment; connection states are precomputed rather than allocating property maps per stem. The strengthened regression reproduced 25 lost connection states in the first scenario before the fix, then passed all 36 scenarios. A natural End generation smoke at seed `0`, chunk `(1024,1024)`, verified six parent-to-dead-flower connections and preserved their full source states across MCDB close/reopen.

Diamond-ore loot with Fortune III now matches the pinned Java Xoroshiro stream and source ore-bonus formula for 16 seeds (14 mismatches before the correction). A single eligible pool entry no longer consumes a selection draw. Enchantment modifiers preserve unmentioned enchantments and implement additive replacement without losing JSON numeric component values.

Six effective-difficulty boundaries were checked against the actual Java `DifficultyInstance` class. The capped Easy result is **1.5**, not 1.625; the source arithmetic was unchanged and the incorrect regression expectation was corrected.

The shared block-state codec roundtrips all 36 surface materials and all eight snow-layer thicknesses against the pinned native palette. Full snow and layered snow remain distinct; uncoloured terracotta maps to native hardened clay. This is codec coverage, not complete terrain-chunk parity.

The embedded-template/feature-state corpus now checks **1,926 distinct source states**, **1,511 templates** and **513 feature configurations**, reaching 1,645 native primary states. It includes builtin bare block-name references, identified against the executed source registry. Native-only torch directions, ceiling skull attachments, horizontal jigsaw rotations and structure modes are explicitly rejected when Java cannot represent them. This is not coverage of every builtin Java state, nor proof that Java-only gameplay properties survive native palette projection.

A rotated placement smoke now traverses all **1,291 builtin jigsaw templates/palettes**, retains **4,272 connector records**, resolves **969,710 native blocks** and emits their actor payloads without the previous heart/sensor failures. All **1,275 placed actors** also decode, encode to native NBT and reload without changing their native block state. Compound and string lists retain the Java decoder's typed representations; inventories encode native unsigned slot bytes and use Java's default slot zero. This is placement/codec coverage, not a comparison of complete generated structure layouts.

A complete palette-only projection probe enumerated all **35,723 Java 26.3 states**. Material aliases, copper double slabs/torches, sea-pickle count/hydration, portal axes, frame eyes and sniffer-egg cracking corrections increased exact projections from **26,868 to 27,148** and reduced rejected families from **101 to 81**. The remaining 225 lossy families and 81 rejected families are not a generation-failure count: actor-backed state is intentionally outside this palette-only probe. A separate real placement smoke now rotates a blue shulker box to face east while preserving three diamonds in slot seven through native actor decoding; shulker facing is actor data, not an invented native palette property.

Source-aware projections now retain Java-only properties in primary palette entries instead of collapsing states that share a native runtime ID. A probe of the same **35,723 source states**, including actor-backed projections, exactly roundtripped **35,602**; **8,388** required retained source metadata. The remaining **121 states across 16 families** are moving pistons, wall hanging signs and test blocks without native mappings. Process-local source IDs are never persisted: disk entries carry an `adamant:java` compound with the canonical source name and properties, while native `name`/`states`/`version` and network runtime IDs remain unchanged.

A public decorator/world/MCDB smoke preserved two different leaf distances sharing one native RID across restart; replacing one leaf with stone cleared only that cell's metadata, and the other distance survived another restart. **BDS 1.26.52.3 / build 51798919** opened production-written palette extensions and found both native leaf blocks. This establishes BDS read compatibility, not preservation of Java extensions when BDS itself re-saves the world, nor source neighbor-shape or post-processing parity.

A public codec smoke preserved all **28 wall-head type/facing combinations** and decoded free water/lava from the native secondary layer. Wall heads retain their wall-specific Java identifier rather than acquiring a standing head's default rotation.

An isolated BDS 1.26.52.3 build 51798919 capture established that leaf litter defaults to `growth=0`; native loot commands returned one through eight items for `growth=0..7`. The codec now maps Java's one through four segments to `growth=0..3`, rather than shifting their counts by one. Reading the saved native column through the public codec preserved those four counts and rejected native five-through-eight-segment states instead of silently clamping them. Java's north-facing default remains distinct from BDS's south-facing default.

Feature pool elements execute their held placed feature through the structure decoration region rather than being discarded or treated as templates. An isolated real-executor smoke placed an acacia tree with seven logs and 59 leaves; it is an execution check, not a Java layout comparison.

A strict live-world generation smoke generated seeds `0`, `1` and `-1` at `(0,0)`, `(-1,-17)` and `(1024,1024)` in all three dimensions: **27 sampled columns** without generation error logs. This exercises the integrated placement pipeline, contextual rule-based state providers, nested placed-feature holders and layered/heightmap placement; it is not a source chunk-layout comparison.

Reference executables, decompiled sources and temporary oracle programs are not committed. A PDB may only supply RVAs for an executable with the matching CodeView GUID and age; the supplied 26.51.1 symbols do not match the pinned 1.26.52.3 executable.

### Gameplay integration checks

Live in-memory worlds exercised:

- End-gateway creation of a paired destination and fallback island, preservation of ordinary entity velocity/rotation, and the 40-tick reuse cooldown.
- Dragon death leaving 19 gateway slots, placing the first dragon egg and 20 exit-portal cells; a four-crystal respawn ritual and restoration of the portal when a participating crystal is destroyed. Fight state was saved through the provider on close.
- Four dried-ghast hydration transitions, each after 5,000 scheduled ticks, retaining water and spawning a baby happy ghast.
- Nine registered mob species with expected health, mutable movement speed and application/removal of Speed I. A size-64 phantom retains 20 health rather than scaling health with its size.
- A source-NBT shulker with byte-valued attachment, peek and colour fields restores east attachment, peek 73, colour 5 and health 18 in a live world.
- All 16 native suspicious-sand/gravel palette states retain brushing progress without exposing Bedrock's `hanging` flag as a Java property. Ten timed strokes restore sand/gravel and release one buried diamond in each live interaction.
- Loot random streams resume independently after world close/reopen in the Overworld, Nether, End and a registered custom dimension sharing one in-memory provider. This is process-lifetime storage, not a disk-restart claim. Custom dimension persistence uses registered numeric IDs, not formatted Go values.
- Default-registered crafting through a powered crafter consumes nine iron ingots for one iron block. A live dispenser creates one small fireball, and a two-tick pulse still makes a dropper transfer exactly one diamond; all three paths were checked after 12 world ticks.
- Creaking-heart night spawn, damage-triggered resin, comparator output, dawn removal, next-night respawn, player-break teardown and natural-heart experience from a player-owned TNT explosion.
- Normal and calibrated sculk-sensor vibration selection, wool occlusion, sneaking filters, forced step-on activation, calibrated back-input filtering and distinct active-phase durations. A real LevelDB close/reopen retains in-flight source UUIDs and the exact remaining sensor/shrieker scheduled delays; shrieker reception attributes warning level one to the originating player. Chunk-unloaded time pauses saved delays, matching Java's `SavedTick.unpack`.
- Actual `Player.Move` calls preserve Java's float32 distance accumulation and next-step thresholds; the resulting walking vibration reaches a calibrated sensor after 12 ticks with power four. Movement regression scenarios cover crouching, walking versus swimming in water, vertical climbing, nearest-support selection at a wool seam and server-authoritative displacement.
- Primed TNT decrements before expiry, including zero/one-tick fuses. A LevelDB smoke saves after 40 ticks, restores the same native entity ID and living igniter, and explodes exactly 40 ticks later. The TNT is removed before the explosion while retaining player responsibility for natural-heart experience.

Biome spawn settings decode the heterogeneous Java environment-attribute map without treating unrelated scalar attributes as spawn records. A runtime probe matched all 15 Badlands rows, including weights and scalar/uniform count bounds, and retained empty Deep Dark/Void spawn tables. This verifies source data loading, not the complete natural-spawn algorithm.

Native LevelDB map reopening now covers creation, cross-dimension scaling, locking and independent cloning with four distinct Java/native ID pairs. Scaling retains the source dimension and tracking flags but starts with a blank coarser grid; locking preserves explored pixels and decorations. Transparent native pixels remain unexplored Java pixels rather than becoming black.

A real offline gophertunnel client received `CraftingData` with 1,125 shaped, 1,899 shapeless and 18 concrete smithing-trim recipes. The packet preserves the acacia pressure-plate source ID and distinct full bolt/coast trim IDs. Bedrock-only compass map upgrades are not Java operations and are no longer advertised.

Nether portal travel uses the source search radius of 16 blocks into the Nether and 128 into the Overworld. A queued Ender Pearl transfer with an intentionally panicking destination spawn handler restored source ownership and the exact original position. This checks the rollback path, not complete portal geometry or travel parity.

Startup also exercises shrieker, bookshelf, vault and trial-spawner registration against the pinned native palette; Java-only actor state is not added as invented native block-state properties. These checks do not establish complete AI/ecology or loot-function coverage, and do not verify rendering or input on a Bedrock client.

Isolated BDS captures establish sensor phases as native palette state, with Java power/listener memory stored separately in `adamant:java`. Native sensor listeners include `event=28`; shrieker listeners do not. A real offline RakNet client receives the emitted `LevelEventGeneric` event **2027** and decodes its **109-byte** ordered network-NBT payload: `origin`, float32 speed **20**, `target`, float32 lifetime **0.2** for a four-tick vibration. This verifies the packet boundary, not graphical-client presentation.

## Getting Started
Requirements: **Go 1.26+**

Run from source:
```shell
git clone https://github.com/dm-vev/adamant
cd adamant
go run .
```

Stop the server with `Ctrl+C`.
Use `go run . -headless` for automation without an interactive console; termination signals still shut down the server normally.

### Configuration
Server settings are in `config.toml`.
- `Network.Address`: listen address and port (default `:19132`).
- `Network.Transport`: `nethernet` by default, including when the field is omitted. `raknet` is an explicit option for older/custom clients; there is no automatic downgrade. BDS 1.26.52.3 reports NetherNet as its only supported client transport.
- `Network.IdentityFile`: persistent P-384 private key, default `server-key.pem`. Back it up privately and retain it across restarts. A corrupt existing key causes startup to fail instead of replacing the trusted identity. Newly created keys have owner-only permissions.

  NetherNet serves HTTP signaling on `Network.Address` (TCP) and uses WebRTC UDP connectivity for gameplay. Deployments behind NAT/firewalls must allow the negotiated ICE connectivity; opening only the old RakNet UDP port is not sufficient. The built-in endpoint is HTTP; TLS termination can be provided by a reverse proxy. Keep `Server.AuthEnabled = true` outside isolated test environments.
- `Server.Name`: server name in the list; `AuthEnabled`, `DisableJoinQuitMessages`, `MuteEmoteChat`, `ReconnectPolicy`.
- `World`: `Seed`, `SaveData`, `GeneratorWorkers`, `GeneratorQueueSize`, `Folder`.
  `Seed` only applies when creating a world; existing saved metadata takes precedence. See the migration warning above for worlds created by older Adamant versions.

  The asynchronous generator defaults to `GeneratorQueueSize = GeneratorWorkers * 4`. Under
  extreme preloading you may need to raise both values to keep the queue from saturating; watch
  the server logs for `world generator queue saturated` warnings. Profile the generator first if
  it is limited by LevelDB or other I/O to ensure additional workers do not become the bottleneck.
- `Players`: `MaxCount`, `MaximumChunkRadius`, `SaveData`, `Folder`.
- `Resources`: `AutoBuildPack`, `Folder`, `Required`.
- `Whitelist`: `Enabled`, `File` (default `white-list.txt`), `Reason` (kick message).

### Whitelist Management
- Enable/disable via `config.toml` → `[Whitelist].Enabled`.
- Manage entries in‑game/console:
  - `whitelist add <player>`
  - `whitelist remove <player>`
  - `whitelist list`

### Query Support
The Bedrock Query adapter is available with `Network.Transport = "raknet"` and `Network.EnableQuery = true`. Query is disabled by default and is not exposed by the NetherNet listener.

## Development
Adamant retains Dragonfly's structure and Go API conventions. Behaviour changes should include a reproducible scenario against the pinned BDS release.

### Native registry comparison

`scripts/vanilla_compare.py` runs the pinned BDS and Adamant in isolated directories, captures their initial item and biome registries, and compares world settings and registry metadata. Use an unpacked BDS distribution:

```shell
python3 scripts/vanilla_compare.py --bds-dir /path/to/bds --output /path/to/new-capture --seed 1 --timeout 90
```

Add `--reference-only` to capture BDS without starting Adamant. The offline probe needs no Microsoft account; keep these unauthenticated reference servers isolated.

Each capture includes a JSON snapshot and a `*-biomes.bin` packet body. `cmd/vanilla-probe -biomes-packet PATH` advertises client-generation support and preserves the complete decoded biome-definition payload without its packet header. The harness explicitly enables BDS's `client-side-chunk-generation-enabled` property; without this negotiation, BDS omits the optional generation rules. The pinned capture contains 89 biome definitions, all with generation data.

`cmd/vanilla-compare -reference reference.json -candidate candidate.json` compares biome names, climate, precipitation, water colour and tags in addition to items and world settings. Snapshots without the `biomes` field must be recaptured. Packet positions remain available as `index` for diagnostics but are not an equality criterion: enabling generation data changed 71 positions in two native captures with otherwise identical metadata. Tag ordering is likewise ignored. The comparator does not compare the optional generation rules or establish gameplay/terrain parity.



Upstream docs: https://pkg.go.dev/github.com/df-mc/dragonfly/server

## Contributing
Contributions are welcome! Please open issues/PRs with focused changes, behaviour references and explicit edition/version information. Do not replace reference expectations with hashes generated by the implementation under test.

Thanks to the df‑mc maintainers and community for the excellent base.
