# Bedrock redstone audit

Status: **paused at the user's request; audit incomplete (2026-10-31)**.
This report tracks the full redstone implementation,
not just the fixes already made. An existing Go test or a matching happy-path
delay is not evidence of complete Bedrock equivalence.

The requested reference is the disassembled **Windows client 1.26.51.1**. Its
image SHA-256 is
`8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5`.
Unmodified Windows BDS 1.26.51.1 supplies additional world-level observations.
The repository's Java gameplay baseline does not override the requested
Bedrock reference for this audit.

The current effort estimate is approximately **70%** of the full audit. This is
a planning estimate, not a percentage of proven equivalence or a count of closed
matrix rows. Every row below retains its outstanding work. The published checkpoint
includes capture tooling, comparators/analog outputs, switches, pressure plates,
tripwire, daylight/live lighting, lightning rods and the tested sculk paths.
The distance/selection, range, button event/producer, plate/hook vibration and
plate sound changes are included in the published `dev` checkpoint. The resumed
hook producer fixes below are committed in `ff0c9154`; the vibration obstruction
fixes are committed in `0b106d01`. The former opt-in lifecycle
diagnostic is now an ordinary regression test; all 72 retained lifecycle and
64 additional permutation traces match after the fixes.

Daylight sampling, the tested full-day weather outputs and body geometry have
now been corrected against native evidence. Live roof changes, source removal
and producer registration have also been checked. Lightning-rod pulses,
retriggers, early producer registration and tested support conduction now match
100 native traces. Rod search geometry/ordering/eligibility now also matches
executed client functions with supplied world inputs. Broader lighting and
lightning lifecycle remain open. A further 26 original-BDS horse-trap cases
exercise the shared selector in real terrain and exposed precipitation-shape
and trap-integration differences, now corrected.

The sculk/player-use comparison has additionally corrected missing lever/button
vibrations, the calibrated sensor's disabled output face, comparator analog
sampling cadence, per-chunk block-actor shuffling and the block queue's clock
before its pass. The tested 120 complete player-use/heap traces and 130 executed
client shuffle cases match. The resumed comparison adds 24 diagonal-use and 15
adjacent-switch traces, plus executed client distance/selection, strength and
range gates. Another 88 contact-driven traces and 64 executed hook event cases
verify the new plate/hook vibration paths. Seventeen plate/context traces with
34 actual client sound packets also verify their sound events and tested
adjacent-input ordering. This does not close the sensor or
circuit ordering coverage rows.

The resumed hook lifecycle comparison separates scene-component strength and
setup direction from visible hook state. It corrects the zero-strength pending
producer, active-map-only updates, event ordering during explicitly powered
placement, and output direction retained across same-identifier rotation.
Another 216 native button/rod rotation and replacement traces independently
check the shared producer configuration change. These are controlled command
states, not actual player rotations or lightning hits.

The vibration obstruction path now uses the inspected native binary32 offset
and clip traversal rather than Java's endpoint-expanded traversal. The full
native clip, wool predicate and ray/AABB body execute with supplied air/wool
cells. Another 168 original-BDS contact traces independently check diagonal
wool seams, solid/partial non-wool controls and positive/negative coordinates
up to one million blocks. This corrects false reception through a blocked seam
at large coordinates and the tested same-cell/intersection boundary behavior.

The resumed piston audit replaces immediate neighbour-driven movement with a
scene consumer sampled on the shared circuit clock and actor-owned transition
requests. Initial native comparisons cover direct and weak/strong conducted
inputs, short and repeated pulses, linear push limits and a filled movable chest.
The checked-in evidence and remaining piston work are recorded at the end of
this report and included in this paused checkpoint.

## Reproducible evidence

`scripts/redstone_native_probe.py` reads the local client PE, verifies the
inspected function hashes, copies only relocation-free evaluator bodies to
executable memory, and calls the actual machine code in a separate Windows
Python process. No game binary or extracted machine code is stored here.

| Native function | RVA | Evidence |
|---|---|---|
| `ComparatorCapacitor::evaluate` | `0x66d8710` | 8,192 analog-input cases and 512 direct-input/sentinel cases |
| `RepeaterCapacitor::evaluate` | `0x66db530` | 16 fixed traces plus 128 deterministic transition traces (12,288 steps) with changing input, delay and lock state |
| `RedstoneTorchCapacitor::evaluate` | `0x66d9c60` | 1,536 state-copy and self-power-counter cases |
| `RedstoneTorchCapacitor::cacheValues` | `0x66d97d0` | Go cache/evaluate algorithm matches 2,240 executed cases with explicit source lists and dependency rings; world graph and scheduling are not injected by this probe |
| `RedstoneTorchBlock::neighborChanged` | `0x995c7f0` | Disassembled eligibility-flag/counter reset; ten native boundary traces at both setup parities |
| `TorchBlock::getPlacementBlock` / `RedstoneTorchBlock::setupRedstoneComponent` | `0x402f6b0` / `0x995bf40` | Disassembled default-permutation/facing selection and capacitor initialization; 60 BDS setup/world traces replayed through kernel UseOnBlock |
| `PulseCapacitor::evaluate` / `setStrength` | `0x66d93c0` / `0x66d93e0` | 256 executed evaluator cases and observer electrical output world traces |
| `TransporterComponent::cacheValues` / `evaluate` | `0x66dc3c0` / `0x66dc420` | Disassembled source maximum/attenuation cache; 256 executed cases and 48 network traces |
| `ObserverBlock::neighborChanged` / `tick` | `0x6ea8810` / `0x6ea8480` | Front filtering, pending ticks and phase alignment; 38 native scenarios |
| `ObserverBlock::onRemove` / `movedByPiston` | `0x6ea9450` / `0x6ea95c0` | Tick cancellation and movement pulse; 12 lifecycle and 12 piston scenarios |
| `RedstoneLampBlock::_onRedstoneUpdate` / `tick` | `0x995ae20` / `0x995a970` | Consumer phase, pending-off cancellation and delayed extinction |
| `RepeaterCapacitor::addSource` / `checkLock` | `0x66db3a0` / `0x66db7f0` | Source-type and directional lock filtering; six isolated native world scenarios |
| `RepeaterBlock::updateDelay` | `0x995dba0` | Configured delay is clamped to 0..3 and stored directly as capacitor insertion index |
| `Dimension::Dimension` / `tickRedstone` | `0x23039a0` / `0x230ade0` | Circuit tick rate and initial counter are both 2; evaluation resets and advances the counter |
| `LevelChunk::tickImpl` | `0x32c79d0` | Calls `CircuitSystem::updateBlocks` at `0x32cabe9`, separately from dimension evaluation |
| `PistonConsumer::addSource` | `0x66d8ee0` | Disassembled source-direction filtering; world captures below |
| `PistonBlock::setupRedstoneComponent` / `ConsumerComponent::evaluate` | `0x6eb32f0` / `0x66d8900` | Scene consumer setup, extended-state strength seed and shared source evaluation |
| `PistonBlockActor::tick` / `_spawnMovingBlocks` | `0x4ae13d0` / `0x4ae3330` | Actor transition requests, half-step progress, endpoint completion and positional sorting |
| Positional sort called by `_spawnMovingBlocks` | `0x4af9d70` (call at `0x4ae33e0`) | Original full sort body executes with at most twelve supplied positions; nearest PDB label is unrelated |
| `CircuitSystem::evaluate` / `evaluateComponents` | `0x66d63e0` / `0x66d6790` | Disassembly identifies cached-input and component evaluation passes; integration audit pending |
| `CircuitSceneGraph::add` / `update` | `0x66ce1f0` / `0x66ce960` | BlockPos hash, pending-add traversal, persistent chunk entries and component-vector construction; 26 native heap/world captures |
| Native BlockPos map rehash helper | `0x1cc1ec0` | Disassembled bucket growth and list splicing; pending tables grow 8/64/512 and active-chunk tables 8/64 in the native stress fixture |
| `ComparatorBlock::_refreshOutputState` | `0x8e15e90` | Disassembled block-state update path; world capture verifies subtract/equal lit state |
| `ComparatorCapacitor::addSource` / `updateDependencies` / `cacheValues` | `0x66d8420` / `0x66d8510` / `0x66d87e0` | Rear/direct versus side source filtering and separate cache/evaluate phases; 48 settled side-source scenarios |
| `ComparatorBlockActor::tick` / `ComparatorBlock::_installCircuit` | `0x4ab6900` / `0x8e16860` | Rear analog override, initialization and block-actor sampling; 30 settled rear-priority cases and 60 exact timing traces |
| `ComparatorBlock::use` / `_refreshOutputState` | `0x8e15360` / `0x8e15e90` | Direct configured-mode update at `0x8e15781`, strength-zero-crossing publication, independent variant/lit properties; 32 exact mode/property/replacement traces |
| Scalar analog outputs | `BeehiveBlock` `0x2a5d9f0`, `CopperBulbBlock` `0x6eb96d0`, `CauldronBlock` `0x8e02350`, `CopperGolemStatueBlock` `0x6ebcea0` | Disassembled signal getters and 77 settled world scenarios across honey, bulbs, cauldrons, cake, composter, anchor, portal frame and default statues |
| Campfire analog virtual dispatch | vtable `0xe86f390`, slots `+0x2d0` / `+0x2d8` | Both point to constant-zero `BlockType` methods at `0x76c20`; actual client instructions executed with image/function hash checks |
| Container fullness / decorated-pot getter | `0x26aefc0` / `0x9782170` | Native initialization, per-stack maxima, binary32 accumulation and flooring; 81 settled container cases and nine hopper-filled pot cases |
| Lectern/book and record outputs | `0x34547d0` / `0x66c3c80` | Native two-page-spread arithmetic and record-component signal; 165 lectern and 46 jukebox cases, including all 22 registered records playing/paused |
| Chiseled bookshelf comparator memory | `0x8e0d660` | Native actor's one-based last-interacted slot; 56 authored persistence states and 48 actual hopper-transfer cases with two barrel controls |
| Frame directional analog read | `0x66bf730` | Native facing check before actor/rotation read; 792 settled cases cover both frame types, four comparator directions, direct/conducted input, empty and rotations 0..7 |
| Switch geometry and projectile-operated buttons | `0x3193120` / `0x3195570` / `0x3194b00` | Arrow/trident collision and contact queries, fixed facing boxes and 20/30-tick constants; 196 source states, 480 output/conduction cases and 180 actual-arrow traces |
| Tripwire player destruction and hook sounds | `0x4035320` / `0x4038b70` | Disassembled pre-removal tool branches and sound/publication ordering; 58 actual player-cut traces and 230 native sound packets replayed |
| Daylight detector clock, signal and interaction | `0x4abfe70` / `0x977f7c0` / `0x977f250` | Inspected actor/variant/dimension gates, 16 native dimension/time traces, 28,800 full-day/weather source samples and 12 accepted player interactions |
| Daylight environment and body | `0x23122e0`, `0x230b0de..0x230b205`, constructor `0x977eacd..0x977eaf2` | Inspected binary32 celestial-angle/darkening instructions and constructor bounds; no native world callbacks or runtime sine-table initialization executed |
| Lightning rod hit, expiry and producer setup | `0x345a720`, `0x3459c30`, `0x3459110`, `0x3459360` | Inspected oxidation decrement, tick cancellation/rescheduling and active producer lookup; 100 actual-bolt BDS traces compare visible state, direct dust and support-conducted dust |
| Lightning hit position and rod geometry | `0x95966b0`, `0x32c8f33`, `0x3459fe0` | Inspected floored hit coordinates, integer natural rod spawn and six facing bounds; height-boundary traces and all 96 registered rod states checked |
| Rod attraction geometry, order and selection | `0x5502750`, `0x32f9750`, `0x2d9d600`, `0x32cbd00` | Actual paletted-cylinder arithmetic, group filter, distance sort and selection executed with synthetic world callbacks: 670 selection cases and 120 direct sort/heap cases |
| Trap selector dispatch and precipitation obstruction | `0xcca2418`, `0x32dcbf0` | Inspected shared selector call and precipitation-component scan; 26 original-BDS trap cases check bolt position and rod pulse occurrence through actual horse/player AI |
| Block-actor insertion, removal, shuffle and identity guard | `0x32c0a60`, `0x32c2dd0`, `0x32cb470` | Inspected stable insertion/removal and saved-component recheck; the original shuffle loop executes in 130 supplied-RNG cases, including all 24 four-actor permutations |
| Comparator analog sampling gate | `0x4ab6956..0x4ab697b` | Native dimension-counter comparison permits one sample per two game ticks; 44 read-only owned-BDS checkpoints independently observe the sampled rear value and electrical strength |
| Sculk activation, cooldown and calibrated output | `0x4ae91e0`, `0x81b51d0`, `0x81b3ae0`, `0x3198700` | Disassembled phase durations, strength and disabled input-side output; 36 actual player lever-use traces test both sensor types, inclusive range boundaries, both input parities and on/off frequencies |
| Calibrated filtering and vibration obstruction | `0x3198ac0`, `0x2dc1ea0`, `0x25d84a0` | Inspected input query, frequency/phase gates and six offset clip rays; 24 controlled-input and 48 material traces compare both accepted player lever uses, sensor phases and every output-wire frame |
| Button vibration and block queue clock | `0x319294a`, `0x31954ad`, `0x2d8e026`, `0x2d8e890` | Inspected press/release event dispatch and queue-local due-time arithmetic; eight actual-player button/sensor traces reproduce the 19/29-tick observed expiry and subsequent vibration pulses |
| Vibration distance, selection, reception and range | `0x25d8ec0`, `0x25d9295`, `0x4ae91e0`, `0x25d660d..0x25d668b` | Original selector executes in 80 cases, sensor receive in 110 cases with supplied activation callback, and range arithmetic in 92 cases with supplied radius; 24 actual-player BDS traces independently check diagonal sensor strength and every downstream wire frame |
| Button event/producer ordering | `0x319294a`, `0x3192b27`, `0x31954ad`, `0x31954f4`, `0x8bba9f0` | Inspected explicit event-before-active-producer update; 15 actual-player adjacent lever/button traces cover all four calibrated orientations plus normal-sensor controls, including every source/sensor/wire frame |
| Plate contact events and hook sound/event dispatch | `0x2a50be7..0x2a50bf1`, `0x2a50e50`, `0x4038b70` | Inspected zero/nonzero plate transition gate and source actor argument; 68 plate and 20 hook original-BDS contact traces compare all 18,480 source/sensor/wire frames. The full original hook `_emitState` executes in 64 flag/visible-state/position cases with supplied world callbacks |
| Plate sound and adjacent calibrated input | `0x2a50d0e`, `0x2a50d27`, `0x2a50d95`, `0x2a50e50` | Inspected resulting-state hash, Y+0.1 sound point and sound-before-event call order; seventeen original-BDS context traces and 34 actual player-client packets verify every plate material and observed adjacent filter strength/frequency behavior |
| Hook setup, calculation and producer direction | `0x4039490`, `0x4035830`, `0x3454bd0`, `0x66d7700` | Inspected powered-bit reset, zero producer setup, active-map strength setters and stored component direction; 72 lifecycle and 64 permutation BDS traces compare 32,640 full frames. Another 216 native button/rod rotation/replacement traces compare 19,440 frames through old/new support lamps |
| Vibration offsets, clip traversal and ray/AABB intersection | `0x25d892c..0x25d8ae0`, `0x2d8aaa0`, `0x25f12d0`, `0x2d95aa0` | Inspected 0.001 binary32 offset; complete original clip/predicate and ray/AABB bodies execute with supplied world/unit-wool shapes: 208 obstruction cases, 133 cell traversals and 202 direct intersections. Another 168 actual BDS source/sensor/wire traces retain 35,280 frames |

The evaluator fixture is
`server/block/testdata/bedrock_1_26_51_redstone_evaluators.json`. Component buffers
are synthetic. Repeater input-edge bookkeeping is transcribed from
`cacheValues`; the evaluator itself is executed native code. A trace step is
one evaluator call, **not** a claim about world-tick phase, graph construction,
block update ordering or rendering.

`scripts/redstone_bds_probe.py` starts the original BDS executable in a fresh
temporary directory with its own world, configuration and ports. Resource assets
are read through junctions; existing server worlds/configuration are untouched.
The saved JSON records the binary hash, build log and commands/responses.
Positive piston controls verify that the reference world is actually ticking.
The server is stopped on completion or failure. A failed/partial capture is not
accepted by the regression test.

## Corrections so far

### Piston power range

`Piston.shouldExtend` also read power around the block above the piston, a Java
quasi-connectivity rule. In the native world, a rear source extends an east-facing
piston, but a source two blocks above or diagonally above does not. Adamant
accepted both nonadjacent sources. The extra query loop has been removed;
directional adjacent power and exclusion of the extending face remain.

Native world inputs are retained in
`server/block/testdata/bedrock_1_26_51_redstone_world.json` and exercised by
`TestRedstoneWorldMatchesNativeCapture`. Full piston mechanics and indirect
conduction are separate, still-open audit items.

### Subtract-mode comparator state

With rear and side input both 15, the native subtract comparator has output zero
and `output_lit_bit=false` on `unpowered_comparator`. Adamant produced output zero
but set `Powered=true` and advertised `powered_comparator`: its lit predicate
ignored subtract mode and treated any rear input of 15 as powered.

Scheduled and pending updates now derive the lit flag from the calculated output.
The native world regression failed before this change and passes afterward.
`TestComparatorArithmeticMatchesExecutedClient` separately checks the arithmetic
against all 8,704 executed client cases. Rear/side source selection, through-block
reads, analog block outputs, timing and persistence require separate
world-level evidence, scoped below.

### Comparator source filtering, analog priority and circuit phase

Comparator sides previously accepted ordinary direct producers. Native
`ComparatorCapacitor::addSource` accepts wire, repeaters and comparators on
the sides; torch, lever and redstone-block side sources are excluded. A diode
must also point into the comparator. The new side filter reproduces all 48
settled source/mode/side combinations; 12 cases failed before the fix.
Adjacent analog blocks such as cake, cauldron and composter also remain
excluded from side sources. This matrix verifies the final eight frames of
each 24-frame capture, rather than claiming full startup parity for every
source type.

The rear analog override is not subordinate to direct power strength 15.
With cake behind a stone conductor and an independent repeater strongly
powering that stone, BDS still outputs the cake's 14, 8 or 2. Adamant used
15 because it attempted the through-block analog read only below full power.
That guard is removed. All 30 settled combinations of five intervening
materials, three cake levels and independent power match; the three powered
stone cases previously failed. Redstone block remains a direct strength-15
source and the tested glass/air/glowstone arrangements do not pass the far
analog reading. These five materials do not establish the full material
matrix or every analog block's signal calculation.

Comparators now participate in the world's common two-game-tick circuit
cache/evaluate phase and publish their output in the following chunk pass.
The old neighbor-triggered private two-tick timer sampled and propagated
pulses at different times. Analog values are sampled once per two game ticks,
separately from direct electrical input, in the shuffled chunk actor pass
before the next circuit evaluation. The first
electrical cache also waits for the initial actor sample, following
`mHasAnalogBeenSet`. Reading the current analog block directly in the
electrical cache would activate some initial states and pulses one circuit
step too early. Legacy scheduled callbacks only invalidate the current
component and cannot publish the receiver's stale permutation.

All 60 captures now match every sampled comparator permutation and wire
strength, including the initial side comparator, two-comparator propagation,
compare/subtract mode, rear redstone-block/wire/cake sources, pulse widths
1/2/3/4/8 game ticks and both absolute input tick parities. Fixtures are
`bedrock_1_26_51_comparator_sources.json`, `_rear.json` and `_timing.json`,
with matching scripts and Go replays. Analog rear reads use loaded blocks
only. Player interaction explicitly refreshes configuration, but mode changes,
bare direction/property edits, publication edits, full connection/material
rules, every analog-output block and native persistence still need their own
captures; they are not declared equivalent by these pulse cases. The mode
and direction cases below supersede the corresponding open claims only
within their captured scenarios.

### Comparator configured mode, independent lit bit and rotation

`ComparatorBlock::use` writes the capacitor's mode directly. A bare
`output_subtract_bit` edit on the same identifier does not. Both Script API
permutation edits and `/setblock` retain the old electrical calculation until
an identifier setup applies the new mode. For example, changing a powered
rear-14/side-8 compare comparator's property to subtract leaves output 14;
after a later off/on identifier transition it outputs 6. The world-owned
capacitor now retains its configured mode separately, while `Activate`
explicitly refreshes it as the inspected interaction callback does.

The block identifier and `output_lit_bit` are also independent. Adamant could
not represent a powered comparator with its lit bit cleared, which a native
command can produce while retaining a positive actor output. `Lit` is now
registered/hashed independently of `Powered`. Native `_refreshOutputState`
changes the identifier and lit bit when electrical strength crosses zero;
a positive-to-positive change does not automatically repair externally edited
properties. `ComparatorCapacitor::evaluate` publishes strength changes rather
than forcibly reconciling the permutation on every step. Cancelled outputs
remain retryable through a separate comparison with the live actor output.

The 32 complete mode traces compare 80 frames each, including both modes,
rear levels 14 and 8, both setup parities, a side-8 source, a second comparator,
and property/command/identifier/remove-and-replace changes. They verify every
sampled wire signal, identifier, lit bit, mode and facing. The original replay
failed all 32 cases before these changes. The same cases exposed a wire-side
cache error: the side filter read the wire's previous visible strength rather
than its flattened current source graph. That additional circuit-step lag is
removed. The existing 60 pulse traces continue to pass with lit/mode/facing
checks enabled. A dedicated activation regression checks 14 -> 6 -> 14 without
an identifier transition; a cancellation regression checks suppression and
retry. These Go integration checks are distinct from an actual native player
packet capture, which has not been performed.

Direction has a similar setup lifetime. A bare cardinal-direction edit keeps
the original capacitor connections and output face. The block actor still
reads the live direction: a perpendicular edit writes its side analog field,
while an opposite edit leaves the previous analog fields intact. An explicit
identifier replacement initializes the new direction and analog samples while
retaining output strength. Removal followed by placement creates a new actor
with zero output. Internal publication is distinguished from an external
replacement so that it does not discard configured direction prematurely.
Torch dependency source-face discovery uses the same configured comparator
direction as ordinary electrical queries.

The 40 rotation captures compare two directions, both modes, both
parities and all four edit methods, with three-block wire outputs east and
south, independent cake edits, an explicit later identifier replacement and
an air/replacement sequence. All 3,200 frames match; 16 property/command cases
failed in the initial 32-case replay before retaining configured direction.
The eight additional cases force an internal zero crossing after rotation:
an old rear-8 input and new side-14 analog input turn a subtract comparator
off, then a side reduction turns it back on through its original output face.
These captures verify that internal identifier transitions preserve configured
direction, unlike explicit replacements. Full player-packet timing,
pending-publication edits in larger graphs, native persistence, the complete
source/material graph and all analog-output blocks remain open.

### Comparator replacement while a result is pending

The new publication matrix changes the source first, then edits the comparator
one or two game ticks later. It covers rising/falling input, both absolute
edge parities, and six methods: no edit, a mode property, a facing property,
an identifier replacement, removal, or removal followed by placement. All
48 traces retain every sampled comparator permutation and wire signal.

Four identifier-replacement cases failed before the fix. A previously evaluated
result remained queued after the old capacitor was recreated. On a rising edge
it lit the new comparator too early; on a falling edge it removed the signal
one circuit step too early. External identifier replacements now discard
pending block/strength publication and its pending cause for components with
explicit reinitialization semantics. Their own publication still preserves
the capacitor. The policy applies to comparators and torches; the new native
publication matrix specifically covers comparators, and the existing torch,
repeater and observer regressions also pass. This does not establish every
component's replacement semantics or larger simultaneous-edit graphs.

### Scalar analog block outputs

`redstone_bds_analog_blocks_probe.py` captures 77 native permutations: both
hive types at honey levels 0..5, all eight copper-bulb variants lit/unlit,
water/lava/powder-snow cauldrons at fill 0..6, seven cake levels, candle cake,
nine composter levels, five anchor charges, both portal-frame eye states and
four waxed statue variants at their default pose. Each capture has 16 frames;
the regression checks the final eight, leaving general transition timing to
separate traces. Twenty-six cases failed in the unchanged analog code.

- Hive/nest output is the honey level. Both were missing and returned zero.
- A lit copper bulb outputs 15 independently of oxidation and waxing; an
  unlit bulb outputs zero. The comparator override was missing.
- Cauldron output is `(fill_level + 1) / 2` for all three liquids. The old
  special case returned 3 for every nonempty lava state. Four partial lava
  permutations were wrong; these registered states can be selected by commands.
- Copper-golem statues were missing their analog output. Client getter
  `0x6ebcea0` reads `SerializedActorBlockActor::mPose`, clamps 0..14 and adds
  one. Normal poses therefore supply 1..4. The world capture verifies the
  default pose's signal 1 for four waxed oxidation variants; nondefault pose
  world interaction/persistence still require captures.

Campfire had the opposite problem: it invented an inventory-fullness signal.
The actual client `CampfireBlock` vtable at `0xe86f390` points both analog
support and signal slots to constant-zero `BlockType` methods at `0x76c20`.
`redstone_native_analog_probe.py` verifies the image/vtable/function hashes and
executes those original call-free methods. Campfire's analog override is
removed. Twenty Go configurations cover both fire types, lit/extinguished
state and zero to four occupied cooking slots. This is an executed-method
dispatch check, not a native world capture with a player cooking food.

The new fixtures are `bedrock_1_26_51_comparator_publication.json`,
`bedrock_1_26_51_analog_blocks.json` and
`bedrock_1_26_51_analog_dispatch.json`. Later sections cover initial container,
pot, book, record and frame outputs. Sensor frequencies, command success
counts, shelf variants and broader native save/load remain open.

### Inventory fullness and decorated pots

`Container::getRedstoneSignalFromContainer` at `0x26aefc0` first calls
`initializeContainerContents` (vtable slot `+0x140`). It then sums each occupied
stack's count divided by its item-specific maximum, divides by the number of
slots, multiplies by 14, floors, and adds one if anything is present. The
ratios, accumulator and final arithmetic use SSE binary32 operations. The
inspected main-body span has size `0x1c6`, SHA-256
`14e324bfc06de68b2bdd04028311322eb3851758dea25d899859af669b891f34`;
the multiplier at `0xe7be570` is 14.0. Adamant now uses a shared binary32
implementation for ordinary inventories and the pot's single stack. This
precision change follows the disassembly; no separate floating-point boundary
failure is claimed for the registered 64/16/1-stack items in this matrix.

Decorated pots had no comparator override, despite native getter `0x9782170`
delegating to that one-slot container calculation. Their getter now initializes
pending loot through the existing pot path and computes the stack signal.
Dispenser and dropper had inventories and viewer methods but lacked
`ContainerSize`, so they failed the complete `Container` interface used by
`comparatorOverride`. Both now expose their nine slots. The regression fills
their actual inventories through the narrower inventory-reading interface,
independently of the missing-method bug, rather than accidentally leaving
those test inventories empty. Twenty-four nonempty cases across these three
block types failed before the fixes and now pass.

The ordinary matrix has 81 native cases: chest, trapped chest, barrel, hopper,
dispenser, dropper, furnace, brewing stand and a paired 54-slot double chest,
each under nine content patterns. Stone, egg and wooden sword exercise stack
maxima 64, 16 and 1; patterns include empty, single items, partial/half/full
stone stacks, mixtures and a signal-boundary neighborhood. Actual native
inventories, including per-item maxima, are read back and checked unchanged
during the trace. Source preparation precedes comparator placement by eight
ticks; the Go replay checks the final eight of 16 sampled frames.

The brewing stand has an explicit empty fuel slot. Diagnostics found that BDS
consumes invalid stone in fuel slot 4 one item per game tick while its fuel
charge remains zero. That is an evolving inventory, rather than a scalar
rounding failure; it is not used as a supposedly stable reference. The
`full_stone` brewing pattern therefore fills the other four slots, not all
five. Fuel-charge/recipe transitions and a fully stocked charged brewing
stand remain separate open cases.

The stable Script API does not expose the pot inventory, and `/replaceitem`
does not populate it. Failed/partial attempts are not accepted fixtures. The
nine valid pot cases instead transfer the actual items through native hoppers:
the pot is the only outlet, the intake is empty, hopper contents are read
until empty, and item drops invalidate the capture. The input counts follow
that conservation check, explicitly labelled in `input_mode`; they are not
claimed to be direct pot inventory readback. All nine cases finish, including
a nonstackable sword (signal 15), 17 stone (4), 32 stone (8) and 64 stone (15).
The Go replay sets these final pot contents and checks settled comparator
output; it does not replay or establish the kernel's hopper transfer timeline.

Fixtures are `bedrock_1_26_51_inventory_signal.json` and
`bedrock_1_26_51_pot_signal.json`, with corresponding scripts and the shared
`TestInventorySignalsMatchNativeCapture` replay. Shulker/copper-chest variants,
blocked lids and openers, loot-backed native worlds, other container-specific
initialization, inventory transition timing, chunk boundaries and persistence
remain open; this is not a claim of complete container or hopper parity.

### Books, music discs and bookshelf memory

`LecternBlock::getComparatorSignal` at `0x34547d0` reads the stored spread
index at actor offset `+0x288` and total written-page count at `+0x28c`.
For a nonempty book with fewer than four pages it returns 15. Otherwise it
computes `floor(float32(page) / float32(totalPages / 2 - 1) * 14) + 1`;
integer division happens before the binary32 calculation. The constants
at `0xe7be570` and `0xe5e9578` are 14.0 and -1.0. An empty lectern returns zero.
Adamant previously used the Java-style individual-page denominator and
returned one for a short book. Both the short-book result and the spread
calculation are corrected. `Lectern.Page` now documents the native spread
index, which is already the stored and packet-facing value.

The 165 lectern cases cover empty and zero-page books, both writable and
written books, lengths 1/2/3/4/5/6/7/8/15/16/49/50 and every valid spread
at those lengths. Inputs are explicitly authored `.mcstructure` persistence
states, loaded by the original BDS structure loader. Native inventory readback
verifies the item and written-page count. This is **not** a player page-turn
or insertion trace: pulse duration/retrigger, packet validation, malformed
actor values, total-page memory after external edits and broader save/load
remain open.

`JukeboxBlock::getComparatorSignal` at `0x66c3c80` reads the record component's
signal at `+0x3c`; it does not test playback state. The 46 native cases cover
all 22 registered records and empty, each playing or explicitly paused through
the native record-player API. Relic's signal is 14, not Adamant's former 15.
Creator, Creator Music Box, Precipice, Tears, Lava Chicken and Bounce now
return 12/11/13/10/9/8 rather than zero. Fourteen jukebox and 142 lectern cases
failed before the corrections; all 267 cases in the combined matrix pass now.
The replay compares the last eight of 16 native frames. It makes no claim
about playback completion, sound/dancer state or insertion/ejection timing.

The remaining 56 cases exercise `ChiseledBookshelfBlock::getComparatorSignal`
at `0x8e0d660`: it returns the actor's one-based last-interacted slot at
`+0x2a0`, regardless of occupancy. All four facings, empty/full occupancy and
last-slot values 0..6 match the existing kernel behavior. These inputs also
use authored persistence states; they do not establish player hit mapping.

A second capture uses actual native hoppers, rather than directly setting
final bookshelf contents: 48 combinations of four shelf facings, insertion
from above/extraction from below, and one to six books. Native hopper counts,
shelf occupancy and comparator output are recorded for 128 ticks. Two barrel
controls verify both transfer paths are working. The kernel replays actual
hopper transfers and matches the final 16 frames in all 50 cases. In
particular, extracting every book leaves signal equal to the last slot
removed, rather than resetting it to zero. Individual transfer phases,
horizontal hopper faces, non-book filters, player interactions and reload
remain separate open cases.

The reproducible scripts are `redstone_bds_book_record_probe.py` and
`redstone_bds_bookshelf_hopper_probe.py`. `redstone_audit_structure.py`
encodes only authored little-endian NBT; no game assets or binary code are
embedded. The reference harness writes these structures into its fresh
temporary development pack, never the asset junction. Fixtures are
`bedrock_1_26_51_book_record_signal.json` (267 cases, 4,272 frames) and
`bedrock_1_26_51_bookshelf_hopper.json` (50 cases, 6,400 frames).

### Frame direction and comparator reads

`ItemFrameBlock::getComparatorSignal` at `0x66bf730` compares the queried
direction with the native `facing_direction` before reading the actor. With
an item present, it divides the actor's degree rotation by the binary32
constant 45.0 at `0xe82f930`, truncates, clamps the upper value to 14 and adds
one. In the normal rotation range, outputs are 1..8; an empty frame gives zero.
Adamant already computed the ordinary rotation values, but discarded the
queried direction and supplied the signal from every side. The comparator's
analog read now passes its input direction to both direct and through-block
queries and filters frame output by the corresponding native facing. Kernel
`ItemFrame.Facing` points toward the support, opposite to the native property.

The initial west-reading matrix reproduced 160 failures among 198 cases
before the direction fix. The expanded native fixture then covers all four
horizontal comparator directions: 792 cases, both normal/glowing frames,
all six frame facings behind a stone conductor, five supported facings for
direct reads, empty frames and ordinary item rotations 0..7. The remaining
direct facing is deliberately omitted: it would require using the comparator
itself as the frame's support. The corresponding valid stone-backed facing
is included and supplies 1..8. Every sampled frame is checked still present,
so missing support cannot masquerade as a zero-signal result.

All 792 cases match in the final eight of 16 frames. Inputs use authored
persisted frame actors, loaded by the unmodified native structure loader;
individual player rotations/removal, map-specific rotation steps, non-stone
supports/conductors, malformed actor rotations, transitions and reload remain
open. The script is `redstone_bds_frame_signal_probe.py`, and the retained
fixture is `bedrock_1_26_51_frame_signal.json` (12,672 native frames). Four
successive batches share one fresh reference world to keep the active area
within the native ticking-area limit; each batch has controlled setup parity.

### Switch outputs and projectile-operated buttons

The source-state matrix covers all 15 registered button materials in six
mounting directions, pressed/unpressed, plus the eight lever orientations
on/off: 196 native cases. Script API producer readback identifies output
strength, all six connected faces and the strongly powered support face.
The kernel matches every one of the 80 sampled source/wire frames per case,
including initially pressed command permutations remaining pressed. These
are explicit permutation inputs, not player click/release traces.

Another 480 cases cover wood/stone buttons and all lever orientations,
six output directions, on/off and direct versus one-stone-block conduction.
The last eight of 24 frames match for the switch, conductor and lamp. A
direct lamp can itself serve as the mounting support; the native capture
checks the source remains present. Strong conduction goes into the support,
while direct output reaches all six faces. Three batches share one fresh
reference world, with controlled even setup parity.

Projectile activation was missing entirely from `Button`: it had no
`EntityInside` callback, released unconditionally, and its empty physical
model made arrows pass through the button into its support. Client method
`ButtonBlock::getCollisionShape` (`0x3193120`) explicitly permits only arrow
and thrown-trident actor types, then reads six fixed facing boxes. The shape
is independent of pressed depth. Ordinary entities and other projectiles
have no such collision. `ButtonBlock::entityInside` (`0x3195570`) scans on
an unpressed wooden button; `_checkPressed` (`0x3194b00`) queries both arrow
and trident actors in that fixed box, presses on contact, rechecks after
30 ticks while a projectile remains, and releases on a later check when
none remain. `buttonPressed` selects 20 ticks for stone and 30 for wood.

The kernel now supplies those entity-specific projectile collision boxes
without adding ordinary physical collision. Projectile traces and attachment
checks use them, so arrows stop and remain at the button face. Wooden buttons
scan actual arrow/trident bounding boxes, including a touching face, keep
their signal while a projectile remains, and release at the next scheduled
check after removal. Querying only entity origins would miss resting arrows;
the local search is expanded and then tests each body's actual bounds.

The initial 48 native arrow traces exposed another phase error during real
kernel-arrow integration: post-movement contact pressed a button one game
tick too early. Native snapshots show the arrow already stopped at the
button before its pressed bit changes. Button contact for arrows/tridents
now runs before projectile movement, with its former post-movement callback
omitted. Other inside-block effects keep their existing phase. No arbitrary
extra tick was added to the native 20/30-tick durations.

The expanded capture has 180 cases: all 15 button materials, six mounts and
both absolute spawn parities. The original BDS simulates actual shot arrows;
their positions, velocities and AABBs are read back. Arrows are removed 45
ticks after spawning. All 13 wooden materials activate and hold, then release
at a later recheck; stone and polished blackstone remain off. Each case
retains all 100 source/wire frames. Restoring the original button code
reproduced 156 failing wooden cases in each replay layer. The working file
was restored unconditionally after that baseline run.

One replay supplies the recorded native body poses, evaluating contact in
the world entity phase between interval snapshots. A separate replay uses
real kernel arrows from the recorded spawn positions/velocities. Both match
all 180 redstone traces. The latter also verifies that every arrow, including
stone-button controls, remains attached to the actual button and agrees on
the native collision-axis coordinate. This establishes the tested button
behavior and impact plane, not full numeric parity of projectile flight.

`redstone_native_button_probe.py` verifies the complete client image hash,
reads the actual six floating-point box tables, and records the inspected
collision/query actor constants and both press/recheck delay constants.
It records disassembly-span hashes; it does **not** execute these methods.
The geometry regression covers all button materials, both pressed states,
all six faces, arrows/tridents and excluded snowball/item controls.

Fixtures are `bedrock_1_26_51_switch_states.json` (196 cases, 15,680 frames),
`bedrock_1_26_51_switch_faces.json` (480 cases, 11,520 frames),
`bedrock_1_26_51_button_projectiles.json` (180 cases, 18,000 frames) and
`bedrock_1_26_51_button_geometry.json` (inspected client data). Their scripts
are `redstone_bds_switch_state_probe.py`, `redstone_bds_switch_faces_probe.py`
and `redstone_bds_button_projectile_probe.py`.

### Player-operated switches and click packets

The loopback packet client in `scripts/redstone_client` now completes the
native loading-screen acknowledgement and sends one authenticated input per
client tick. `redstone_bds_switch_use_probe.py` drives actual native player
interactions: all 15 button materials, all six mounts for wood/stone, all eight
lever orientations, and both single/repeated use. All 66 traces match the
kernel's source state and adjacent wire at every snapshot. Accepted before/after
interaction events and an actual powered permutation are required positive
controls. Both actual input parities occur; network arrival is observed rather
than assumed from the requested parity. Inputs are aligned by independently
recorded absolute game time, not the SDK's deferred event tick label.

An earlier experimental client sent an extra authenticated input for each
click. That advanced its simulation clock and shifted later native release
samples by one tick. This capture is excluded; the retained capture uses only
the regular input cadence. No button timer was changed to compensate for a
measurement error. `bedrock_1_26_51_switch_use.json` retains the 66 final traces.

Native sound packets revealed a separate implementation defect: buttons were
silent and levers used generic power events. Both use `button.click_on` and
`button.click_off`, with the resulting block state's signed network hash in
ExtraData and the block centre as position. Button press/release and lever use
now send those events. `redstone_bds_switch_sound_probe.py` retains six native
packets from stone/wood buttons and a toggled lever; the regression compares
names, hashes, entity type and positions. The before-fix check fails for all
three sources. Repeated use of a pressed button does not produce an extra click.

`Reference(local_client=True)` explicitly opts a fresh temporary reference into
an offline creative profile for the credential-free loopback client. The normal
reference profile is unchanged. Client commands are local stdin JSON, and block
hashes come from native SDK permutations rather than kernel runtime IDs.

Broader support/material checks, projectile boundary/multiple-entity combinations,
rotation/replacement, unload/reload, and the full sound material/mount matrix
remain open. The inspected projectile query/collision paths include tridents,
but stable Script API refuses to summon that entity; an attempted capture was
rejected and is not a fixture. Native world interaction for tridents remains open.

### Pressure-plate states, entity queries and weighted output

Ordinary Bedrock plates encode the pressed state as `redstone_signal=1` and
produce electrical strength 15 only for that data value. The previous kernel
stored 15 in the block property and returned the property unchanged as power.
The native scalar methods at `0x9952600` and `0x9952610` were executed for
inputs -1..16; the weighted identity method and both 20/10-game-tick recheck
methods were also executed. All five methods are retained with client hashes
in `bedrock_1_26_51_pressure_plate_methods.json`. Ordinary plate publication and
power now follow those conversions, including zero output for other data values.

Entity detection previously used origins inside a full-width, half-block-high
box. This missed dropped items resting exactly on its lower boundary and counted
bodies outside the native contact outline. The inspected sensitive box is
[1/16, 0, 1/16] to [15/16, 1/4, 15/16]; the query now tests actual entity AABBs.
Wood and weighted plates accept objects but exclude experience orbs explicitly.
Stone and polished-blackstone plates query the native Mob category; kernel
living entities and armour stands qualify, whereas arrows and dropped items do
not. The earlier implementation excluded only dropped items from stone plates.
Weighted strength uses the native binary32 ratio, multiply by 15 and ceiling,
with caps 15 and 150 entities for light and heavy plates.

The two native BlockSource overloads also differ at chunk edges: the typed
actor query at `0x2d850b0` scans the sensitive box's chunk range directly;
the untyped query at `0x2d845f0` expands that range by two blocks before scanning.
An armour stand overlapping a stone plate from the neighbouring chunk is
therefore ignored in the retained native case, despite its overlapping AABB.
The kernel now preserves that selection rather than applying an arbitrary
origin expansion. Reads scan already available entities and do not load chunks.
The replay keeps actual native coordinates so a chunk-edge observation cannot
be silently moved to the centre of a chunk.

`redstone_bds_pressure_plate_probe.py` records 302 cases / 24,160 frames:
all 17 materials against pigs, armour stands, items, arrows and experience orbs
at both input parities; 52 light/heavy weight cases from 2 to 151 armour stands;
and 80 horizontal-edge/elevated-body controls for wood, stone and both weighted
plates. Every sampled source state and adjacent wire value matches. Native
body positions and AABBs are replayed using stand-ins to isolate plate queries
and scheduled release from independent kernel AI, movement and item merging.
Captured entity counts and positive activation controls are checked; a reference
that merely spawns bodies without ticking plates cannot satisfy the capture.

This does not close the entire plate/tripwire area. Player game modes,
sneaking/other excluded actor states, additional actor types, exact touching
boundaries, explicit negative/cross-chunk matrices, count-changing weighted
transitions, native sound packets, support materials, replacement/load and
real kernel entity contact integration still need dedicated evidence. Tripwire
and hooks are covered separately below; their remaining cases stay open.

### Tripwire contact, hook scans and command removal

Tripwire previously queried entity origins in an entire block cube and used a
carpet collision model. Native contact instead queries actual body bounds
against `getVisualShape`: a non-suspended string spans Y=0..3/32; a suspended,
attached string spans 1/16..5/32; a suspended, unattached string spans 0..1/2.
The object-query chunk range is the same untyped BlockSource overload described
for plates. A shared private helper now applies that range and actual AABB
intersection to both components. Unlike plates, experience orbs do trigger
tripwire; the native traces explicitly retain this distinction.

`TripWireBlock::onPlace` starts from the default permutation and sets
`suspended_bit=true`. Requested `/setblock` disarmed/suspended flags do not
survive initial placement, while a later same-identifier permutation edit does.
The kernel now performs that initialization through the placement callback.
Initial connection/source scans run for both strings and hooks. Contact capture
therefore records placement first, then explicitly edits the existing string at
a known tick and confirms the requested properties were actually applied.
The earlier experimental command-only disarmed setup was rejected as evidence
of a disarmed string: native placement had reset the property.

Native `_updateSource` scans south and west up to distance 41; one hook scan
updates both ends. `calculateState` considers hook distances 2..41, allowing
40 strings. The old kernel stopped at distance 40 and rejected that valid maximum.
The scan now continues across gaps to find and update the opposite hook while
marking the line discontinuous. String attachment is published only when the
starting hook's attachment changes and an opposite hook was found, following
the inspected native callback rather than reconciling arbitrary edited bits.

Disarmed string can have `powered_bit=true` without powering hooks. A source
change at that index temporarily detaches the line and schedules the originating
hook's ten-game-tick recheck. That recheck can reattach a continuous disarmed
line without making it electrically active. The kernel previously ignored
disarming and did not implement the hook recheck. Native string holding/release
also uses ten-tick checks. Both delay immediates are retained in the client
geometry/constant fixture.

Ordinary neighbouring-block notifications update connections/support; they do
not rescan the whole line. In all 80 native command-removal cases, removing
string with `setblock ... air` leaves the existing hook attachment bits set.
The kernel previously detached the line through its broad neighbour callback.
Source callbacks, hook placement and pending rechecks now drive line calculation.
Player cutting uses a separate preprocessing callback; its capture and port
are described below. Command removal does not invoke that player callback.

The tripwire collision virtual slot at vtable `0xe8b4960 + 0x20` points to the
constant-empty getter at `0xe1d20`. Its actual relocation-free instructions were
executed and returned six zero bounds. The kernel's artificial 1/16-block carpet
collision is removed; a regression checks every registered string permutation.
`redstone_native_tripwire_probe.py` records the image/method hashes, executed
collision result, inspected visual tables, scan limits and tick immediates. It
does not execute the entity/line callbacks in a fabricated world.

`bedrock_1_26_51_tripwire_contacts.json` contains 140 cases / 11,200 frames:
connected, explicitly disarmed and unhooked strings, both suspended properties,
five native actors, both input parities, six selected body heights and four
horizontal overlap controls. Every string property, both hook states and the
output wire are replayed. Recorded native body poses/AABBs isolate component
queries from the independent kernel movement integrator.
`bedrock_1_26_51_tripwire_lines.json` adds 80 cases / 5,120 frames covering all
four directions, 1/2/39/40/41 strings, disarmed/unarmed command removal and both
parities. Every sampled string permutation, hook and output matches. Scripts are
`redstone_bds_tripwire_probe.py` and `redstone_bds_tripwire_line_probe.py`.

Open tripwire work includes previously powered/disarmed player-cutting states,
additional support materials and loss timing, more player hook-placement/contact
paths, rotations/replacement during pending checks, explicit gaps followed by later
source changes, crossings, full weak/strong input/output/material matrices,
additional actor states/game modes, real kernel movement/contact phase, exact
touching boundaries, other sound/state combinations and save/unload/reload behavior.

### Hook producer setup, replacement and permutation edits

`TripWireHookBlock::setupRedstoneComponent` at `0x4039490` first clears the
visible powered bit, creates a producer using the setup direction, and sets its
strength to zero. In `calculateState` at `0x4035830`, the opposite hook's block is
published before its active producer strength and sound/event dispatch. The
starting hook emits its sound/event before publishing its own block and active
strength. Both producer lookups use the active map; a newly pending component
does not receive the update. These setters still matter when the visible flags
have not changed. The previous kernel read hook electrical power directly from
the visible bit and omitted both active-component updates.

Hooks now implement `RedstoneCircuitProducer`, seed zero, and update active
strength at the two native publication points. Fresh placement clears the
authored powered bit before calculating the line, preserving the native event
selection. This also avoids a false release vibration when an explicitly powered
hook is placed into an idle line, and restores the actual activation vibration
when it is placed into an occupied line.

The native component additionally keeps its original direction across a
same-identifier permutation edit. Rotating the visible hook north while keeping
valid south support can leave the original west support electrically powered.
Producer state now stores its setup output configuration alongside strength;
weak/strong queries use that configuration. An ordinary air/identifier replacement
creates a new configuration. The circuit-lock path used by daylight inversion
and lightning-rod oxidation retains the existing configuration and strength.

The following original-BDS fixtures read every observed frame before scripted
changes, preserving actual actor identities/poses for contact-driven lines:

| Fixture | Cases / frames | Coverage |
|---|---|---|
| `bedrock_1_26_51_hook_lifecycle.json` | 72 / 17,280 | Hook/string/support removal, unsupported rotation, stone replacement, repair, missing-string completion and adding a hook; idle/occupied lines, filters 9/10 and both parities |
| `bedrock_1_26_51_hook_permutations.json` | 64 / 15,360 | Explicit powered/attached placement, same-ID flag edits, supported rotation, repair and later string removal; direct wire independently observes rotated output |
| `bedrock_1_26_51_producer_rotation.json` | 216 / 19,440 | Stone/wooden buttons and rods, six initial faces, selected same/opposite/orthogonal rotations, both edit parities, with/without air replacement and final reset; old/new support lamps observe independent conduction |

The lifecycle capture was repeated with disabled tile drops and an asserted
absence of native item actors. The rotation capture checks that every source
retains its commanded powered flag and lights its old support lamp before the
edit; the replay also rejects a fixture lacking that lit-lamp control. It is a
command-state/configuration test, not a player-use or lightning-lifecycle test.
The original 72-case replay failed 32 cases before the producer fix. The new
64-case replay then exposed twelve additional event/direction mismatches, all
corrected. The lifecycle tests now run normally without an opt-in variable.
The full pressure-plate/tripwire matrix still retains the open work listed above.

### Actual player hook placement

`bedrock_1_26_51_hook_placement.json` adds sixteen accepted real-player placements
and 665 per-tick line snapshots: four horizontal directions, creative/survival,
and continuous/gapped three-string lines. Before-interaction and after-placement
events confirm the held hook, target block and accepted input. Replay uses real
`Player.UseItemOnBlock` at the recorded before/after-sample input phase and checks
every hook/string permutation. The player stands outside string contact bounds.

The first replay matches the visible states but emits 32 attachment packets
against sixteen native packets. `TripwireHook.UseOnBlock` calculated the line
again after `place` had already invoked `Placed`. Its second call used the
original hook flags, generating another pair of attachment sounds/events. Native
`onPlace` at `0x4037f70` calls setup and then calculates once at `0x4037fb9`.
The explicit duplicate scan is removed; `Placed` owns this calculation.

The native fixture retains all 24 hook sound packets. The placement replay
compares the sixteen attachment packets, including exact order, point and visible
state hash. Eight detach packets from clearing the preceding native circuit are
retained as capture context; each Go scenario uses a fresh world and does not
replay that cleanup. This does not replace the existing full 230-packet cutting
comparison or claim untested removal sound paths.

The placement driver waits for the client to receive the actual held stack,
and equips it with the native inventory command. The clicked support is end
stone, whose Script API state has no legacy stone alias to hash. Early attempts
that sent an empty held stack, or timed out waiting for the Script API inventory
write to reach the client, produced no accepted placements and were rejected.
The final fixture comes only from the complete sixteen-case capture.

### Actual player cutting and hook sound packets

Native `TripWireBlock::playerWillDestroy` at `0x4035320` changes the string while
it still exists and calls `_updateSource`. Ordinary tools set its powered bit;
shears clear that bit. Shears do **not** set `disarmed_bit` in this callback.
This distinction matters: a previously ordinary line remains attached until
the hook's delayed recheck, whereas a disarmed source update can detach it
immediately. The player then removes the string. The kernel previously had no
player-destruction callback, so a cut by hand emitted no pulse and neither tool
requested the later detachment.

`Tripwire.PlayerWillDestroy` now ports that preprocessing. The optional
`world.EntityDamageSource` interface exposes the responsible entity without
requiring blocks to import the entity package. `entity.AttackDamageSource`
implements it, allowing the callback to inspect the actual player's main hand.
The existing player break path already invokes the callback before removal.
No new method is required of other existing DamageSource implementations.

`redstone_bds_tripwire_break_probe.py` drives actual authenticated block actions
from the loopback client and records native before/after break events, held
items, game mode, block positions, absolute game time and interval snapshots.
A use-item break transaction alone was ignored by BDS and is excluded. A further
experimental client omitted aborting mining: its surviving mining state broke
a later scenario's newly placed string before the requested action. That capture
is rejected. The retained client sends start/predicted-destroy actions in one
normal input tick and aborts mining on the next tick. A native event before the
request, at another position, with the wrong tool/mode, or without an actual
air block afterward cannot pass the capture controls.

The retained `bedrock_1_26_51_tripwire_break.json` contains 58 cases / 3,568
frames: creative and survival, hand and shears, 1/3/40/41 strings, end/middle
cuts, all four directions, and both actual input parities. The 41-string cases
are an unattached length-limit control. Tile drops and mob spawning are disabled
in the isolated reference to keep incidental item actors out of this component
comparison. The kernel replay uses actual `Player.BreakBlock` and inventory,
not a manually injected hook state, and checks every sampled string permutation,
both hooks and the conducted output wire. Real player replay runs in Overworld:
using End with the full entity registry allowed its independent dragon-fight
portal initialization to overwrite the test layout and was corrected in the
harness before using the result as evidence.

The same native packet capture revealed missing hook sounds. Native `_emitState`
at `0x4038b70` prioritizes power transitions over attachment transitions and emits
`power.on`, `power.off`, `attach` or `detach` at (x+0.5,y+0.1,z+0.5). ExtraData is
the network hash of the block currently at the sound position. `calculateState`
publishes the opposite hook first, sounds that hook then its own, and only then
publishes its own state. Thus one sound uses the new permutation and the other
uses the old permutation. Both transition comparisons use the starting hook's
previous flags. The kernel now preserves this ordering and payload selection.
All 230 retained native hook packets match in sequence for event name, position,
state hash and entity type, in addition to the complete world-state replay.

This covers the recorded ordinary-line cuts with tile drops disabled. Cutting
already powered/disarmed strings, cutting with other live inputs, ordinary item
drops interacting with surviving strings, support destruction, cancellation,
packet/viewer publication ordering beyond sounds and native reload still require
separate scenarios. The basic hook contact vibrations and helper transition
priority are verified below; wider cut/support/attachment lifecycle integration
still needs source/sensor captures.

### Daylight sampling, weather and body geometry

The native detector actor tests the signed 32-bit **day clock** for divisibility
by twenty. The kernel previously tested elapsed simulation ticks. A day clock
frozen at 6001 therefore still made the kernel resample, while BDS retained the
authored signal indefinitely. Frozen time zero/6000 samples every simulation
tick in BDS. The corrected actor follows that clock, including time commands.
The native `hasSkylight` gate also forces both variants to zero in Nether/End;
the kernel previously let the inverted detector produce power there.

The detector now has dedicated Bedrock angle/darkening helpers. They preserve
binary32 lookup-index, easing and multiplication steps from the inspected
client rather than using Java 26.3's environment-attribute timeline. The native
tick path supplies an interpolation fraction of one while time advances and
zero while frozen; captures around time 12040 distinguish these paths. The
world's general Java sun angle, sleeping and raw-brightness APIs are unchanged.
Rain and thunder use the inspected multiplicative 5/16 reductions of daylight
before integer darkening. The current world supports boolean full-strength
weather, so partial weather levels and fog are not implemented by these helpers.

`bedrock_1_26_51_daylight.json` records 16 setups, each containing 48 cells and
60 frames: all three dimensions, both variants, initial strengths zero/fifteen,
clear/glass/stone/leaves covers and frozen/advancing time. The replay compares
all **46,080 source frames and adjacent wire frames**, preserving the independent
SDK simulation-clock parity and day clock. The SDK's `getSkyLightLevel` is
already darkened (four under open sky at midnight); its values are not injected
into the kernel's raw light storage.

`bedrock_1_26_51_daylight_cycle.json` contains another 18 native segments:
every eligible time 0,20,...,23980 in clear/rain/thunder, both variants and four
covers, for **28,800 source samples**. Actual world lighting and the detector
getter match all source values, plus the native SDK's darkened sky observations.
The generator supplies the authored static floors/roofs before light filling;
this deliberately isolates the detector from the separate live-lighting gap
below. No measured light or expected signal is substituted into world storage.

Those static roofs exposed two initial-lighting defects: the light height map
ignored partially filtering blocks such as leaves, and a required propagation
node was prefilled to strength fifteen, causing the queue to skip it. Air at the
bottom edge of a roof consequently received one level too little light. The
height map now includes diffusers while `HighestLightBlocker` retains its
opaque-only contract; required source nodes propagate before being marked lit.
The native roof matrices pass with these corrections.

The native constructor writes a full-width body of height **3/8**. The kernel
used carpet's **1/16** height. All 32 registered detector states now use the
inspected native bounds. `redstone_native_daylight_probe.py` records the image,
instruction-span hashes, numeric constants and constructor bounds in
`bedrock_1_26_51_daylight_methods.json`. This is inspection evidence, not native
callback execution. The runtime-initialized client sine table was not extracted;
the helper's generated mathematical table is checked indirectly by the native
integer outputs, not claimed bit-identical to all native table entries.

`bedrock_1_26_51_daylight_use.json` adds 12 accepted native player inversions
at frozen time 6001, both starting variants, strengths 0/7/15 and both actual
input parities. Inversion publishes the complementary state and then immediately
calls the new variant's signal update, even though actor sampling is frozen.
The replay applies the activation callback at the recorded SDK before/after
sample boundary and compares every source frame and the event result. This does
not yet validate the kernel's full network/player interaction pipeline or its
intermediate viewer publications.

The initially missing live relighting path is addressed by the next batch below.
Other open cases include additional weather transition levels/fog, arbitrary-time
interactions, full face/conduction and support matrices, sound/game events,
replacement/load/unload/restart and lightning-rod timing/conduction. This row
is not complete.

### Live lighting and producer registration

`bedrock_1_26_51_daylight_roofs.json` records 24 native live-world traces,
80 frames each, with normal/inverted sensors and adjacent dust: add/remove a
five-by-five stone/glass/leaves roof at X=4/15/16/-1 and both setup parities.
Both the positive and negative chunk edges are exercised. All **3,840 detector,
wire and sky samples** now match, without prelit terrain or injected light.

World block/liquid/native-state/structure mutations now queue cells whose
filtering or emission changed. A light query or the transaction boundary removes
dependent old light and floods from surviving neighbours/current emitters. The
two channels are updated on the owner in ready terrain only; changes do not
generate neighbouring chunks. Changed columns invalidate their cached network
payloads. A no-load regression checks immediate reads after both `SetBlock` and
`BuildStructure`. That test also exposed an existing exclusive-bound error in
the structure chunk loops: a one-cell structure at (15,64,15) loaded three
untouched adjacent chunks. The loops now end at the last occupied cell.

`bedrock_1_26_51_live_light.json` adds 16 sealed-corridor traces, 72 frames each,
with one/two glowstone sources, independent removal/reinsertion and clearing both
sources. Air, glass, leaves and an opaque wall are tested at both input parities.
The replay compares transmitted block light and absence of skylight at every
frame. The SDK's opaque-source brightness getter is not equated with stored
intrinsic source light. Native leaves transmit 14 from a neighbouring strength-15
source: the kernel incorrectly subtracted `filter+1`. Both initial spreading
and live updates now use `max(filter,1)`.

Live roof traces then exposed a producer-registration error, separate from light:
an initially zero detector installed before an even circuit step powered dust
two ticks too early. `DaylightDetectorBlock::updateSignalStrength` looks up the
active component map at `0x977fae5..0x977fb26`; it does not update pending setup
objects. The block actor must repeat this lookup even if its visible signal
property is unchanged. The engine now retains separate active/pending producer
strengths and applies registration after actor callbacks each game tick, before
the shared capacitor evaluation. Fresh setup values remain independently visible
to the source graph.

This lifecycle was checked by **ten native heap checkpoints** in
`bedrock_1_26_51_daylight_components.json`, using two starting signals and both
setup parities. Immediately after setup the new producer is pending. On the
first following tick the block is 15 while its active producer can still be zero;
on the second tick both are 15. On identifier replacement the old component
remains marked removed until collection. Read-only access targets only the
temporary child BDS. An owned Script API callback holds after each marker; map
descriptors are verified before/after reading. No process memory writes,
injection, suspension or existing world are used. The kernel replay compares
visible signal, component strength and active/pending status at every checkpoint.

The daylight interaction's native circuit-lock guard (`0x977f3a6`, `0x977f5ba`)
retains the producer while switching block identifiers. It now has an explicit
transaction operation instead of ordinary replacement setup. The 12 native
player-use traces were recaptured with adjacent dust and now check wire strength
and publication timing as well as the block/event result.

An additional even-step initialization matrix in
`bedrock_1_26_51_daylight_initial.json` replays all 48 initial cells in all three
dimensions (2,880 further source frames). Its driver first requires a sentinel
detector to change from strength one to the expected noon output in each of the
12 native chunks. Terrain availability alone did not prove actor ticking: an
earlier startup capture contained partially promoted chunks and failed the
synchronous-world replay. That observation is retained in
`.tmp/daylight-phase-native.json` / `.tmp/daylight-initial-all-dimensions-go.log`;
it is not used to invent sensor-specific startup delays. Fresh chunk promotion
remains a separate lifecycle audit item. The retained matrix explicitly targets
already ticking chunks.

Open lighting scope includes other filters and liquid states, previously
unavailable neighbours/load/unload/restart, large edits and broader timing,
performance and viewer behavior. These traces do not close the entire lighting
engine or the audit's persistence/graph rows. The following batch addresses
lightning-rod pulses without closing their entire lifecycle.

### Lightning-rod pulses and real bolt integration

`redstone_bds_lightning_probe.py` drives actual `minecraft:lightning_bolt`
entities in an isolated original BDS. The retained
`bedrock_1_26_51_lightning.json` contains **100 scenarios / 3,600 frames**,
each comparing the rod permutation, adjacent dust and a separate dust output
through the solid support. It records actual bolt IDs/positions and controls
both setup parities. Cases include all six facings, all eight material/wax
variants pointing up, retriggers after three ticks on every facing, additional
seven/eight/nine-tick retriggers, setup-frame/next-frame strikes for every
material, and integer strike heights at/above the rod on every facing.

The client hit callbacks cancel an existing queued expiry before scheduling
eight ticks from the latest hit. The kernel previously kept the first queued
tick, truncating a retriggered pulse. An unwaxed native rod loses exactly one
oxidation stage per hit; the former entity handler cleared all stages and could
leave an expiry queued for the old block identifier. Oxidation now belongs to
the rod hit callback, expiry uses the resulting identifier, and the producer is
retained across replacement under the equivalent native circuit guard.

Visible `powered_bit` and circuit strength are separate native state. A bolt
spawned in the setup frame can set the bit while its active-map strength lookup
misses the not-yet-registered producer. Both dust outputs then remain zero for
the whole visible pulse. The same strike one frame later produces output.
The kernel now uses the producer state rather than reading the bit as strength;
the replay preserves these zero-output traces instead of rejecting them as
failed positive controls. Every trace requires a real bolt, and the established
late direct-hit controls require powered dust.

`LightningBolt::normalTick` invokes the hit callback at `floor(position)`;
subtracting an epsilon changed the target at integer Y. Natural native rod
strikes use the rod's integer position, not the block above. Copper cleaning
starts below the bolt or on the struck rod's opposite-facing support. It may
invoke another rod's hit callback, including when the bolt is just above that
rod. Those callbacks now power/retrigger rods rather than treating them only as
an oxidation-state mutation. The height-boundary traces exercise this actual
entity path, not a direct call to `LightningRod.Strike`.

`redstone_native_lightning_probe.py` retains inspected span hashes, virtual
dispatch entries and six facing bounds in
`bedrock_1_26_51_lightning_methods.json`. The existing EndRod model already
matches those bounds for all 96 registered rod states. The hit event is corrected
from a centered, axis-dependent spark to native event 2033 at the integer block
position with payload zero. Event evidence here is instruction inspection;
network packet capture and client rendering remain unverified.

The initial 38-case matrix reproduced 18 failures before the fix (12 retrigger
and six oxidation/expiry cases). All expanded 100 traces now match, including
the separate direct/support dust timelines. This is **not** complete lightning
equivalence: remaining work includes powered authored/reloaded states, replacement
and chunk/restart behavior, copper-neighbour/random-walk matrices, projectile
hits, fire/damage/visual-only behavior and events. The following search batch
addresses the attraction algorithm; native-world climate/exposure and lifecycle
still require further verification.

### Natural lightning attraction: cylinder and candidate selection

`redstone_native_lightning_search_probe.py` executes the original client bodies
for the one-bit paletted cylinder query (`0x5502750`), lightning-rod group filter
(`0x32f9750`), candidate distance sort (`0x2d9d600`) and
`LevelChunk::findExposedLightningRod` (`0x32cbd00`). It maps these unmodified
bodies at their image-relative addresses in an isolated Python process, with
the image hash checked. Palette snapshots, chunk enumeration/height, rain and
effective biome temperatures are **supplied callbacks**. Byte movement uses the
host C runtime; native comparisons, partitioning, heap and selection execute
unchanged. This is component/control-flow evidence, not a BDS natural-storm
capture, native climate/exposure implementation or actual chunk promotion.

`bedrock_1_26_51_lightning_search.json` retains 670 cases: 405 cylinder boundary
inputs, 216 material/eligibility/fallback combinations, a clear-weather case
and 48 multi-candidate cases with 2/8/32/33/64/129 candidates. The native
selector always requests radius 64 and vertical half-height 64. Both boundaries
are inclusive, with horizontal squared distance at most 4096; accepted candidates
are ordered by full three-dimensional squared distance. The kernel formerly
used a radius-128 sphere. It also selected the nearest POI **before** checking
exposure, so one covered nearest rod prevented a farther eligible rod from
attracting the bolt. It now filters the loaded cylinder and walks candidates
until exposure, precipitation and the native binary32 temperature threshold
allow one. Actual loaded block identifiers are checked against all eight rod
identifiers; stale indexed positions do not load terrain.

Gather order follows loaded chunk X, chunk Z, subchunk Y and packed X/Z/Y
order. Equal-distance ordering cannot be replaced by lexicographic position
or a different unstable sort: the inspected MSVC sort performs different tie
rotations above its 32-element insertion-sort threshold. Its median selection,
partition rotations and heap fallback are reproduced. The fixture adds 120
direct calls to the native sorter: 0..2048 entries, sorted/reversed/equal/mixed
and full-width unsigned keys, both ordinary budgets and a forced heap budget.
The replay compares **every candidate and distance**, not only the selected
rod, and every tag in the direct-sort outputs. All match.

The world replay supplies climate through registered test biomes. Its altitude
adapter reproduces the effective temperature provided to the native callback;
it does not establish equivalence of the world's temperature getter. Native
chunk/precipitation callbacks likewise supply the intended exposure/availability.
An additional internal regression covers an unloaded rod still present in the
POI index and proves that querying it neither selects it nor loads its chunk.
Before the correction, 432 of the 670 selection replays fail. The next batch
adds real-terrain observations; the synthetic callback coverage is not presented
as a complete precipitation, temperature or chunk-lifecycle implementation.

### Shared rod selector in original BDS terrain and skeleton traps

The inspected `SkeletonHorseTrapGoal::tick` calls the same selector at
`0xcca2418` that natural chunk weather calls at `0x32c8f2e`. This provides a
controlled, known search origin without waiting for random thunder. The
`redstone_bds_lightning_search_probe.py` pack places blocks, creates an actual
trap horse and lets a regular loopback player activate its native AI. **The
pack never spawns a lightning entity.** Native spawn events record its result.
Rain enables selection while preventing unrelated random thunder bolts. A
fresh-server clear-weather control runs first after 160 settling ticks; setting
clear after rain without settling initially retained native rain fade and was
not treated as a settled clear-weather observation.

`bedrock_1_26_51_lightning_traps.json` retains **26 completed native traps**,
each with exactly one observed bolt, the source horse position, actual block
permutations and rod samples. Cases cover no rods, one exposed rod, a covered
nearest/farther exposed pair, horizontal offsets 64 and 65, glass, leaves, both
daylight variants, carpet, slab, all 13 wooden fence types and nether-brick fence.
The kernel replay triggers its own actual trap AI with a player, then checks
the resulting bolt position and rod pulse occurrence. This does not assert
exact per-tick trap animation, entity movement, networking or pulse publication;
the separate 100 direct-bolt traces cover the tested pulse timelines.

The trap's visual bolt previously bypassed attraction completely. It now uses
the shared `Tx.RedirectLightning` operation, while natural weather continues
to use the same search helper. Original-BDS terrain also exposed why checking
only solid model faces was insufficient: fences, daylight detectors and leaves
obstruct rain. `LevelChunk::getTopRainBlockPos` consults the native precipitation
component; behavior values one/two obstruct it independently of physical faces.
The explicit `RainObstructor` property now provides this behavior for those
types, and the lightning exposure scan uses only loaded terrain. All 26 bolt
positions and pulse-presence results match, including every tested fence type.
BDS retains redundant legacy properties such as `wood_type` and `color` beside
split identifiers; the fixture adapter matches every canonical kernel property
and allows only extra native aliases in the input.

Remaining work includes actual random-storm scheduling/sampling, broader native
climate/rain-height and precipitation-component coverage for other occluders,
rain transition levels, uniform/fluid-layer and broader palette/lifecycle cases,
chunk promotion/replacement/restart, copper cleaning, projectile hits, visual-only
damage/fire/events and the wider lightning lifecycle. Other block types still
use the existing face-based fallback and are not claimed to have complete native
precipitation semantics.

### Repeater side locking

Adamant treated every nonzero direct side input as a lock. The client capacitor
accepts only repeaters and comparators on the sides. In six isolated BDS
scenarios, a rear-powered repeater remained on beside a redstone block, powered
wire, a lit torch, or a parallel repeater pointing away. A repeater or comparator
pointing into its side locked it off. The source-type filter now matches this
behavior without changing comparator side-input rules. Unloaded neighboring
chunks are not loaded by the new filter.

`server/block/testdata/bedrock_1_26_51_repeater_lock.json` retains native commands
and results; `TestRepeaterSideLockMatchesNativeCapture` reproduced the three
incorrect locks before the fix and now passes all six cases. Locking an already
powered repeater and mid-pulse locking/unlocking are undergoing world-trace
verification below.

### Sculk vibrations and block-actor ordering

`scripts/redstone_bds_sculk_probe.py` uses an actual loopback player to operate
a native lever; it never injects a vibration or directly activates a sensor.
The 36 retained scenarios use both sensor types, distances 1/4/7/8/9/15/16/17,
both requested interaction parities and four additional on/off toggles. Both
actual accepted input parities occur. All three dust outputs, the comparator
identifier/lit bit and the sensor phase/strength are compared every game tick.
Normal sensors accept distance 8 but reject 9; calibrated sensors accept 16
but reject 17. Active phases last 30/10 ticks and both cooldowns last 10 ticks.
Lever activation supplies comparator frequency 10; deactivation supplies 9.

The kernel's lever callback previously generated no game event, so the actual
player interaction produced no sensor response. It now emits the corresponding
block-activate/deactivate event with the player and affected lever state. A
north-facing calibrated sensor previously disabled its north output; native
setup at `0x3198700` instead disables the south input side. The output guard
now follows the opposite facing, matching the existing input-side filter.

The remaining apparent two-tick comparator discrepancy was not a fixed delay.
`LevelChunk::tickBlockEntities` copies its insertion-order actor vector and
shuffles that copy every game tick using a forward Fisher-Yates loop with
unsigned 32-bit draw modulo `i+1` (`0x32cb7a6..0x32cb839`). Native actor storage
appends at insertion and preserves surviving order when erasing. The kernel
previously rebuilt a cached Go-map order when actor data changed, and sampled
all comparators after all other actors on every tick. It now preserves actor
membership order, shuffles each chunk separately every tick, and runs the
comparator in that actor pass only on its sampling interval. Decoded persisted
actor lists retain their input order. Generation still supplies a map whose
initial fallback order is sorted; native generation/load order and global
random-stream equivalence remain unproven. Cross-chunk snapshot/callback order,
the complete native actor roster and random-block-tick interleaving also remain
outside this component proof.

The native pass also rechecks the saved actor component pointer before each
callback (`0x32cb952..0x32cb985`). A replacement at the same position must not
run the new actor through the old snapshot. Versioned kernel actor references
now skip removed/replaced actors while preserving ordinary data updates;
replacement actors enter the next pass. Removal, replacement and data-update
regressions exercise this distinction without changing the native baseline.

`scripts/redstone_native_actor_order_probe.py` executes the untouched client
shuffle instructions in isolated Python, with an authored ABI wrapper and
supplied RNG draws. All 130 cases match, including all 24 four-actor orders,
large 128/257-actor vectors, high-bit random draws, draw consumption and whole
record movement. This component probe does not execute a native world.

`scripts/redstone_bds_sculk_component_probe.py` additionally reads its own
unmodified BDS at 44 Script API checkpoints. The callback briefly holds its
own work; Python reads component maps without injection, suspension or writes
and checks their descriptors before/after. Two arrival samples already contain
frequency 10; two still contain 0 and remain unchanged through the next tick,
then become 10 on the following sampling tick. This confirms the sampling gate
and both possible actor orders independently of downstream output timing.

The 36 original black-box traces do not record BDS's random stream. Their
replay therefore enumerates only the two possible actor orders at each
vibration arrival, requiring one coherent replay to match the entire recorded
trace exactly. It does not allow per-frame timing tolerances. The additional
four heap traces instead use the independently observed analog value to select
the arrival order before replay, with no choice search. Their complete sensor,
comparator and dust traces also match.

The environment capture adds 24 calibrated-input cases with four facings and
stable producer strengths 0/9/10/15 behind the sensor, plus front-side controls.
A native daylight detector frozen at day time 6001 supplies each input; the
pack checks its actual power before operating the lever. Zero accepts both
uses, 9 accepts only deactivation, 10 accepts only activation, and 15 rejects
both. A producer on the front side does not select frequency. Every sensor and
wire frame matches, including all four rotations of the disabled output side.

Another 48 cases cover both sensors with every registered wool colour and
eight controls: air, stone, glass, white/red carpet, moss carpet, a snow layer
and oak leaves. In this straight, four-block path, all sixteen wool blocks
occlude the vibration; the eight controls transmit it. This matches the kernel
without changing the obstruction logic. It does not prove diagonal/corner rays,
partial shapes at other ray heights, source-block dampening or entity events.

Eight additional actual-player traces expose the same missing event on buttons.
`ButtonBlock::buttonPressed` posts block activation with its activating actor;
`_buttonUnpressed` posts deactivation without an actor. Both kernel paths now
emit those events. Wooden projectile activation carries the detected projectile
as its source, following the inspected call path; broader projectile/vibration
integration remains open. No event is added when an already pressed button
rejects another use or an embedded projectile keeps it held.

These combined source/sensor traces also expose the block queue clock being
advanced before the entity pass. Native `_addToNextTickQueue` adds delay to
queue `mCurrentTick` at `+0x8`; `tickPendingTicks` updates that field when its
own pass starts. Advancing the kernel block queue at the start of the whole
world tick made a player action before that pass expire one tick late. The
player activation now uses the last processed block queue clock through
`Tx.WithBlockInteraction`, while contacts and lightning retain the current
simulation clock. Applying the previous clock globally also changed those
already captured paths and was rejected by their regressions. The replay
invokes real player use from an entity-pass controller, preserving the
separate current event clock and input queue clock. It changes neither the scheduled delay nor the
recorded release frame to make the test pass. Wooden/stone button expiry is
observed at click+29/+19 in these captures; vibration arrival is click+4.
The shorter calibrated cycle also hears wooden release at click+33, while
stone release occurs during cooldown and is ignored. Fluid queue phase,
per-chunk queue lifetime, item placement and complete session/network action
scheduling remain open.

`scripts/redstone_bds_sculk_distance_probe.py` adds 24 accepted native lever
uses at twelve diagonal positions for both sensor types. It records all 3,167
source/sensor/wire frames, including negative positions and outside-radius
controls. Before the correction, eight traces fail on sensor strength: at
offset `(1,0,1)`, for example, the normal sensor produces 13 instead of native
14, and the calibrated sensor 14 instead of native 15.

The original `VibrationSelector::addCandidate` floors the event coordinates,
computes cell-to-cell Euclidean distance, truncates that distance to an integer
and stores it as binary32. Both same-tick selection and the arriving sensor use
that stored integer distance. The old kernel selected by continuous distance
and recalculated an unrounded cell distance on arrival. It now shares the native
integer distance across selection, travel and reception. Different Euclidean
distances within the same integer shell therefore compete by event frequency;
the first equal-frequency event is retained. Reception preserves a loaded
vibration's stored distance rather than calculating it again.

`scripts/redstone_native_vibration_selector_probe.py` executes the unmodified
selector with no source actor: 74 fractional/negative/translated coordinate
cases and six multi-event sequences, including rejection of a later collection
tick. The coordinate cases include all three axes near cell boundaries and
large positive/negative coordinates: twelve cases distinguish floor-after-
binary32-conversion from flooring the original binary64 value. Event and entity
listener cell conversion now follows that order. Only CRT `floorf` is supplied.
It also executes the original sensor receive
function in 110 distance/radius cases, including adjacent binary32 values around
strength boundaries, with an inactive check and activation callback supplied.
The core strength calculation now follows the inspected binary32 operations.

Range admission is a separate native calculation: binary32 point distance
squared is truncated before comparison to the listener radius squared and the
dispatcher limit 256. The original dispatcher arithmetic span executes in 92
fractional/translated/boundary cases through an authored ABI wrapper, with only
the virtual radius query supplied and exits at the original accept/reject
continuations. Kernel vibration admission now follows this gate. This is a
component execution, not execution of the complete dispatcher or a native world;
fractional entity-source delivery and its source-specific event origin still
need independent world-level captures. The diagonal BDS tests alone cannot prove
fractional range behavior. No game machine code is stored in the repository.

The `adjacent` suite of `redstone_bds_sculk_environment_probe.py` adds fifteen
two-use traces (3,481 source/sensor/wire frames). A source at `(0,65,0)` directly
adjoins the sensor at `(1,65,0)`; all four calibrated orientations are tested
with a lever, wooden button and stone button, plus normal-sensor controls.
At the calibrated back-input orientation, the lever's activation is rejected
and its deactivation accepted, whereas each button's activation is accepted
and its deactivation rejected. The other orientations provide transmitting
controls. The old kernel fails exactly the two back-input button traces.

Native button press/release changes the visible block, posts the game event
and only then calls `BlockRedstoneProducerComponent::setRedstoneSignal` on the
active circuit component. The event's calibrated filter therefore reads the
old electrical strength despite the new pressed bit. Buttons now implement the
shared independent producer interface, initialized from their permutation, and
update active strength after their event. This fixes that ordering without
reordering the visible block update or changing the lever's observed behavior.
Broader producer startup and lifecycle remain open; these settled traces alone
do not prove them.

`scripts/redstone_bds_sculk_contact_probe.py` adds 68 plate scenarios: all
seventeen registered types, frequency filters 9/10 and both absolute contact
parities. Every case spawns one native armour stand, then adds another after
35 ticks (ten more for the heavy plate), then removes them after 70 ticks.
The added load changes weighted strength while the calibrated sensor is idle,
so an erroneous extra activation would be observable. Native
`checkPressed` dispatches only when old/new strength crosses zero; positive
weighted changes do not vibrate. The kernel now emits the matching activation
and deactivation after the visible state change. Contact activation carries
the caller's actor; scheduled release has no actor, matching the inspected
arguments at `0x2a50e50`.

The same probe adds twenty connected-hook scenarios with pigs, armour stands,
items, arrows and XP orbs, each at both contact parities and both frequency
filters. Cells are 34 blocks apart to isolate their listeners. Native physics
and actual entity bounds are recorded on every frame; natural native merging
or removal is retained, not replaced by a custom entity implementation. The
replay preserves one handle per recorded native actor and feeds its actual
next-phase geometry into real kernel contact callbacks. It checks source actor
presence as well as every plate/string/hook/input/sensor/output/wire permutation
and sensor strength. The 14,280 plate and 4,200 hook frames match. The Script
API's redundant legacy sensor `direction` is validated against actual facing;
the native palette projection retains `minecraft:cardinal_direction`.

`TripWireHookBlock::_emitState` broadcasts its sound, then posts a game event
without an actor or affected state. Electrical transitions take priority over
attachment changes. The kernel's shared hook state emitter now produces
activate/deactivate/attach/detach with that priority and order. The existing
opposite-hook-before-self publication and sound hash selection are retained.
`scripts/redstone_native_hook_events_probe.py` executes the entire untouched
function in 64 cases: all sixteen old/new flag combinations, visible old/new
block permutations and two positive/negative positions. Block/level lookup,
sound broadcast and game-event delivery are supplied callbacks; event globals
are initialized to the inspected enum IDs. This is component execution, not
native world or circuit execution. The Go replay verifies callback order,
sound name, binary32 sound point, current visible state hash, event identity,
event position and absent source actor. Together with the actual contact traces,
this proves the tested event paths without claiming complete hook lifecycle
or full listener equivalence.

`scripts/redstone_bds_plate_context_probe.py` drives each of the seventeen plate
types while an actual local player client receives the resulting sound packets.
It records 2,567 frames after an explicitly checked settled start, including an
adjacent calibrated sensor facing east with the plate on its back input, a
frequency-10 control sensor four blocks away, both output wires and a native
comparator diagnosing the adjacent sensor's heard frequency. Input is applied
only after the settled observation window starts; capture begins at setup+60,
contact at setup+80/81 and continues through setup+210. This tests settled
contact/release behavior, not startup or the omitted settling interval.

For every material, the adjacent listener rejects the press while the plate
is powered and hears release with frequency 9 after its strength returns to
zero. Its native comparator independently observes that frequency. The existing
kernel electrical update order already matches every recorded sensor phase,
strength and wire frame. The comparator is a frequency diagnostic here; its
own shuffled-actor output transients are not replayed by this fixture.

All seventeen replays initially fail because plate sounds are absent. Native
packets use `pressure_plate.click_on` / `pressure_plate.click_off`, positioned
at `(x+0.5,y+0.1,z+0.5)`, with the resulting plate permutation's network hash.
The kernel now emits these named events before its vibration on each zero/
nonzero transition. Changing a positive weighted strength produces neither
another click nor another activation. All 34 captured packets match event name,
state hash, entity type and binary32 sound position. The raw packet controls
are non-baby, relative volume enabled, entity unique ID -1 and no entity type;
the existing session defaults agree with them. This does not prove wider audio
material/support, player-mode, cancellation or lifecycle paths.

Still needed here: source flags, broader calibrated-input/producer startup and
lifecycle; hook attachment/detachment/cutting world integration and broader
lifecycle. Fractional entity-damage captures can use
the dispatch in `Mob::hurtEffects` at `0x207a80c`; its actual source origin and
ordinary entity physics must be retained by that probe.

Still open: other event sources/frequencies and filter strengths, all strong/
weak output/material paths, fractional positions, broader occlusion and
listener selection/rejection, waterlogging, resonators, shriekers, producer
startup, phase replacement and native save/load/unload behavior.
The existing Java-derived listener/actor persistence is not treated as proof
of Bedrock equivalence. None of the 22 audit areas is declared complete here.

### Native vibration obstruction and coordinate precision

The old ray path expanded its endpoints by `1e-7` and accumulated double-precision
boundary fractions, following the Java baseline. Its six source offsets were
`0.00001`. The client uses a **0.001** binary32 offset, read from `0xe69fdc0`,
and floors/centres both positions before casting the whole calculation to
binary32. At sufficiently large coordinates an offset rounds back to the
original coordinate. The old kernel retained a double-precision displacement
and could therefore hear a vibration that BDS rejects at a diagonal wool seam.

`BlockSource::clip` at `0x2d8aaa0` recomputes the next boundary fraction from
the current binary32 point after every step; ties choose Z, then Y, then X.
It does not expand the endpoints and stops after at most 200 transitions.
Precision-induced repeated cells near the world boundary are retained in the
executed-client fixture, including their native termination. The wool predicate
at `0x25f12d0` reads the block type's `0x134` flag byte, bit `0x10`.

The ray/AABB body at `0x2d95aa0` checks face intersections rather than treating
every visited cell as a hit. It uses binary32 relative coordinates, an inclusive
segment/face test and a squared parallel threshold of binary32 `1e-7`. A short
segment entirely inside one wool cube crosses no face and returns no hit. The
kernel's unconditional tagged-cell hit incorrectly blocked that case. The new
`vibration_ray.go` ports the tested traversal and boolean unit-cube intersection,
with the native arithmetic rounding and bound; it queries only already loaded
blocks. Default vibration-occluding blocks are the full wool cubes.

`scripts/redstone_native_vibration_occlusion_probe.py` executes the untouched
complete clip, predicate and ray/AABB instruction bodies. Block lookup and unit
wool geometry are supplied callbacks; border lookup reports an ordinary interior
cell, no actor/liquid is supplied and compiler cookie validation is bypassed.
The vibration centring/offset arithmetic is inspected/transcribed, not executed
as a fabricated complete listener. The fixture records image/function hashes
and that distinction explicitly; no extracted instructions are stored here.
`bedrock_1_26_51_vibration_occlusion.json` contains 208 obstruction cases,
133 complete cell sequences and 202 direct unit-cube intersections, including
near-parallel thresholds, short/inside/tangent rays, negative positions, exact
integer endpoints, distant coordinates and the 200-step limit. The high-coordinate
unit shapes in this component fixture are supplied models; native world geometry
at those extreme positions is not claimed as tested by the fixture.

`scripts/redstone_bds_sculk_occlusion_geometry_probe.py` independently runs the
original BDS with 168 cases and 35,280 source/listener/input/wire frames. Native
armour stands operate stone plates, while calibrated sensors accept only
frequency 10. Their actual identities/poses/AABBs are retained for the contact
replay. Cases cover four diagonal directions, anchors 0 / 128 / 256 / -257 /
1,000,000 / -1,000,000 and seven masks: open, either side of a seam, both sides,
central wool, stone and carpet. Open/stone/carpet cases must hear; central wool
must block; each source and its fixed filter have independent positive controls.
The captured barriers are checked unchanged. The kernel replay also validates
the SDK's redundant wool/carpet colour alias against the actual block colour.

After fixing that harness alias, the pre-fix replay fails eight large-coordinate
seam cases: the kernel activates with strength 11 while the native listener stays
idle. The initial executed-client comparison additionally exposes endpoint,
traversal, short-inside and bounded-work differences. The final replay checks
the full recorded frames and all supplied native component cases. Broader dynamic
world occlusion, fractional actor-source integration, custom models, waterlogging,
resonance, shriekers and persistence remain open.

### Repeater pulse pipeline and shared circuit clock

The old scheduled-tick callback re-read the rear input when its delay elapsed.
If the pulse had already fallen, it never switched on. Every repeater also
started its own timer on an arbitrary neighbor update. Bedrock instead caches
inputs on a shared two-game-tick circuit clock and retains queued transitions.
Its four-stage pipeline stretches accepted short pulses to the configured
delay and distinguishes a frozen side lock from internally held pulse stages.

`repeaterCircuit` implements the inspected checkLock/cacheValues/evaluate rules.
Its outputs match the 16 fixed executed-client traces and all 12,288 changing
delay/input/lock steps. Input caching and pure evaluation happen before any
output is published. Block permutations are published in the following chunk
update phase. The phase separation is necessary: shifting an output by one
game tick gives visibly different results from the reference captures.

`scripts/redstone_bds_timing_probe.py` creates a stable Script API behavior pack
inside the temporary server, with only `@minecraft/server` allowed. It runs
commands and reads permutations once per game tick; BDS supplies all redstone
logic. No native binary or circuit code is patched. Its 48 completed scenarios
cover four delays, input widths 1/2/3/4/8/16 game ticks, both absolute tick
parities, and two repeaters in a chain. Every sampled repeater permutation now
matches Adamant. A one-game-tick source pulse on the nonsampling phase is
discarded by BDS; on the sampling phase, it is stretched to 2/4/6/8 ticks.

The world owns transient circuit states, keeps them across output/delay
permutations, removes them with their chunks/components, and installs them
from saved/generated chunk palettes and structure placement. Pending outputs
merge with the live permutation, preserve a later delay change, and cannot
restore a removed/replaced/rotated component. Publication honors cancellation
of the redstone update event. These integration paths have Go regression
checks; they do not prove native save/reload equivalence for in-flight pulses.

`scripts/redstone_bds_repeater_state_probe.py` captures a further 40 world
traces: five lock/unlock patterns, delay changes on rising/falling transitions,
and three repeating pulse trains, each at all four delay settings. All 40 traces
now match. `scripts/redstone_bds_repeater_delay_probe.py` additionally verifies
eight delay-change traces using actual `/setblock` commands rather than the
Script API permutation setter; these match as well.

### Repeater delay setup versus interaction

Changing `repeater_delay` on the same powered/unpowered block identifier does
not immediately replace the capacitor's insertion index in BDS. The next
identifier transition runs component setup and picks up the new delay. This
behavior occurs with both Script API permutation changes and `/setblock`.
For example, changing delay 4 to 1 during an already queued falling transition
retains that transition's previous delay. Adamant previously read the current
block delay for every evaluator call, so it applied the new index too early.

The insertion index is now retained in the capacitor state. Identifier changes
refresh the native setup state; ordinary permutation changes preserve it.
Player interaction is different: client `RepeaterBlock::use` at `0x995d630`
calls `updateDelay(..., true)` directly at `0x995d6c7`. Adamant's `Activate` now
explicitly refreshes the index without discarding the pipeline. Regression
checks cover both rising/falling activation and the command/native world
captures. Native machine-code traces separately cover arbitrary immediate
insertion-index changes.

Placement into an already powered circuit, native restart phase,
unload/reload of in-flight pulses, source graph boundaries and ordering across
chunks also remain open.

### Observer notifications, electrical output and removal

Notifications now run during the notifying transaction, using an explicit
loaded-neighbor listener. The old deferred callback could execute a tick later,
after the observer's off tick, and turn an ignored mid-pulse change into a second
pulse. Pulse ticks now align with the shared circuit step: native input changes
take two or three game ticks according to parity, with a two-tick pulse width.
All 38 front/side/rapid-change scenarios match, in all six orientations.

The visible `powered_bit` is not the electrical output. PulseCapacitor state now
retains pending strength, evaluates it on the circuit step, and publishes it in
the following chunk pass. Native captures show an adjacent lamp switching one
game tick after the bit, and dust/repeaters three ticks after it. All 28
orientation/phase/output traces match. The 256 executed client pulse cases also
verify strength transfer and clearing the pending input.

Native `ObserverBlock::onRemove` calls `BlockTickingQueue::remove`. Adamant kept
the old callback, so replacing an observer at the same position could pulse the
new observer. Removal now cancels queued and already-collected callbacks of
that type. Same-observer permutation changes retain callbacks, matching native
rotation. All 12 replacement/removal/rotation captures pass. The process-global
pending-observer map is removed; the world's actual queue supplies pending state.

### Observer piston movement

An observer arriving after a native push or sticky-piston pull starts a pulse,
regardless of its observed face. Adamant restored the block without its native
`movedByPiston` callback. Piston completion now invokes that callback and starts
the observer's visible/electrical pulse.

Twelve native scenarios cover six observer orientations and ordinary/sticky
pistons. Regressions align the completion callback to each native arrival and
compare 18 push/pull pulse windows. They do not prove piston start/progress
timing, which remains a separate open item; full movement traces are retained.

### Dust transport and lamp consumer phases

Dust used to change on every immediate graph invalidation. Native transporters
cache a flattened, attenuated source list and publish after the circuit step.
Dust now has that transient circuit state. The compiled network supplies all
wire inputs together, including nonadjacent vertical links; querying just six
faces lost power on the existing glowstone ladder. Input cause metadata survives
publication, so delayed wire changes caused by a torch do not accidentally
become external updates that reset its burnout.

Lamps evaluate after source capacitors. Merely delaying dust still lit lamps
from ordinary sources too early. Late consumer sampling now reads this step's
evaluated source strengths, while dust/repeater input snapshots retain the
previous source step. Lamp changes publish in the next chunk pass; losing power
retains the four-game-tick off delay and renewed power cancels it. Publication
events use consumer input strengths instead of a nonexistent lamp output.

All 48 native four-wire-network traces match: redstone-block/observer/repeater
sources, lamp/repeater endpoints, widths 1/2/3/8, both parities. Every sampled
source, dust strength and endpoint permutation is checked. The 256 executed
transporter cases and existing glowstone-ladder regressions also pass. This is
not proof for every dust shape, conductor material or feedback graph.

### Torch circuit clock, burnout and neighbour notifications

Torches now use the translated native capacitor as world-owned transient
state. Private scheduled inversion timers, root-cause feedback heuristics and
the old eight-turn-offs/60-game-tick rolling window have been removed. Input
snapshots distinguish a real own source from independent producers, retain
zero-strength structural feedback, and stop at directional capacitors. Cache
and evaluate run on the shared two-game-tick circuit clock; changes publish
in the next chunk pass. Native half-pulses publish after ordinary components
in that pass, as `CircuitSystem::updateBlocks` does.

The native self-input path counts circuit steps through the 16/33 thresholds.
`RedstoneTorchBlock::neighborChanged` at `0x995c7f0` checks `mCanReignite` before
clearing the counter and that flag, then runs ordinary support handling.
Adamant now delivers this notification synchronously, including notifications
from other circuit components. A change during a fresh oscillation does not
prematurely reset its counter. Unchanged BDS `/setblock` commands are no-ops;
trace replay preserves that command behavior instead of forcing a neighbour
notification for an unchanged permutation.

`TestTorchWorldTimingMatchesNativeCapture` replays **all 34 complete native
world traces**, 6,840 frames, including the initial oscillations, burnout,
independent repeater input trains, ordinary neighbour changes, loop-wire
removal and reconnection. All sampled torch/repeater identifiers and wire
strengths match. This replaces the earlier limited settled-blackout checks.
The new reignition fixture adds changes at offsets 32/48/64/66/96, at both
setup parities; it verifies the eligibility boundary and prevents an early
or repeated counter reset. The source captures are:

- `bedrock_1_26_51_torch.json`: six initial loop and input/control traces.
- `bedrock_1_26_51_torch_recovery.json`: 18 loop, recovery and strong-input
  traces. The above/below stone commands are intentionally idempotent controls,
  not evidence about vertical replacement behavior.
- `bedrock_1_26_51_torch_reignite.json`: ten eligibility-boundary traces.

The old kernel also falsely burned out under fast independent repeater input:
four strong-input traces had 64-79 differing block samples. They now match
completely. A stone change beside the support or an adjacent wire no longer
relights a burned-out loop, while a real adjacent notification after the
native eligibility flag does restart it. All initial loop timing differences
previously observed at offsets 2/3 are resolved in these 34 traces.

`scripts/redstone_native_torch_cache_probe.py` executes the unchanged, hashed
client cache body, preserving its RIP-relative virtual-call dispatch reference
in an isolated reservation. Only the dispatcher and strength/type/group getters
are synthetic. `torchCircuit` matches all 2,240 cache/evaluate cases, including
following-torch counter writes, and all 1,536 separately captured evaluator
cases. The world state uses this algorithm; synthetic source-list/dependency
rings still do **not** prove native graph construction or dependency discovery.
The common-support dependency path is now connected to world-owned torch
states. `updateDependencies` at `0x66d9cd0` and `PushCircularReference` at
`0x66dadc0` construct a north/west/south/east ring when the support has dust
above and the candidate torches have powered components above them. The cache
pass can write a following member's counter, so those snapshots are installed
together before evaluation.

All 36 ascending-chain/return-wire scenarios in
`bedrock_1_26_51_torch_network.json` match every sampled block and signal over
4,032 frames. The original 16 common-support cases and 20 translated cases
now also match **every frame**, including initial and post-input phase.
`TestTorchClusterExactPhaseMatchesNativeCapture` is enabled again; its former
skip is no longer a passing-suite limitation.

The remaining phase mismatch came from using a coordinate sort in place of
`CircuitSystem::evaluate`'s active-chunk hash-map traversal and component
vectors. The world now registers static producers as well as capacitors,
drains a separate native-order pending-add map, and traverses the resulting
chunk vectors. The BlockPos hash combines three little-endian 32-bit FNV-1a
hashes; bucket collisions, growth, erasure and retained empty chunk entries
affect traversal. No coordinate-specific phase adjustment is used.

`scripts/redstone_bds_circuit_order_probe.py` reads these lists from its own
unmodified temporary BDS process. The inspected client's 70-byte torch
evaluator is byte-identical at BDS RVA `0x4bd91c0`; the vtable at `0xa851830`
is independently checked against its strength getter. ReadProcessMemory uses
read/query access only. Component counters are asynchronous diagnostics, not
tick-atomic observations. The stable Script API supplies the tick traces.
Scoreboard handshakes hold each finished geometry until inspection finishes;
optional 500 ms script checkpoints expose the pending map before graph update.

The two new complete fixtures contain 20 placement/history cases and six
table-growth cases. Together they provide 24 independently observed pending
queues and 26 final chunk/vector layouts. The stress cases add/remove up to
160 unpowered levers, exercising pending-table growth from 8 to 64 to 512
buckets, active-chunk growth from 8 to 64 buckets, and reuse after removal.
`TestCircuitTraversalMatchesNativeCapture` reconstructs both queues and final
vectors from placements; `TestTorchClusterNativeTraversalExplainsPhase`
checks the capacitor algorithm against the observed traversal. The full world
replay independently reproduces all 2,080 frames without injecting native
order into the engine.

All 62 common-support traces now match 5,472 frames exactly. Replay preserves
the native sequence of setups in one world, including older circuits outside
the next fill region. Starting each case in a fresh world had incorrectly
discarded the chunk/vector history needed to compare phase. Broader dependency
graphs, dependency removal/replacement, native publication traversal for other
groups and native unload/reload ordering still require investigation.

The 60 native scenarios in `bedrock_1_26_51_torch_faces.json` compare five
mounting faces against every output face, both direct lamp activation and
transmission through one stone block. All settled kernel states match. They
confirm attachment-face exclusion and upward-only strong output in these
geometries, not every conductor material or initial output-consumer timing.

Saved callbacks from the old implementation now only invalidate the live
capacitor. `TestTorchLegacyCallbacksUseLiveCircuitState` verifies that a stale
receiver cannot immediately invert a new orientation, start a private tick
clock, or restore a removed torch. Counter lifetime now follows the circuit
map, including removal and chunk cleanup. Obsolete synthetic tests that forced
the old rolling-window counter were replaced by native algorithm/world-trace
checks; the real world-scheduler burnout/recovery regressions remain.

The transition sound follows the disassembled block callback's counter/state
condition and is emitted only after an uncancelled publication. Native sound
packet/order verification is still open. Broader player-placement interactions,
full source/material/attenuation matrices, broader rotation/replacement cases, multi-torch dependencies, native
unload/reload/restart phase and broader cancellation behavior also remain open.
This section is **not** a claim of complete torch parity.

### Torch initialization on a powered support

The kernel's `UseOnBlock` previously inverted attachment power immediately,
placing an unlit torch on an already-powered support. Client
`TorchBlock::getPlacementBlock` starts from the item's default permutation and
changes its facing. `RedstoneTorchBlock::setupRedstoneComponent` initializes
the capacitor's on state from the actual placed block variant at
`0x995c16e`, without an immediate attachment-power inversion. Placement now
starts lit and lets the shared circuit step perform inversion.

`bedrock_1_26_51_torch_placement.json` contains 60 complete native cases: all
five mounting faces, both setup parities, initially on/off inputs, and three
arrangements (direct redstone-block attachment, stone strongly powered by its
attached lever, and the negative control of a redstone block behind stone).
Sources settle before the torch is created, then change at offsets 12/24.
The reference validates the expected steady torch state before those changes.
All 60 immediate placement snapshots and all 2,400 later frames match through
the kernel's actual `UseOnBlock` path, including facing, support/source state
and the lamp above the torch. A torch on a powered support remains visibly
lit for 1-2 game ticks, depending on the shared phase; the lamp correctly
does not flash from that initial visible state.

The original 40-case replay reproduced ten immediate-placement mismatches
before the change; it passed afterward, as does the expanded 60-case replay.
The redstone-block-behind-stone negative controls already matched and do not
represent an additional conduction fix. These observations combine client
disassembly with BDS command-created permutations: no native player packet
exchange, client prediction, permission check or placement cancellation is
claimed as tested by this fixture. The probe records and verifies the client
code hashes without redistributing or executing those placement bodies.

### Torch direction and explicit identifier replacement

A bare `/setblock` facing-property change preserves the native capacitor's
configured direction even while the visible block points elsewhere.
`setupRedstoneComponent` returns early for an existing live component; the
configured direction is a separate component field, written by
`BaseCircuitComponent::setDirection` at `0x66d7700`. The kernel previously
reconstructed its capacitor from the visible facing during cache, changing
both the input connection and directional output prematurely.

Torch state now retains its configured direction separately. Electrical input,
source-face filtering and dependency-ring discovery use that direction;
support checks and visible permutations keep using the block's actual facing.
An external torch identifier replacement reruns initialization, including when
its visible facing has not changed. The engine marks its own publication
internally, so a normal lit/unlit transition preserves direction and burnout
state instead of behaving like an external replacement. Other component types
keep their existing setup/refresh policy, including repeater delay callbacks.

`bedrock_1_26_51_torch_rotation.json` captures 24 scenarios, all 1,536 frames
checked exactly. They compare a property edit, an identifier change, and an
air/remove-and-replace sequence at both setup parities and input polarities.
They also replace the identifier after an earlier rotation without changing
the visible facing, change independent lever inputs, and remove/recreate the
torch later in the trace. Half the cases use a lamp as the new support, which
checks directional output as well as the old/new input connection. The lamp
above the torch is checked in every case.

The original twelve-case replay reproduced four direction mismatches before
the change. A subsequent same-facing identifier refresh reproduced another
four mismatches until internal publication and external replacement were
distinguished. Both are resolved, and the expanded 24-case output matrix
passes. Multi-component registration order after explicit replacements,
pending-publication edits, burnout-loop rotations, native persistence and
broader cancellation still require separate native evidence.

The obsolete `RecordTurnOff`, `MarkSelfTriggered` and `ConsumeSelfTriggered`
transaction helpers were removed with the incompatible counter model.
`Torch(...).BurnoutStatus()` and explicit `ClearBurnout()` now access the actual
world-owned capacitor rather than a separate history map.

## Coverage and remaining work

Every row requires native-code evidence plus suitable executed scenarios before
it can be marked verified. No row is complete merely because its Go tests pass.

| Area | Current status / next evidence required |
|---|---|
| Circuit cadence and ordering | Shared clock and capacitor traversal installed; registration/bucket growth/chunk vectors verified in 26 heap/world cases. Per-chunk actor shuffle matches 130 executed client cases; actor identity rechecks, insertion order and inline chunk callbacks corrected. Native raw MT stream, bounded draws, snow-coordinate budget and player-area raster/LFSR traversal, staggered phases and tested circular bounds are verified below. Open: complete publication traversal, native load/generation order, remaining whole-world RNG consumers, standalone area grouping, other groups, startup/restart, simultaneous changes, priorities and sound |
| Source/consumer/conductor graph | 480 switch output/conduction cases match tested direct and one-stone-block paths. Open: broader weak/strong activation, other sources/materials, face filtering, nonconductors, attenuation, removal and independent circuits |
| Redstone wire | Transport phases and 48 basic network traces verified. Open: horizontal shape, directional output, native vertical/material matrices, other component connections, feedback, cancellation and reload |
| Repeaters | Side-source filter, pulse pipeline and delay setup corrected; fixed/random evaluator traces and 96 world timing/lock/train/delay traces match. Open: powered placement, native load/pulse persistence, source graph and cross-chunk ordering |
| Comparators | Arithmetic, independent lit/variant state, side-source filter, tested rear analog precedence, configured mode/direction, pending replacement output and shared clock corrected; 8,704 arithmetic cases, 78 settled source/priority cases, 60 pulse traces, 32 mode traces, 40 rotation traces and 48 publication-edit traces match. Open: complete source/material graph, all analog blocks, broader interaction/property/publication lifecycle, output-change events and block actor persistence |
| Redstone torches | Native counter, shared clock and registration traversal installed; 34 one-torch traces, 36 ascending networks, 62 common-support traces, 60 placement traces, 24 rotation/replacement traces, 60 settled face/conduction cases and 3,776 algorithm cases match. Immediate placement inversion and premature direction refresh fixed. Open: broader player interactions, full source/material/attenuation graph, replacement registration order in larger graphs, dependency lifecycle, native sound/publication order, restart/unload/reload and broader cancellation |
| Observers | 38 face/phase/retrigger traces, 28 output traces, 12 lifecycle traces and 18 movement callback windows match. Open: notification-only changes, larger chains, powered placement/load, unload/reload and complete piston timing |
| Pistons and sticky pistons | Scene consumer and actor transition phases replace immediate neighbour movement. 432 direct/weak input and pulse traces, 576 retrigger traces, 144 strong conducted-input traces, 96 linear limit/obsidian/filled-chest traces and 528 adhesive branch traces match. Native positional spawn sorting is executed in 334 component cases. Eight actor/consumer heap traces expose and correct premature serialized NewState. Another 96 vibration traces, sixteen actual packet traces and 384 interruption traces verify the scoped event, publication and orphan restoration changes below. Open: broader source/registration/property/load lifecycle, additional adhesive/support/notification/material contexts, other block actors and inventories, drops, partial geometry, moving entities, sounds/events/packets and persistence |
| Levers and buttons | 196 source states, 480 direct/conducted output cases, 180 actual-arrow traces and 66 real player-use/retrigger traces match. Missing wooden-button activation/holding, projectile collision/contact phase and switch sound events corrected; six native click packets verify three sources. Fifteen adjacent-sensor traces expose and correct button event-before-producer strength ordering. Another 64 player-charge and 16 actual breeze-charge grids verify activation, pulse release and the tested wall/range footprints; executed client ray/set functions independently check selection and order. Open: broader burst materials, mixed-device ordering/events/packets, trident world capture, multiple entities/boundaries, broader support/materials, full sound material/mount matrix, producer startup, replacement and reload |
| Pressure plates and tripwire | Plate conversion/query fixes verified in 302 native traces and five executed scalar methods. Tripwire initialization, contact bounds, 40-string limit, disarming/recheck and neighbour callback scope corrected; 140 contact + 80 line traces match. Artificial carpet collision removed against an executed native getter. Another 58 actual player-cut traces and 230 hook sound packets match. Missing plate/hook vibrations corrected against 68 all-type plate and 20 hook contact/sensor traces; 64 executed hook-helper cases verify event priority, actor absence, sound order and visible-state hash. Seventeen additional settled all-material traces and 34 actual player-client packets verify added plate click sounds and tested adjacent calibrated-input ordering. Hook producer setup, supported rotations and replacements now match 136 additional full traces. Sixteen actual-player hook placements expose and remove the duplicate line scan; sixteen native attachment packets are compared separately from cleanup sounds. Open: player modes/actor states, complete boundary/transition/contact integration, broader producer startup/load/order, previously powered/disarmed cutting, gaps/crossings/support/materials, wider audio/lifecycle events and persistence |
| Daylight detectors and lightning rods | Detector clock, environment, initial/live roof lighting, body and producer registration corrected; 48,960 dimension/initialization source frames, 28,800 weather samples, 24 live roof traces, 16 source-removal/overlap traces, ten producer heap checkpoints and 12 player inversions with wire timing match. Rod hit/expiry/retrigger/oxidation/producer and tested support output match 100 real-bolt traces; 96 rod shapes match inspected bounds. Attraction geometry/order/selection matches 670 executed client cases with synthetic world inputs plus 120 native sort/heap cases; 26 actual BDS traps verify shared selector integration, bolt positions and pulse occurrence through tested rain occluders. Open: other lighting filters/liquid states and lifecycle, partial weather/fog, wider detector face/conduction/support/events/persistence; random weather scheduling, broader precipitation/climate/occluders, copper/projectile/interaction/events and lifecycle |
| Sculk sensors/calibrated sensors/shriekers | Missing lever/button/plate/hook events, calibrated disabled-output face, integer distance/selection/reception, binary32 range/cell conversion and button event/producer ordering corrected. 568 controlled traces (four include heap readback) cover tested phases/strengths, diagonal positions, adjacent switches and plates, all plate types, hook contacts, filters in four facings, all 16 wool colours and eight material controls. Original client selector/strength/range gates execute in 80/110/92 cases and hook sound/event dispatch in 64 cases. Native binary32 ray offsets, traversal and unit-cube intersections replace the Java obstruction path; 208 executed obstruction cases, 133 full cell sequences, 202 direct intersections and 168 additional BDS seam/material/coordinate traces match. Open: other event/filter/occlusion/output paths, fractional entity-source integration, waterlogging, resonance, shriekers, startup, network action phase and persistence |
| Lamps and copper bulbs | Lamp source/consumer phase corrected in observer/wire matrices. Bulb comparator output verified lit/unlit for all eight variants. Open: broader lamp retrigger/cancellation/load cases, bulb toggle timing/interaction and persistence |
| Doors/trapdoors/fence gates | 192 native consumer pulse traces, 192 dust/stone/glass signal traces and 192 independent redstone packet traces cover the tested lower/upper/split inputs and command edits. Another 176 material and 192 separate-input wind traces check the attack gate. Wire regressions retain 3512 redstone and 168 wind interaction packets, normal/split flushing. Native upper-door bits are retained. Open: broader analog/conduction, actual player placement/use while powered, exact network delivery tick, full material/geometry/event matrices, persistence and lifecycle |
| Powered/detector/activator rails | Detector inventory strength, command-cart eligibility/container, typed body selection and producer state corrected against 87 settled/lifecycle and 32 source/wire/lamp timing cases; query geometry matches 324 executed client cases. Powered/activator cadence, configured axis, tested source overlap, straight/slope range, connection placement and zero-strength activator publication corrected against 256 BDS scenes. Cart height, binary32 motion, container drag and detector chunk ownership match 320 native motion scenes; stationary rider/TNT/hopper activator effects match 24 further scenes. Open: full source-list construction/order, loops, broader detector and producer lifecycle, multi-cart order, chunk persistence, cart curves/derailment/collisions, command-cart activation, broader moving-cart transfers and activation lifecycle |
| Dispensers/droppers/crafters | Nine-slot contract, consumer publication, tested pulse timing and cart origin/newborn phase corrected. Native reservoir selection matches 2,048 executed cases; dropper's additional container pass matches 20 owned RNG traces. Crafter routing matches 102 hopper scenes, pulse/animation timing 36 scenes, sided output/remainders 240 scenes and saved animation 12 scenes. Ordinary/crafter ejection matches 36 exact-input motion traces and 1,440 executed velocities; ordinary effects match 96 real-packet scenes. Initial flags match 32 container load scenes. Open: broader source graphs/queues, other item/recipe behaviours, actor lifecycle and full world unload/restart persistence |
| Hoppers | Settled bookshelf transfers verified in 48 cases with two barrel controls. Early lock publication corrected against 144 lock-pulse traces. Transfer clock/counter units match 110 actual push traces and 25 native heap checkpoints; recursive chain transfers match 60 scenes across placement orders/chunk boundaries. Block-item pickup matches 142 scenes including holes, simultaneous stacks and covers; native query/type gates and seven-box hopper geometry are checked separately. Slot selection matches 72 generic push/pull, 78 sided furnace and 40 brewing scenes; twenty persisted-counter loads verify the tested decode/clock path. Tested hopper-cart collection and transfer effects match 48 further scenes. Open: broader lock graph/lifecycle, larger hopper graphs and complete world RNG consumption, other container transfers/sides, moving cart interactions and reload |
| TNT/note blocks/bells | TNT consumer timing matches 72 direct/conducted scenes; 12 wire updates verify state aliases. Note clock/instruments/effects match 112 event, 46 packet and 12 load scenes; the two unsealed-water dependencies now match alongside 12 activation controls. Bell clock/toggle/animation match 64 redstone and 16 load scenes; 64 player-use and 64 arrow-hit scenes verify direction gates, actor fields and wire sequences. Another 128 support scenes and 416 packets match with normal/split/backlogged publication. Player wind charges and actual breeze-fired charges add 128 actor scenes plus 128 independent packet scenes with 640 packets, including ignored repeated hits and the tested adjacent-support bursts. Sixteen executed shapes and 28 exact binary32 arrow flights check shared geometry/movement. Open: broader sources/lifecycle, TNT motion/fuse/chains/unstable/underwater behavior, note tuning/vibrations and wider persistence, wider bell support/placement eligibility, broader wind-burst geometry/occlusion/order, other attack/hearing/vibration behavior and chunk lifecycle |
| Command/structure blocks | Open: trigger/cycle timing, modes, conditionals, powered-state persistence and cross-chunk execution |
| Analog-output blocks | 77 settled scalar cases, 81 inventory cases and nine pot cases verify the tested paths; campfire's unsupported override removed. Another 267 book/record/shelf states, 50 hopper-transfer results and 792 frame cases match; lectern spread arithmetic, seven disc values and directional frame reads corrected. Open: broader transitions/through-block reads/materials, additional container variants/init/loot, player book/shelf/frame interactions, jukebox lifecycle, lectern pulse/packet validation, frame maps/support variants, nondefault statue world poses, shelves, sensors, command output and creaking hearts |
| Persistence and lifecycle | Open: pending ticks/phase memory, state replacement at the same position, chunk unload/reload, world teardown and viewer updates |
| Event cancellation and scheduling safety | Open: cancelled mutations/actions versus native baseline, bounded work, no accidental chunk loads, loops and simultaneous multi-component changes |

## Verification log

- Initial four native world scenarios reproduced two nonadjacent piston inputs
  and the subtract/equal comparator lit mismatch in the unchanged kernel.
- After the first fixes, those world scenarios, the executed-client comparator
  arithmetic cases, and existing piston regressions passed together.
- The completed native piston matrix covers all six facings: 36 adjacent input
  cases and 12 nonadjacent-above input cases. Together with the comparator case,
  all 49 scenarios pass against the kernel.
- The six native repeater lock cases and all 8,704 comparator cases pass.
- `go test ./server/block ./server/world -skip '^TestBlockActorDataNBTBytes$'
  -timeout=180s` passed after the side-lock fix. The skipped existing test is
  unrelated and has a nondeterministic NBT map-order assertion.
- All 96 world-tick repeater traces and 12,288 changing-input evaluator steps
  pass. Lifecycle regressions cover saved-chunk component installation,
  removal/replacement/rotation before publication, delay preservation,
  cancellation/retry and explicit player-interaction refresh.
- The wider `go test ./... -skip '^TestBlockActorDataNBTBytes$' -timeout=240s`
  run passed every package except `server/entity`, where
  `TestVillagersRunFromZombies` failed its random panic-behavior check. That
  exact test then passed 10 isolated runs. The full-suite result remains a
  failure; this audit does not claim a clean global run.
- A later block/world run passed the block package, but the world package
  timed out in `TestWeakExecDoesNotBlockOwnerWhenQueueFull`: the stack shows
  the test blocked filling the transaction queue at `task_test.go:215` while
  its owner was waiting for the test's `proceed` channel. That exact test
  passed three isolated runs. This timeout is retained as a verification
  limitation, rather than reported as a passing suite.
- `go build ./...` and `git diff --check` passed after the circuit changes.
- Further regressions pass for observer timing, movement callbacks, lifecycle,
  electrical outputs, wire networks, and 512 executed simple-capacitor cases.
  A block/world run passed with the two previously identified unstable checks
  excluded. Torch world captures inform the next stage; exact torch equivalence
  is not yet established.
- The scheduled-tick cancellation batch regression passes: cancelling a
  callback already collected for this tick does not execute it or discard a
  subsequently rescheduled callback, and an unrelated callback still runs.
- The torch recovery matrix reproduced two incorrect recovery locations and
  one parity-dependent feedback case. Targeted torch checks and the updated
  block/world suite pass (same two unstable tests excluded). Only settled
  burnout windows are claimed here; full torch parity remains unverified.
- Full strong-input replay then reproduced false burnout from independent
  pulse trains. After source-identity classification, all six full traces
  match. The cache/evaluate port passes 3,776 executed-client cases; all 60
  mounting/output/conduction scenarios also pass. The block/world suite
  (same two unstable tests excluded), build and diff checks pass after these
  changes. Persistent native counter and full oscillator integration remain
  unfinished.
- The native torch counter is now installed in the world scheduler. All
  34 full traces and the legacy-callback regression pass. The 60 output-face
  cases and 3,776 algorithm records also pass; older settled-window claims
  above are superseded by full replay. The real scheduler burnout/recovery
  regressions remain, while synthetic rolling-window injection tests were
  removed with that model. Multi-torch graph discovery and native lifecycle
  equivalence remain unproven.
- The subsequent wide run with the same two unstable checks excluded passed
  every package except `server/entity`: `TestHappyGhastsFloatAround` reported
  a random-flight height of 64.960066, below its 65-block threshold. Its setup
  has stone ground and a happy ghast, with no redstone components. The exact
  test passed 10 isolated repetitions afterward. The wide run is still a
  **failure**, not a clean global test result; the log is retained at
  `.tmp/redstone-go-test-native-counter.log`. No entity AI changes were made
  as part of this redstone audit.
- After adding common-support dependencies, the block/world suite passed
  (same two unstable checks excluded): block 15.542 s, world 11.453 s, recorded
  in `.tmp/redstone-block-world.log`. This includes all 36 exact ascending
  network traces and 36 cycle-shape checks. The skipped exact cluster-phase
  gate remains unfinished; neither this result nor those scoped cycle checks
  prove native component traversal equivalence.
- The subsequent native traversal port removes the exact-phase skip. All
  original 36 cluster traces and 26 new heap/world traces now pass strict
  frame-by-frame checks. The 26 registration cases also match all observed
  queues/chunk vectors, including both table-growth paths. The targeted
  cluster/traversal run passed block (3.887 s) and world (1.547 s), recorded
  at `.tmp/torch-traversal-growth.log`. The earlier skipped-gate limitation
  above is superseded by these checks, not applied to the broader open matrix.
- The block/world suite after installing traversal passed with the same two
  unstable checks excluded (13.019 s / 13.943 s); build and diff checks also
  passed. The later growth checks above cover the additional stress fixture.
- The wider run after traversal/growth passed block (19.125 s), world
  (24.902 s) and the remaining packages except `server` and `server/entity`.
  `TestContainerRemoveViewerPanicDoesNotBlockServerShutdown` exceeded its
  shutdown deadline, and `TestPigsWanderWithoutCarrotsOnSticks` observed a
  3.55-block random walk in the rider's direction. The exact teardown test
  then passed three isolated repetitions (14.607 s), and the exact pig test
  passed ten (2.979 s). Their setups concern session teardown and pig AI;
  no changes to those systems were made here. Isolated success does not turn
  the failed wide run into a clean result. Logs are retained at
  `.tmp/redstone-go-test-native-traversal.log`,
  `.tmp/redstone-server-teardown-isolated.log` and
  `.tmp/redstone-pig-wander-isolated.log`.
- Torch placement replay passes all 60 immediate snapshots and 2,400 later
  frames (2.956 s). The following block/world run passed with the same two
  unstable checks excluded (16.974 s / 11.925 s), and build/diff checks passed.
  Logs: `.tmp/torch-placement-extended-test.log` and
  `.tmp/redstone-placement-block-world.log`.
- After separating configured torch direction and internal publication from
  external setup, targeted torch/repeater/observer/traversal checks passed
  (block 7.722 s, world 1.553 s). The block/world suite with the same exclusions
  then passed (12.084 s / 11.820 s), with a successful build/diff check. The
  additional 24-case directional-output replay passed afterward (2.600 s).
  Logs: `.tmp/torch-rotation-refresh-after.log`,
  `.tmp/redstone-torch-direction-block-world.log` and
  `.tmp/torch-rotation-output-test.log`. The last wide-run failures recorded
  above remain failures; no new clean global run is claimed here.
- After the comparator source/priority/clock corrections, all comparator
  replays passed together (3.974 s). The block/world suite passed with the
  same two unstable tests excluded (17.705 s / 12.920 s). Logs:
  `.tmp/comparator-analog-clock.log` and
  `.tmp/redstone-comparator-block-world.log`. Earlier wide-run failures
  remain recorded above; this scoped run is not a clean global result.
- The subsequent mode/rotation/state port passes all comparator and original
  redstone-world checks together (5.201 s), including the 40-case expanded
  rotation fixture, activation refresh and publication cancellation/retry.
  The original subtract/equal test now actually advances the world with
  its captured rear and repeater source rather than invoking an obsolete
  private scheduled callback on an already-zero output. Log:
  `.tmp/comparator-rotation-expanded.log`. The preceding block/world run
  passed world but failed the new cancellation test because its cake lacked
  support; the corrected setup passes the targeted replay. This does not
  retroactively turn that broader run into a pass.
- The final wider run after the comparator lifecycle changes passed every
  package with the same two previously unstable tests excluded:
  `go test ./... -skip '^(TestBlockActorDataNBTBytes|TestWeakExecDoesNotBlockOwnerWhenQueueFull)$' -count=1 -timeout=240s`.
  Representative package times were block 38.716 s, world 16.759 s, entity
  129.968 s and Overworld generation 165.992 s. Build and diff checks also
  passed. Log: `.tmp/redstone-go-test-comparator-lifecycle.log`. This is a
  passing run with explicit exclusions, not an unqualified all-tests pass;
  the earlier failures remain part of the history above.
- Overall redstone parity has **not** yet been established. Work was paused
  and published at the user's request on 2026-10-25, then resumed on 2026-10-26.
  The coverage matrix above remains the full audit checklist.
- Before publication, the committed server was launched with `go run .` in
  an isolated temporary worktree. A loopback RakNet listener used an ephemeral
  port, world/player saving and resource building were disabled, and the
  console `stop` command closed the server with exit code zero. Existing worlds
  and server configuration were not used. Log: `.tmp/go-run-server-smoke.log`.
- On resuming, four pending-publication failures were reproduced and fixed;
  the targeted comparator/torch/repeater/observer run passed (8.643 s).
  The scalar analog matrix then reproduced 26 failures; after the analog
  corrections, targeted scalar/comparator/torch checks passed (10.286 s).
  The block/world suite with the same two exclusions then passed after also
  removing the unsupported campfire override (11.324 s / 10.245 s).
  `go build ./...` and `git diff --check` also passed after these changes.
  Logs: `.tmp/comparator-publication-before.log`,
  `.tmp/comparator-publication-after.log`, `.tmp/analog-blocks-before.log`,
  `.tmp/analog-blocks-after.log`, `.tmp/redstone-analog-publication-block-world.log`.
- The subsequent inventory/pot replay passes all 90 cases (targeted combined
  inventory/scalar/comparator run 5.174 s). The baseline with populated
  inventories reproduced 24 nonempty failures in dispenser/dropper/pot;
  log `.tmp/inventory-signal-filled-before.log`, followed by
  `.tmp/inventory-signal-after.log`. The wider run then passed every package
  with the same two known exclusions and `-count=1 -timeout=240s`: block
  13.794 s, world 13.137 s, server 18.378 s, entity 70.314 s and Overworld
  generation 80.690 s. Build/diff checks passed as well. Logs:
  `.tmp/redstone-go-test-inventory.log` and `.tmp/redstone-inventory-build.log`.
- The book/record baseline reproduced 156 failures (14 jukebox, 142 lectern)
  in 3.752 s; all 267 combined cases pass after the fixes. The targeted
  book/record/inventory/scalar/bookshelf run passed (4.511 s), followed by
  the 50 settled bookshelf/barrel hopper cases (2.841 s). Block/world then
  passed with the same two explicit exclusions (17.096 s / 11.302 s), and
  `go build ./...`, Python compilation and diff checks passed. Logs:
  `.tmp/book-record-before.log`, `.tmp/book-record-after.log`,
  `.tmp/bookshelf-hopper-go.log`, `.tmp/redstone-book-record-block-world.log`
  and `.tmp/redstone-book-record-build.log`. Reference world logs are
  `adamant-redstone-bds-dt5lelj9` and `adamant-redstone-bds-u45kib1a` in the
  temporary directory; full paths and BDS hashes are in each fixture.
- The initial 198-frame-case replay reproduced 160 incorrect directional
  reads before the fix (3.311 s). After passing the query direction, combined
  frame/book/inventory/scalar/comparator checks passed (7.520 s). The expanded
  four-direction frame matrix then passed all 792 cases (6.064 s). Block/world
  passed afterward with the same two explicit exclusions (21.435 s / 11.505 s),
  as did `go build ./...`, Python compilation and diff checks. Logs:
  `.tmp/frame-signal-before.log`, `.tmp/frame-signal-after.log`,
  `.tmp/frame-signal-cardinal-go.log`, `.tmp/redstone-frames-block-world.log`
  and `.tmp/redstone-frames-build.log`. The final native reference log is
  `adamant-redstone-bds-nag1vr7w`; the fixture retains its full temporary path.
- Switch source states pass all 196 full 80-frame traces (3.608 s), and the
  output/conduction replay covers 480 cases. The initial 48 projectile cases
  pass with recorded native body poses (2.682 s). Real-arrow integration
  first reproduced 24 one-tick-early contact failures (2.698 s); moving
  button contact before projectile movement fixes those traces (3.213 s).
  Logs: `.tmp/switch-state-go.log`, `.tmp/button-projectile-phased-go.log`,
  `.tmp/arrow-button-integration.log`, `.tmp/arrow-button-integration-phased.log`.
- The final expanded projectile fixture covers all 15 materials in 180
  cases. Temporarily restoring the original button code reproduces 156
  failures in each replay layer; the diagnostic backs up the working file,
  reads the baseline from Git, and restores the fix in `finally`.
  Log: `.tmp/button-projectile-original-expanded-before.log`. The final
  combined source/geometry/projectile/impact-plane/stair-ray checks pass
  (block 8.180 s, entity 2.758 s). Log:
  `.tmp/redstone-switch-projectile-targeted.log`.
- After the button/contact/trace changes, `go test ./...` passed every
  package with the same two known exclusions and `-count=1 -timeout=240s`:
  block 26.684 s, world 14.008 s, server 23.198 s, entity 78.629 s and Overworld
  generation 99.261 s. Build, Python compilation and diff checks passed.
  Logs: `.tmp/redstone-go-test-switch-projectiles.log` and
  `.tmp/redstone-switch-projectiles-build.log`. The three native world logs
  are `adamant-redstone-bds-wax18vqd`, `adamant-redstone-bds-ruki29rw` and
  `adamant-redstone-bds-ggfn007p`; fixtures record their full temporary paths.

- Real player-use replay passes all 66 cases (2.941 s). The sound baseline
  fails for all three sources (2.651 s); sound packet replay passes (2.575 s).
  Logs: `.tmp/switch-use-clock-go.log`, `.tmp/switch-sound-before.log` and
  `.tmp/switch-sound-go.log`. Reference roots are
  `adamant-redstone-bds-3bpfhgoo` and `adamant-redstone-bds-_11v3sve`.
- The following full `go test ./...` run with the same two known exclusions
  passed block (26.139 s), world (14.204 s), session (5.370 s) and the other
  packages except entity: `TestOcelotsHuntChickens` failed its random hunt
  assertion. Five isolated repeats passed afterward. The full run remains
  a failure; no extra exclusion was added. Logs:
  `.tmp/redstone-go-test-switch-use.log` and `.tmp/switch-use-ocelot-recheck.log`.

- The initial 170-case plate replay reproduced 120 failing cases (3.569 s).
  Correcting binary publication/power, actor selection and AABB queries passes
  those cases (3.430 s). The expanded replay exposed a typed native query's
  chunk-edge restriction; retaining native coordinates and matching the
  disassembled chunk ranges passes all 302 traces plus executed scalar methods
  (5.730 s). Logs: `.tmp/pressure-plate-before.log`,
  `.tmp/pressure-plate-after.log` and `.tmp/pressure-plate-expanded-go.log`.
  The final native reference root is `adamant-redstone-bds-1rf4o8a2`.

- After the plate fixes, the block/world/entity/session run passed block
  (50.422 s), world (19.792 s) and session (3.362 s), but entity failed
  `TestAnimalsPanicWhenHurt/minecraft:sniffer` (122.034 s). Five isolated
  sniffer repeats subsequently passed. Both broad-run failures remain recorded;
  no random-behavior test was silently excluded. `go build ./...`, Python
  compilation and diff checks passed. Logs:
  `.tmp/redstone-pressure-plate-packages.log`,
  `.tmp/pressure-plate-sniffer-recheck.log` and
  `.tmp/redstone-pressure-plate-build.log`.

- Restoring original tripwire/hook sources reproduces 186 failing cases across
  the final 220 native traces (5.029 s); the diagnostic restores the working
  files in `finally`. The initial replay with only 60 contacts exposed property
  initialization and item/orb-origin failures; a later expanded line replay
  also corrected an erroneous direction mapping in the test harness before
  using it as evidence. Logs: `.tmp/tripwire-original-before.log` and
  `.tmp/tripwire-contacts-before.log`.
- The 140-contact/80-line replays, prior plate cases and existing tripwire
  checks then pass in the broader block/world/session run with the same two
  known exclusions: block 40.860 s, world 13.041 s, session 3.366 s.
  `go build ./...` and Python compilation passed. Logs:
  `.tmp/redstone-tripwire-packages.log` and `.tmp/redstone-tripwire-build.log`.
  Native roots are `adamant-redstone-bds-hpu03d83` and
  `adamant-redstone-bds-nznhdez9`; fixture metadata retains full paths.
- Executing the native empty collision getter then reproduced the artificial
  carpet collision in the unchanged model (2.870 s). Log:
  `.tmp/tripwire-collision-before.log`. This was a further production change
  after the package run above; subsequent targeted verification is recorded
  separately rather than retroactively claiming that run covered it.

- After removing the carpet collision, targeted block/entity checks passed
  (9.357 s / 3.313 s), including all 220 tripwire traces, the executed collision
  regression across all registered permutations, plate native regressions,
  actual arrow/button flight, stair-ray interception, item landing and insider
  paths. Log: `.tmp/tripwire-final-targeted.log`. Final build, Python compilation
  and diff checks passed; build log `.tmp/redstone-tripwire-final-build.log`.

- The real-player cutting baseline reproduced 46 failing cases out of 58
  (3.289 s). After adding the before-removal tool callback, all 58 pass
  (3.568 s). The sound check then reproduced zero kernel hook events versus
  230 native packets (3.491 s); after preserving native publication/sound order,
  the complete world-state and sound sequence replay passes (4.319 s).
  Logs: `.tmp/tripwire-break-expanded-before.log`, `.tmp/tripwire-break-after.log`,
  `.tmp/tripwire-sound-before.log`, `.tmp/tripwire-break-sounds-after.log`.
  Final native root: `adamant-redstone-bds-dhbahg82`. The client probe also retains
  inspected spans for `playerWillDestroy` and `_emitState` with image/span hashes;
  these callbacks were observed through BDS rather than executed in synthetic
  memory.

- After the cutting callback and hook sound changes, `go test ./...` passes
  every package with the same two explicitly known exclusions and
  `-count=1 -timeout=240s`: block 29.806 s, entity 73.128 s, player 7.470 s,
  session 3.226 s, world 13.181 s and Overworld generation 82.173 s.
  The ocelot/sniffer tests were not additionally excluded. Build, Python
  compilation and diff checks also pass. Logs:
  `.tmp/redstone-player-cut-full.log` and `.tmp/redstone-player-cut-build.log`.

- Before the 2026-10-26 publication, `go run .` was launched from the root of
  a separate worktree at `b9e5f6f9`, containing all current production changes.
  Its temporary configuration used loopback RakNet on an ephemeral port,
  disabled world/player saving and resource building, and did not access the
  user's worlds or configuration. All three dimensions opened; console `stop`
  closed the server and `go run` exited with code zero. No launcher change was
  needed. Log: `.tmp/go-run-publish-smoke.log`.

- Resuming the daylight audit added 48 dimension/time replays, 18 full-day
  weather segments, 12 player inversions and the constructor-bounds check.
  Temporarily restoring the original detector and two chunk-lighting sources
  reproduces 69 failing subcases (48 time/dimension, 18 full-day, three player
  cases), plus the old body mismatch, in 3.135 s. The diagnostic restores all
  working bytes in `finally`. Log: `.tmp/daylight-original-baseline.log`.
  The final targeted daylight run passes in 3.662 s:
  `.tmp/daylight-final-targeted.log`. Native roots are
  `adamant-redstone-bds-9ay5ffn2`, `adamant-redstone-bds-xkg59cc6` and
  `adamant-redstone-bds-ijmwvcai`; each fixture retains its full reference path.
- After the daylight and initial-lighting changes, `go test ./...` passes with
  the same two known exclusions and `-count=1 -timeout=240s`: block 32.157 s,
  entity 81.419 s, player 10.060 s, session 5.713 s, world 14.639 s and Overworld
  generation 100.159 s. No additional exclusions were added. `go build ./...`,
  Python compilation and diff checks also pass. Logs:
  `.tmp/redstone-daylight-full.log` and `.tmp/redstone-daylight-build.log`.
  The detector/body/initial-fill corrections are uncommitted; no new publication
  was requested after resuming. At this checkpoint live mutation relighting
  was still unresolved; its subsequent investigation is recorded below.

- Live roof traces initially fail in 20/24 cases (2.737 s): roof light was stale
  and newly sampled sources could power wires prematurely. After relighting and
  separate producer state, all 24 match. Disabling only the refresh step in a
  diagnostic then reproduces 32 failures across the final roof/corridor matrices
  (16 opaque/leaves roofs and all 16 corridor cases; 2.786 s). The diagnostic
  restores source bytes in `finally`. Logs: `.tmp/daylight-roof-before.log` and
  `.tmp/live-light-no-refresh-before.log`.
- The expanded targeted run passes (4.391 s), including the 17 dimension/initial
  setups, weather, player/wire, 24 roof and 16 corridor traces, ten native heap
  checkpoints, body geometry and no-neighbour-loading checks. Log:
  `.tmp/daylight-initial-ready-go.log`. New native roots are
  `adamant-redstone-bds-6kiawk5p` (roofs), `adamant-redstone-bds-d3ns6ix3`
  (source overlap), `adamant-redstone-bds-j6plw95u` (player/wire),
  `adamant-redstone-bds-6vshkdne` (heap) and `adamant-redstone-bds-21h_gpah`
  (ticking-controlled initialization).
- The first broad run after live relighting passes block (47.419 s), world
  (15.560 s), player (11.427 s), session (5.906 s) and Overworld generation
  (88.117 s), but entity fails `TestCamelCalvesFollowAdults` and
  `TestEndFightNativeLifecycleAndMCDBRestart` (209.180 s). Three isolated repeats
  of each subsequently pass (143.394 s). Logs:
  `.tmp/redstone-live-light-full.log` and `.tmp/live-light-entity-rechecks.log`.
- The subsequent full run, still using only the same two known exclusions,
  passes block (56.749 s), world (18.194 s), player (11.309 s), session (5.879 s)
  and generation (124.809 s), but entity fails `TestFishSwimAround` and
  `TestWitchThrowsPotionsAtPlayers` (242.066 s). Five isolated repeats of each
  then pass (11.035 s). Both full runs remain failures; no AI failure was
  silently excluded. Logs: `.tmp/redstone-live-light-final-full.log` and
  `.tmp/live-light-fish-witch-rechecks.log`. The readiness-controlled initial
  matrix was added afterward and is covered by the targeted run above.
- The small loaded-chunk source-replacement benchmark (ten iterations) measures
  1.181 ms and 588,026 allocated bytes per placement/query/removal/query pair on
  the local i7-12700H. It does not establish performance for large edits.
  Log: `.tmp/live-light-benchmark.log`.
- Final build, Python compilation and diff checks pass. The explicit
  ticking-readiness guard in the added initialization replay also passes
  (2.662 s). Logs: `.tmp/redstone-live-light-final-build.log` and
  `.tmp/daylight-initial-readiness-check.log`. The broad-run AI failures above
  remain unresolved as broad-run failures; the isolated repeats do not turn
  either full run into a pass. All changes in this batch remain uncommitted.

- The initial 38 real-bolt traces fail in 18 cases before the lightning fixes
  (twelve retriggers and six oxidation/expiry cases; entity package 3.576 s).
  Log: `.tmp/lightning-before.log`. The final expanded capture is complete:
  100 scenarios, 3,600 source frames and both direct/support wire outputs, from
  `adamant-redstone-bds-ehwnxtfc`, BDS 1.26.51.1 build 51061361. The first expanded
  replay passes (4.776 s), log `.tmp/lightning-expanded-go.log`.
- After the rod event adjustment, selected entity redstone/lightning tests pass
  three repetitions (20.626 s), log `.tmp/lightning-entity-redstone.log`. Block
  (56.056 s), world (13.080 s) and session (3.412 s) packages pass with the same
  two known exclusions `TestBlockActorDataNBTBytes` and
  `TestWeakExecDoesNotBlockOwnerWhenQueueFull`; no additional exclusions were
  added. Log: `.tmp/lightning-packages.log`. `go build ./...` passes, log
  `.tmp/lightning-build.log`. A subsequent inspection correction makes queued
  rod expiry clear producer strength even when its visible bit is already
  false; its final targeted replay/build results are recorded separately.
  This package selection is not a new complete-suite pass: the earlier broad
  AI failures above remain recorded. This batch is uncommitted.
- The final expiry callback correction passes the targeted rod/body/actual-bolt
  replay (block 2.738 s, entity 3.936 s) and `go build ./...`. Logs:
  `.tmp/lightning-final-targeted.log` and `.tmp/lightning-final-build.log`.
  Both new Python probes compile and `git diff --check` passes. No commit or
  push was made. The 22-area coverage matrix remains open.

- Rod search initially fails 432/670 selection cases (world package 5.852 s),
  log `.tmp/lightning-search-before.log`. Cylinder geometry, every candidate
  ordering entry, all 120 native direct-sort/heap cases and the 670 world
  selection cases then pass (6.134 s). The unloaded-index regression passes
  in the subsequent targeted run (6.595 s). Logs:
  `.tmp/lightning-search-order-go.log` and
  `.tmp/lightning-search-final-targeted.log`.
- Original-BDS trap capture finishes all 26 cases in native root
  `adamant-redstone-bds-swdwb8ki` with exactly one actual bolt per case.
  The earlier 12-case settled capture is `adamant-redstone-bds-yu5sji01`.
  Thunder was replaced by rain after an uncontrolled extra native bolt
  invalidated the first diagnostic; no such incomplete capture is used as
  the retained fixture. An intermediate replay exposed the remaining leaf
  obstruction failure (entity package 4.532 s), log
  `.tmp/lightning-trap-after.log`. After the explicit property, all 26 actual
  trap cases, 100 direct-bolt cases, the existing horse-trap integration check,
  670 selection/cylinder cases, 120 sort cases and unloaded-index check pass
  (entity 9.202 s, world 5.851 s), log
  `.tmp/lightning-search-trap-targeted.log`.
- The broad run with the same two known exclusions passes block (83.652 s),
  world (24.824 s), player (15.374 s), session (7.911 s), Overworld generation
  (179.041 s) and the other reported packages. Entity fails the goat subcase
  of `TestAnimalsPanicWhenHurt`, then the four-minute package limit expires
  while entering `TestTamedHorsesBreed` fixture generation (247.565 s).
  Log: `.tmp/lightning-search-full.log`. Five isolated goat repeats pass
  (15.477 s); three complete horse-breeding repeats pass (19.259 s), logs
  `.tmp/lightning-search-goat-recheck.log` and
  `.tmp/lightning-search-breeding-recheck.log`. The broad run remains a failure;
  no new exclusions or claim of unrelatedness is inferred from isolated passes.
- Final block/world/session packages pass with only the same two exclusions
  (71.008 / 18.450 / 3.595 s), log
  `.tmp/lightning-search-final-packages.log`. Selected entity lightning,
  trap/button/plate/tripwire checks pass three repetitions (57.586 s), log
  `.tmp/lightning-search-final-entity.log`. `go build ./...` passes, log
  `.tmp/lightning-search-final-build.log`. The subsequently expanded inspected
  method hashes preserve the shape replay, log
  `.tmp/lightning-search-final-metadata.log`. Python compilation and diff
  checks pass. No commit or push was made; the full audit remains incomplete.

- Initial actual-player sculk replay fails all four cases because lever use
  emits no vibration, log `.tmp/sculk-use-before.log`. After the lever event
  and calibrated output-face corrections, the expanded 36-case replay still
  fails eight comparator-output traces (player package 4.285 s), log
  `.tmp/sculk-expanded-before.log`. No dust/comparator assertion was removed
  to hide those failures.
- Original-BDS component capture completes four cases and 44 held-callback
  read-only checkpoints in `adamant-redstone-bds-lssldlz5`. The 36 unheld
  traces are from `adamant-redstone-bds-ohb2qi22`. After sampling cadence and
  actor-pass changes, all 40 complete traces pass (4.755 s), log
  `.tmp/sculk-order-replay.log`. The 36-trace replay accounts explicitly for
  unrecorded native actor random order; the four heap-based replays choose
  their order from the observed native analog sample before replay.
- Original client shuffle execution completes 130 cases, including all 24
  four-actor permutations. After adding saved-actor identity protection, its
  exact replay and three removal/replacement/data-update regressions pass
  together with the viewerless actor regression and all 40 sculk traces
  (world 2.547 s, player 4.894 s), log
  `.tmp/sculk-actor-final-targeted.log`. Native execution is repeated after
  explicit arena cleanup and again completes 130 cases, log
  `.tmp/actor-order-native-final.log`.
- Before the final actor-identity guard, block/world/player/session packages
  pass with the same two historical exclusions (51.847 / 19.040 / 11.516 /
  3.366 s), log `.tmp/sculk-order-packages.log`. Final `go build ./...`,
  compilation of the three new Python probes and diff checks pass, log
  `.tmp/sculk-actor-build.log`. Results of the broader final run are recorded
  separately below; these package results alone do not close the audit.
- The final broad `go test ./...` run passes with only the same historical
  exclusions `TestBlockActorDataNBTBytes` and
  `TestWeakExecDoesNotBlockOwnerWhenQueueFull`, a 300-second per-package limit
  and one repetition. Block passes in 67.798 s, entity in 303.267 s (reported
  package time includes initialization), player in 14.518 s, session in
  6.447 s, world in 22.295 s and Overworld generation in 147.778 s. Log:
  `.tmp/sculk-actor-full.log`. This is a successful selected broad run; it
  does not erase the earlier recorded failures or prove either excluded test.
  No commit/push was made, and the 22-area audit remains incomplete.

- The environment probe completes 24 filter, 48 obstruction and eight button
  cases: 18,670 additional per-tick source/sensor/wire frames. Native roots are
  `adamant-redstone-bds-tlmgdvaf`, `adamant-redstone-bds-nmtf9hfx` and
  `adamant-redstone-bds-iac7nru_`, respectively. Filter replay passes (3.761 s),
  log `.tmp/sculk-filter-before.log`; the material paths also match without
  an obstruction change.
- Initial button/sensor replay fails all eight cases because no vibration is
  emitted (2.293 s), log `.tmp/sculk-buttons-before.log`. Adding events reaches
  the exact native activation frame but exposes one-tick-late button expiry
  in all eight cases (3.117 s), log `.tmp/sculk-buttons-events.log`. Moving input
  into the entity pass alone still fails because the queue clock was advanced
  before that pass (6.264 s), log `.tmp/sculk-environment-after-events.log`.
  After the queue-local clock correction, all 120 full sculk traces pass
  together (8.423 s), log `.tmp/sculk-environment-block-clock.log`.
- The first pre-publication broad run with that global clock change fails
  `TestButtonProjectileSignalsMatchNativeCapture`,
  `TestPressurePlatesMatchNativeCapture`, `TestTripwireContactsMatchNativeCapture`,
  `TestArrowButtonSignalsMatchNativeCapture` and
  `TestLightningRodsMatchNativeCapture`. Logs:
  `.tmp/redstone-publish-final-tests.log` (block 61.492 s, entity 279.801 s).
  The player cases pass (18.564 s); that is insufficient to accept a global
  scheduler change. The correction retains the current simulation clock for
  contacts/bolts and scopes the last processed clock to actual player block
  activation. The targeted switch/projectile/plate/tripwire/bolt and all sculk
  replays then pass (block 11.243 s, entity 4.602 s, player 6.571 s), log
  `.tmp/redstone-input-phase-targeted.log`. The final broad rerun is recorded
  below; no newly failing test was excluded.
- After scoping the input clock, the final selected broad run passes block
  (70.892 s), player (18.691 s), session (6.135 s), world (20.615 s), Overworld
  generation (139.870 s) and the other reported packages. Entity reaches the
  five-minute package limit while
  `TestWanderingTradersSpawnAtTheVillageBellWithLlamas` has been running for
  four seconds (reported entity package time 306.018 s). Log:
  `.tmp/redstone-publish-input-phase-full.log`. The broad run remains a failure;
  no additional exclusions were introduced and the timeout is not treated as
  proof of an unrelated failure. Its isolated trader recheck passes (10.288 s),
  log `.tmp/redstone-publish-trader-recheck.log`.
- The final selected entity arrow/button, lightning/rod/trap, plate and
  tripwire replays pass (6.909 s), log
  `.tmp/redstone-publish-final-entity-native.log`. Final `go build ./...` passes,
  log `.tmp/redstone-publish-input-phase-build.log`. All fifteen added Python
  probes compile and diff checks pass. Fixture provenance descriptions were
  clarified to include actual button uses; every recorded native trace is
  unchanged. The checkpoint is committed on `dev`, with the full audit paused
  and the planning estimate at approximately 60%.

- The resumed original-BDS diagonal capture completes 24 actual-player cases
  and 3,167 source/sensor/wire frames in `adamant-redstone-bds-kouqokkq`, log
  `.tmp/sculk-distance-native.log`. The old distance calculation fails eight
  cases (player 4.435 s), log `.tmp/sculk-distance-before.log`. After sharing
  the integer distance, all old and new selected vibration/sculk tests pass
  (world 2.350 / block 2.982 / player 12.361 s), log
  `.tmp/sculk-distance-after.log`.
- The original client selector, receiver and dispatcher range span execute in
  80 / 110 / 92 retained cases. Range/selection/strength, all sculk traces and
  selected warden/allay checks pass after the first range correction (world
  2.739 / block 2.331 / player 11.079 / entity 7.979 s), log
  `.tmp/sculk-distance-range-targeted.log`. The later boundary expansion adds
  24 coordinate cases; twelve distinguish flooring before and after conversion
  to binary32. Its final combined validation is recorded below.
- Before the adjacent-button change, the selected broad `go test ./...` run
  passes with only the same historical exclusions
  `TestBlockActorDataNBTBytes` and `TestWeakExecDoesNotBlockOwnerWhenQueueFull`,
  one repetition and a 600-second package limit. Block 60.538 / entity 321.527 /
  player 22.984 / session 5.554 / world 21.114 / Overworld generation 140.862 s.
  Log: `.tmp/sculk-distance-full.log`; build also passes, log
  `.tmp/sculk-distance-build.log`. This precedes the final button and binary32
  cell-boundary corrections and is not their broad-run validation.
- The original-BDS adjacent-switch capture completes all fifteen two-use cases
  and 3,481 frames in `adamant-redstone-bds-l2xlbsme`, log
  `.tmp/sculk-adjacent-native.log`. The replay initially fails exactly the two
  calibrated back-input button cases (player 3.058 s), log
  `.tmp/sculk-adjacent-before.log`. After independent button producer strength
  and event ordering, all selected sculk/vibration/button/arrow/switch/plate/
  tripwire/bolt checks pass (block 13.269 / entity 6.397 / player 12.052 /
  world 1.683 s), log `.tmp/sculk-adjacent-after.log`.
- After expanding binary32 cell conversion, the combined selector/strength/range,
  sculk, button/arrow/switch and warden/allay checks pass (world 2.642 / block
  6.478 / player 16.996 / entity 11.233 s), log
  `.tmp/sculk-distance-adjacent-final-targeted.log`. The same cell conversion
  is subsequently applied to occlusion endpoints; the final selected broad run
  and build include that adjustment. Their completion is recorded separately.
- The final selected broad run includes the button producer and occlusion-cell
  conversion. Block passes in 66.714 / player 24.775 / session 5.988 / world
  22.214 / Overworld generation 116.421 s. Entity fails `TestFishSwimAround`
  (the fish reaches Y 64.00192683060897) and `TestVillagersRunFromZombies`
  (no panic within the sensor window), and reports 228.623 s. Log:
  `.tmp/sculk-distance-adjacent-final-full.log`. The run remains a failure;
  neither test is excluded. The expected portal recovery panic logs are not
  additional test failures. Final `go build ./...` passes, log
  `.tmp/sculk-distance-adjacent-final-build.log`. A separate twenty-repetition
  comparison of both failing tests on the current tree and the unchanged
  `667e4cfc` checkpoint is recorded separately when complete.
- The current-tree twenty-repetition pair reproduces one fish failure and one
  villager failure (entity 45.570 s), log `.tmp/sculk-ai-current-recheck.log`.
  An unchanged, clean detached worktree at
  `667e4cfc73bb459c31d85ed5d916f35db95d748c` reproduces one fish failure in the
  same twenty-repetition pair (43.982 s), log
  `.tmp/sculk-ai-baseline-recheck.log`. Its additional eighty villager repeats
  reproduce eleven identical missing-panic failures (35.700 s), log
  `.tmp/sculk-ai-baseline-villager-recheck.log`. Both failure modes therefore
  also occur before this batch; this does not turn the failed final broad run
  into a pass or establish their root causes. No AI implementation or test was
  changed, and neither failure was excluded. The clean comparison worktree
  was removed after its processes exited. Python compilation and diff checks
  pass; this batch remains uncommitted and the full audit remains active.
- The initial retained contact capture has sixteen plate and twenty hook
  source/sensor traces. Before adding events, all thirty-six fail on the sensor
  phase/strength at the native arrival (block 3.385 s), log
  `.tmp/sculk-contacts-before-events.log`. The harness compares native palette
  properties and validates the sensor's redundant legacy facing alias; its
  earlier raw-encoder-only comparer was insufficient for that alias. After
  adding the actual event paths, all thirty-six full traces pass (3.476 s), log
  `.tmp/sculk-contacts-after-events.log`.
- The expanded original-BDS capture completes sixty-eight plate cases in
  `adamant-redstone-bds-bg96l9ni` and twenty hook cases in
  `adamant-redstone-bds-kgqtsaf2`, logs
  `.tmp/sculk-plates-native-expanded.log` and
  `.tmp/sculk-tripwire-native-expanded.log`. There are 18,480 retained
  source/sensor/wire frames. An earlier hook capture was rejected when the
  audit assumed spawned actors could not disappear; the final capture retains
  natural native actor merging/removal and its actual population/geometry.
  No incomplete capture is used as a fixture.
- Block/world/player/session packages pass with the same two historical
  exclusions (44.066 / 17.031 / 18.997 / 4.110 s), log
  `.tmp/sculk-contacts-packages.log`. Selected entity sculk/vibration/button/
  arrow/warden/allay/rod/trap checks pass (14.470 s), log
  `.tmp/sculk-contacts-entity.log`. This is a selected package/integration run,
  not a fresh all-package pass; the earlier recorded full-run AI failures
  remain recorded.
- The complete original hook `_emitState` executes in all sixty-four supplied-
  world cases. Its exact Go sound/event replay and all eighty-eight contact
  traces, with persistent replay identities and source-presence assertions,
  pass together (5.361 s), log `.tmp/sculk-contacts-hook-events.log`.
  Final `go build ./...` passes, log `.tmp/sculk-contacts-build.log`. The two
  new Python probes compile and diff checks pass. No commit or push was made;
  all 22 audit areas retain their listed outstanding work.
- The original-BDS plate/context capture completes seventeen settled traces
  (2,567 source/control/adjacent/output frames) and 34 actual client sound
  packets in `adamant-redstone-bds-y53c9uz2`, log
  `.tmp/plate-context-native.log`. The existing electrical order matches the
  native adjacent listener's release-only frequency 9 behavior for all seventeen
  materials. The pre-fix replay fails all seventeen on absent source sounds
  (block 3.311 s), log `.tmp/plate-context-before.log`.
- After adding the plate-specific named sounds at Y+0.1, the seventeen context
  traces, earlier eighty-eight contact traces, sixty-four original hook dispatch
  cases and existing switch packet checks pass together (6.088 s), log
  `.tmp/plate-context-after.log`. No independent plate producer change was
  needed by the observed electrical/filter behavior.
- Block/world/player/session packages pass with only the same two historical
  exclusions (72.970 / 20.537 / 23.380 / 4.671 s), log
  `.tmp/plate-context-packages.log`. `go build ./...` passes, log
  `.tmp/plate-context-build.log`. After tightening the new fixture's sound and
  adjacent-context completeness guards, its exact replay passes (3.265 s), log
  `.tmp/plate-context-final-targeted.log`. Python compilation and diff checks
  pass. These results are not a new all-package pass; the previously reproduced
  broad-run fish/villager failures remain recorded. No commit or push was made,
  and the full audit remains active.

## Paused checkpoint: 2026-10-27

The user requested a pause, commits and publication of the current work. The
planning estimate remains approximately **60%** of the full redstone audit;
this is not measured Bedrock equivalence, and none of the 22 matrix rows is
closed by this checkpoint.

The retained implementation changes are separated into:

- `8b0df879`: native vibration cell/distance/range/strength arithmetic and
  button event-before-producer ordering, with executable-client fixtures and
  original-BDS diagonal/adjacent player-use replays.
- `c47d0770`: plate and hook vibration events, source context and plate sounds,
  with native event, contact and adjacent-listener/sound captures.
- `88f96266`: shared support-loss breaking respects `block_drops=false`.
  A regression test removes a hook's support in a world without an item actor
  factory and verifies that the hook is removed without creating a drop.
- `ae58b843`: the unfinished hook lifecycle investigation, kept separately
  from the fixes. Its opt-in diagnostic remains a known failure.

The hook lifecycle capture contains 72 original-BDS scenarios and 17,280 frames,
covering removal, support loss, replacement, unsupported rotation, repairs and
line completion at both tick parities. Capture directory:
`adamant-redstone-bds-0zf96roy`; log `.tmp/hook-lifecycle-native.log`; fixture
`server/block/testdata/bedrock_1_26_51_hook_lifecycle.json`. Native command removal
can leave the surviving hook/string's attachment and power bits unchanged.
Do not replace this observed behavior with assumed forced detachment.

The latest replay, after fixing the harness's legacy stone-property comparison,
still fails 32 of 72 cases (block 4.964 s), log
`.tmp/redstone-pause-lifecycle-known-gaps.log`. Rebuilt powered lines can have
matching visible hook flags but different wire strength. Next work is to resolve
the native hook producer's creation/registration and strength-update ordering;
no hook producer fix is included in this checkpoint. The probe source adds an
item-absence assertion under disabled tile drops; the retained capture predates
that assertion, so its execution is not claimed as evidence.

The diagnostic is skipped by default with an explicit unfinished-audit message.
To reproduce the known failures in PowerShell:

```powershell
$env:ADAMANT_REDSTONE_LIFECYCLE_AUDIT = '1'
go test ./server/block -run '^TestTripwireLifecycleMatchesBDS$' -count=1
Remove-Item Env:ADAMANT_REDSTONE_LIFECYCLE_AUDIT
```

Final checkpoint validation:

- Selected sculk/vibration/tripwire/plate/switch/button/support-loss checks pass
  in block 26.007 / world 2.306 / player 15.873 s, log
  `.tmp/redstone-pause-targeted.log`.
- After moving the opt-in diagnostic into its own file, the contact/context and
  disabled-drop checks pass (block 5.892 s), with the lifecycle diagnostic
  explicitly skipped, log `.tmp/redstone-pause-checkpoint.log`.
- `go build ./...` passes, log `.tmp/redstone-pause-build.log`. All seven Python
  probes in this batch compile, and `git diff --check` passes.
- This is not a fresh passing all-package run. The earlier full-run fish and
  villager failures and their baseline reproductions remain recorded above.

The personal files `banned-players.json`, `ops.json` and `tmplog.txt` are excluded
from the checkpoint. The full audit is paused pending the user's continuation.

## Resumed hook producer and placement investigation: 2026-10-27

The user resumed the original full-audit goal. The following changes are
uncommitted; all 22 matrix rows still retain outstanding work. The former
72-case lifecycle diagnostic is now an unconditional regression test in
`tripwire_lifecycle_native_test.go`. Its previous opt-in instructions above are
historical checkpoint information, not the current invocation requirement.

- Inspected client hook setup/calculation and stored producer direction.
  Adding the zero-initialized hook producer and native active-map setters makes
  the original 72 lifecycle traces pass. The combined initial lifecycle/contact/
  hook-helper check passes (block 6.798 s), log
  `.tmp/hook-producer-first.log`.
- Repeated all 72 lifecycle cases with the native item-absence control under
  `doTileDrops=false`. The canonical fixture now comes from
  `adamant-redstone-bds-dg03jr7w`, log `.tmp/hook-lifecycle-recapture.log`.
  All 17,280 frames are retained.
- Captured 64 additional hook permutation cases / 15,360 frames in
  `adamant-redstone-bds-5tlp0og6`, log `.tmp/hook-permutations-native.log`.
  The first replay exposes twelve event/direction mismatches (block 8.446 s),
  log `.tmp/hook-permutations-before.log`. Clearing the authored powered bit
  before line calculation and retaining setup direction corrects them.
- Captured 216 shared producer rotation/replacement cases / 19,440 frames.
  The canonical fixture comes from `adamant-redstone-bds-fjfindnj`, log
  `.tmp/producer-rotation-controlled.log`, with a native powered/lit-lamp
  positive control for every case. An earlier strengthened attempt failed that
  pre-edit control and was rejected; it supplies no retained fixture. The
  initial complete rotation fixture also replayed successfully (block 5.042 s),
  log `.tmp/producer-rotation-replay.log`. The canonical capture is validated by
  the broad and final targeted runs below.
- The lifecycle, permutation and existing selected redstone integration checks
  pass after the producer configuration change (block 27.275 / world 9.281 /
  player 16.332 / entity 15.055 s), log
  `.tmp/hook-producer-configuration-targeted.log`.
- The selected broad `go test ./...` run uses only the same historical
  exclusions `TestBlockActorDataNBTBytes` and
  `TestWeakExecDoesNotBlockOwnerWhenQueueFull`, one repetition and a 600-second
  package timeout. Block passes in 72.812 / world 23.118 / player 24.803 /
  session 4.554 / Overworld generation 120.582 s. Entity fails in 295.689 s:
  `TestEndFightNativeLifecycleAndMCDBRestart` loses the first-kill egg and has a
  Windows DB cleanup sharing error; `TestFishSwimAround` leaves the pool at
  Y 64.01579437105211; `TestFoxesPickSweetBerries` does not harvest;
  `TestVillagersRunFromZombies` does not panic. Log:
  `.tmp/hook-producer-full.log`. The broad run remains a failure. No additional
  test is excluded, and these observations do not establish their causes.
- A clean detached worktree at the unchanged published `55002a7a` reproduces
  seven fox failures in forty repetitions (29.658 s), log
  `.tmp/producer-baseline-fox.log`. The earlier fish/villager baseline evidence
  remains recorded above. Three dragon lifecycle repeats on that clean
  checkpoint pass (117.249 s), log `.tmp/producer-baseline-dragon.log`; three
  current-tree repeats also pass (104.742 s), log
  `.tmp/hook-producer-dragon-recheck.log`. These passes do not prove that the
  failed broad-run egg/cleanup behavior is unrelated to this batch. Its cause
  remains unresolved. The clean comparison worktree was removed after its
  processes completed. No AI, dragon, persistence implementation or test was
  changed to obtain these results.
- Captured sixteen accepted actual-player hook placements / 665 frames in
  `adamant-redstone-bds-fsoljiwv`, log `.tmp/hook-placement-native.log`.
  The first Go replay matches block state but sends 32 attachment packets
  instead of sixteen (player 4.175 s), log `.tmp/hook-placement-before.log`.
  Removing the explicit second scan from `TripwireHook.UseOnBlock` corrects
  the duplicate sound/event path. `Placed` retains the native single scan.
- Final selected hook/player-placement/cutting, sculk/vibration, producer
  rotation, plate/switch/button/arrow/rod/trap checks pass (block 29.981 /
  world 7.588 / player 16.582 / entity 16.166 s), log
  `.tmp/hook-producer-final-targeted.log`. This includes all 352 command-driven
  producer cases, all sixteen real-player placement traces, the sixteen
  attachment packets and the existing 230 cutting sound packets.
- Final `go build ./...` passes, log `.tmp/hook-producer-final-build.log`.
  The six added/changed Python probe/client files compile and diff checks pass.
  The final targeted run and build include the single-scan player fix; the
  failed broad run predates that final player-only change.

Next work includes powered/disarmed player placement and cutting, remaining
support/contact boundaries and materials, crossing/gapped lines under later
source changes, load/unload/persistence and the other open rows. These fixes
do not establish full native circuit or redstone equivalence. No new commit or
push was made during the continuation.

## Resumed vibration obstruction investigation: 2026-10-27

The original full audit remains active and incomplete. The obstruction change
is uncommitted, as are the preceding hook/producer changes. All 22 matrix rows
still retain their listed outstanding work.

- Inspected `VibrationListener::handleGameEvent` endpoint centring and six-offset
  clip calls, the complete native clip traversal, the wool predicate and the
  ray/AABB constructor. The actual offset constant is binary32 **0.001**, not
  the previous Java-derived **0.00001**. The ray/AABB parallel cutoff is squared
  binary32 `1e-7`.
- The original-BDS obstruction capture completes 168 cases / 35,280 frames in
  `adamant-redstone-bds-xd5pz4tr`, log
  `.tmp/sculk-occlusion-geometry-native.log`. Its canonical fixture is
  `server/block/testdata/bedrock_1_26_51_sculk_occlusion_geometry.json`.
  The first Go harness run fails the SDK's redundant `color` alias. That alias
  is now checked against the actual wool/carpet colour; it is not discarded.
  The valid pre-fix replay then fails eight million-coordinate seam cases
  (block 8.546 s), log `.tmp/sculk-occlusion-geometry-before-valid.log`.
- The initial executed-client fixture has 148 obstruction cases and 133 raw
  traversals. Before the algorithm change, thirteen obstruction cases and
  112 cell sequences differ (world 2.380 s), log
  `.tmp/vibration-occlusion-before.log`. This includes repeated-cell/endpoint
  behavior and a short ray entirely within wool, in addition to large-coordinate
  false reception. The native 200-step limit is preserved, not replaced with an
  assumed acyclic or endpoint-expanded traversal.
- The final client fixture expands to 208 obstruction cases, 133 cell sequences
  and 202 direct unit-cube intersections. It is generated by
  `scripts/redstone_native_vibration_occlusion_probe.py` and retained at
  `server/world/testdata/bedrock_1_26_51_vibration_occlusion.json`. The complete
  original clip, wool predicate and ray/AABB bodies execute with supplied world
  and unit-shape callbacks. The inspected vibration offset span is explicitly
  marked unexecuted in its function metadata. These component cases do not
  imply complete native-world or complete listener execution.
- After porting the binary32 offsets, current-point traversal and unit-cube
  face tests, the BDS frames and initial expanded component comparison pass
  together (world 2.446 / block 8.218 s), log
  `.tmp/sculk-occlusion-after-first.log`. After adding the direct-intersection
  boundary controls, all selected sculk/vibration/warden/allay, plate/switch/
  button/arrow/rod/trap, hook placement/cutting and producer-rotation checks pass
  (world 6.858 / block 26.483 / player 12.275 / entity 14.703 s), log
  `.tmp/sculk-occlusion-targeted.log`.
- The final selected broad `go test ./...` run passes with only the same two
  historical exclusions `TestBlockActorDataNBTBytes` and
  `TestWeakExecDoesNotBlockOwnerWhenQueueFull`, one repetition and a 600-second
  package timeout. Block 58.033 / entity 247.117 / player 19.999 / session 5.514 /
  world 17.707 / Overworld generation 85.384 s. Log:
  `.tmp/sculk-occlusion-full.log`. This successful run does not erase earlier
  failed broad runs or establish the causes of their intermittent AI/egg/DB
  failures, whose evidence and baseline comparisons remain recorded above.
- Final `go build ./...` passes, log `.tmp/sculk-occlusion-build.log`. Both new
  Python probes compile and `git diff --check` passes. No Go source was edited
  while those final checks were running. No commit or push was made.

The new ray code reads already loaded cells and caps each ray at 200 transitions.
Broader source/context filtering, fractional actor integration, dynamic occluder
changes, native-world geometry at extreme coordinates, liquid/resonance/shrieker
paths, persistence and the other redstone areas remain open.

## Paused checkpoint: 2026-10-27

The user requested a pause until tomorrow and authorized commits and publication
to `dev`. The completed changes are split into:

- `ff0c9154` — hook lifecycle, scene producer configuration, native command and
  actual-player placement fixtures, and their capture/replay tools.
- `0b106d01` — native vibration obstruction, binary32 traversal/intersection
  behavior, and the executed-client and original-BDS fixtures.
- A separate documentation commit records the evidence, limitations and pause.

The last targeted tests, broad test run with the two explicitly recorded
historical exclusions, and `go build ./...` passed as detailed above. No source
change followed those checks. Earlier intermittent failures remain documented;
the successful final run does not establish their causes.

The effort estimate remains **approximately 60%**, not measured compatibility.
All 22 coverage rows retain outstanding work. Piston client disassembly and
existing code were inspected next, but no piston fix, capture or equivalence
claim is included in this checkpoint. Resume with piston power directions,
short pulses and movement timing against original-BDS observations, then
continue the remaining matrix. Private local player/permission files and
`tmplog.txt` are excluded from the commits.

## Resumed piston investigation: 2026-10-27

The user reactivated the full audit and explicitly requested continued work
until they ask to stop. No commit or publication is authorized by this
continuation. The previous pause above is historical; all 22 coverage rows
remain incomplete.

`PistonBlock::setupRedstoneComponent` creates a `PistonConsumer`, seeds it at
strength fifteen only when both actor state fields are extended, and configures
the excluded source direction. `PistonConsumer` inherits the inspected
`ConsumerComponent::evaluate`; movement reads that evaluated strength on the
actor clock. Immediate neighbour-driven movement in Adamant skipped these
separate circuit/actor phases.

`scripts/redstone_bds_piston_timing_probe.py` drives original BDS with stable
Script API readback. It clears the complete tested movement volume, including
downward paths, controls absolute input parity, records before changes, reads
the native piston state/moving flag/attached positions, and retains every
observed payload/source/conductor permutation. The larger layouts use isolated
32-block cells and a 100-chunk ticking area. Filled-chest observations also
retain each nonempty inventory slot.

- The canonical timing fixture has 432 cases / 43,200 frames from
  `adamant-redstone-bds-x51zd2_o`, log `.tmp/piston-timing-all.log`. It combines
  288 direction/direct-versus-weak-conducted cases and 144 pulse cases with
  widths 1/2/3/4/8/16, both piston kinds, all six facings and both parities.
  A redstone block beyond stone is a weak-input control, not a strong source.
  The first complete attempt incorrectly required that control to move and
  was rejected; the canonical capture applies the movement control only to
  the direct back input. An earlier smoke attempt also rejected unavailable
  legacy Script API properties; the retained captures use `state` and
  `isMoving`, with explicit type checks.
- Before the timing fix, 264 of those 432 replays fail (block 5.080 s), log
  `.tmp/piston-timing-before.log`. After the consumer/actor change all 432
  pass (block 6.163 s), log `.tmp/piston-timing-first.log`.
- The retrigger fixture has 576 cases / 80,640 frames from
  `adamant-redstone-bds-v6s_v88h`, log `.tmp/piston-retrigger-native.log`.
  Widths 1/2/4/16 and gaps 0/1/2/3/4/8 include a source removed and restored
  in the same script pass. All requested transitions are retained, including
  two changes sharing one tick.
- The strong-input fixture has 144 cases / 14,400 frames from
  `adamant-redstone-bds-6qq73337`, log `.tmp/piston-strong-native.log`. A lever
  mounted on each tested stone supplies actual strong conducted input; its
  orientation and visible powered state are compared, not inferred.
- The blocks fixture has 96 cases / 9,600 frames from
  `adamant-redstone-bds-1y_mlhjc`, log `.tmp/piston-blocks-native.log`.
  Twelve versus thirteen stone blocks, obsidian and a chest containing seven
  diamonds are tested in all six directions, both phases and both piston kinds.
  The valid first replay fails 24 cases solely on attached-list order (block
  3.114 s), log `.tmp/piston-blocks-first.log`; its other block, timing and
  inventory observations match. The SDK's chest `facing_direction` alias is
  checked against the chest's actual cardinal facing.
- Native `_spawnMovingBlocks` sorts attached positions by movement projection
  before spawning. The complete original sort body now executes in
  `scripts/redstone_native_piston_sort_probe.py` with the host supplying only
  byte movement. Its 334 cases include equal-projection ties, sizes zero
  through twelve, negative/large coordinates and signed-int32 wrap boundaries.
  The fixture records the original image and function hashes. This establishes
  the sort for supplied discovery orders; it does not establish native branch
  discovery order. The production path now sorts and spawns in this order.

At the tested long-pulse edge, phase zero starts visible movement at +4 GT and
finishes at +6; phase one starts at +3 and finishes at +5. A one-GT input in
phase zero is ignored, while the phase-one input completes a normal move.
Retraction removes an ordinary head at movement start. These observations do
not imply that every source, placement phase or registration state has the
same latency.

The former immediate-action piston unit test now explicitly isolates moving-
block restoration; full input timing is tested by the independent BDS fixtures.
The observer movement-callback replay initializes its artificial piston scene
as already extended, matching the native arrival it intentionally isolates.
It no longer creates an unpowered retracted scene component before invoking
the completion callback.

After the first ordering correction, selected piston/observer, redstone,
comparator/torch, sculk/vibration and tripwire/producer checks pass (block
35.526 / world 3.029 / player 11.853 / entity 3.270 s), log
`.tmp/piston-final-targeted.log`. After replacing the linear reversal with the
inspected stable positional sort, all selected piston tests, all 1,248 BDS
traces and all 334 executed-sort cases pass (block 10.579 s), log
`.tmp/piston-all-final.log`. The following broad run passes with only the same
two historical exclusions `TestBlockActorDataNBTBytes` and
`TestWeakExecDoesNotBlockOwnerWhenQueueFull`, one repetition and a 600-second
package timeout (block 66.118 / entity 215.678 / player 21.064 / session 5.885 /
world 18.633 / Overworld generation 86.430 s), log `.tmp/piston-full.log`.
`go build ./...` also passes, log `.tmp/piston-build.log`. These results precede
the next adhesion replay, whose failures or fixes require their own validation.
Earlier intermittent AI/dragon failures remain documented above; a successful
broad run does not establish their causes.

Remaining piston work includes adhesive branches and their discovery/tie
order, other source families and registration/rotation, startup/load, sound
and vibration/network events, moving block actors beyond the tested chest,
partial geometry, entity displacement and full movement persistence. Adhesion
world captures are the next independent comparison, not a claim of coverage
from the linear cases above.

### Adhesive branch comparison in the resumed investigation

The original-BDS adhesion capture adds 240 cases / 24,000 frames in
`adamant-redstone-bds-347bndn3`, log
`.tmp/piston-adhesion-native-offline.log`. Two earlier launches time out waiting
for Minecraft services, before simulation starts; they are rejected. The accepted
run opts into the existing isolated loopback/offline reference configuration.
Its fixture explicitly records `offline_reference: true`. It has no player,
and no production server configuration is changed.

Crosses of stone, slime, honey or obsidian around a slime/honey centre and
multi-level adhesive chains are exercised for both piston kinds, all six
directions and both input phases. Every authored branch and its destination
is sampled; startup controls verify that the branch layout is actually present.
The pre-fix replay fails 192 of 240 cases (block 4.526 s), log
`.tmp/piston-adhesion-before.log`: mixed slime/honey branches incorrectly move,
deeper branches are omitted, and equal-projection attachment ordering differs.

The inspected complete `PistonBlockActor::_attachedBlockWalker` at
`0x4ae4510` uses depth-first discovery, excludes the incoming edge from an
adhesive branch, and filters distinct adhesive types except when pushing
directly along movement. Its neighbour table at `0xe8f7430` converts to
Up/Down/South/North/East/West in physical block coordinates. The production
calculator now follows this traversal, then runs the independently executed
native positional sort. The previous line-splicing/one-pass branch algorithm
is removed. All piston checks, including the 240 new cases, pass (block
16.995 s), log `.tmp/piston-adhesion-first.log`; after removing the unused
old algorithm, selected integration checks also pass (block 37.326 / world
2.750 / player 11.430 / entity 3.331 s), log
`.tmp/piston-adhesion-targeted.log`.

The expanded comparison adds 288 cases / 28,800 frames for loops, backwards
branches, mixed-material forward pushes, obstructed branch destinations and
branched 12/13-block limits. Its complete original-BDS capture comes from
`adamant-redstone-bds-0yuzfn7i`, log
`.tmp/piston-adhesion-extended-native-fixed.log`. An initial generated script
had an extra brace, failed before collecting scenarios and is rejected; the
corrected script passes `node --check` before recapture. All 288 extended cases
pass without another production change (block 5.460 s), log
`.tmp/piston-adhesion-extended-first.log`.

The following broad run uses the same two historical exclusions and fails
only `TestFoxesPickSweetBerries` in entity (219.193 s). Block 74.550 / world
19.948 / player 22.218 / session 6.184 / Overworld generation 91.561 s pass.
Log: `.tmp/piston-adhesion-full.log`. The logged portal spawn/teleport panics
come from the explicit panic-recovery tests; they are not the failing assertion.
The build passes, log `.tmp/piston-adhesion-build.log`. Forty isolated fox
repetitions fail ten times (20.160 s), log `.tmp/piston-fox-recheck.log`.
The clean `55002a7a` baseline previously failed seven times in forty identical
fox repetitions, as recorded above. No additional exclusion or AI change is
made; these observations do not establish the cause of the intermittent test.

### Native piston actor and consumer checkpoints

`scripts/redstone_bds_piston_actor_probe.py` reads only its owned original BDS
child process. A unique 96-byte client tick prefix locates BDS tick RVA
`0x4872840`; the tick slot at offset `0x20` identifies the native tick-component
vtable at `0xa83fd80`. The position accessor is separately checked to return
`this-0x38`. An initial lookup treated tick as slot zero and failed all actor
lookups; that capture is rejected. No offsets are accepted from that failed
lookup.

A separate unique client consumer-evaluator prefix locates BDS evaluator RVA
`0x4bd7e60`. The component type getter at vtable offset `0xd0` is checked against
the inspected piston getter, locating consumer vtable `0xa8514a0`. Heap objects
must match the expected positions, component type `0x20001`, live identity and
unique actor/consumer pair. During each checkpoint the Script API callback holds
its own work for 400 ms. Reads must finish before that recorded deadline; actor
bytes are reread to detect any concurrent change. There is no injection,
suspension or process write.

The accepted combined fixture comes from `adamant-redstone-bds-xfohttwo`, log
`.tmp/piston-actor-consumer-native.log`. It retains eight 90-GT scenarios, both
input parities, both piston kinds and blocked/unblocked payloads, with 33
checkpoints / 264 actor-and-consumer observations. The fixture records both
original image hashes and the locator prefix hashes. Native movement/blocked
controls and both zero/fifteen consumer strengths are checked per scenario.
An earlier actor-only complete capture in `adamant-redstone-bds-cdpk4phv` was
used to establish the actor field reads; the canonical combined capture adds
the independently identified scene consumer.

The valid pre-fix replay fails all eight cases (block 2.642 s), log
`.tmp/piston-actor-before.log`. Progress, last progress, visible state, attached
positions, block permutations and consumer strengths match, but the serialized
`NewState` changes one GT before native in twelve observations. Circuit-to-actor
handoff now uses transient runtime fields. It does not publish a prospective
NBT state before the native movement attempt; a blocked attempt still publishes
the native extending `NewState` while retaining retracted `State`.

After that correction all piston checks pass (block 15.236 s), log
`.tmp/piston-actor-after.log`: 1,776 world timing/geometry/inventory traces,
334 executed sort cases and the eight combined actor traces. This verifies
the compared serialized fields, not native packet emission count/order or all
movement persistence. The next broad run and build include this actor change;
the build passes (`.tmp/piston-actor-build.log`) but the broad run fails only
`TestHappyGhastsFloatAround` (entity 236.973 s), whose height is
64.9993931179464 against its 65-block threshold. Block 78.006 / world 20.182 /
player 22.711 / session 6.187 / Overworld generation 96.732 s pass. Log:
`.tmp/piston-actor-full.log`. Forty isolated repeats reproduce six happy-ghast
failures (23.656 s), log `.tmp/piston-happy-ghast-recheck.log`. An earlier
redstone checkpoint already recorded this exact test failing at height
64.960066, as retained above. No AI assertion is changed or additionally skipped,
and these observations do not establish its cause.

### Piston vibration and packet publication

The client global-event initializer at `0x25f1dd0`/`0x25f1de1` assigns IDs
37/38 to the globals used by piston contraction/extension; the name table
initialization at `0x25d24a0` through `0x25d2555` identifies
`piston_contract`/`piston_extend`. The complete frequency getter at
`0x25d7c70` executes with those event IDs and returns **9**/**10**. It indexes
the table at `0xe829988` by **ID minus one**. An initial hand-indexed check
missed that subtraction and an overstrict capture control rejected the run;
no fixture from that rejected attempt is used.

The canonical vibration capture has 96 cases / 17,280 frames from
`adamant-redstone-bds-206aq5e8`, log
`.tmp/piston-vibration-native-frequency.log`. Calibrated listeners separately
accept frequency nine or ten; extension and retraction are sixty GT apart,
so their arrivals are independent. Native controls verify the stable filter,
extension-only reception before power-off, contraction reception afterward,
and silence for an obsidian-blocked piston. All six facings, both piston kinds
and both input parities are included.

The isolated Go listener replay initially omitted the surrounding 3x3 columns
that BDS's ticking area had loaded. Its candidate therefore reached zero travel
but remained pending under `RequiresAdjacentChunksToBeTicking`. Those initial
replays are not used as production failure evidence. After matching the loaded
area, the valid replay with the two event emissions absent fails 48 cases
(block 3.911 s), log `.tmp/piston-vibration-before-valid.log`. Restoring the
native-named contraction/extension emissions, with frequencies nine/ten, makes
all 96 sensor traces and the actor checkpoints pass together (block 3.943 s),
log `.tmp/piston-vibration-after-valid.log`.

Events and sound follow the actor's previous-state transition, one actor pass
after visible movement starts, rather than the move-construction callback.
The native load body sets previous state from `NewState`; the runtime now does
the same on decode. The retained heap fixture additionally compares previous
state. Sound uses the integer block position inspected in the native body.
Piston-specific event names inherit the common block-transition listener
categories; dedicated native warden/shrieker integration remains outside these
sensor captures and still requires verification.

`scripts/redstone_bds_piston_packet_probe.py` adds an actual loopback client
after its owned reference world is ready. It retains eight nearby concurrent
piston traces, eight native sound packets and 32 block-actor packets from
`adamant-redstone-bds-8d6cxvca`, log `.tmp/piston-packets-native.log`. Of the
32 actor packets, 24 are `PistonArm`; eight are `MovingBlock`, including duplicate
sticky-retraction payloads retained without deduplication. The helper logs
block-actor packets in addition to the existing sound packets.

The real session replay initially fails all eight scenarios (session 2.611 s),
log `.tmp/piston-packets-before.log`: unblocked sound packets use `":"` instead
of native empty entity type, and blocked pistons repeatedly publish `NewState`
requests that BDS keeps server-side. Blocked requests and their cancellation
now update saved actor state without broadcasting an animation. Actual start
and finish retain their client publications. The session's piston sound mapping
now uses native empty entity type.

After these changes, selected piston/observer, redstone/comparator/torch,
sculk/vibration, tripwire/producer and native sound tests pass (block 42.289 /
world 3.074 / player 12.530 / entity 3.537 / session 1.896 s), log
`.tmp/piston-packets-targeted.log`. The packet comparison checks complete
PistonArm sequences per position and all sound fields through the real session
publication path. It does not yet assert cross-position packet ordering or the
MovingBlock duplicate/first-publication timing; those retained packets are the
next comparison. These results do not supersede the failed broad runs above.


### Moving-block first publication and occupied destinations

Timestamped packet capture `adamant-redstone-bds-9ivudnaf` retains the wall
clock beside both Script API frames and packets (`.tmp/piston-packets-clock.json`).
The parent starts at relative GT+4, the moving actor and sound arrive around
GT+5, and completion is observed at GT+6. Wall clocks supplement the actor
observations; client input ticks are not treated as native simulation time.
A six-direction capture (`adamant-redstone-bds-qr_cvl01`,
`.tmp/piston-packets-directions.json`) independently retains 48 cases, 48 sounds,
and 232 actor packets. Its reused positions include 40 parent cleanup packets;
these are retained, not deduplicated into fictitious fresh-world sequences.
Every direction reproduces one extension notification for a single block and
two identical sticky-retraction notifications on the following actor pass.

The actual native moving-actor tick at client RVA `0x4ade460` increments its
counter and tests the dirty bit before calling `fireBlockEntityChanged`.
The unique original-client instruction prefix independently locates BDS tick
`0x486f9d0`, tick-component vtable `0xa83fab0`, with its checked position
accessor. Owned-heap captures `adamant-redstone-bds-6e5zyh8h` and
`adamant-redstone-bds-segpqfin` observe counter 0 / flags 3 immediately after
creation, then counter 1 / flags 2 one GT later. The observed main-component
dirty callback is BDS RVA `0x75fb0`, a return instruction. The two retraction
packets therefore do not imply two moving-actor ticks. An initial capture's
control compared Python tuples against lists and was rejected; the corrected
complete captures, not that failed control, supply these observations.

The session replay was extended to compare MovingBlock payloads/counts and
first-publication delay. It fails before the change (session 2.484 s), log
`.tmp/piston-moving-packets-before.log`: data is sent on creation rather than
one GT later, and a sticky pull loses its duplicate notification. Nested block
states are compared after a network-NBT round trip: nil and empty Go maps have
the same empty-compound wire representation, unlike their JSON representations.
No native duplicate or payload field is discarded.

A three-block chain capture (`adamant-redstone-bds-1em08d01`) adds eight cases /
800 frames, eight sounds, and 48 actor packets. It is checked in as
`bedrock_1_26_51_piston_packets_chain.json`. Its two inner destination cells
receive two identical extension compounds each; the previously empty far cell
receives one. The sticky pull into the former head also receives two. A second
owned-heap chain capture (`adamant-redstone-bds-ukqt_dt0`,
`.tmp/piston-moving-actor-chain.json`) observes all twelve extending actors with
counter 0 / flags 3, then all twelve with counter 1 / flags 2. Two retracting
actors follow the same counter/flag sequence. Repeated packets still do not
mean repeated actor ticks. The exact native notification ownership mechanism
beyond these observed occupied-destination cases remains an open detail.

The initial Go chain replay loaded only one viewed column, missing packets
beyond X=15; that replay is rejected as production evidence. Loading the actual
viewed area leaves four genuine failing movement cases (session 2.561 s), log
`.tmp/piston-moving-chain-before-valid.log`: missing inner-cell duplicates.
The runtime now delays moving-actor publication to its first tick and retains
the additional notification for a destination occupied before movement starts.
Destination occupancy is captured before source clearing. The packet queue
retains those notifications separately from the coalesced block replacement,
orders a pending parent before its moving actor, respects the block batch limit,
and discards stale actor notifications when a newer block replacement arrives.
Replacement of an already queued position is allowed even at the backlog limit;
leftover older entries no longer overwrite updates enqueued during a flush.

Both native packet fixtures and three backlog/actor-publication checks pass
(session 2.743 s), log `.tmp/piston-moving-packets-after.log`. They verify the
compared packet fields/counts, parent dependency, one-GT publication delay and
stale replacement behavior, not full native ordering between unrelated actors,
notification ownership for every replaceable/liquid destination, reload or
orphan recovery, or piston/entity collision equivalence. All audit rows remain
open. The broader post-change verification is recorded below when it finishes.


### Piston removal during movement

`scripts/redstone_bds_piston_interruption_probe.py` records 384 original-BDS
cases / 38,400 complete frames from `adamant-redstone-bds-b5js06cl`, log
`.tmp/piston-interruption-native.log`. Both piston kinds, all six facings,
both input parities, extension/retraction, the first/second observed moving
frame, removal/replacement with stone, and three stone blocks/a chest with
seven diamonds are independent axes. Interruption is triggered by the observed
native actor state, not an assumed number of ticks after the source changes.
The fixture retains each action's tick and pre-action actor, both power edges,
all four front cells and every nonempty chest slot. Controls check that the
replacement persists and the expected motion was actually reached.

The pre-fix replay fails 288 cases (block 4.639 s), log
`.tmp/piston-interruption-before.log`: moving blocks remain permanently after
the parent disappears, including inaccessible moving chests. The normal-piston
retraction cases provide controls without a moving payload. Native
MovingBlockActor::tick (`0x4ade460`, especially `0x4ade5d0..0x4ade715`) checks
the referenced piston block/actor and restores the carried block when absent.
Its final wrapped-block virtual call is slot `0x1b0`, independently identified
as `BlockType::movedByPiston`; it is shared with ordinary movement completion.

MovingBlock now checks its parent on every actor tick and restores itself when
the parent is absent. Restoration reuses the normal completion path, decodes
the carried actor through the world's registry at its new coordinates, and
calls movedByPiston. Both restoration paths preserve the filled chest contents.
All piston, observer and moving-actor checks pass together (block 18.721 /
session 1.857 s), log `.tmp/piston-interruption-after.log`. The first attempted
build after extracting the shared helper exposed a still-used maps import;
restoring that import fixed the compile error before the successful run.
The 384 world comparisons include full geometry and inventory readback, not
network ordering during an interruption or actual player destruction/drop
behavior. Those remain open.

Before this orphan-restoration change, the moving-publication targeted run also
passed (block 41.157 / world 2.578 / player 10.688 / entity 2.645 / session
1.098 s), log `.tmp/piston-moving-targeted.log`, and `go build ./...` passed
(`.tmp/piston-moving-build.log`). A new broad run follows the restoration change;
its final outcome is appended after completion.


The broad post-interruption run, with only the two previously documented
exclusions, fails `TestEndFightNativeLifecycleAndMCDBRestart` (entity 213.951 s):
its first-kill egg is air after MCDB restart, followed by a locked LevelDB file
cleanup error. Block 81.186 / world 19.486 / player 21.441 / session 5.960 /
Overworld generation 89.887 s pass. Log: `.tmp/piston-interruption-full.log`.
An isolated repeat passes (entity 27.849 s), log
`.tmp/piston-end-fight-recheck.log`. This does not identify the cause or turn
the failed broad run into a pass. No dragon/AI/persistence assertion or
implementation was changed to hide it, and there are no new exclusions.


### Native movable-component classification

The inspected walker calls the original-client movable-component accessor at
`0x9ab2410`, then reads MovementType and StickyType bytes. The independently
identified native enums are PushPull=0, Push=1, Popped=2, Immovable=3, and
None=0/Same=1 for adhesion. A small owned-BDS Script API check confirms that
`minecraft:movable` exposes these actual classifications. The generated native
component lookup replaces incomplete manually maintained lists.

`scripts/redstone_bds_piston_movability_probe.py` records all 1,477 native block
types from `adamant-redstone-bds-340dz2pg`, log
`.tmp/piston-movability-native-valid.log`. Five immediately transform when placed
(bubble column, fire, soul fire and the two flowing liquid IDs); their requested
states and resulting IDs are retained without assigning the replacement's
component to the requested type. The other 1,472 components classify 1,083 types
as PushPull, seventeen as Push, 331 as Popped and 41 as Immovable. Exactly honey
and slime have Same adhesion. The first attempt stopped at bubble-column
survival and is rejected as a complete export.

All 1,472 readable names have registered counterparts. The genuine component
replay fails 344 types before the change (block 2.535 s), log
`.tmp/piston-movability-before.log`: 31 missing immovable classifications,
308 missing popped classifications and five incorrectly popped types. The
fixture itself is retained, and
`scripts/redstone_generate_piston_movability.py` validates its binary hash,
counts, domain values and unique names before generating
`server/block/piston_movable_native.go`. The generated source records the input
fixture hash. This evidence is component data at default states; it does not by
itself prove every state, runtime actor gate or full world behavior.

An attempted broader permutation export is retained as an explicitly incomplete
probe outside the canonical fixtures. Its initial host-array iteration failed;
serialized domain arrays fix that. Legacy property values can be invalid for a
particular renamed type, so rejected changes are recorded. The later original
BDS run stopped after 72 types without a complete result. None of those partial
runs is accepted as all-state equivalence evidence. The exact reason for that
native process exit has not been established.

Twenty material choices add 480 original-BDS world traces / 48,000 frames,
including fifteen immovable types, heavy core/two glazed colors, stone and a
filled chest. The canonical supported capture is
`adamant-redstone-bds-b8n6e1qn`, log `.tmp/piston-materials-native-supported.log`.
Each grindstone has its actual standing support, and all large-cell footprints
are checked loaded, not merely two opposite corners. Earlier captures/replays
with incompletely loaded interiors, unsupported grindstones or chest-only
inventory assumptions are rejected as production evidence. Supported replay
also validates the inactive sculk/heart and ender-chest legacy aliases against
actual runtime fields rather than ignoring those native fields.

The corrected pre-fix replay fails 396 movement cases (block 5.419 s), log
`.tmp/piston-materials-before-final.log`, with no unmapped payload or alias.
After the classification table, the component comparison passes and exactly
36 sticky heavy-core/glazed cases still fail (block 5.595 s), log
`.tmp/piston-movability-after-defaults.log`. These expose the missing Push-only
rule. The walker now accepts a forced forward Push block but excludes it from
sticky pull and side adhesion. All piston/observer/moving-actor checks then pass
(block 21.086 / session 1.775 s), log `.tmp/piston-materials-after.log`.

A separate 288-case / 28,800-frame capture compares the three Push-only materials
on slime/honey side crosses and directly ahead of them, with all directions,
both piston kinds and both input parities. It comes from
`adamant-redstone-bds-zhrwhzd5`, log `.tmp/piston-push-only-native.log`, fixture
`bedrock_1_26_51_piston_push_only.json`. The subsequent replay and side-adhesion
regression are recorded after completion below. Broader popped-block drops,
all-state/runtime actor gates, liquid destinations and support lifecycles remain
open, alongside the other audit rows.


With only the new side-Push exclusion temporarily absent, the 288-case adhesive
replay fails exactly 144 cross cases (block 4.625 s), log
`.tmp/piston-push-only-before.log`. The forward-push controls pass. The production
rule was restored after that process completed. All selected piston/observer,
redstone/comparator/torch, sculk/vibration, tripwire/producer and native sound
checks then pass (block 47.940 / world 2.482 / player 11.089 / entity 2.573 /
session 1.096 s), log `.tmp/piston-push-only-targeted.log`. These include the 288
new native traces and the earlier complete piston fixtures. No broad run after
the movability changes has yet superseded the documented End-fight failure.


### Moving a piston as payload

The native walker additionally checks a piston actor's movable flag, independent
of its PushPull component classification. A piston is movable only in retracted
State=0; this is also the isMovable field saved by the native actor. The runtime
previously used only a block-name immovable list and could push an extended
piston while detaching its collision head.

`--suite piston_payload` adds 96 original-BDS traces / 9,600 frames from
`adamant-redstone-bds-mp7t5k59`, log `.tmp/piston-payload-native.log`. Both driver
and payload piston kinds, both target power states, all driver directions and
both source parities are retained. The target points perpendicular to the
driver, with independently authored rear power, so its head does not itself
obstruct the driver. At GT+30 a native control independently verifies that the
target reached its requested extended/retracted state. Every frame retains the
target's original/pushed actor states and attached positions, its old/new head
cells, independent power and all driver movement cells.

The genuine pre-fix replay fails exactly 48 extended-payload cases (block
3.119 s), log `.tmp/piston-payload-before.log`; all 48 retracted controls pass.
There are no unmapped payloads. The immovable predicate now honors the live
piston State in addition to native component data. The post-change result is
recorded after the running verification completes. This covers settled target
states, not every handoff during target animation or arbitrary saved movable
flags on other actor types.


After the live piston-state gate, all piston/observer/moving-actor tests pass
(block 24.568 / session 1.820 s), log
`.tmp/piston-payload-after.log`. The 48 extended payloads are blocked and the
48 retracted controls move without losing their actors. A new broad run after
the material/Push/runtime-state changes is in progress and will be recorded
separately; the older broad failures above remain part of the audit evidence.


The broad run after native movability/Push/live-piston-state changes fails only
`TestAnimalsPanicWhenHurt/minecraft:camel` (entity 209.230 s): maximum panic
movement is 0.130 blocks/tick against the test's 0.252 threshold. Block 82.185 /
world 17.419 / player 20.207 / session 3.988 / Overworld generation 85.839 s pass.
Log: `.tmp/piston-payload-full.log`. The command retains only the two historical
exclusions; no camel/AI assertion or implementation was changed. An isolated
repeat is running to characterize this failure; its outcome will be appended
without treating it as a replacement for the failed broad run.


Twenty isolated camel panic repeats reproduce one failure (entity 11.800 s),
log `.tmp/piston-camel-panic-recheck.log`: 0.240 blocks/tick against the same
0.252 threshold. This characterizes variability without establishing its cause
or superseding the broad result. No additional test exclusion is introduced.


### Supported fragile payloads and a movable candle control

The new `popped` suite independently places a payload before extension and
inserts one ahead of the settled extended head ten GT after power-on. The
native script verifies that the head is extended at insertion, retains the
pre-insertion destination and exact inserted permutation, then records
retraction. Four horizontal directions, both piston kinds, both source parities
and seven payloads produce 112 traces / 11,200 frames. Six payload types are
Popped; candle is an explicit PushPull control, already identified by the native
component export. The native rule `dotiledrops=false` is matched by the replay's
`block_drops=false`; these traces compare blocks/actors, not dropped items.

An initial capture incorrectly required every extension destination to become
air. It rejected the candle control because BDS moves candles. The corrected
probe retains the actual pre-insertion block instead of assuming a classification.
A subsequent replay exposed an authored unknown-facing torch without appropriate
kernel support and missing repeater/comparator legacy aliases. These are not used
as production failure evidence. The canonical capture authors a top-facing torch
with floor support and compares both diode direction aliases against their real
facing. It comes from `adamant-redstone-bds-hvjc66iu`, log
`.tmp/piston-popped-native-supported.log`.

`go build ./...` passes after the movability/live-piston changes, log
`.tmp/piston-payload-build.log`. The supported fragile replay and its next
correction are recorded below when verified. The source and destination supports
are authored in both worlds; dropped-item behavior and other support/placement
contexts remain open.


The corrected supported pre-fix replay fails exactly 48 sticky Popped-payload
cases (block 3.266 s), log `.tmp/piston-popped-before-valid.log`. There are no
unmapped payloads or unvalidated legacy aliases. The six fragile block types
are deleted at retraction, while BDS leaves them in front of the contracted
piston. Normal-piston controls and the candle PushPull control pass.
The initial alias helper used shortened diode type names and failed to compile;
those were corrected to the actual RedstoneRepeater/RedstoneComparator types
before the valid replay. That compile failure is not production evidence.

The walker now excludes a Popped root payload from a sticky pull, alongside
Push-only payloads. This matches the native initial incoming edge at
`0x4ae46f5..0x4ae4705`; it does not suppress destruction when an adhesive branch
actually forces a fragile block forward. The new targeted verification is
running; its completed outcome will be appended separately.


After the root-Popped correction, selected piston/observer, redstone/comparator/
torch, sculk/vibration, tripwire/producer and native sound tests pass (block
49.539 / world 2.892 / player 11.582 / entity 3.299 / session 1.818 s), log
`.tmp/piston-popped-targeted.log`. All 112 supported fragile/candle traces are
included. This run does not verify their dropped items; a separate original-BDS
capture enables tile drops and records first-observed item identity/counts
without treating item fall/despawn as a second spawn.


### Popped drop identities, quantities and first-observed ticks

The separate `popped_drops` suite enables native tile drops, retaining each
first-observed item entity ID, stack identity/count, position and tick. Counts
are cumulative by distinct native entity, so falling or despawning is not
misreported as another spawn. Each batch clears prior items and positive
controls reject pre-input drops or a missing fragile drop. Candle remains a
no-drop PushPull control. All 112 cases / 11,200 frames are retained from
`adamant-redstone-bds-xkw7bqdn`, log `.tmp/piston-popped-drops-native.log`, fixture
`bedrock_1_26_51_piston_popped_drops.json`. The same controls were independently
checked against the retained capture before its replay.

Native redstone wire, repeater, comparator, torch, lever and stone button each
produce one corresponding item in all sixteen cases for that type; candles
produce none. The real kernel drop factory is observed with inert test bodies
rather than replacing the block logic. Both block transitions and cumulative
stack identities/counts match every native sample, including the observed
first-appearance frame (block 3.300 s), log `.tmp/piston-popped-drops-first.log`.
This verifies the retained deterministic stack/drop clocks. It does not compare
item motion, pickup, merging, random loot tables, dropped NBT or client spawn
packet timing, and no such equivalence is claimed.


### Same-transaction piston removal completion

A further inspected native path refines the earlier next-frame interruption
observations: PistonBlockActor::onRemoved at `0x4ae4f80` normalizes NewState to
the endpoint, then calls `_spawnBlocks` (`0x4ae3930`) on the server. The native
restoration completes inside the replacement transaction, before the next actor
tick. The earlier 384-frame-series test observed only the next GT and therefore
could not distinguish that from the orphan-tick fallback.

The interruption probe now additionally records the complete block/actor/front
snapshot immediately after the command returns. The canonical fixture is
refreshed from `adamant-redstone-bds-qidnf35c`, log
`.tmp/piston-interruption-native-immediate.log`, with all 384 controls and
38,400 ordinary frames plus 384 immediate observations. BDS restores carried
stone/chests immediately, preserving all inventory contents, while the detached
extended head is still present until its neighbour update. No immediate-head
removal is invented.

With only the tick fallback in place, the stronger replay fails 288 immediate
cases (block 4.732 s), log `.tmp/piston-interruption-immediate-before.log`.
Piston now implements the existing block-removal lifecycle callback and restores
its carried blocks in native movement order before replacement completes.
Normal completion shares that restoration helper. MovingBlock retains its
independent orphan fallback for missing-parent actors. Same-piston permutation
updates retain the current lifecycle; different-family and additional authored
replacement/rotation cases remain open. Verification is running and its result
will be appended separately.


The immediate-removal replay and all piston/observer/moving-actor checks pass
(block 26.097 / session 1.833 s), log
`.tmp/piston-interruption-immediate-after.log`. Immediate chest inventories,
base replacement, delayed head cleanup and subsequent complete frames are
included. Source changes remain uncommitted while the audit continues.


### Head loss, survival notification and queued recheck

Client `PistonBlock::canSurvive` at `0x6eb36f0` accepts its normal/sticky head
identifier, retraction, or zero-fraction animation progress no greater than
binary32 0.0001 (constant at `0xe75d7c4`). The progress expression observes
LastProgress, not the newly advanced Progress. `neighborChanged` at `0x6eb3940`
checks only a change at the extending face and queues one GT when survival
fails. `PistonBlock::tick` at `0x2a4d6e0` rechecks survival before destroying the
base; the queued call can therefore become harmless if the head is restored.

The interruption probe's head target adds 384 cases / 38,400 frames and 384
immediate observations. Normal-piston retraction without a head is an explicit
negative control. The native outcome depends on the interruption phase: all 96
second-frame extension removals destroy the base; first-frame extension cases
retain it. A stone replacing the collision head is also cleared when a normal
piston later retracts. Retraction tolerates a removed moving payload rather than
inventing a surviving carried block.

The initial head replay compared an unrecorded Attached field against an empty
slice and is rejected. The probe now records native attached positions in every
ordinary and immediate actor snapshot, and the test rejects missing observations.
Both base and head captures are refreshed completely: base
`adamant-redstone-bds-xu47z6xz`, log `.tmp/piston-interruption-native-attached.log`;
head `adamant-redstone-bds-8moc6p1n`, log
`.tmp/piston-head-removal-native-attached.log`. The corrected pre-fix replay
passes base interruption and fails 120 head cases (block 7.928 s), log
`.tmp/piston-head-removal-before-valid.log`: 96 missing base destructions and
24 uncleared foreign-stone normal retractions. No missing native attached data
is treated as a production mismatch.

Piston now samples survival during the existing synchronous notification hook,
queues a one-GT update when invalid and rechecks on that queued tick. Retraction
clears the head cell except for its own active moving block. The old unconditional
post-completion two-GT electrical poll is removed: movement already polls its
scene consumer every actor tick, and a stray completion tick would wrongly
interpret a tolerated first-frame head loss as a later structural check.
The first combined replay of these changes is running; its result follows.


All piston/observer/moving-actor checks pass after the head-survival and
retraction-clear changes (block 43.287 / session 1.841 s), log
`.tmp/piston-head-removal-first.log`. This includes the 768 refreshed base/head
interruption traces with full attached-block observations and all earlier
complete piston fixtures. A broad run after these lifecycle changes is still in
progress. Actual player destruction is captured separately from command-based
head/base replacement.


The broad run after same-transaction completion and head-survival/clearing
changes passes with exactly the two previously documented exclusions:
block 141.115 / entity 299.687 / player 29.704 / session 4.955 / world 25.607 /
Overworld generation 153.673 s. Log: `.tmp/piston-head-removal-full.log`.
This run overlapped independent reference-server captures and is slower than
earlier broad runs; it is not used as a performance benchmark. The historical
failed broad runs remain documented, and no new exclusions were added.

### Actual player destruction of piston bases and heads

The player probe captures 24 creative and 24 survival cases, covering both
piston families, all six directions and both base/head targets. These use real
client PlayerAuthInput destruction actions, native before/after player events,
absolute clocks, complete subsequent block/actor snapshots and actual client
LevelEvent destruction particles. Creative capture: `adamant-redstone-bds-ugfq7_ml`;
survival capture: `adamant-redstone-bds-h8trayb9`. Fixtures retain all received
level events; replay checks type 2001 positions, block hashes and ordering.

The initial instant survival request was rejected by BDS, so it is not evidence
of a survival destruction result. The client driver now holds mining for twenty
input ticks, with ContinueDestroyBlock actions before PredictDestroyBlock and
an abort on the next input. An intermediate helper build had an int/int32
action mismatch and was corrected before the accepted capture. All 24 final
survival cases have an accepted native player-break result. The creative
fixture's level events were recovered from that same capture's retained raw
client log; the probe now writes them directly on reproduction.

Client PistonArmBlock::playerWillDestroy at `0x6eb2230` removes any normal or
sticky piston at the head's rear position for a creative player, emitting the
base's destruction event before removing it. The normal player path then
destroys the head. Survival does not take this branch: its head is destroyed
first and the base is removed by the queued survival check. The original replay
passes all 24 survival cases (player 3.140 s) but fails twelve creative head
cases on particle order (player 3.053 s); logs:
`.tmp/piston-player-break-survival-before.log` and
`.tmp/piston-player-break-creative-before.log`.

PistonHead now implements the existing player pre-destruction callback for the
creative branch. All 48 real-player replays pass (player 3.833 s), log
`.tmp/piston-player-break-after.log`. Generic block-destruction game-event
ordering remains separately open: this particle result does not establish
that broader player lifecycle ordering matches the client.

### Head survival, direction and state-dependent queued cleanup

PistonArmBlock::canSurvive at `0x6eb0740` requires a piston actor at the rear
position, matching facing, and either State 1 or State/NewState 2/2. It accepts
either normal or sticky base regardless of the head identifier. The inspected
neighborChanged (`0x2a496f0`) queues a one-GT recheck only when invalid;
queued tick (`0x6eb06b0`) rechecks and silently clears an invalid head.

The original-BDS head probe adds 720 cases / 72,000 complete frames, including
both piston families, six directions, both setup parities, retracted/expanded
and both observed extension/retraction frames, adjacent notifications, head
family/facing edits, live base rotation and same-transaction head repair.
Before/after injection snapshots include full actor attached positions.
Capture: `adamant-redstone-bds-q57e43_a`, log
`.tmp/piston-head-survival-native.log`.

The old deferred head check fails 168 cases: 72 opposite-family heads,
48 head repairs, 24 ordinary notifications and 24 facing edits (block 7.783 s),
log `.tmp/piston-head-survival-before.log`. Head survival now follows the native
state/facing gates and samples invalidity synchronously before its queued
recheck. All 720 traces pass (block 7.975 s), log
`.tmp/piston-head-survival-after-first.log`. Combined piston, observer, moving
actor, queue and player replay checks pass (block 49.240 / player 3.945 /
session 2.729 s), log `.tmp/piston-head-survival-targeted.log`.

### Paused checkpoint and next piston entity work

At the user's request, audit work stops here on 2026-10-27. The effort estimate
remains approximately 60%; no coverage-matrix row is declared complete.
The completed piston fixes above and their passing regression traces are saved
separately from the following unfinished comparison.

The entity probe adds two passive, gravity-free behavior-pack bodies, with
explicit half-block collision boxes, pushable/piston-pushable flags, no AI,
and collision enabled/disabled controls. The fixture retains their complete
definitions. They isolate the original BDS piston/movement paths; these bodies
do not establish compatibility for vanilla mob AI or all entity types.

An exploratory 32-case capture (`adamant-redstone-bds-midup5_b`, retained locally
in `.tmp/piston-entity-smoke.json`) covers upward/east pushes with empty heads,
stone, honey and slime payloads. Native head contact moves the body by the
intersection depth rather than a fixed half block; carried-block contact has
a different observed phase. Slime also supplies a velocity impulse, so its
subsequent motion requires separate entity-physics evidence. No piston entity
production fix or passing replay is claimed at this checkpoint.

The checked-in displacement fixture has 576 cases / 57,600 complete frames:
both piston families, six directions, both input parities, empty/stone/honey
payloads, two body distances, collision controls and pre-extension/pre-retraction
spawns. Capture: `adamant-redstone-bds-viktm4vx`, log
`.tmp/piston-entity-displacement-native.log`. The reference capture is complete,
but no Go replay has been implemented yet. Slime is deliberately kept outside
this displacement-only fixture because its native velocity/free-flight path is
not yet reproduced. Next: implement the controlled body replay, inspect client
`PistonBlockActor::_moveCollidedEntitiesHelper` (`0x4ae3e00`),
`moveEntityLastProgress` (`0x4adde80`) and MovingBlockActor contact shapes
(`0x4add8b0`), and correct verified displacement/phase differences. Real player,
mob, dropped-item, honey carrying, slime velocity and obstruction cases remain
open.

The experimental all-permutation movability probe is also retained as unfinished
tooling. Its latest local capture stopped after 72 block types with an
unestablished cause; it is not used as proof that every state was checked.
The canonical default-type movability fixture and its 1,472 readable native
classifications remain the evidence for the generated table.

Checkpoint validation: the final `go build ./...` passes, log
`.tmp/piston-paused-build.log`; all thirteen added/updated Python capture and
generation scripts compile. `git diff --check` passes. The latest scoped tests
are the passing block/player/session run recorded above. The latest broad run
predates the final creative/head-survival changes and passes with exactly the
two historical exclusions already documented; no newer broad pass is claimed.

### Resumed piston/entity displacement comparison, 2026-10-28

The worktree was rechecked at published checkpoint `06d34e78` before resuming.
The 576-case native displacement capture contains another mechanism that the
previous checkpoint had not separated: all 144 collision-enabled bodies spawned
inside an already extended block acquire native escape motion/velocity. Those
cases remain retained and open for a physics replay; they are not silently
treated as stationary bodies. The direct passive replay covers the other 432
complete traces (both collision settings for pre-extension spawns, collision
disabled for pre-retraction spawns). It checks absolute ticks, piston state and
attached positions, base/front/source permutations, entity position/velocity
and full body AABB on every frame. Bodies have no gravity, AI or autonomous
motion in these cases. No captured entity positions are injected during replay.

Before production changes, 348 of those 432 traces fail (block 4.978 s), log
`.tmp/piston-entity-displacement-before.log`: all 288 extension cases and sixty
sticky retractions. The zero-motion controls remain useful negative cases.

Inspection of `_moveCollidedEntities` (`0x4ae2bb0`), its helper (`0x4ae3e00`),
MovingBlockActor::moveCollidedEntities (`0x4adc6a0`) and wrapped shape queries
(`0x4add8b0`) distinguishes these paths:

- Contact moves an entity by the binary32 overlap depth along the selected
  axis, rather than a fixed half block. Different collision objects may act on
  the same entity; there is no global once-per-piston-pass deduplication.
- Normal-piston retraction performs no collided-entity movement. A sticky
  piston's carried block moves bodies in the retraction direction, while the
  head collision helper still pushes along the original facing direction.
  The head's world box also follows facing, not the reversed pull vector.
- Wrapped block shape queries floor binary32 positions at LastProgress and
  Progress and enclose the resulting world AABBs. They do not continuously
  translate each shape by fractional animation progress. This accounts for
  the different first contact phase on positive/negative axes.
- Bedrock honey collision has horizontal 1/16 insets and full height, as
  established by the original BDS contact traces in all six directions. The
  client's HoneyBlock::getCollisionShape at `0x66ba020` reads runtime AABB
  bounds; those runtime constants were not recovered from static PE data.
  No Java 15/16-height shape is substituted for the BDS observations.

Piston contact processing now follows those paths. HoneyBlock's physical model
uses the observed inset/full-height box. MovingBlock's ordinary physical model
shares the native enclosed world shape; missing parents use the unshifted
wrapped shape, and a missing/nested wrapped moving block uses the native unit
box fallback. Existing face-support behavior is retained pending separate native
support evidence; these shape fixes do not close that coverage.

The first native displacements pass after the head/contact/honey corrections
(block 5.292 s), log `.tmp/piston-entity-displacement-after-honey.log`.
The additional executed-client probe runs the original full 0x505-byte wrapped
shape function, with supplied parent/direction, wrapped AABB and floorf callbacks.
It records both original query cells and both original world AABBs. The 1,152
cases cover six faces, both movement directions, present/absent parents, four
progress pairs, full/inset/partial authored shapes and positions at zero,
positive/negative one million and near thirty million. Shapes supplied by this
probe are explicit inputs, not evidence of a vanilla material's own shape.

Both the piston contact bounds and ordinary MovingBlock physical boxes match
the executed function, including binary32 coordinate rounding. The combined
piston/observer/moving-block/queue/player/honey replay passes
(block 35.358 / player 3.149 / session 1.946 s), log
`.tmp/piston-entity-targeted.log`. A new broad run is in progress after these
shared geometry changes. Real armor-stand/item contacts and slime velocity are
being captured separately; no successful vanilla-entity replay is claimed yet.

The new broad run finishes with one failed test, TestVillagersRunFromZombies
(entity 209.882 s), log `.tmp/piston-entity-full.log`. Block 92.734 / player
19.029 / session 3.810 / world 16.657 s pass. The two historical exclusions
remain unchanged. Twenty isolated runs of the failing villager test yield one
failure (entity 10.954 s), log `.tmp/piston-villager-panic-recheck.log`; this
establishes intermittency on the current worktree, not its cause or a clean
baseline result. Its assertions and implementation were not modified, and it
was not added to the exclusion list. The recovered "teleport panic" log belongs
to the existing panic-recovery test and is not an additional failed test.

Original BDS now also supplies 512 real armor-stand/dropped-item traces on a
stone floor (`adamant-redstone-bds-0mzrl5yg`), including slime, and 256 traces
with a forward obstruction (`adamant-redstone-bds-wgm7fihv`). These are retained
in the new vanilla-entity fixtures. Their replay uses the real entity types and
ordinary tick/movement paths; no observed trajectory is injected. The first
attempt lacked the Script API's stone_type alias projection and is rejected as
a complete production before comparison. That alias is now checked against an
actual nonsmooth Stone, and a corrected replay is running. Item escape motion,
slime depenetration and obstruction clipping remain uncorrected at this point.

#### Corrected vanilla fixture setup and further movement evidence

The initial real-entity floor captures are superseded. Adhesive payloads also
move a row of floor blocks, leaving stone one cell beyond the old four-block
cleanup radius. Reusing that cell in a later batch could therefore alter the
initial world and subsequent attached-block list. The affected before/after
logs are not accepted as complete production comparisons.

Vanilla cells now start at chunk offset eight, clear radius six, and explicitly
validate all 338 cells in the two relevant height layers against the authored
initial scene, including the surrounding air. All 64 interior chunks must be
loaded before setup; an attempted refresh that observed an unloaded interior
cell is rejected. Every replay requires those native setup controls.
The canonical fixtures are refreshed completely: 512 ordinary cases from
`adamant-redstone-bds-d2hse5pa` and 256 obstruction cases from
`adamant-redstone-bds-9hrp_2a7`. The old raw captures remain in their original
temporary roots for provenance.

Client `_spawnMovingBlock` calls MovingBlockActor::moveCollidedEntities at
`0x4ae0dfa`, immediately after creating each moving actor. This is separate
from the next half-step contact pass. Its horizontal top-surface query is 0.05
blocks high and carries eligible bodies by half a block, tracking them only
within that moving actor's contact pass. That callback and carry path have
been added. Entity piston moves now resolve against terrain instead of using
the command-style unchecked Move path. All 128 armor-stand obstruction traces
pass with those changes; the retained passive 432 cases still pass.

Native moveEntityLastProgress (`0x4adde80`) temporarily resolves movement with
Progress set to LastProgress. The kernel supplies that read view through a
BlockSource override rather than changing the live actor/scene clock. This
does not yet establish complete actor-movement/depenetration equivalence.

The original client's head collision table builder (`0x6eb1020`) and complete
addCollisionShapes body (`0x6eb0ac0`) now execute with supplied block-state,
rear-actor and direction callbacks, thread-guard bookkeeping and the original
AABB constructor. The six three-part tables and 192 complete world queries
are retained in the shape fixture. They include missing-parent controls,
both animation directions and binary32 collapse of shapes near thirty million.
The head's physical model now matches those three moving collision parts,
rather than a full cube. Parent absence produces no collision. The six tables,
192 head queries, 1,152 moving shape queries and 432 passive displacement cases
pass together (block 6.297 s), log
`.tmp/piston-vanilla-clean-after-head-shapes.log`.

The clean real-entity replay still fails 292 of 768 cases at this checkpoint:
144 ordinary item, 124 obstructed item and 24 ordinary armor-stand cases.
The latest previous-progress read-view attempt has not closed those cases;
log `.tmp/piston-vanilla-clean-after-previous-progress.log` (entity 24.126 s).
These remain ordinary failing regressions while the implementation continues,
not skipped coverage or a claim of completed entity compatibility.

A separate original-client probe now executes AABB::clipCollide (`0x3327d80`)
and SweptMovement::computeMoveWithDepenetration (`0x82a4890`) with authored
body boxes, movement spans, obstacle lists and explicit per-axis penetration
limits. The 2,072 cases include ordered multi-move/multi-obstacle inputs,
empty controls, touching/overlapping/separated boxes and large coordinates.
The Go arithmetic port matches both full ClipCollideResult fields and full
MoveResult fields (entity 2.710 s), log `.tmp/piston-native-sweep-after.log`.
The initial port had eighty-one incorrect unused clipped-vector fields for
separated boxes; its full sweep outputs already matched, and those fields are
now corrected too. This primitive is not yet wired into ordinary entity physics:
native caller flags, penetration limits and ItemActor::postNormalTick's
Actor::pushOutOfBlocks path still need to be reproduced before integration.

#### Paused checkpoint, 2026-10-28

The user requested commits and publication before continuing tomorrow. The
overall effort estimate remains approximately **60%**; none of the 22 coverage
rows is closed by the narrow piston checks above. The ordinary real-entity
regressions intentionally retain the 292 known mismatches. This checkpoint is
unfinished audit work, not a passing full-suite or compatibility release.

The new item-escape probe executes the original complete
Actor::pushOutOfBlocks (`0x1a1e250`, 0x8bc bytes) with supplied world and actor
inputs. Its 1,216 cases are preserved in
`server/block/testdata/bedrock_1_26_51_piston_item_escape.json`. The native body
chooses escape motion, depenetration bit 3 and status flag 48 (HasCollision).
Neighbour solidity, material bits, shapes and height offset are explicit
inputs; optional ECS components are absent. The fixture does not establish
vanilla material flags or the caller's physics policy, and no Go escape port
or runtime integration is claimed.

Continue by inspecting the caller of computeMoveWithDepenetration around
`0x82d4710` (calls at `0x82d4b2a` and `0x82d4b88`) and the item post-tick path
at `0x3348110` (pushOutOfBlocks call at `0x334893a`). Establish the actual
per-axis penetration limits and component flags before wiring the verified
sweep arithmetic into entity physics. Then resolve the remaining ordinary
armor-stand cases and item escape/obstruction cases against the retained clean
native captures. Re-run relevant checks after integration; the earlier broad
run predates the last movement changes and is not a passing current baseline.

Final checkpoint validation reruns the passive displacement, moving/head shape
and sweep arithmetic tests together: block 6.468 s and entity 1.056 s pass.
`go build -o .tmp/adamant-audit-checkpoint.exe .` also succeeds, verifying the
root server entry point. The four Python capture scripts pass py_compile.
These checks do not supersede the 292 known real-entity failures or the earlier
intermittent villager failure. The production geometry changes are committed
in `c51c1fd4`; the tested, runtime-unintegrated sweep primitive is in `d37b4d2d`.

#### Resumed item physics, 2026-10-28

The user reactivated the full goal. The previous turn published the requested
checkpoint; that was progress, not completion of the audit. Work remains on the
original 22-row matrix, with the overall effort estimate still approximately
60%. The following changes remain uncommitted pending a new commit request.

The item-escape input names in the earlier probe needed correction: BlockType
`+0x164` bit 2 is **mPushesOutItems**, not mSolid. The word `solid_neighbours`
in the fixture means that same item-escape flag on each neighbour. BlockType
`+0x130` is **BlockProperty**, not Material; its bit 0 is Stair. The original
function chooses 0.1 escape speed for stairs and 0.25 otherwise. These labels
are now explicit in the capture metadata and the Go port. The expanded 1,600
executed-client cases include partial/multiple shapes, nonintersecting controls,
nonzero initial velocity and different height offsets; all result velocities,
stuck bits and collision flags match.

A read-only owned-BDS probe additionally reads all 1,477 registered native
BlockType objects. It validates their runtime vptr, namespace, full name and
raw name. There are 970 types with mPushesOutItems (including 97 stair types)
and 507 without it. The generated item lookup uses these observed flags:
piston heads and moving_block do not initiate item escape even though they
have collision shapes. The fixture is
`bedrock_1_26_51_entity_physics.json`, and the generator retains its digest.
Unknown/custom names are not classified as observed vanilla types.

ConfigureDepenetration is the original leaf body at `0x9027730`, identified
through the ConfigureDepenetration system registered at `0x681b0e5`. It selects
the original constant vectors at `0xe6d7d88` (1,1,1) and `0xe6d8608` (0,0,0).
Stuck-item bit 3 selects ones; otherwise a nonempty one-way block list or bits
0/2/4 selects zeros. Per-axis component minima and temporary overrides are
then applied. Another 576 executed-client cases independently validate this
policy, including every bit combination, both list states and different
minima/overrides. ActorMoveSystem calls the sweep with Y, X, Z movements;
UpdateDepenetration at `0x901c270` remembers consecutive penetrating moves.

Dropped items now use the checked sweep and caller policy, item escape before
gravity, consecutive-penetration state, and the native resting-item movement
gate (age modulo four, on-ground and squared pre-gravity speed <= 0.0001).
Entity piston pushes share the item's collision state and previous-progress
block-source view. The item path filters collision shapes against its movement
query; complete native shape-fetch caching, ordering and other actors' movement
policies remain open. The 768 complete-world traces remain the integration
gate; primitive matches do not close this work.

Owned-BDS heap observations establish that a fresh item spawned inside stone
already has penetration bit 1 set at age zero while its requested spawn origin
is retained. Actor::_doInitialMove (`0x19f1030`) invokes move with a zero vector.
The kernel seeds that collision state when opening the item for the first time.
The refreshed heap capture uses exact ActorUniqueID component matches
(`0x18f957af`), not coordinates alone. The four checkpoints are retained from
`adamant-redstone-bds-q6egzkll`. Live state/body/component bytes are checked for
stability, and native/script position agreement is recorded explicitly.

Additional moving-item experiments exposed a phase distinction: a live actor
may advance while a Script API snapshot still reports the preceding state.
The earlier held-callback assumption alone is therefore insufficient evidence
for moving entities. Failed five-entity captures are rejected; heap bits are
compared only when age and position agree with the Script API frame. Immutable
block flags and the matching fresh-stone checkpoints remain valid evidence.

Item forces and positions now retain binary32 arithmetic. Native movement also
preserves the actual AABB across ticks and derives position from its rounded
centre. Reconstructing a fixed-size box from position loses asymmetric bound
rounding, visible by the second air step. The separate stable Script API
capture `bedrock_1_26_51_item_motion.json` from
`adamant-redstone-bds-innpqga_` supplies three air and three landing steps.
Those position/velocity/on-ground checks use exact equality with BDS. The old
double-precision scalar expectations were replaced with these independent
observations; the partial-hopper identity check now compares motion with an
uncollected control item rather than asserting a double-precision trajectory.

After spawn-state and binary32 corrections, the complete native entity replay
has 116 failing cases instead of 292: 18 ordinary item, 74 obstructed item and
24 ordinary armor-stand cases. Log `.tmp/piston-vanilla-after-native-float.log`
(entity 20.176 s). This count predates the final persistent-AABB refinement;
its complete replay is being checked separately. Item/shape/policy/sweep and
existing item-interaction checks pass after that refinement (block 5.263 /
entity 5.057 s), log `.tmp/piston-item-native-physical-box.log`.

Next evidence targets include honey's side-slide eligibility, obstruction
contact/carry ordering, the 24 armor-stand floor/carry failures, full native
shape-fetch ordering/caching, and interactions outside these retained scenes.
The client HoneyBlock::triggerSlidingDownEffects (`0x66ba110`) has direct callers
at `0xc60095a`, `0xc600bda` and `0xc6010a5`; the current Go honey side-slide path
still produces a -0.05 item velocity in cases where BDS reports -0.0392.

The final persistent-box replay retains the same 116 failures, without changing
the native fixture or its tolerances: entity 17.153 s, log
`.tmp/piston-vanilla-after-persistent-box.log`. All 128 armor-stand obstruction
traces still pass. The exact three air and three landing steps, fresh-stone
spawn checkpoints, all item-interaction tests selected by the item name filter,
1,600 escape cases, 1,477 flag entries, 576 policies, 2,072 sweep cases and the
block piston shape/passive-body checks pass together (block 5.263 / entity
5.057 s), log `.tmp/piston-item-native-physical-box.log`.

The shared piston/observer/moving-block/queue/honey/pressure-plate/player/session
checks also pass (block 40.084 / player 4.046 / session 2.814 s), log
`.tmp/piston-item-shared-regressions.log`. The root server builds successfully
as `.tmp/adamant-item-physics-check.exe`. These targeted results do not replace
a new broad suite run, and the ordinary 116 failing entity traces remain.

For the next honey comparison, the eligibility code at `0xc600968..0xc6009ce`
checks actor-origin Y against block Y + 0.9375, downward velocity, and horizontal
distance against half the stored body width plus binary32 0.43125
(`0xe98a5cc`). The current Go fixed 0.4375 threshold omits the body width.
This is inspected eligibility for the sliding-effects call; the matching
velocity policy and full actor path must still be checked before porting it.

#### Honey and armor-stand continuation, 2026-10-28

The original honey-contact body at `0x63af270` is now executed independently
with supplied contact vectors, actor origin, stored collision width, velocity
and fall distance. Its 10,752 cases establish that every inside contact damps
horizontal velocity by binary32 0.4 and clamps downward velocity to -0.12.
The height/edge predicate controls fall-distance reset, not velocity damping.
The original HoneyBlock::onStandOn body at `0x66ba420` supplies another 144
cases: unless sneaking or moving upward at least 0.1, its horizontal factor
is abs(vertical velocity) * 0.2 + 0.4. Both original bodies, including their
binary32 arithmetic, are reproduced in the honey block implementation.

The new `bedrock_1_26_51_honey_contact.json` retains those primitive results.
The separate BDS Script API capture `bedrock_1_26_51_honey_entities.json`
retains 124 real-entity scenes from `adamant-redstone-bds-54xrgo9_`: 60 side
contacts per entity type and four top-landing scenes, each with twenty frames.
These observations come from BDS, not from the Go implementation. All 62 item
scenes and 60 of 62 armor-stand scenes pass. The two remaining top-landing
armor-stand scenes first diverge on the second frame's horizontal friction.
The current previous-on-ground/block-below sample sees honey even while the
actor is airborne above it; native support/friction selection remains open.

Armor stands now preserve their collision AABB and depenetration state across
ordinary travel and piston displacement. Their movement uses observed mob
travel ordering: move with existing velocity, then apply gravity 0.08 and
vertical drag 0.02, with binary32 horizontal friction. Initial zero movement
seeds collision state while preserving the spawn origin. These changes
resolve the side-contact armor-stand traces; they do not close the outstanding
piston carry or support/friction work.

ScriptActor::getVelocity (`0xbd8ac30`) returns a separate last-travel component
for these mob actors when present, rather than StateVector's next velocity.
The real-entity comparison now projects the armor stand's last ordinary travel
delta accordingly. A piston displacement is separate from that travel delta.
This corrects the observation mapping without changing the native fixtures,
position assertions or tolerances.

The 768 retained piston scenes now have 108 failures: 10 ordinary items,
24 ordinary armor stands and 74 obstructed items. This is down from 292 at
the published checkpoint and 116 after the item-only corrections. Together
with the two honey top-landing failures, 110 regression scenes remain open.
They remain normal failing tests. Next work should establish native support
selection for initial airborne stands, then contact/carry order and collision
shape-fetch ordering for the remaining piston failures. All 22 original audit
matrix rows remain open; these narrow results do not establish full parity.

#### User-requested pause and publication, 2026-10-28

The user requested that the current changes be committed and pushed before
continuing tomorrow. Stop implementation at this checkpoint. The overall
effort estimate remains approximately **60%** of the audit scope, not a measured
vanilla compatibility percentage or a passing-test percentage. Preserve the
known failures and the independent captures for the next session. The earlier
full-suite run and intermittent villager failure have not been superseded by
a new passing full-suite run.

Checkpoint validation confirms the root server builds successfully as
`.tmp/adamant-audit-pause-2026-10-28.exe`; the root entry point remains usable
with `go run .`. All seven added/modified Python scripts pass py_compile, and
`git diff --check` passes. The combined item/honey/sweep/depenetration and
piston body/shape regression selection passes in the block package (6.933 s)
and fails in the entity package (20.724 s) on the same 110 known scenes:
two honey, 34 ordinary piston and 74 obstructed piston scenes. Log:
`.tmp/audit-pause-2026-10-28-tests.log`. No new implementation changes were
made after this validation. Private player/permission files and `tmplog.txt`
are excluded from the commits.

#### Resumed support and collision-fetch audit, 2026-10-28

The user reactivated the goal and explicitly requested continued work until a
new pause instruction. The preceding publication checkpoint is historical.
No new commit or push is authorized by that continuation alone.

Ten additional original-BDS honey scenes bracket the first-travel friction
boundary at 0, 0.0625, 0.099, 0.101 and 0.25 blocks above honey, with both
downward and upward initial motion. The complete 134-scene capture from
`adamant-redstone-bds-nojwi96n` retains the earlier 124 scenes as well. Native
horizontal damping changes between 0.099 and 0.101 even for initially airborne
armor stands. The original ground-travel bodies (`0x66588d0`, `0x666cee0` and
`0x666f530`) sample floor(AABB.minY - binary32 0.1), with position X/Z. The
kernel now uses that sample instead of floor(position Y) - 1. All 134 honey
scenes pass in the combined replay `.tmp/piston-after-honey-friction-boundary.log`.

The original MovingBlockActor contact body also checks AABB.minY against the
old top surface (`0x4adcb3d..0x4adcb46`); mere intersection of a tall actor with
the top query slab does not establish a rider. That missing lower-bound check
is now present. It alone did not resolve the remaining piston scenes.

A temporary single-scene trace identified the actual support loss: a stand
carried to Z=43.5 stood over the moving shape stored at Z=44. The ordinary
quarter-block collision scan excluded that owner cell. On the next travel
tick the stand fell into the moving floor and was incorrectly pushed sideways.
The original BlockSource collision fetch expands integer query coverage by
binary32 one on both sides and traverses X, Z, Y. A separate native entity
collision collector now follows those bounds and that order, retaining the
normal shape intersection filter. This resolves the earlier ordinary stand
carry failures and reduces the 768-scene replay to 84 failures: eight slime
armor-stand, two ordinary item and 74 obstructed item scenes. Log:
`.tmp/piston-after-native-cell-fetch.log` (entity 18.555 s).

The new `redstone_native_collision_fetch_probe.py` executes the entire original
fetch traversal at `0x2d87730` with supplied loaded subchunks and empty shape
callbacks. An explicit GetCollisionShapeInterface disables world borders.
The 150 captured cases retain actual visited cells and order, including
negative coordinates, dimension bounds and binary32 collapse near thirty
million. They establish traversal, not geometry returned by arbitrary blocks
or native actor shape-cache behavior. The initial synthetic upper subchunk
allocation omitted the inclusive maximum-height cell and crashed; that probe
input was corrected before accepting the complete capture.

The original MoveCollisionSystem::fetchCollisionShapes body additionally
preserves support below the sweep: at `0x31c31cc..0x31c31fc` it encloses
AABB.minY + (-0.2 - abs(requested Y)) and AABB.maxY + 0.08. Keeping those
shapes prevents a horizontal piston request from depenetrating an item down
through a touching floor. This fixes the initial obstruction-frame positions;
it exposed a later item-escape phase mismatch instead of closing the traces.
Native auto-step/sneak envelopes and request shape-cache reuse remain open.

Level::_subTick calls the dimension tick through Level vtable slot 0x390 at
`0x1171981`, then tickEntitySystems through slot 0x398 at `0x11724de`. The Level
constructor installs vtable `0xe7a7aa0`, whose slots resolve to tickEntities
(`0x1177650`) and tickEntitySystems (`0x1177690`). The earlier mob-only phase
change still left the item obstruction cases failing after payload restoration.
Their BDS frames show item escape in the very tick the moving block becomes
stone. Item movement/post-normal-tick processing must observe that restoration
as well. The world now supports entity types requesting a post-block tick;
armor stands and dropped items use it. Their existing loaded-chunk, sleeping,
age and duplicate-tick bookkeeping remains in the same dispatcher.

Moving slime now writes its native contact impulse separately from positional
displacement, before the carried-actor exclusion. With support collection and
both entity phases corrected, all 256 obstructed scenes and all 256 ordinary
item scenes pass. Of the 768 piston scenes, eight armor-stand retraction/slime
scenes still fail; all are positive original facing axes. All 134 honey scenes,
item interactions, sweep/depenetration/cell traversal checks, the selected block
piston checks and world tick checks pass in the same run (block 35.419 s,
entity 22.153 s with those eight failures, world 1.747 s). Log:
`.tmp/piston-after-item-post-block-phase.log`.

A separate executed-client probe now retains 336 explicit fetched-candidate
side-contact cases from the complete MovingBlockActor body (`0x4adc6a0`).
It records requested movement and actual slime velocity writes; the supplied
move callback does not apply or resolve movement. Candidate inclusion is an
input, not evidence of native spatial selection. The raw fixture is
`bedrock_1_26_51_piston_side_contact.json`. It includes zero-distance velocity
reset and signed displacements for authored candidates outside the query.
Those outside-query inputs must not be treated as valid complete-world scenes.
The initial probe omitted the original half constant and crashed; only the
complete corrected capture is retained. Native actor movement, collision-state
updates and spatial selection remain to be reconciled for the eight failures.

The remaining eight failures were resolved by propagating collision clipping
back to the entity's stored velocity during a piston move. The previous code
discarded that velocity result: a later blocked piston request left the slime
impulse active for ordinary travel. Clearing each clipped axis fixes both the
position and observed travel delta without special-casing slime, facing or
armor stands. All 768 retained real-entity piston scenes, all 134 honey scenes
and the selected existing item-interaction tests now pass together (entity
19.235 s), log `.tmp/piston-after-push-velocity-clipping.log`. No native scene,
position assertion or tolerance was removed or loosened.

The first broad resumed `go test ./... -timeout 180s` run passes the block,
world, player, session and other completed packages, but reaches the entity
package timeout while starting TestPigsWanderWithoutCarrotsOnSticks. It also
contains the eight piston failures because it predates the final velocity
fix. That is an incomplete entity-suite run, not evidence of a pig failure or
a passing overall baseline. Log `.tmp/redstone-resumed-full-suite.log`.
The entity package is being rerun with a ten-minute allowance; shared packages
are also being checked against the final velocity change.

The next independent comparison expands to actual bodies riding on top of
moving blocks, rather than the retained side-contact scenes. The new
`vanilla_riders` capture suite authors push/pull spawn positions on the payload
top, four lateral offsets, stone/honey/slime controls, both piston families,
four horizontal facings and both setup parities for items and armor stands.
Passing the earlier 768 scenes does not establish those interactions, other
entity types, player movement, full auto-step or arbitrary redstone coverage.

Final validation of the support/phase/velocity changes passes the complete
entity package (223.195 s with a ten-minute allowance), plus complete block
(86.953 s), world (14.600 s), player (19.338 s) and session (4.227 s) packages.
Logs: `.tmp/redstone-resumed-entity-suite.log` and
`.tmp/redstone-resumed-shared-suite.log`. The other packages had completed
successfully in the preceding broad run. The root server builds successfully
as `.tmp/adamant-redstone-resumed-check.exe`. These validations predate the new
rider fixture/test; they do not establish that the rider scenes pass.

The BDS rider capture is complete: 768 unique scenes / 76,800 frames from
`adamant-redstone-bds-hmo2ex_k`, with all initial-environment controls checked.
It is retained as `bedrock_1_26_51_piston_vanilla_riders.json` and replayed by
the ordinary TestPistonVanillaRidersMatchBDS regression. Its first comparison
is being checked separately. The executed contact fixture also now contains
336 additional explicit top-candidate cases, bringing its total to 672. The
top inputs specify a fetched candidate for the top query and no candidate for
the side query. Container insertion/copy are supplied host operations, while
eligibility, impulse selection, carried-ID exclusion and requested movement
remain original client instructions. An intermediate callback exception run
was rejected; callback failures now prevent accepting a capture.

The first rider replay reported eight item/slime mismatches, but its world
capture is not an isolated oracle. A traced item leaving the scene at
Z=19.4376 hit the next scene's stone floor beginning at Z=20 in BDS; that
foreign floor is absent from the single-scene Go replay. Thus its stop at
Z=19.875 is explained by different terrain, not an established movement bug.
The rejected capture remains only in `.tmp/bds-piston-vanilla-riders.json` for
diagnosis. The new capture uses a four-by-four grid with 32-block spacing and
radius-14 initial-environment checks (1,682 cells per scene), instead of an
eight-by-eight grid with 16-block spacing. It preserves every authored rider
scenario and all 100 frames; no failing scene is removed. Its corrected
fixture is being captured and will replace the contaminated rider fixture
only after complete capture validation. The original 768 side/obstruction
scenes are unchanged.

Initial inspection for the next rail audit identifies another open path:
DetectorRailBlock::getComparatorSignal (`0x9783e50`) performs a typed minecart
query and reads container-component output. The kernel DetectorRail currently
has no comparator-signal implementation. Native comparator/contact queries
also use horizontal inset 0.2, whereas detectorRailHasMinecart uses 0.125.
These are inspection targets; no rail correction or world-level equivalence
is claimed until its own native capture/replay has been completed.

The isolated rider capture also needed a wider force-loaded simulation area.
Its first 32-block-spaced version (`adamant-redstone-bds-087zmej5`) left two
items crossing X=128: BDS then stopped ticking them outside the ticking area,
whereas the synchronous Go replay kept simulating them. This is another
environment mismatch, not a movement correction. Both rejected rider captures
remain in `.tmp` only. The accepted capture is
`adamant-redstone-bds-9hfkorv4`: 768 unique scenes, 76,800 frames, scene spacing
32, and 1,682 initial-environment control cells per scene. Its two ticking areas
cover [-32,-32] through [159,159], with all 144 chunks verified loaded; every
observed body remains inside that area. The rider regression requires this
simulation-bounds metadata. All original 768 side/obstruction scenes, all 768
accepted rider scenes, all 134 honey scenes, collision-fetch traversal and the
new settled rail-inventory check pass together (entity 33.948 s), log
`.tmp/piston-riders-loaded-and-rail.log`.

Detector-rail settled analog output is now captured independently in original
BDS: `adamant-redstone-bds-4qy6ra0w`, 15 scenes, 32 frames each, retained as
`bedrock_1_26_51_detector_rail_inventory.json`. Plain/TNT minecarts and an armor
stand are controls. Chest/hopper minecarts cover empty inventories, single
diamonds, full stacks, full inventories, stack-limit-16 ender pearls and
unstackable shields. The new regression first failed because DetectorRail had
no comparator-output provider. It now reads the first container minecart and
reuses the native binary32 container-strength calculation. In BDS, a full
diamond stack gives strength 1 in a 27-slot chest cart and 3 in a five-slot
hopper cart; completely full carts give 15. A powered rail with a plain cart
gives comparator strength 0. This regression compares settled output at the
recorded native position; it does not validate minecart travel or update timing.

Both native detector queries construct the same binary32 horizontal inset
0.2 and vertical range [Y,Y+0.8], independently of rail powered state for the
analog getter (`0x9783e50`; pressure query `0x9783660`). The kernel now shares
that query box. Inspection of typed BlockSource::fetchEntities reveals that
its final selection compares actor AABBs inclusively (`0x2d853c8..0x2d85447`),
whereas World.EntitiesWithin filters actor positions. Edge selection is being
captured separately before treating this path as reconciled.

The boundary capture (`adamant-redstone-bds-70unox8k`) retains 34 settled
plain/chest-cart scenes along a continuous rail at the detector's north/south
edges. The analog getter and forced scheduled presence resample now match all
49 inventory/boundary scenes together (entity 2.972 s), log
`.tmp/detector-rail-boundary-replay.log`. Selection uses inclusive actor-body
bounds, not actor origins. It also retains the native typed query's owner-chunk
restriction: a cart at negative Z across this query's chunk boundary is not
fetched, even when its body extends into the detector area. This is independently
visible in original BDS and the inspected typed fetch's chunk loop.

`redstone_native_detector_query_probe.py` additionally executes the entire
original analog getter with an empty supplied typed-fetch result. All 324 query
boxes and minecart-family filters are retained in
`bedrock_1_26_51_detector_query.json` and match exactly in the block regression
(2.848 s). Inputs cover negative coordinates, chunk edges, float32 precision
boundaries and coordinates near 30 million. The callback records the original
query only; it does not prove native spatial selection or container math.

The new actual-cart timing capture is complete:
`adamant-redstone-bds-_8ouijju`, 32 scenes / 3,200 full source/wire/lamp frames,
both setup parities, plain/chest carts, dwell lengths 1,2,3,8,19,20,21,30.
Native rail activation is observed one frame after spawn. The next presence
check is 20 ticks after activation; a cart removed just after that check holds
power until the following check. The kernel's separate minecart callback
currently requests one-tick checks instead; its regression is being run before
changing that path. No timing-equivalence claim is made yet.

The timing regression reproduced mismatches in all 32 retained traces. The
legacy MinecartBehaviour callback bypassed DetectorRail and requested one-tick
resamples. It now delegates to the rail's ordinary contact path. That path
ignores already-powered contacts, as native entityInside does, leaving the
scheduled 20-tick presence check in control. All 81 settled/timing cases now
pass together (entity 4.333 s), including every recorded wire and lamp frame.
Log `.tmp/detector-rail-after-shared-contact.log`. This does not establish rail
producer lifecycle, arbitrary cart motion, powered/activator rails or persistence.

The boundary suite is expanded to 51 scenes, including command-block carts
(`adamant-redstone-bds-cwkzgwwb`). The type filter previously omitted those
carts. Native readback also exposes a one-slot command-cart inventory, missing
from the kernel. A separate five-scene native capture
(`adamant-redstone-bds-iitpiner`, `bedrock_1_26_51_command_cart_inventory.json`)
records empty, one diamond, full diamond stack, ender-pearl stack and shield:
their analog strengths are 0,1,15,15,15. Command carts now expose that one-slot
inventory and preserve its contents in their existing Items NBT path; it is
independent of command execution success. This is not proof of hopper/player
inventory interaction or full command-cart behavior.

Sixteen detector producer-lifecycle scenes from
`adamant-redstone-bds-jsg1ljff` retain 1,280 source/direct-wire/support-lamp
frames in `bedrock_1_26_51_detector_rail_lifecycle.json`. Eight same-identifier
flag/shape edits reproduced immediate kernel signal changes absent in BDS.
Native setup seeds producer strength from the initial flag; subsequent flag
edits retain the circuit component, while an air replacement installs a new
component. DetectorRail now uses the shared scene-producer state and updates
only its active component on actual presence transitions, matching the
inspected native setup/check bodies. All 119 inventory, boundary, actual-cart
timing and producer-lifecycle scenes pass together, as does the existing
command-cart NBT test (entity 4.164 s), log
`.tmp/detector-rail-family-producer-after-fix.log`. A new one-slot Items
round-trip check and full block/entity/world validation are running against
these final changes. Multi-cart candidate order, more lifecycle/contact
integration, travel, slopes and other rail types remain open.

The next native rail capture (`adamant-redstone-bds-t8rjv81k`) contains 32
straight powered/activator rail scenes, both axes/parities and lengths 1,8,9,16,
with 80 frames each. Native input changes publish on the shared circuit clock:
the even-phase changes first appear three frames later, odd-phase changes two
frames later. At most nine connected rail blocks power from this adjacent
source. Original BaseRailTransporter::evaluate (`0x66d8220`) and its original
strength getter (`0x66d76d0`) also execute in 852 supplied-source cases, retained
in `.tmp/native-rail-evaluator.json`. The evaluator selects the greatest
remaining rail range among non-null sources whose strength exceeds the stored
loss. These inputs do not establish graph construction; allowConnection and
addSource (`0x66d7f80`, `0x66d7ff0`) are being inspected separately. No powered
or activator rail correction is claimed yet.

Final detector changes pass the complete block (136.464 s), entity (365.684 s)
and world (18.203 s) packages with `-count=1 -timeout 10m`, including the new
command-cart container NBT round trip. Log
`.tmp/redstone-rail-shared-suite.log`. These runs precede the new powered-rail
regressions and do not imply those scenes pass.

Another 32 powered/activator input scenes from
`adamant-redstone-bds-xtzb4305` check wire/repeater inputs, both axes/parities
and lengths 1/9, recording input permutations as well as every rail state.
The retained fixtures are `bedrock_1_26_51_powered_rail_straight.json` and
`bedrock_1_26_51_powered_rail_input.json` (5,120 frames combined).
Their ordinary regressions are being run against the existing kernel before
changing rail publication. Native circuit evaluation at `0x66d6534` and
`0x66d653e` runs two component groups; the rail evaluator reads live source
strengths and evaluates after the producer/capacitor group. Source-list
construction and the full rail topology remain separate audit obligations.

Both ordinary powered-rail regressions reproduced publication mismatches
against the existing neighbour-driven path. PoweredRail and ActivatorRail now
own rail circuit state, sample after source evaluation and publish through the
shared next-pass path. Immediate neighbour/scheduled callbacks only check
support. The range scan now retains remaining range, including maximum choice
when both authored height paths are possible, instead of discarding it into a
boolean. All 64 direct/wire/repeater native scenes pass after this change
(block 7.340 s), log `.tmp/powered-rail-circuit-first-replay.log`. These are
straight-line cases; slope, crossing, mixed-rail, source-filter and lifecycle
coverage remain open. Shared rail/minecart/piston regressions and the root
build are running before proceeding to topology comparisons.

The shared targeted run passes block (8.419 s), entity (45.908 s) and the
world package selection (no matching tests), log
`.tmp/rail-circuit-shared-regressions.log`. It includes all retained piston
body/rider and honey scenes plus rail/minecart checks. The root server builds
successfully as `.tmp/adamant-redstone-rail-check.exe`. The earlier complete
world suite remains the world baseline; a no-matching-tests selection adds
no world behavioral coverage.

The 32-scene slope/crossing capture is now retained from
`adamant-redstone-bds-pwzud_gl` as
`bedrock_1_26_51_powered_rail_topology.json`. It includes both rail types,
axes/parities, flat/rising/falling ten-rail runs and a powered perpendicular
crossing, with authored supports and initial shape validation. Two earlier
attempts were rejected because native placement flattened the upper end of
the descending run; the corrected physical setup uses a flat upper endpoint
and descending shapes on the remaining cells. No accepted failing trace is
removed. The first Go replay passes 20 scenes and fails 12: rising runs power
an extra tenth rail, and the Z-axis crossings propagate to perpendicular rails
where BDS does not. X crossings differ in native behavior from Z crossings,
despite the final visible center shape. Component direction and native source
records are being read independently before assigning a root cause. Log
`.tmp/powered-rail-topology-first-replay.log`.

Read-only native heap checkpoints now independently expose the relevant rail
components. `adamant-redstone-bds-f2cfvo0o` retains three checked snapshots of
40 components, their configured direction, strength and complete source
records in `bedrock_1_26_51_rail_components.json`. The reader accepts only its
owned BDS child, validates the circuit map before/after each sample and uses
the authored script callback's brief hold. An initial locator run omitted
negative chunks; another lacked the required torch dependency ring. Neither
failed run is accepted as evidence.

The native X crossing's visible center shape is north/south but its component
axis remains west/east. The Z crossing's center component is also west/east,
so its north/south line is disconnected. Native onPlace resolves rail
connections before setup (`0x2a56288`, `0x2a562db`). Kernel powered/activator
placement now resolves those connections and refreshes the component through
the existing explicit setup API. Later same-ID shape edits retain the
configured axis. An initial attempt reused the older rail placement helper;
the native shape controls exposed its incorrect straight/slope choices. The
replacement follows the inspected straight-axis/elevation selection and
connects adjacent rails. With configured-axis filtering, all crossings and
descending/flat scenes pass; eight rising scenes remain due to source aliasing.
No world API change was needed or retained.

For a rising line, the original source records contain range 9 at the first
rail and range 8 at the second, although that second rail also touches the
source horizontally. BaseCircuitComponent::addSourceItem (`0x66d7850`)
replaces an existing source-pointer/entry-face pair in full, including range.
The complete original function's replacement branch additionally executes in
216 cases, covering reduced/increased/equal ranges, all entry faces and four
destination indices. These are retained alongside the 852 evaluator cases in
`bedrock_1_26_51_rail_evaluator.json`. The synthetic opaque source identities
do not execute allocation, graph traversal or source discovery.

The kernel now avoids restarting range at nine for a horizontal source entry
that is replaced by the same source through a connected, vertically fed rail.
All 96 earlier straight/input/topology scenes pass after this change (block
9.609 s), log `.tmp/powered-rail-after-source-alias.log`. The general native
source-list construction, replacement traversal order, loops and conductor
graphs remain open; this is the tested overlapping-source correction.

Additional source-face captures retain 36 flat/short-slope scenes from
`adamant-redstone-bds-w7_njldy` and 24 ten-rail lower-end slope scenes from
`adamant-redstone-bds-2i62qwnf`. All six source faces, both axes and both slope
signs are covered, with stone retained when removing a source that also
supports a rail. Native lower-end side sources can legitimately power all ten
cells when a second rail is fed vertically; the other tested source positions
power nine. These controls prevent replacing the alias correction with a
blanket nine-cell cap. Their ordinary regression replay is running.

All 156 straight/input/topology/source-face scenes pass together (block
15.495 s), log `.tmp/powered-rail-all-faces.log`. A separate eight-case curve
control (`adamant-redstone-bds-czl6_36h`) caught a regression in the new placement
code: updating every nearby rail straightened already connected ordinary
curves. Placement now updates only selected, connectable neighbours and
retains the ordinary curve choices. The expanded accepted capture is
`adamant-redstone-bds-qrj0_og1`: sixteen scenes cover preserving all four
existing corners and creating each corner beside a new powered/activator rail.
All 172 power/placement scenes pass (block 16.063 s), log
`.tmp/rail-placement-and-power-native.log`. Placement and power scanning also
use loaded-block lookups; a chunk-edge invariant verifies that they do not
load neighbouring chunks. The final targeted run passes in 13.673 s, log
`.tmp/rail-complete-targeted.log`.

Complete final placement/alias validation passes block (102.491 s), entity
(229.535 s), world (14.659 s), player (18.334 s) and session (3.260 s) packages,
with `-count=1 -timeout 10m`, log `.tmp/redstone-rail-final-shared-suite.log`.
The root builds as `.tmp/adamant-redstone-rail-final-check.exe`. These checks
precede the small activator publication change described next.

Native allowConnection and its original direction/type getters now additionally
execute in 3,456 cases: both rail families, all six owner/source/entry directions,
rail/non-rail source types and both direct-input flags. The retained evaluator
fixture has separate `connections`, `replacements` and evaluator sections.
This verifies the connection predicate and unchanged direct flag; it does not
execute the entire scene-graph builder or prove all kernel graph behavior.

The twenty mixed-rail scenes (`adamant-redstone-bds-cmkses8z`) verify family
barriers, ordinary rails and inactive/active detector sources inside a line.
Thirty-two lifecycle scenes (`adamant-redstone-bds-e4npkr5k`) cover initially
powered placement, same-ID flag/axis edits, air replacement, removal, repair
and repowering at both input parities. Four activator cases failed: its native
zero-strength callback flips the live rail flag, while positive strength forces
it on (`0x860d6f5`, `0x860d715..0x860d847`). Thus a manually cleared flag can
become set when power is removed. The kernel now applies this callback in
RedstoneCircuitOutput at publication, using the current live flag.

Another thirty-two native traces (`adamant-redstone-bds-ri2zen_x`) place flag
edits zero through three ticks after input removal, including the interval
between evaluation and publication. Both flag values, input parities and rail
types are covered. All 256 powered/activator power, placement, mixed-family,
lifecycle and publication traces now pass together (block 16.364 s), log
`.tmp/rail-publication-coverage.log`. Full block/entity/world validation and
the root build are running against the final activator change. Native cart
motion and activator entity effects are the next independent comparison;
the electronic trace coverage does not establish those paths.

The final ordinary block regression including the publication-window cases
passes in 16.364 s. Three-package full validation is still running against
that final publication behavior. The next native motion capture authors 96
actual-cart scenes: plain/chest/hopper carts, basic rails, powered rails on/off,
detector rails, both flat axes and four initial velocities. Each eight-scene
batch uses 128-block spacing, a 97-rail track with a five-wide checked floor,
and a fully force-loaded 512-chunk domain. Body bounds are checked inside each
isolated track. Spawn is delayed until the initial rail power has settled.
No matching cart-motion result is claimed before capture validation/replay.


### Resumed cart motion comparison (2026-10-28, active)

Final activator publication validation passed block (104.365 s), entity
(235.412 s) and world (14.155 s), log
`.tmp/redstone-rail-publication-shared-suite.log`. The root built as
`.tmp/adamant-redstone-rail-publication-check.exe`.

The accepted flat motion capture is `adamant-redstone-bds-1h21toka`:
96 scenes cover plain/chest/hopper carts, four rail modes (basic, powered
on/off and detector), both axes and initial speeds 0, 0.1, -0.1 and 0.4.
Another 160 scenes from `adamant-redstone-bds-t0t95hfw` cover single diamonds,
full diamonds and full pearls in chest/hopper carts and empty TNT/command carts.
Both producers use eight-scene batches, 128-block spacing, checked 97-rail
tracks and five-wide floors in a checked, fully force-loaded 512-chunk area.
The 120-frame traces include 40 quiet setup frames and 80 actual body frames.
Both complete captures are retained as `minecart_motion_flat` and
`minecart_motion_inventory` fixtures; no script teleports the moving bodies.

The first regression harness compared container spawn velocity before applying
the authored impulse, producing an invalid spawn failure. After correcting that
harness ordering, the unchanged motion implementation failed all 96 scenes at
the first physical tick (`.tmp/minecart-flat-motion-baseline.log`).

Native StateVector positions address the actor centre; Script locations address
its base, subtracting the binary32 0.35 height offset. The old implementation
looked for rails at the base and could derail immediately after a native-style
spawn. It also exposed the rail projection's centre height as the base height.
The corrected cart code converts explicitly between these origins and supplies
that offset consistently for spawn, movement and teleport packets for all five
cart types. Native collision bounds and position arithmetic use binary32.
Existing slope regression expectations now address the base instead of centre.

Minecart::preNormalTick (`0x334b890`) applies its virtual natural slowdown before
rail movement. Plain/TNT factors are 0.96 without a rider and 0.997 with one
(`0x334c560`). Chest/hopper/command carts instead use
`0.98f + (15 - containerSignal) * 0.001f` (`0x334d200`), giving empty carts
0.995 and full carts 0.98. Rail projection, braking, direction changes and
post-move acceleration now follow the inspected native helpers (`0x95e7df0`,
`0x33db740`, `0x33dbac0`, `0x33dc070`), retaining binary32 operation ordering.
Flat rail snapshots have OnGround=false; rail contact alone no longer sets it.

The original ScriptActor::getVelocity (`0xbd8ac30`) selects last travel for
plain carts and StateVector velocity for the other four tested types. The
regression accounts for this accessor difference while comparing every body's
position, velocity, AABB centre/extents and exact ground state. It also checks
actual detector/powered rail state, without widening body tolerance (2e-5).

Moving detector scenes exposed a further spatial-selection error at chunk
boundaries. Native typed fetch uses the actor's owning chunk, which may still
precede its new position during movement. Filtering by the position's chunk
advanced those detector edges. The query now uses the owning entity list via
Tx.EntityChunk. Attempts to move the contact phase instead were rejected by
other cases; no special detector contact-phase override is retained.

All 256 motion traces, native detector entity replays and the existing slope
regressions pass together (entity 71.294 s), log
`.tmp/minecart-motion-256-scenes.log`. Full block/entity/world/player/session
validation is running. A separate native slope producer checks authored
terrain/permutations before accepting motion; its first capture stopped on a
power-state setup control and is not accepted as a complete fixture.
Cart curves, derailment, rider input/collisions, activator effects and broader
rail graph behavior remain open. None of these results closes the rail row or
the full audit.


The five-package motion validation passed block (100.710 s), world (14.751 s)
and player (18.513 s), but failed in two existing tests: fox berry collection
and exact NBT byte ordering. The new network test initially had a harness-only
selector limited to falling-block AddActor packets; it now selects each cart's
actual entity type. All eight entity types' spawn/move/teleport positions pass
20 repeated network runs (session 3.771 s), log
`.tmp/minecart-network-offset-20.log`.

Both unrelated failures were independently reproduced on unchanged commit
5ff95e72 in detached worktree `.tmp/redstone-check-baseline`: fox berry
collection failed 18/60 repetitions; NBT byte comparison failed 10/60
(`.tmp/redstone-unmodified-baseline-flakes.log`). NBT failures differ only in
the order of the same two compound members. The main tree reproduced one fox
failure in 20 and five NBT failures in 20. The full suite is therefore not
reported green or silently filtered. No fox/NBT production change is included.
The root builds as `.tmp/adamant-redstone-cart-motion-check.exe`.

The first slope producer attempts additionally exposed native setup reuse:
recreating a previously powered rail at the same position within one callback
can retain a true flag even after its source is gone. The setup control rejects
this unintended powered-off initial state. Reordering modes avoids that control
failure, but combined captures later terminated in BDS after 32 scenes. These
incomplete captures are not canonical evidence. Per-family fresh-process
captures are in progress, and the general reference harness now reports native
exit codes on early termination. Source graph/consumer reuse remains a separate
open lifecycle case, not a motion workaround in production.


The accepted native slope capture combines fresh-process runs
`adamant-redstone-bds-33_5rgy3` and `adamant-redstone-bds-_whtsthd`.
All 64 scenes retain 81-rail tracks, individually checked solid support columns,
eight-block rises/falls, both axes and travel signs, plain/chest carts and
basic/powered-on/powered-off/detector rails. The full authored environment is
read back before spawning. Replay normalises only Script API's known
`stone_type=stone` alias and checks every initial block against the capture;
it does not discard terrain/permutation differences. Both position/velocity
and AABB/ground comparisons remain unchanged. All 64 scenes pass (entity
13.621 s), log `.tmp/minecart-slope-motion-replay.log`. Motion coverage is now
320 complete native scenes. Per-family processes avoid the combined native
capture termination; incomplete predecessors remain outside canonical evidence.

### Activator cart effects and event phase (2026-10-28, active)

`adamant-redstone-bds-9nin4ve0` supplies 24 complete 200-frame scenes:
plain carts with/without a cow rider, TNT carts and hopper carts; steady-off,
steady-on and repeatedly toggled activator rails at both input parities.
An initially powered rail leaves the rider mounted in the first physical
snapshot and ejects it in the second. A rail flag published at relative tick
63 ejects the rider at 64. Native TNT carts explode at relative tick 122 for
steady power and 144 for the repeated-toggle controls; repeated power does
not restart their fuse. A hopper pulls its first item before the initial
activation takes effect, then transfers every two ticks while enabled.

The unchanged implementation failed twelve of these scenes: two premature
initial ejections, four TNT carts that never ignited and six hopper transfer
or initial-enable traces (`.tmp/activator-cart-effects-baseline.log`).

World ticking now permits a pre-block entity to sample block/circuit state
once after block updates, without repeating its normal tick, movement or age.
The cart queues that rail event and applies it on its following normal tick,
through the concrete cart behaviour. This retains the original movement and
detector phases rather than moving all cart physics after the circuit.
TNT activation now reaches its specialised behaviour, ignores repeated priming
while already lit and explodes on the tick after its stored fuse reaches zero.
Hopper activation now takes effect through the same event phase and its tested
pull cadence is two ticks rather than nine. Broader push/collection and moving
hopper behavior remains under separate investigation.

All 24 effect traces and existing hopper/tick checks pass (entity 3.788 s,
world 6.080 s), log `.tmp/activator-cart-effects-first-fix.log`. Rider scenes
check rider counts/events, not equivalence of autonomous cow movement; all
rider-free cart bodies retain full geometry/velocity/ground comparisons.
The callback loop additionally handles removal without skipping the next
entity and ignores frozen ticks. Its removal/freeze/step regression passes
(world 2.658 s), log `.tmp/entity-post-block-phase.log`. Another complete
five-package validation and root build are running against these changes.
No complete rail-row or full-audit equivalence is claimed.


Final activator shared validation passes block (101.243 s), world (14.022 s),
player (18.685 s) and session (3.337 s). Entity validation (291.504 s) reports
one failure in TestVillagersRunFromZombies; the same test fails 4/40 repetitions
on unchanged 5ff95e72 (`.tmp/redstone-baseline-villager-repeat.log`). Earlier
fox/NBT instability remains recorded above. No entity regression from the
new native scenes is hidden by this unrelated AI failure. The root builds as
`.tmp/adamant-redstone-cart-activator-check.exe`.

The next independent hopper controls retain 24 original-BDS container scenes
from `adamant-redstone-bds-731htgrq`: a source barrel above and destination
barrel below are independently present/absent, with 0/32 initial cart items,
basic rails and activator rails on/off. Native carts do not push into the
barrel beneath them; above-container pulling remains independent of it.
The old automatic push branch fails the positive destination controls.

Another 24 item-collection scenes from `adamant-redstone-bds-36p7aaf7` cover
3/64-item drops and initial cart contents of 0, 32, 319 and 320 items. Native
carts collect the entire fitting stack, including on their initial powered
activator tick. A cart with one free item of capacity collects one and leaves
2/63; a full cart leaves the whole drop. Every native snapshot conserves the
combined item count. Earlier 12-case collection controls from
`adamant-redstone-bds-8c1g3gxu` were expanded rather than silently discarded.

The kernel now removes automatic downward pushing, transfers the fitting
portion of a dropped stack and updates a partial remainder in place through
the existing item-count notification path. It retains the remaining entity's
identity and lifetime rather than removing/recreating it. Existing unit tests
that required the contradicted push/one-item collection behavior were rewritten
as pull-capacity/conservation checks; the 48 native traces independently cover
real world transfers. All 48 new transfer/collection traces, previous 24
activator-effect traces and existing hopper tests pass (entity 4.937 s), log
`.tmp/hopper-cart-native-transfer-fixed.log`. Native collection boundaries,
actor order, moving-container interaction and hopper-block extraction remain
open beyond these stationary controls.


### Cart item/dispenser placement and newborn movement

The item entry path still used a Java-like base-height offset and an additional
slope bonus after the cart physics/network origin correction. Original client
MinecartItem::_useOn (`0x24a05d0`, position construction
`0x24a0797..0x24a07f1`) instead requests the binary32 block centre independently
of rail shape. `redstone_native_minecart_placement_probe.py` executes those
original instructions in 120 cases across all five relevant cart kinds and
signed/large world coordinates. Supplied callbacks classify a rail, report no
obstructing actors and record/reject the spawn request. This establishes
request construction, not world actor creation, obstruction selection or ticks.

The initial regression fails all 120 cases: height and, at large coordinates,
rounding before adding 0.5 differ (`.tmp/minecart-item-placement-baseline.log`).
Cart item placement now constructs the binary32 centre and converts it to the
created type's base using its network offset before adding the worldless
handle. It removes the unsupported slope bonus. All 120 requests pass across
flat rails and all four ascending directions (item 2.810 s), log
`.tmp/minecart-item-placement-fixed.log`. The complete client request fixture
is retained; normal missing-factory/non-rail behavior remains outside this
specific arithmetic probe.

Actual dispenser captures now retain forty controlled on-track scenes
(`adamant-redstone-bds-qcw3b_3k`): all five carts, four horizontal faces and
flat/ascending tracks. Rails are placed from far to near after the dispenser;
all nine rail permutations are verified/read back after settling, before item
loading. Earlier `adamant-redstone-bds-5sbp5mw_` lacked this full direction
control and is superseded for the full slope comparison. Spawn events and
all 140 tick snapshots are retained. The observed flat positions are a further
0.125 along the facing direction from the front cell's centre. Original
MinecartItem::dispense (`0x24a0e70`) uses constants +0.125 (`0xe61e13c`) and
-0.125 (`0xe8166d0`), independent of rail shape.

Sixty additional native controls (`adamant-redstone-bds-si4brb9o`) cover no
rail, a solid obstruction and a rail one block lower, for all five cart kinds
and horizontal faces. No-rail/blocked scenes eject exactly one item; lower
rails create exactly one cart. All initial inventory/outcome conservation
controls pass before canonical copying. The kernel now supports the lower-rail
case and uses the native binary32 centre plus facing offset, converted through
the created cart type's network origin. The old extra slope-height bonus is
removed. Native on-track power was applied at relative tick 60 and the first
cart is observed at 67, already through its first movement in the same snapshot.

The original DispenserBlock::_onRedstoneUpdate (`0x9786380`, queue request near
`0x978654a`) schedules four game ticks after its scene callback. An immediate
neighbour-driven callback published the kernel flag two ticks early in the
tested scenes. A dispenser circuit consumer now caches after source evaluation,
publishes in the following circuit pass and schedules on its rising edge.
Publication merges the triggered flag into the live block, preserving the
current facing and inventory.

Forty additional native chunk-boundary scenes (`adamant-redstone-bds-0daef6lh`)
cover plain/chest carts, all horizontal facings and offsets 0/1/7/14/15 within
the source chunk. Forty-eight signed-coordinate scenes
(`adamant-redstone-bds-afl7ydgg`, `adamant-redstone-bds-90sx_0hp`) cover anchors
-17/-16/-1/0/15/16. A newly created cart moves in its birth tick in the same
chunk or a later chunk. A north/west spawn entering an earlier signed chunk
waits until the next tick; the behavior is retained at negative-to-zero chunk
boundaries. This is a tested native scheduling observation, not an unsigned
chunk-key assumption.

Block-created carts meeting that condition join a post-circuit movement queue,
before the existing after-block entity callbacks. Ordinary cart movement stays
in its pre-block phase. A spawn-time processed marker is distinguished from an
actual completed entity tick, so admitting a newborn actor does not rerun an
actor that already ticked. Removed and duplicate queued actors are ignored.
All 188 dispenser scenes compare trigger state, inventory, creation tick,
physical position, velocity, body and item fallback each recorded tick.
They pass together with the overlap controls below (entity 19.663 s, world
1.847 s), log `.tmp/dispenser-cart-birth-marker.log`.

The independent world regression queues two newborn actors, duplicates a queue
entry, removes a third actor before processing and tries to queue an existing
actor that already ran. It checks exactly one normal/post-block callback and
50 ms of age per tick, including the following ordinary tick. This and the
existing frozen/step/removal phase test pass ten repetitions (world 3.471 s),
log `.tmp/entity-queued-birth-regression.log`.

### Cart collisions from an existing overlap

Seventy-two original-BDS scenes (`adamant-redstone-bds-90blb5sf`) exercise plain
and chest carts starting outside or inside stone, glass and a dispenser, at
four initial velocities. The old shared recovery path incorrectly displaced
fast carts already intersecting a solid block. Cart movement now clips new
contacts without unsolicited recovery from a pre-existing overlap. This is
the kernel's collision policy; no literal native cart bitfield value is claimed.
All 72 full-body/motion traces pass. The same correction allows carts returning
down a slope to enter the dispenser block with the native trajectory, instead
of being pushed backwards to its boundary. Wider cart/cart, rider and geometry
coverage remains open.

The six-package shared regression started before the user's brief pause was
explicitly stopped and provides no full-suite pass claim. The resumed run
passed entity (424.762 s), world/player/item (cached) and session (3.975 s),
but block failed its old immediate falling-edge assertion. That unit now checks
delayed publication and no additional item consumption; both dispenser/dropper
unit groups pass (block 2.960 s). Exact per-frame cadence is checked against the
native captures below. This is not yet a full shared-suite pass after the
dropper change. Earlier unrelated AI/NBT instability remains documented above.

### Dispenser/dropper pulse and input cadence

Another 288 native traces (`adamant-redstone-bds-9su5nyb4`) cover both block
types, all six source faces, direct redstone blocks or a one-stone-block path,
both input parities, one/two/four-tick pulses and three repeated-pulse spacings.
One-tick pulses may be missed at the unsampled parity. An accepted pulse still
dispenses after the source goes low. Repeated accepted edges schedule separate
actions in the tested intervals. The redstone-block/stone controls remain
unpowered; this source does not supply the strong conducted input.

The dispenser already matches all 144 of its traces after the earlier circuit
change. The old dropper fails all 72 direct-input cases by publishing too early
(`.tmp/dispenser-pulse-replay-baseline.log`). Its constructor in the client
registers the same DispenserBlock::_onRedstoneUpdate callback at `0x9791b62`.
The kernel now shares the circuit consumer and four-tick rising-edge queue
between these blocks, preserving each live inventory/facing on publication.

An additional 144 native source-registration traces
(`adamant-redstone-bds-1n07kx11`) use supported stone buttons in all six facings,
directly or through stone, at both parities, with 8/12-tick pulses and a repeated
12-tick pulse. Each transition removes/recreates its source. All scenes require
positive dispensing before acceptance, and every button permutation is retained.
Merely editing button_pressed_bit on an existing native block does not update
its producer; the earlier all-zero diagnostic is superseded and is not evidence
of a working source. This capture tests registration, not actual player presses.
All 432 source/pulse traces now match full trigger and inventory timing (entity
20.052 s), log `.tmp/dispenser-pulse-replay-all-sources.log`. The old unit's
overly precise startup-clock assertion still failed in that same run, then
passed after separating its bounded-delay contract from these exact native
cadence tests. Broader source graph/lifecycle, retrigger intervals and container
actions remain open.

### Dropper container rejection and sided insertion

The next original-BDS capture (`adamant-redstone-bds-elprqonc`) contains 108
container-action scenes. Ninety-six are retained for ordinary replay: all six
dropper faces into empty/full/one-space-left barrels, chests, locked hoppers
and furnaces, six empty brewing-stand rejection controls, and eighteen
dispenser/barrel controls. Full source/target slot contents, flags, dropped
items and all 128 tick snapshots are recorded. The initial source load and
per-scene total item conservation are validated before copying.

Twelve forced full/partial brewing inventories are excluded explicitly in the
fixture metadata: assigning stacks of diamonds to potion/fuel slots produces
invalid gameplay states that native brewing consumes independently of the
dropper. The original 108-scene raw capture remains diagnostic evidence; the
capture script now generates the 96 accepted scenes directly. This exclusion
does not claim that the invalid-inventory brewing path matches.

Previously a full destination fell through to world ejection and consumed the
chosen source item. A rejecting destination now retains it in the dropper.
Specialized containers apply their sided insertion rules to only that randomly
selected item through the existing transfer contract. This fixes the tested
furnace-side and brewing-stand diamond rejection cases. A missing destination
still takes the ejection path, and the dispenser controls still eject instead
of inserting into the barrel.

The locked-hopper controls exposed another premature flag update: the kernel
locked on the first tick while BDS published on the third. The inspected
HopperBlock::_onRedstoneUpdate (`0x66baf90`) publishes toggle state without a
scheduled dispense action. Hoppers now share the late container consumer but
keep their own actor ticking and live cooldowns/inventory. All 96 container
scenes pass together with the 432 dispenser/dropper pulse scenes, 188 cart
dispense scenes and existing hopper-cart/activator controls (entity 41.279 s,
block 2.708 s), log `.tmp/container-circuit-and-transfers.log`.

Another 144 original-BDS hopper-lock traces (`adamant-redstone-bds-qz9qo4kp`)
cover direct/stone-path inputs on all faces, both parities, short pulses and
repeated edges. All retained item counts stay 64 as intended: no destination
container is present. Their ordinary Go replay passes in the complete shared
regression below.

The last 30 native scenes (`adamant-redstone-bds-vrvuys5l`) additionally record
actual hopper pushes into empty/full/partial barrels in all five legal output
directions, with lock/unlock inputs at both parities. Immediate load readback
and per-frame conservation pass. An empty destination receives items at
relative ticks 42/50/58, then 80 after unlocking, in these controlled scenes.
The first transfer follows the scripted load at 40 by two snapshots. These
fixtures and their script are retained for the next audit step: no Go replay
or transfer-phase correction is claimed for them yet. The distinction between
block-actor phase, inventory publication and cooldown must be resolved from
native evidence before changing that path.

The user requested this paused checkpoint, commits and push. The full 22-row
audit remains open; the approximately 65% estimate describes effort only.

Final checkpoint verification: `go test ./server/entity ./server/block
./server/world ./server/player ./server/session ./server/item` passes all six
packages: entity 372.679 s, block 98.188 s, world 14.901 s, player 18.525 s,
session 3.397 s and item 2.583 s. This includes the new 144 hopper-lock traces
and all normal regressions, without filtering the previously unstable tests.
Log: `.tmp/container-circuit-shared-suite.log`. This run proves the checked
regressions at this checkpoint; it does not close the audit's outstanding rows.
The root entry point also builds with `go build -o
.tmp/adamant-container-audit-check.exe .`; `go run .` remains the server entry.
Code checkpoints: `a7ae2f8` (piston contacts/entity phases), `d76fd761`
(rails/carts) and `26048efd` (container signals/dispensing/transfers).

### Resumed 2026-10-29: hopper transfer clock and counter units

The ordinary replay of the last 30 hopper push/lock traces failed twenty
empty/partial-destination scenes: the first transfer happened one tick early.
Ten full-destination controls passed. Eight inventory-load offsets in each of
five output directions, repeated at both placement parities, add 80 native idle
scenes (`adamant-redstone-bds-qs58n6v7`, `adamant-redstone-bds-o9oa8spi`). The first transfer always
appears on an even game tick, irrespective of placement parity; subsequent
transfers remain eight game ticks apart. The kernel's one-GT idle polling did
not match this first-transfer phase.

The original HopperBlockActor::_tick (`0x4acb450`) first rejects a repeated
Level::getCurrentTick value, then decrements its transfer counter once. The
constructor (`0x4acaf40`, store at `0x4acaf9e`) initializes the transfer speed
to **4**, not 8. A new read-only owned-BDS heap probe locates the tick component
through a unique original-client prefix, validates its position accessor and
the body offsets for last-tick/transfer counters, and reads two hoppers at 25
bounded Script API checkpoints. Native actor tick callbacks advance every GT,
but last_tick stays unchanged on odd ticks and the transfer counter only
decrements on even ticks. After a transfer it holds 4, producing the observed
eight-GT interval. This establishes the counter's units rather than inferring
them only from the visible item interval.

Hopper transfer processing now follows that even-GT clock and uses four native
counter steps after success. Receiver initialization and source timer handling
are corrected by the subsequent chain comparison below. Inventory insertion on an odd tick
can run on the next even tick; insertion on an even tick waits two snapshots.
The separate item-actor collection timer keeps its previous GT units pending
its own native comparison. This is not a claim of item collection parity.

All 110 idle/lock-unlock push scenes, 96 dropper/container scenes, 144 lock-input
scenes, the existing bookshelf controls and the new direct heap-counter replay
pass (entity 16.700 s, block 1.455 s), log
`.tmp/hopper-actor-clock-replay.log`. The counter replay checks every retained
native counter and inventory snapshot, including opposite load parities. Full
native fixture load, clock and per-frame item conservation controls passed.
Broader traversal graphs, transfers into and out of other containers,
collection, and persistence remain open.

### Hopper recursive transfer order

Sixty original-BDS chain scenes (`adamant-redstone-bds-thgk678e`) vary all five
output directions, source counts 1/4/16, both actor creation orders and a chunk
boundary. The previous implementation failed 41 scenes. Native recursion visits
the hopper below itself, below its destination and below the paired half of a
destination double chest. Three candidates, including absent entries, undergo
the original two-word shuffle. The kernel now uses that algorithm with the
world RNG, shares the actor's last-transfer-tick guard across block-value copies,
limits recursion to the native entry budget of 50, and preserves live changes
made by a recursive neighbour. The world's complete random stream has not yet
been matched to the native generator.

Receiving a first item initializes a hopper's timer to four steps if its pass
has run, or five before its pending decrement. Further received items do not
reset it. Pulling from another hopper does not reset that source's timer. These
rules permit the tested vertical pair to push and pull two items in the same
eight-GT cycle. Transfer and recursive lookups now avoid loading absent chunks.
All 60 chain scenes, 110 idle/push scenes, heap-counter and dropper/bookshelf
controls pass (`.tmp/hopper-chain-recursion-fixed.log`: entity 13.924 s, block
3.101 s). Chain/clock checks also pass three repetitions (entity 23.850 s).
Separate synthetic guards verify the recursion boundary, item conservation and
no neighbour-chunk loading; both pass three repetitions (block 2.703 s).

### Unfinished dropped-item pickup comparison and paused checkpoint

The user requested commits, publication and a pause on 2026-10-29. The effort
estimate remains approximately **65%**; all 22 matrix rows retain open work.

Ninety-eight original-BDS captures (`adamant-redstone-bds-o9_jfgc6`) preserve
full inventory slots, dropped-actor identities, positions, velocities, bounds
and grounded state over 120 frames. They vary empty/partial/full hoppers, item
counts, height/edge placement, locking, output containers and sequential item
types. Spawn velocities are recorded native API inputs, not a claim of matching
the API's random toss generator. All capture conservation controls pass.
The new replay still fails **35/98** scenes (`.tmp/hopper-items-replay-baseline.log`).
No pickup fix is claimed or included in this checkpoint. The diagnostic is
explicitly opt-in until resolved; reproduce in PowerShell with:

```powershell
$env:ADAMANT_REDSTONE_PICKUP_AUDIT = '1'
go test ./server/entity -run '^TestHopperItemCollectionMatchesBDS$' -count=1
Remove-Item Env:ADAMANT_REDSTONE_PICKUP_AUDIT
```

The executed-original-client probe `scripts/redstone_native_hopper_shapes_probe.py`
retains 126 collision constructions and 40 empty-query paths in
`server/block/testdata/bedrock_1_26_51_hopper_shapes.json`. Original functions
are HopperBlock::addAABBs (`0x66bb380`, length `0x4a1`), spout construction
(`0x66bb870`, `0x286`), Hopper::_tryAddItemsFromPos (`0x2060eb0`, `0x672`) and
world pickup (`0x2062b80`, `0x137`). Function hashes and client hash are retained.
Host callbacks supply state, allocation and empty actor queries; candidate
selection and successful collection are not exercised by this empty-query probe.

At ordinary coordinates the native model has seven boxes: a floor at local
Y .625..6875, four walls from .6875..1, a neck and a facing-dependent spout.
The current Go model has five boxes and a floor surface at .625. Native world
translation uses binary32 arithmetic and omits degenerate boxes at large
coordinates. A block hopper queries [x,y+.625,z]..[x+1,y+2,z+1], unless the block
above reports isContainerBlock; an entity hopper has two centred queries.
The original pickup code shrinks relative query bounds by 1e-4 and returns
after the first successfully acquired actor, including partial insertion.
Before implementing selection, inspect the candidate filtering loop beginning
at `0x20611c0`: its exact position/body test remains unresolved. Slot-order
insertion also needs comparison; Inventory.AddItem currently merges stacks
before filling earlier empty slots. The geometry, item-actor pickup path and
inventory insertion have deliberately not been changed at this pause.

The resumed six-package suite before the recursion changes passed block
(100.315 s), world (15.809 s), player (18.771 s), session (3.552 s) and item
(3.184 s). Entity failed only TestGoatsRamMobs (340.244 s). The same goat test
failed 1/30 repetitions on unchanged baseline `5ff95e72`
(`.tmp/redstone-goat-baseline-repeat.log`), establishing existing instability.
Intentional portal recovery panic logs are not additional failures. These
results do not claim a green complete suite at this checkpoint; focused
hopper regressions are reported separately above.

Final pause verification: `go test ./server/entity ./server/block -run
'Test(Hopper|DropperContainerActionsMatchBDS|BookshelfHopperSignalsMatchNativeCapture)'
-count=1` passes entity (20.954 s) and block (1.456 s), log
`.tmp/redstone-paused-2026-10-29-focused.log`. The unfinished item-pickup
diagnostic is skipped by default as documented above, not counted as passing.
All six new/updated Python probes parse, and all six new native fixtures report
complete captures with the expected scene/checkpoint counts. The root server
build passes with `go build -o .tmp/adamant-redstone-paused-2026-10-29.exe .`;
`go run .` remains available. The transfer/chain correction is committed in
`7dd2da9e`; this checkpoint retains the open collection comparison for resumption.

### Resumed: dropped-item collection, geometry and container covers

The user resumed the full audit and requested uninterrupted work. The preceding
paused checkpoint is historical; its pickup diagnostic and 35/98 divergence
count are superseded by the ordinary regression results below. No new commits
or publication have been requested during this resumed work.

Block hoppers now collect during their own even-GT transfer pass, after pushing
and attempting a container pull. World collection shares the native four-step
transfer timer; the separate item-actor pickup/cooldown path has been removed.
A successful partial pickup retains the same item actor and sends its updated
stack count. Only the first successfully acquired actor is processed per pass.
Inventory.AddItemInSlotOrder implements the native ascending-slot insertion
policy for block-hopper pushes, pulls and world pickup: an earlier empty slot
precedes a later matching stack. Player inventory AddItem retains its own policy.

The original Hopper::_tryAddItemsFromPos body loop at `0x20611c0` tests AABBs
in relative binary32 coordinates against bounds shrunk by 1e-4. The complete
BlockSource::fetchEntities overload at `0x2d850b0` first visits only the chunks
covered by the unexpanded query and then checks actor AABBs. This distinction
explained two remaining scenes: bodies can overlap the query while belonging
to the adjacent, unvisited chunk. The kernel now reproduces both stages,
including current actor-chunk ownership. Merely expanding an origin query or
requiring the origin inside the pickup box gives incorrect results.

All original 98 item scenes now match. Eight additional original-BDS scenes
(`adamant-redstone-bds-8w2jgddg`) verify inventory holes, matching later stacks,
simultaneous diamond/dirt actors and sequential spawns, both with and without
an output barrel. Full slots, actor identity, partial leftovers and physical
state match. Their fixture is `bedrock_1_26_51_hopper_item_order.json`.
The diagnostic environment gate has been removed; all are ordinary tests.

Another 36 scenes (`adamant-redstone-bds-b4yh6w2n`) place 18 types of blocks
above the hopper: ordinary solids, empty containers, an ender chest, lectern,
bookshelf, composter, decorated pot and jukebox. They retain native cover
states and alias controls. A native container suppresses world pickup even
when empty; solidity alone does not. All retained inventory and physical
snapshots match after the movement/shape corrections below. Fixture:
`bedrock_1_26_51_hopper_item_covers.json`. All 142 scenes pass every-frame item
conservation controls, not just final inventory totals.

The new `redstone_native_hopper_pickup_probe.py` executes the original query
construction, relative shrink and geometric rejection instructions in 648
boundary cases, including large coordinates. One supplied actor bypasses the
source prefilter; at the original geometry-success branch `0x206125e` a local
marker replaces inventory transfer and jumps to the original loop tail
`0x2061370`. Thus these are geometry results, not simulated successful pickups
or proof of source ordering. Original code hashes, interception points and
scope are retained in `bedrock_1_26_51_hopper_pickup_geometry.json`. The same
probe reads the actual vtable slot +0xb8 and executes the constant native
isContainerBlock getters for 18 block classes. Both 648 geometric results and
18 container classifications match the Go helpers (block 2.716 s).

The hopper collision model now contains the original seven boxes with a floor
surface at .6875, four upper walls, neck and facing-dependent spout. Native
collision fetching translates corners in binary32 and discards degenerate
boxes; 126 executed-client constructions cover all facings and extreme
coordinates. This changes the native collision path, not generic Java movement.
The cover scenes exposed two additional differences. ItemActor::postNormalTick
at `0x3348b76` applies block friction after a vertical collision with resulting
Y velocity <= 0, even after a ceiling hit while OnGround is false. The two ECS
hashes are verified against ActorCollision::hasVerticalCollision (`0x2539f10`)
and isOnGround (`0x2539fc0`); this policy is now enabled specifically for items.
BrewingStandBlock::addAABBs (`0x318c730`, length 0x1fa) constructs its central
column from Y 0 to .875, followed by the .125-high base. The old column extended
below the block and pushed items down prematurely. That model is corrected,
and 21 original constructions have been added to the shape fixture.

The combined 142 real item scenes, 126 hopper shapes and pickup helper checks
pass (entity 8.725 s, block 2.688 s), log
`.tmp/hopper-pickup-covers-friction-fixed.log`. Before the final ceiling-friction
and brewing-shape correction, the wider native/BDS regression selection also
passed entity (141.044 s), block (83.239 s) and world (6.398 s), log
`.tmp/hopper-pickup-native-regression.log`. A complete shared-suite run and
further source-slot/side comparisons are reported below. The broader 22-row
audit remains incomplete.

### Source-slot continuation after a rejected transfer

Sixty original-BDS push scenes (`adamant-redstone-bds-0rxithg2`) vary all five
output directions, barrel/chest/locked-hopper destinations and four slot
layouts: a blocked first type with a fitting later type, an empty source slot
before those stacks, an earlier empty destination before a matching stack,
and an empty destination with two source types. Twelve additional pull scenes
(`adamant-redstone-bds-e3d80uwz`) exercise the same layouts with containers
above the receiving hopper. Exact initial slot readbacks, both clocks and
every-frame item conservation pass before replay.

The previous push loop failed thirty of the sixty scenes by stopping at the
first rejected stack, leaving a later fitting stack untouched. The original
Hopper::_pushOutItems loop returns to source-slot increment at `0x2061d10`
after a rejected attempt (`0x2061fda..1fe5`). The kernel now continues instead
of returning false. All 72 push/pull scenes match full per-frame slots and
flags, including the already corrected ascending destination-slot policy.
Fixtures: `bedrock_1_26_51_hopper_slot_transfers.json` and
`bedrock_1_26_51_hopper_slot_pulls.json`. The baseline log records all thirty
failures; `.tmp/hopper-pickup-and-slot-transfers-fixed.log` passes the combined
hopper, dropper/container, bookshelf and item-identity checks (entity 29.424 s,
block 3.177 s).

The first complete shared run after pickup changes passed block (104.634 s),
world (15.174 s), player (18.749 s), session (3.501 s) and item (2.739 s).
Entity (354.152 s) failed only TestPartialHopperCollectionKeepsItemActor: its
direct item.Tick call encoded the removed item-actor pickup ownership. The
test now invokes the block actor at an actual even world tick, then checks
that the partial remainder keeps its handle and the same motion as a control
item. It passes in the combined focused run above. Other entity tests,
including all 147 executed hopper/brewing collision constructions, passed
that initial full run. Log: `.tmp/hopper-pickup-shared-suite.log`.

The repeated full shared regression after the source-slot correction passes
all six packages: entity 360.200 s, block 102.791 s, world 15.707 s, player
18.713 s, session 4.301 s and item 2.931 s. Inventory has no standalone tests;
its insertion policy is exercised through the native scene replays. No AI or
NBT tests are filtered. Log: `.tmp/hopper-pickup-slot-order-shared-suite.log`.
This verifies the tested checkpoint, not completion of the outstanding audit.

### Furnace sides, extraction holes and dropper slot order

Sixty original-BDS furnace insertion scenes (`adamant-redstone-bds-cfsv5yhq`)
cover ordinary/blast furnaces and smokers in all five hopper output directions,
with diamonds before fuel or raw iron and both input/fuel source orders.
Eighteen extraction scenes (`adamant-redstone-bds-5x15nbfo`) separately cover
outputs, retained input/fuel, an empty bucket, a blocked bucket with fitting
output, a nearly full receiver and an earlier empty receiver slot. Actual
furnace/smoker output items are used. Initial readbacks and every-frame total
conservation pass; these scenes do not perform new cooking. All sixty insertion
controls already match. Three extraction scenes exposed use of player-style
stack merging: output incorrectly went into the later existing stack. Furnace
extraction now uses AddItemInSlotOrder, matching the native destination scan.

Thirty-six original-BDS dropper scenes (`adamant-redstone-bds-n2ez81xt`) retain
both pulse edges and all six output faces into barrels/chests/locked hoppers.
A single source stack makes random slot selection unambiguous. Eighteen
earlier-empty destination cases failed for the same reason; eighteen
matching-first controls passed. Generic dropper insertion now uses the same
ascending-slot policy. Both complete new corpora and all existing hopper,
dropper, dispenser-pulse, bookshelf, smelter and partial-identity controls pass
together (entity 51.216 s, block 3.200 s), log
`.tmp/hopper-furnace-dropper-slot-fixed.log`. The two routing fixes follow the
last green shared-suite run above; that full-run timing does not include them.
Their baseline log retains all twenty-one divergences.

The new fixtures are `bedrock_1_26_51_hopper_sided_transfers.json`,
`bedrock_1_26_51_hopper_furnace_pulls.json` and
`bedrock_1_26_51_dropper_slot_transfers.json`. Broader cooking, experience,
container actor lifecycle and receiving-hopper timer interactions remain open.

### Persisted hopper counter inputs

Twenty original-BDS structure-load scenes (`adamant-redstone-bds-fumjl1kf`)
load authored TransferCooldown values -3/-1/0/1/2/3/4/5/8/2147483647 at both
load parities. Initial slot readbacks, the actual load event tick and all-frame
four-item conservation pass. The kernel's DecodeNBT counter and subsequent
clock match all twenty cases (entity 3.850 s), log
`.tmp/hopper-persisted-clock-replay.log`. Slot contents are controlled separately
in this counter replay. This is not a world-save or chunk unload/reload test.
The fixture is `bedrock_1_26_51_hopper_cooldown_load.json`.

### Brewing bottle slots and still-open phase comparison

Twenty real BDS insertion scenes (`adamant-redstone-bds-mes3iw4z`) use legitimate
source stacks of nether wart, blaze powder, a water potion and empty glass
bottles, each after a rejected diamond stack, in all five hopper facings.
Source and destination initial readbacks pass. Fuel powder can become brewing
fuel, so raw inventory-only conservation is not asserted for that conversion.
The test explicitly registers the same vanilla recipes as server startup;
a standalone World alone does not initialize the recipe registry.

BrewingStandBlockActor::canPushInItem (`0x2dbdd10`, slot-empty checks at
`0x2dbded2..1df09`) accepts each bottle/potion slot only while empty. The old
kernel merged several empty glass bottles into one slot. Automated insertion
now fills the three slots one bottle at a time and retains a fourth source
bottle. After the RNG/phase changes below, eighteen of twenty original full
traces matched; two south-facing fuel/bottle traces differed at a transfer
boundary. The original raw native fixture is
`bedrock_1_26_51_hopper_brewing_inserts.json`. Native setContainerChanged marks
the actor dirty; its subsequent tick updates appearance and fuel. Actual random
inputs/order must be controlled before attributing every phase difference to
block logic: the native capture runs multiple scenes together, while the Go
replay isolates them. The independent hardware trace below resolves these
controlled block-logic replays without claiming whole-world scheduling parity.

### Native level random stream and per-chunk actor execution

A new read-only owned-BDS probe (`redstone_bds_level_random_probe.py`) identifies
Random's vtable through the original nextInt load/call prefix and its unique
post-call shift, validates its Core generator and locates the unique LevelRandom
owner through its two getters. Bounded Script callbacks hold at twenty points.
Two complete captures use world seeds 1 and 4294967298; the observed Core seeds
are 1 and 2. Original Core::Random instructions run on local copies of each
state and explain all nineteen consecutive transitions in each capture. The
seed-1 capture has an initial transition of 178 draws, then alternating 62/64;
the entire stream is not merely the tested hopper's two calls. Reference roots:
`adamant-redstone-bds-9gdxzmdz` and `adamant-redstone-bds-5u4dwaac`.

A separate executed-client corpus constructs Random through its original
constructor (`0x2203820`, size 0xc7), then executes Core::_genRandInt32
(`0x26dae0`, size 0x1c8) for 2,000 draws at each of seven seeds. Another 49
bounded cases execute Core::nextInt (`0x26dd50`, size 0x22), including a zero
bound and a following raw-draw control. These are 14,000 raw outputs, 1,960
bounded outputs and 49 draw-consumption controls. Hashes and all inputs are
retained. The Go port matches these outputs, all forty captured states and
all thirty-eight complete state transitions. Native Core twists one MT state
word per draw; eagerly twisting the full array matches seeded outputs but
cannot replay a partially twisted native heap state.

The default world source now uses this MT19937 stream with the low 32 bits of
the resolved world seed. Explicit caller-supplied rand.Source values retain
Go semantics. Block-actor shuffling and hopper neighbour traversal share direct
raw-word draws; they do not obtain and truncate a two-word Go Uint64. Bounded
precipitation draws use the native modulo operation. The generic Go Uint64
adapter and remaining random consumers are not a complete native API/consumer
port. Native precipitation column coordinates also use a separate LCG;
whole-world draw scheduling and this coordinate path remain open.

The chunk dispatcher had another independent ordering error: it copied and
shuffled every chunk's actor list, then executed all actor callbacks together.
LevelChunk::tickImpl calls tickBlockEntities at `0x32cab8f` before returning to
the next chunk. The kernel now processes each chunk's precipitation/random
blocks and its live actor snapshot in that chunk, preserving the original
same-chunk identity guard. Actor RNG calls therefore precede the next chunk's
shuffle. A controlled sequence regression fails before this change with
[a,c,b] instead of [a,b,c], while consuming the same five supplied words, then
passes after it. The complete world package passes (14.378 s) after the initial
phase fix. After bounded draws are integrated, the RNG/actor and combined
container regression passes world (2.586 s); entity (79.302 s) fails only the
two brewing phase cases documented above. No full shared-suite pass is claimed
after this newer RNG/scheduler/brewing work.

New RNG fixtures: `bedrock_1_26_51_level_random_seeded.json`,
`bedrock_1_26_51_level_random.json` and
`bedrock_1_26_51_level_random_high_seed.json`. The earlier native actor shuffle
probe calls its supplied Random wrapper a level; the instructions themselves
are unchanged. ILevel slot +0xa68 returns Random&, whose +0x9f8 points at Core.
Both the chunk shuffle and hopper recursion use that same level-owned source.

### Independently recorded actor RNG inputs for brewing transfers

`redstone_bds_actor_random_probe.py` now traces only its fresh Reference-owned
BDS child using hardware execution breakpoints. No instruction bytes or
on-disk executable are changed. Thread debug registers are restored before
detach. This is explicitly debugger-observed evidence, separate from the
earlier uninstrumented world captures. Native tick timing under a debugger is
not used as wall-clock performance evidence.

The locator matches the client chunk-actor prologue and validates the unique
BDS function containing a direct call to the identified Core generator:
`LevelChunk::tickBlockEntities` at `0x9fd340`, shuffle return caller
`0x9fd6b3`. The two hopper recursive-traversal calls have return callers
`0x57f82d6` and `0x57f82f5`. At each generator entry, the original client
generator executes on a local state copy to record the impending raw word.
The process's generator and state remain untouched. Actor-pass entry and
return boundaries and actual chunk positions distinguish these draws from
the rest of the level stream.

A small smoke capture (`adamant-redstone-bds-m8u729ay`) validates the four
loaded chunk positions and these three callers. The full brewing capture
(`adamant-redstone-bds-xj9dmv6c`) retains twenty scenes, 256 held Script API
checkpoints, all chunk-pass words and outside-pass draws. The first checkpoint
includes startup passes; the replay uses the last pass of each scene chunk
before its held sample. All subsequent checkpoints contain 192 chunk passes.
Fixture: `bedrock_1_26_51_hopper_brewing_actor_inputs.json`.

`TestHopperBrewingInsertionsMatchBDS` now replays these independently recorded
inputs through the existing custom rand.Source contract. High bits supply
actor raw words; explicit low-bit controls suppress unrelated Go precipitation
coordinate selection in each loaded column. There is no production test hook,
seed search, relaxed state comparison or order inferred from expected output.
Every native block state and inventory slot is still compared at each of 128
ticks per scene, and the exact number of supplied draws must be consumed.
All twenty scenes pass (entity package, focused run: 3.688 s). This closes the
two uncontrolled-input brewing replay discrepancies; broader world RNG draw
scheduling remains open. The shared suite result and later changes follow.

### Brewing extraction, full-suite findings and native snow RNG budget

Twenty more owned-BDS scenes (`adamant-redstone-bds-lb8z82a4`) extract glass
bottles, water potions, mixed bottle/potion slots and ingredient-only controls
into empty, full and partially filled hoppers. They include an empty slot before
a later matching stack. All initial slots have native API readback controls;
256 held actor-input checkpoints retain the same independent RNG provenance
as the insertion corpus. Two of twenty scenes initially failed: the kernel
merged into the later bottle stack instead of filling the earlier empty slot.
Brewer extraction now uses AddItemInSlotOrder, as the already-corrected generic
container and furnace extraction paths do. All forty brewing insertion and
extraction scenes pass together (4.941 s).

The complete seven-package command after the initial insertion replay passed
block (101.041 s), world (13.953 s), player (18.445 s) and item (2.662 s);
inventory has no package-local tests. Entity (360.765 s) failed
TestVillagersRunFromZombies, and session (3.459 s) failed the two cross-chunk
MovingBlock timing assertions investigated below. Separate twenty-run villager
checks fail four times on the resumed checkout and once on the unchanged
5ff95e72 baseline. This reproduces existing instability; no AI test was hidden,
relaxed or filtered from the full-suite result. No all-green full suite is
claimed at this checkpoint.

The snow/precipitation attempt path had two additional effects on the shared
level RNG before actor shuffling. LevelChunk::tickImpl reads randomTickSpeed
and performs that many attempts, including zero attempts at speed zero.
Each attempt first advances the chunk's own mSnowRandomValue by
`state = state*3 + 0x3c6ef35f` and selects x/z from bits 2..5 and 10..13; only
then does it draw the level's bounded 1/16 roll. Coordinate selection does not
consume two more level draws after a successful roll. The kernel now follows
this attempt count and separate coordinate stream, preserving the existing
freeze/snow callback behavior. The native constructor initializes the counter
to `0x283ae83`; this per-chunk state is independent of the saved world seed.
This does not claim that every downstream weather effect matches Bedrock.

The exact original client arithmetic/store window (`0x32c9b55`, size 0x4e)
executes for 588 supplied states/origins/heights, including signed and large
coordinates. An owned-BDS hardware trace (`adamant-redstone-bds-zyu0lnei`)
changes randomTickSpeed through 0, 1, 3 and 0 and records all four chunk counters
at twenty held checkpoints. All 76 transitions advance by exactly the preceding
callback's configured attempt count. Native chunk construction in BDS also
stores the same initial constant at `0x9f0386`.

New fixtures are `bedrock_1_26_51_precipitation_column_random.json` and
`bedrock_1_26_51_chunk_snow_random.json`. The native coordinate/counter tests,
existing freeze/snow effect test, level RNG, actor scheduling and forty brewing
scenes pass together: world 2.673 s, entity 4.626 s. The brewing input adapter
no longer needs artificial precipitation controls, since both native and Go
scenes now make zero precipitation draws. The per-chunk callback-order test
likewise consumes only its three actual actor/shuffle words, rather than the
earlier five-word sequence including two weather controls.

### Cross-chunk actor eligibility and once-per-second area traversal

An additional owned-BDS trace (`adamant-redstone-bds-jsx88ziy`), repeated with
bounded-call context (`adamant-redstone-bds-jxfl353f`), drives eight pistons
with three-block payloads across x=16. A fourth hardware breakpoint observes
the original MovingBlockActor::tick entry (BDS `0x486f9d0`, matched against the
client `0x4ade460` prologue), recording position, age and dirty flags. The
fixture `bedrock_1_26_51_piston_chunk_actor_visits.json` contains all 100 held
checkpoints and native chunk-pass order.

Ordinary ticks visit the nine rectangular ticking-area chunks in z/x order.
Ticks 20, 40, 60, 80 and 100 use a different order. The two native bounded-255
draws at callers `0x581e87e` and `0x581e895` seed two nonzero 8-bit LFSR cycles.
The next value is `(s>>1) | (((s ^ (s>>2) ^ (s>>3) ^ (s>>4)) & 1)<<7)`.
Values exceeding the rectangle's width/depth are skipped; accepted values
minus one are the local coordinates. The observed ten raw inputs reproduce
all five exceptional nine-chunk orders exactly. Whole-world area traversal,
overlap and player/forced-area integration remain open in the kernel.

`redstone_native_chunk_visit_probe.py` additionally executes the complete
original BDS rectangular helper (`0x581e840`, size 0x165) with supplied RNG and
visitor callbacks: sixty rectangles/start-word combinations, including a
255-cell side, visit every cell once. The separate original client arithmetic
window (`0x351ab19`, 36 bytes) records every nonzero LFSR transition. The corpus
is `bedrock_1_26_51_chunk_visits.json`; it is algorithm evidence, not a claim
that native world area scheduling has already been integrated.

The old session packet replay compares all actor NBT and sound fields but its
input clock is explicitly not a native simulation timestamp. It had also
asserted a universal one-GT creation/publication delay across chunk boundaries.
That assertion was not supported by the fixture. Same-chunk snapshot timing
is still checked. Cross-chunk eligibility now has a dispatcher regression for
both directions: native actor insertion appends immediately, so a chunk still
ahead may tick the new actor, while a chunk already visited waits until its
next pass. No artificial MovingBlock birth delay was added to hide the still
open area-traversal discrepancy.

### Player-area traversal integration and staggered native phases

The ordinary player-loader dispatcher now uses the native rectangle visitor:
z/x raster on ordinary ticks and the supplied two-start LFSR order on each
area's shuffled ticks. Callbacks execute before the next area selects its RNG
inputs. A per-pass visited set prevents shared chunks from ticking twice.
Loader registration is now retained in a stable vector through close/world
transfer, replacing the randomized traversal of the viewer map. This follows
the native dimension area vector rather than choosing a fresh Go map order.
Explicit per-chunk force tickets and unusually large custom areas retain their
existing coverage fallback. Standalone-area grouping/persistence, native circle
eligibility and complete player activation geometry remain open. Manual
Synchronous worlds deliberately retain their stable all-loaded-column walk;
they have no native area membership and do not claim native area timing.

The complete world package passed after initial integration (14.354 s). A root
server build succeeded. A new complete seven-package run then passed entity
(509.030 s), block (100.620 s), world (14.146 s), player (18.722 s), session
(3.384 s) and item (3.536 s); inventory has no package-local tests. This is the
checkpoint before the staggered-phase refinement below, not a retroactive
all-suite claim after it.

A two-area native capture first exposed a missing detail in that initial
integration: area 0 shuffled on ticks divisible by twenty, while area 1 shuffled
on the next tick. Both areas must not shuffle together. The repeated owned
trace (`adamant-redstone-bds-ujsz0vl8`) additionally breaks on TickingArea::tick
(BDS `0x581fff0`) and records its actual Tick, randomize argument and caller.
The native area clock equals the held Script clock. The caller is in the BDS
Level::_subTick at `0x7cc795`. Its index/clock reduction sequence is byte-for-byte
identical to client `0x1171b40..0x1171b82`: each area uses
`uint64(tick)%20 == uint64(index)%20`. Indices beyond twenty reuse that phase.

The original client arithmetic window (`0x1171b4b`, size 0x37) now executes for
2,115 tick/index pairs, including unsigned 64-bit boundaries. The kernel phase
helper matches every result. The new overlap corpus
`bedrock_1_26_51_overlapping_chunk_areas.json` retains 45 held checkpoints,
area-entry flags, all fifteen distinct chunk visits, actor RNG words, bounded
call context and monotonic hardware event sequence numbers.

TestPlayerAreaDispatcherMatchesOwnedBDSOverlap invokes the ordinary area
dispatcher against 44 complete post-startup checkpoints, with the recorded RNG
inputs interleaved in their actual native order. Bounded modulo results are
mapped into the equivalent interval for an explicitly supplied Go rand.Source;
raw shuffle/hopper words are unchanged. No expected visit order selects inputs.
The two hot chunks reproduce the native three-actor shuffle count and empty
hopper draw count; zero-draw single-actor witnesses observe visits to the other
chunks. This isolates dispatch and draw scheduling, not the entire native block
simulation. Every chunk order and consumed-draw count matches, including shared
chunks and the two staggered shuffled phases. The complete world package passes
after the refinement (13.983 s).

The earlier focused combined run (world 2.567 s, entity 82.682 s) also exposed
TestBlockActorDataNBTBytes comparing different valid compound-key orders. The
same test fails on the unchanged 5ff95e72 baseline in a separate twenty-run
check. Its implementation and assertion were not changed. The later complete
seven-package run above passed it as well as the villager test; their observed
instability is retained here rather than hidden from the verification record.

### Native circular block-ticking bounds

TickingAreaView::tick's ordinary circular-area branch uses binary32 arithmetic
to compare squared distance strictly against
`((inclusive width)*0.5 + sqrt(3))^2`, within the area's inclusive rectangular
bounds. The previous kernel block filter used only radius squared and excluded
native corner chunks. A separate block-area predicate now follows this native
branch; block actors/random blocks and scheduled block/fluid eligibility use
that predicate. Square source-defined areas retain their rectangular bound.

The original contiguous client arithmetic window (`0x351ac1a`, size 0xa9)
executes for 2,240 origin/radius/offset inputs. Supplied local Bounds remove the
need for the surrounding mutex operations; the authored ABI wrapper preserves
Win64 nonvolatile registers. Native arithmetic and its strict comparison are
unchanged. These include both sides of rectangular bounds, radii up to 64 and
large positive/negative chunk origins. The kernel matches every circle result
and checks the rectangular bound independently. These cases extend
`bedrock_1_26_51_chunk_visits.json`.

Two real BDS circular ticking areas independently confirm the active footprint:
radius 2 visits 25 chunks, and radius 4 visits 81, including all four corners.
The radius-2 trace (`adamant-redstone-bds-g64pcxx9`) additionally reads the actual
view's child ChunkViewSource: its native bounds are [-2,0,-2]..[2,0,2], dimensions
5x1x5, and its circle flag is true. The client getter returns this+0x238; that
view's source pointer is at +8. Active bounds come from the child source used
by tick, whose loading parent has its own larger bounds.

Fixtures are `bedrock_1_26_51_circle2_chunk_area.json` and
`bedrock_1_26_51_circle4_chunk_area.json` (radius-4 reference root
`adamant-redstone-bds-khph0sx8`). The ordinary dispatcher reproduces nineteen
complete post-startup checkpoints of each, with actual native raw actor words
and bounded inputs, alongside all 44 overlap checkpoints. Eligible witnesses
come from the independently observed native chunk set, so a wrong Go filter
cannot remove corners from both the input and expected output. All visit
orders and draw counts match. A further regression checks the scheduled
block/fluid eligibility gate at (4,4), while a ready chunk beyond the bounding
box remains ineligible. This is not an end-to-end claim for every queued action.

The complete world package passes after the circle integration (14.063 s).
With the additional scheduled-corner regression, world (14.816 s), player
(18.126 s) and session (4.212 s) pass together, and the root server builds again.
The seven-package all-green checkpoint above predates the staggered/circle
refinements; its entity/block/item results are not relabelled as later runs.
Remaining activation work includes actual player loading/retention boundaries,
entity ticking, source creation/removal order, standalone area persistence and
broader large/overlapping-world layouts. Every audit matrix row remains open.

### Crafter inventory routing and immutable snapshot readback

The hopper/crafter corpus now covers 60 enabled-slot insertion scenes, 30
disabled-slot scenes and 12 bottom-extraction scenes. Every per-tick inventory
matches the kernel, including nonstackable items, earliest tied minima, disabled
holes and rejected first source stacks. This confirms existing crafter routing;
it does not yet establish crafting recipes, output insertion or powered timing.

The stable Script API exposes no crafter inventory component on this BDS build.
Each target observation therefore saves a uniquely named one-block structure
before that tick's source changes. After stopping the owned reference server,
cmd/redstone-structure-readback reads those snapshots from its database without
writing it. Native palette types, orientation, disabled masks and initial source
and target inventories are checked before publishing a complete fixture.
There are 7,740 enabled-slot, 3,870 disabled-slot and 1,548 extraction snapshots.
The command's disk save mode persists structures in the world database, as
documented by [Microsoft's structure command reference](https://learn.microsoft.com/en-us/minecraft/creator/commands/commands/structure?view=minecraft-bedrock-stable).

The first enabled-slot capture exposed a readback limitation: the Go LevelDB
reader rejected the first native journal batch's sequence relative to the
manifest and, in its default non-strict mode, silently omitted 1,224 snapshots.
The helper now defaults to strict reading. Its explicit -immutable-audit-wal
mode independently verifies physical journal CRCs/fragments and complete batch
records, then merges only uniquely named audit snapshots. Conflicting values,
deletions and damaged records fail. It never repairs or rewrites a database and
does not claim to fix general mutable-world recovery. The reference capture's
three verified journal batches restore all 7,740 snapshots.

Integrity tests cover fragmented records, matching duplicates, corruption,
conflicts, deletion and truncation. The combined focused check of those tests
and all 102 crafter-routing scenes passes (command 0.213 s, entity 7.468 s).

### Crafter pulse publication and visual lifetime

Crafter previously used immediate generic redstone updates rather than the
native sampled container-consumer cycle. It now uses the same evaluated/published
cycle as dispensers and droppers, with a four-GT queued rising-edge action.
Publishing merges only Triggered into the live actor, preserving its inventory,
disabled slots and crafting countdown. Viewer notification follows publication.
Falling power no longer resets crafting or its remaining ticks: the client's
CrafterBlock::_onRedstoneUpdate (0x6ec3580) clears only triggered_bit, while the
actor independently owns the six-tick visual counter.

The uninstrumented BDS capture bedrock_1_26_51_crafter_pulses.json has 36 scenes:
two absolute tick parities, nine pulse trains and both a valid iron-block grid
and an invalid dirt grid. Samples compare every crafter block state and every
barrel output slot before scripted source changes. A one-GT pulse is missed in
one parity and sampled in the other, and queued crafting survives falling power.
All 36 replays match after the fix (entity 3.393 s). These scenes establish this
recipe/output and pulse clock, not general recipe parity or world ejection.

### Native random container-slot selection

The client dispenser (0x9787796), dropper outer loop (0x9791cc4) and dropper's
container-transfer loop (0x4ac43c5) each use reservoir selection. Each occupied
slot draws modulo the number of occupied slots encountered so far, replacing
the candidate when the result is zero. The first occupied slot consumes a draw
with bound one; empty slots consume none. The previous kernel counted occupied
slots first and made one Go IntN draw, advancing the shared stream differently.

The three original contiguous native loops now execute against supplied slot
occupancy and bounded-word callbacks. The authored ABI wrappers only preserve
registers and supply their surrounding locals; isNull/container-size callbacks
represent the supplied inventory. All 512 nine-slot occupancy masks with four
word patterns produce matching native results and exact draw counts across
all three loops (2,048 cases, bedrock_1_26_51_container_random_slots.json).
The kernel reservoir helper matches every result and consumes the same words.
The focused crafter/container/dispenser regression passes block (2.759 s) and
entity (29.815 s). The additional native dropper container-selection pass is
verified below; that focused checkpoint predates its integration.

An additional uninstrumented 90-scene capture records mixed dropper sources
with barrels, chests and hoppers in all six directions. The owned hardware trace
then independently records 20 barrel scenes over 256 held tick checkpoints
(adamant-redstone-bds-44oo89ad). At the original Core RNG entry it retains the
raw word, bounded caller, divisor and source actor position. Both relevant BDS
call sites are validated: outer dispenseFrom returns at 0x8a2ca55, while
pushOutItems' second reservoir returns at 0x57f0d77. Only this fresh owned child
is debugged; executable instructions remain unchanged and debug registers are
restored before detach.

When a destination container exists, native DropperBlock::dispenseFrom first
selects a candidate and then calls pushOutItems, which makes a fresh selection.
The kernel now consumes both passes and transfers the second candidate. The
first candidate remains the world-ejection fallback. The native trace also
exposed a missing shuffle participant: the kernel filtered actor storage to Go
TickerBlock implementations. Native storage includes actors such as barrels
and droppers even when their virtual tick does no work. Actor insertion/version
tracking and shuffling now include every stored block actor; only invoking the
Go callback still requires TickerBlock.

The common replay feeds only independently recorded words for each scene's
scheduled actions and actual actor chunks, retaining their consumption order.
All 20 traces now match every source/destination slot at every sampled tick and
consume exactly the observed draw count (entity 3.703 s). The native block-actor
regressions pass alongside it (world 1.721 s). General random consumers and
all other dropper/dispenser item behaviours remain open.

### Crafter sided outputs, remainders and chunk-owned queued actions

Native CrafterBlockActor::tryMoveItemsIntoContainer (0x4abbb60) calls the
dropper's sided insertion helper from ascending destination slots, one item at
a time (0x4abbe50..0x4abbe7e). The kernel now follows slot order for generic
containers and the destination's HopperInsertable rules for filtered/balanced
containers. This fixes bypassing furnace and brewing-stand side restrictions,
merging into a later matching stack before an earlier empty slot, and putting
multiple glass bottles into one brewing slot. Comparable crafting remainders
are grouped before insertion/ejection: cake returns a stack of three buckets,
and honey-block crafting returns four glass bottles.

The uninstrumented native corpus has 144 ordinary-output scenes (iron blocks,
oak planks and glass bottles) and 96 remainder scenes (cake and honey blocks).
Each covers six output directions and barrel, chest, locked hopper, furnace
and brewing stand destinations, including earlier empty/later matching slots.
Each source grid is verified immediately after load and after the scenario
using two uniquely named native structures, for 480 stopped-database snapshots.
The Go replay checks all block states, destination slots, dropped stack sizes
and final source slots. It retains native legacy/canonical direction controls.

The first captures exposed cross-chunk brewing-display publication one tick
too early. The client LevelChunk::tickImpl drains its pending queues at
0x32ca437/0x32ca4f8 before its actor pass at 0x32cab8f. The kernel had drained
all scheduled block actions before every chunk. Components with a ported
RedstoneCircuitComponent now drain queued actions in their owning chunk before
copying/shuffling its actors; other Java block/fluid queue paths remain as
before. Native capacity/priority rules and remaining non-circuit block types
are separate open work, so this is not a full native queue-equivalence claim.

Native area shuffle can reverse two neighbouring chunks on its staggered
phase. The first unrestricted capture demonstrated this (a west-facing honey
scene crafted on area 1's shuffled tick). The repeated fixtures explicitly start
every scene at tick%20==2; the observed crafting sample falls outside both
area-shuffle phases. This isolates the queue/actor dependency with the manual
replay's stable axis order. Original discovery captures remain in .tmp; no
synthetic display delay or omitted block-state assertion substitutes for order.
The checked fixtures come from adamant-redstone-bds-18vqbtdn (144 outputs) and
adamant-redstone-bds-g4q3h_qw (96 remainders). All 240 now match, alongside the
36 pulse scenes, 20 dropper RNG scenes and block-actor/scheduled/area dispatcher
regressions (entity 32.570 s, world 2.302 s).

The complete eight-package check before these output/queue changes passed block
(100.946 s), world (14.023 s), player (18.492 s), session (3.359 s), item (2.605 s)
and the readback command (0.229 s); inventory has no local tests. Entity
(366.741 s) failed TestFishSwimAround. A focused repeat failed once in thirty
current runs; the unchanged 5ff95e72 baseline passed an initial thirty but then
failed five of 120 additional runs (98.319 s). The fish test and implementation
were not changed. This checkpoint is not labelled an all-green full suite.

The complete check after the chunk-owned circuit queue and output changes passed
block (100.580 s), world (14.146 s), player (18.440 s), session (3.399 s), item
(2.573 s) and readback (0.257 s). Entity (378.679 s) failed the goat panic and
happy-ghast floating tests; the goat test's baseline instability was already
recorded above. In sixty focused floating runs, current code fails three times
(35.569 s) and the unchanged 5ff95e72 worktree fails ten times (34.534 s).
Neither floating implementation nor test was changed. No native crafter,
queue, hopper, piston or other redstone
regression failed in that run. These results predate the sound addition below.

### Crafter sound packets

The native loopback client capture has twelve scenes: both input parities for
valid crafting into a barrel, invalid and empty grids, valid world ejection,
blocked-container ejection and cake with returned buckets. There are ten actual
LevelSoundEvent packets. BDS is silent when every result fits into the target
container. Invalid/empty grids emit crafter.fail. Ejection emits crafter.craft
once per crafting action, including cake's multiple output stacks. The first
capture's assumption that every successful craft sends a packet was rejected
by these observations rather than implemented.

Both events use the integer block position, empty EntityType, unique ID -1 and
the network hash of the live pre-animation block permutation in ExtraData.
The fixture's north-up triggered/non-crafting state hashes to 1247582580; the
kernel derives that value from its block registry rather than hardcoding it.
Missing failure/ejection events are now emitted with those fields. Receipt
times associate nearby controlled scenes, not native simulation timestamps.
The fixture is bedrock_1_26_51_crafter_sounds.json, owned reference root
adamant-redstone-bds-yg9krev5.

All twelve sound scenes match their API state/output controls and captured
event fields. The combined crafter pulse/output/remainder/hopper-routing,
mixed-dropper and real piston packet regression passes entity (19.419 s), block
(1.860 s) and session (1.929 s). The root server build succeeds after this sound
addition. World-ejected item trajectories, other recipes and persistence
remain open; current stack/count agreement is not a motion-parity claim.

### Crafter ejection origin and native floating RNG

Twelve additional uninstrumented six-direction/parity scenes record the
world-ejected actor's position, velocity, AABB and ground flag on every tick
(bedrock_1_26_51_crafter_ejections.json, adamant-redstone-bds-wfzukz3m). These are
motion discovery traces; their initial RNG inputs are not yet replay controls.
Their first observations can already include a physics/collision step and must
not be equated blindly with the constructor's initial velocity or position.

The crafter calls DispenserBlock::ejectItem, with a different launch point:
binary32 x/z+0.5 and y+0.3, followed by the facing offset of +/-0.7. The shared
routine subtracts 0.125 vertically or 0.15625 horizontally from launch Y.
The kernel's y+0.5 launch and fixed direction*0.2 velocity were different.
Crafter ejection now uses the inspected origin and shared native velocity:
horizontal speed nextFloat*0.1+0.2, initial Y speed 0.2, followed by three
cached binary32 Gaussian values scaled separately by 0.0075 and then six.
Facing changes the base horizontal components, not the base upward speed.

The unchanged client velocity window 0x9788485 (0x3af bytes) executes against a
supplied actor state and native seeded RNG. Its nextFloat callback executes the
original Core::Random float conversion; inline rejection/caching instructions
remain native. Forty consecutive launches in each of 36 seed/facing streams
retain cache carry-over, independently recorded raw words and exact budgets.
The initial wrapper's X/Z local mapping was corrected against the direction
tables before accepting this fixture. The corrected 1,440 velocities match
the kernel bit for bit, with no tolerance or altered draw budget.

The original crafter position window 0x6ec2569 (0x88 bytes) and generic ejection
Y-correction window 0x97883e8 (0x33 bytes) independently produce 48 launch points
covering all twelve native orientations and large positive/negative positions.
Those also match bit for bit. All belong to
bedrock_1_26_51_ejection_velocities.json.

One subtle numerical dependency is explicit: the copied client velocity code
forwards logf/sqrtf to the host Windows CRT. This capture uses ucrtbase.dll SHA
dedb7611e1abc1ff3852dcac3d4c93a048d729e25b061973b9969f22698f14ca,
logf RVA 0xa9d40 and active FMA dispatch 3. Rounding Go's double math.Log to
float32 differed by one ULP in the recorded stream. A restricted positive-normal
Gaussian-radius logarithm now follows that inspected CRT's table reduction and
fused arithmetic. Tables are derived mathematically from ln(1+i/128): heads
clear twelve mantissa bits, positive tails round toward zero, and reciprocals
use 2/(1+i/128). No library binary or extracted instruction bytes are stored in
production. The helper matches all 30,727 independently called CRT values,
including table-rounding and near-one branch boundaries. Other CRT/CPU profiles
are not claimed equivalent by this comparison.

Tx.RandomFloat32 and RandomGaussianFloat32 use one raw-word conversion and the
world's cached native Gaussian pair, sharing the simulation stream with raw
draws. The focused logarithm/origin/velocity and complete crafter pulse/output/
remainder check passes block (2.756 s), world (1.679 s), entity (12.766 s).
The live trajectory follow-up below resolves initial eligibility and the
isolated six-direction motion scenes. Ordinary dispenser/dropper integration
and broader RNG consumers remain separate work.

### Live crafter ejection: construction draws and chunk activation

The owned BDS follow-up scopes the complete DispenserBlock::ejectItem call,
recording raw RNG calls between entry and return and ItemActor::postNormalTick
entries afterwards. No original instruction bytes are changed. The accepted
fixture is bedrock_1_26_51_crafter_ejection_actor_inputs.json, reference root
adamant-redstone-bds-rqsadg7k: twelve six-direction/parity scenes, each with one
hundred API samples. These are debugger-observed traces, distinct from the
earlier uninstrumented discovery capture.

Construction consumes one shared float before the speed and Gaussian draws
(native parent return 0xd1c84d; speed parent 0x8a23385). The inspected constructor
uses it for an internal actor flag with no Go equivalent; the kernel preserves
the draw without assigning an invented visual property. The replay warms only
RNG state from earlier recorded ejection scopes, then supplies the current
scene's independent native words and checks the exact consumption budget.

Item post ticks remain in the global post phase: the final ejection return
precedes every item post tick in the recorded birth frame. Eligibility depends
on the destination chunk's visit. Down/up/south/east items tick in their birth
frame; north/west items enter an already visited chunk and first tick in the
following frame. Moving item physics into a per-chunk callback would contradict
that ordering. The kernel instead tracks chunks still pending in the block
pass and admits newborn queued items only if their owning chunk is pending.

All twelve scenes now match every recorded position, velocity, AABB and ground
flag through flight and landing, preserving one output actor's identity and
stack (entity 3.295 s before the pickup follow-up). The native post-tick entries
also expose PickupDelay at actor+0x44c as zero. Crafter output now explicitly
uses zero pickup delay; the ordinary item factory's default remains unchanged.
This verifies the field, not player collection latency in an unrecorded scene.

The combined crafter/ejection/Gaussian/hopper/dispenser/dropper/queue/piston
regression passes entity (109.198 s), block (1.268 s) and world (1.682 s) after
the pickup follow-up. Session (1.175 s) fails only TestBlockActorDataNBTBytes:
the identical NBT fields `id` and `rott` are serialized in the other map order,
the previously documented byte-order-sensitive test failure. Its assertion is
retained. The subsequent complete eight-package run passes block (147.560 s),
world (17.854 s), player (21.094 s), session (3.707 s), item (3.421 s) and readback
(0.226 s); inventory has no tests. Entity (584.015 s) fails only
TestPigsWanderWithoutCarrotsOnSticks. A focused current repeat fails once in
sixty runs (70.592 s); an initial sixty unchanged-baseline runs pass (53.942 s).
Another 180 unchanged 5ff95e72 baseline runs fail four times (190.620 s), proving
the unchanged wandering test's instability. No redstone regression failed, but
this checkpoint is not labelled an all-green suite.
Other entity activation graphs, recipe coverage and persistence remain open.

### Ordinary dispenser/dropper follow-up evidence

The ordinary-item follow-up retains twenty-four six-direction/parity scenes
with a three-ingot stack in slot four. Native whole-ejection scopes, the
immediately preceding bound-one selection draw, one hundred API samples per
scene and zero-delay item post ticks are retained in
bedrock_1_26_51_dispenser_ejection_actor_inputs.json (owned debugger-observed
reference adamant-redstone-bds-mofiy18z). Both ordinary blocks use the same
binary32 y+0.3 launch arithmetic as the crafter; the old kernel uses y+0.5 and
fixed direction*0.2 instead. Both now use the shared native item launch,
construction draw, Gaussian velocity, zero pickup delay and chunk-owned birth
eligibility. The replay also consumes the recorded pre-ejection selection word.
All twenty-four ordinary scenes and twelve crafter scenes pass every motion,
stack/slot and exact draw-budget assertion (entity 5.235 s); the independently
executed origin/velocity corpus passes block (2.140 s).

A separate uninstrumented loopback capture records ninety-six scenes covering
both blocks, six directions, both input parities and empty inventory, air,
barrel and full-barrel destinations. All source inventory controls match. The
fixture bedrock_1_26_51_dispenser_sounds.json retains 132 scene-associated
packets (adamant-redstone-bds-zcmaqlmr). Empty inventories emit block.click.fail
at the integer source position. A dropper transferring into either accepting
or full containers is silent. World ejection sends a 2000 smoke LevelEvent and
block.click at the uncorrected launch point, whose containing block determines
ExtraData. Droppers additionally click at their integer source position.
Vertical smoke direction data is four; north/south/west/east are one/seven/
three/five. The positive-facing launch offset is 1.2 from the integer source,
so packet association includes that point rather than using a unit-radius
filter. Every scene's expected packet count is checked; the unrelated player
spawn packet is retained in raw observations and excluded from scene matches.

The kernel now emits these ordinary item effects and empty-inventory failures.
A real session viewer publishes smoke as LevelEvent 2000 with the facing data
above and keeps its order before launch-point click and optional dropper source
click. All ninety-six scenes match the full captured LevelEvent/LevelSoundEvent
objects, source permutations and source/destination counts (session 3.956 s).
The first broader check passes the entity/container/queue regressions (114.656 s)
and world (2.542 s), but finds three old block factory tests expecting fixed
direction*0.2 motion. Those tests now take their exact velocities, origins and
draw budgets from the independently executed client corpus, preserving fallback
factory/count assertions rather than retaining the superseded motion model.
The focused factory check passes block (2.905 s, then 2.819 s).

### Crafter saved animation and initial container publication

Twelve uninstrumented native structure save/load scenes capture every animation
age from zero through five at both input parities. Each retained disk structure
is read from the stopped reference database with verified immutable WAL handling.
The fixture bedrock_1_26_51_crafter_loads.json has 1,344 API frames and twelve
native actor records (adamant-redstone-bds-sk6ba3eq). At ages zero through four,
the saved crafting block state remains true after load, but its animation
countdown is absent from actor NBT. Age five saves a false state. BDS retains the
true state on the loaded copy rather than resuming its old actor's countdown.
This records native behavior even though the retained visual flag is unusual;
it is not an invented countdown repair.

The kernel's private adamant:java.crafting_ticks_remaining made the copied
actor finish the previous countdown. Encoding/decoding no longer persists that
transient timer. A second exposed difference was the loaded triggered flag:
the initial zero consumer input never changed, so a saved true flag could stay
true indefinitely. ConsumerComponent::evaluate at 0x66d899e..0x66d89a5 includes
an initial update flag even when strength stays zero. The container evaluator
now reconciles visible triggered/toggle state with evaluated power even without
an input edge. It still schedules activation only on a visible false-to-true
transition. Every source and copied-block frame in all twelve save/load scenes
matches after both corrections (entity 3.083 s).

An additional native corpus independently checks that initial publication for
crafter, dispenser, dropper and hopper, with stored flag false/true, initial
power false/true, both load parities and a later input reversal. All thirty-two
native captures are complete in bedrock_1_26_51_container_load_flags.json
(adamant-redstone-bds-4oov17nl). All 2,304 flag frames and immediate load states
match the Go replay. Together with the twelve saved-animation and thirty-six
ejection-motion scenes, the focused replay passes entity (5.252 s), and the
ninety-six real-session effect scenes pass session (2.256 s).
Full world restart/unload activation graphs remain open. The root server build
passes after the ordinary effects and saved-animation/publication fixes.

The broader eight-package redstone selection after these production changes
passes session (4.388 s), entity (151.086 s), block (41.384 s), world (3.182 s),
player (3.768 s) and item (2.683 s, no matching tests); inventory has no tests
and readback has no matches in this selection (0.201 s). The selection includes
dispenser/dropper/crafter, hopper, item merge, piston, actor, scheduled-update,
RNG and chunk area/visit checks. It is explicitly narrower than the complete
suite recorded above and does not replace that suite's failure ledger.

### TNT initial pulse and state evidence

The next row begins with seventy-two uninstrumented TNT scenes: six source
faces, direct redstone blocks or buttons strongly powering a support block,
both input parities and pulse widths of one, two and eight game ticks. All
long-pulse positive controls prime exactly one TNT actor. Native TNT ignores a
one-GT pulse at one parity and publishes priming after two or three observed
GT depending on input phase. bedrock_1_26_51_tnt_pulses.json retains 5,184 source/
TNT block frames and actor observations (adamant-redstone-bds-4xd1_605).
Trajectory and fuse equivalence are not implied by actor-count/priming timing.

An apparent state difference was checked on the wire before changing the
palette. Stable Script API reports allow_underwater_bit together with the
resolved tnt/underwater_tnt ID, but native network permutations omit that old
selector. Ten command/legacy-structure scenes and twelve actual block updates
prove all four wire hashes using only explode_bit. Legacy authored structures
load into the same native states; the direct underwater_tnt command spelling
is rejected, while tnt with the selector is accepted. The fixture is
bedrock_1_26_51_tnt_states.json (adamant-redstone-bds-vkxc7jzf). Packet tooling
now records both UpdateBlock and batched UpdateSubChunkBlocks. The failed
initial capture interpretations were not used to rewrite the palette: the
packet hash check retains raw SDK and wire permutations separately and verifies
the alias against the resolved type. Native block comparison handles this
specific verified alias, as it already handles the SDK's legacy stone_type.

The baseline kernel primes too early in all seventy-two pulse scenes. TNT now
uses a late-input scene consumer on the shared two-GT clock and primes from
its accepted publication rather than the immediate neighbour action. The
existing cancellation path still gates publication; gamerule/igniter behavior
is unchanged by this clock fix. All 5,184 block/source/count frames and stable
primed actor identities match (entity 4.179 s). Wire-state and TNT/cancellation
checks pass block (1.964 s) and world (1.997 s); session has no matches in that
focused selection. No TNT trajectory, fuse, explosion-chain, unstable-break or
underwater activation equivalence is claimed by these checks.

The shared TNT/circuit/note/bell/piston/crafter/container regression subsequently
passes block (44.953 s), entity (56.078 s), world (2.898 s), player (4.266 s) and
session (3.494 s). This predates the note corrections below.

### Note-block event, instrument and packet evidence

The note follow-up records BlockSource::blockEvent entry with a hardware
execution breakpoint in a fresh owned BDS. The client body at 0x2d84440 is
located uniquely in the validated BDS; position, event type/data and actual
caller are recorded without patching instructions. Held Script API checkpoints
associate calls with native game ticks, rather than packet receipt time.
bedrock_1_26_51_note_block_events.json contains 112 scenes, 128 checkpoints and
96 actual note callbacks from 0x65edfb8, covering all sixteen instruments,
pitches 0/12/24, both input parities, widths one/two/eight, repeated pulses and
stone/glass/water/leaves above the note block. The accepted reference is
adamant-redstone-bds-6mrv5l9p. An earlier discovery had unsupported sand; the
repeated fixture supports it on stone and verifies the instrument substrate's
actual type every frame. That discovery is not the accepted instrument oracle.

A separate uninstrumented loopback capture has 46 compact scenes with 96 real
packets (bedrock_1_26_51_note_packets.json, adamant-redstone-bds-h8gyd0ll).
BDS sends both BlockEvent and LevelSoundEvent, in that order. The sound is
named note, with empty EntityType, integer block position, unique ID -1 and
ExtraData = (instrument << 8) | pitch. The first hypothesis that the separate
sound was redundant was rejected by these actual packets. The instrument is
identical in the preceding BlockEvent. The kernel instead sent sound first,
used the note block's embedded bass instrument for BlockEvent, and sent ':'
as EntityType. Its immediate neighbour action also played before the native
consumer publication and accepted the missed one-GT pulses.

The kernel now shares the native container consumer clock for Note, merges
only power into the live actor to retain pitch, selects the substrate instrument
once for both packets and publishes BlockEvent before sound with empty
EntityType. The 112 timed-event and 46 packet replays pass alongside container,
crafter and TNT regressions: session (9.504 s), entity (23.210 s), block (3.476 s);
world has no matches in this selection (1.083 s). Saved note power, player tuning,
additional substrate/occlusion paths and vibration integration remain open.


The original unsealed-water scenes expose a separate environment difference:
the kernel replaces the adjacent source cell with flowing water at tick 53,
whereas BDS retains air before the scripted power changes. Both full native
128-frame traces are preserved in bedrock_1_26_51_note_water_flow.json, reference
adamant-redstone-bds-nf1vcg52, and the failing replay is retained in the local
verification log. This is an open setup/activation/flow dependency, not a claim
that one fixed fluid delay is wrong. The accepted acoustic-isolation fixture
explicitly encloses the water above the note in stone; its sealed_water input
is reproduced in Go. Source permutation assertions remain intact and the
unsealed evidence is not discarded. The old rising/falling note unit test now
waits for each published edge rather than inspecting the old immediate state;
the strict 112-scene oracle separately fixes the exact allowed ticks.

The subsequent complete eight-package run passes entity (548.324 s), world
(28.445 s), player (36.309 s), session (11.843 s), item (5.623 s) and readback
(0.373 s); inventory has no tests. Block (181.991 s) fails only the old lever-
through-support note test, which sampled before the consumer publication. This
run predates the note-load and bell fixes below and is not called all green.

### Note actor loading and first-time circuit events

Twelve further owned native traces load pitches 0, 12, 24, 25, 127 and 255,
with both values of the old private powered tag and power already present.
bedrock_1_26_51_note_load_events.json (adamant-redstone-bds-kpkqetd6) records
twelve actual callbacks on the later re-trigger, with pitches above 24 clamped
to 24. The initial positive circuit event does not play. The inspected client
load at 0x4aded37..0x4aded4c zero-extends the note and clamps it; save at
0x4adec90 writes only note after its base save, not the actor's power history.
The redstone handler at 0x6ea6727 checks the event's mIsFirstTime flag at +0x24
and returns before changing that history.

Note loading now clamps pitch, resets transient power and omits the old private
power persistence. Its consumer ignores the first evaluation and subsequently
publishes actual input edges, rather than reconciling actor power with initial
scene strength. The strict 112 original scenes, twelve load scenes and 46 real
packet scenes pass together (session 10.961 s). The old lever/conduction units
must first register an unpowered component, then wait for each published edge;
without that setup they exercise the native initial-event suppression instead
of conduction. The corrected units pass block (3.663 s), alongside the note
replays (session 3.363 s), before the bell implementation below.

### Bell consumer sampling, animation and wire publication

Sixty-four uninstrumented scenes cover four bell directions, direct power and
strong button conduction, both input parities, one/two/eight-GT pulses and a
repeated pulse train. Per-frame structures retain 7,168 native Bell actors,
verified against each API permutation in stopped read-only LevelDB/WAL
(bedrock_1_26_51_bell_pulses.json, adamant-redstone-bds-t0rdhk__). They expose
the missing two-GT consumer gate, actor-owned edge detection and the distinction
between input power and toggle_bit. The latter flips on each accepted ring and
does not clear on a falling input. Native ringing resets Ticks to zero and its
animation counter advances independently of that input.

A separate owned hardware trace records all 72 BellBlockActor::ring entries
inside their owning chunk's actor pass: BDS 0x1f1c400, caller 0x1f1c370, validated
against client 0x2db8720 and its field-reset body. All have null actor and forced
true (bedrock_1_26_51_bell_ring_events.json, adamant-redstone-bds-q75_3v_3).
The client tick increments animation first, gates consumer reads using the
dimension counter at 0x2db850e..0x2db851a, and rings on a positive edge at
0x2db8665..0x2db868b. This explains the extra observed tick after evaluation
without adding a synthetic delay. Bell now reads a dedicated scene consumer
on that actor gate, keeps transient power history separately from toggle_bit,
and retains that toggle across release. Every block/source/actor frame matches
all 64 scenes (entity 6.385 s), with bell/note/TNT/loaded-container units passing
block (2.529 s) and session (4.458 s).

Thirty-two more original-BDS scenes record 304 actual packets: 128 block
updates, 128 actor compounds and 48 bell sounds, with 3,584 per-tick snapshots
(bedrock_1_26_51_bell_packets.json, adamant-redstone-bds-t0dx6u1g). The sound
precedes updates, uses the integer bell position and empty EntityType. A ring
sends block flags 3 then 1, followed by two identical ringing actor compounds
at Ticks zero. Bell state/actor publication now preserves that sequence through
the existing bounded queue. The first complete packet/actor/queue/piston/note
check passes session (8.543 s), entity (4.122 s) and block (2.553 s).

The following flush-isolation check exercises both normal flushing and a viewer
that flushes the ordinary state before the ring action arrives. Redstone ring
NBT is left to its dedicated action, which retains the actor pair and sends
only the remaining neighbour update if the network state was already flushed.
Ordinary bell initialization still includes its actor coordinates. These tests
retain every native field, duplicate and packet order. Full bell placement,
player/projectile ringing, support changes, hearing/vibration behavior and
reload/activation graphs remain open.

The completed normal/split-flush comparison passes session (6.470 s), block
(3.414 s) and entity (4.561 s). A further ordinary regression queues 400 stone
updates before a bell ring and verifies that both state flags and both actor
snapshots survive the bounded state/actor backlog. It passes in the subsequent
session run; that run still failed the two retained water scenes, resolved below.

### Static water activation and actor-only note publication

Twelve further unmodified BDS traces isolate static command water, explicit
flowing_water placement and actual neighbouring placement/removal, at both
input parities. They retain every sampled state of eleven cells for 128 ticks
(bedrock_1_26_51_water_activation.json, adamant-redstone-bds-i35by00_). Static
water remains dormant without a real neighbour mutation. A change above it
starts lateral flow five GT later; descent follows another five GT later.
Explicit flowing_water is converted to water immediately and starts flowing
five GT after placement. Falling streams retain their lateral liquid_depth:
the checked falling cells have depth 9, not 8.

Client LiquidBlock::neighborChanged (0x345b040) schedules against the live block
and chunk queue. LiquidBlockDynamic::onPlace (0x345fbb0) installs its static
mLiquidBlock and schedules activation. LiquidBlock::_tick at
0x345c364..0x345c36a adds the falling bit to the existing depth. Water now receives
loaded neighbour notifications synchronously in either layer, ignores synthetic
self notifications, and no longer consumes deferred notifications created before
the water existed. Physical SetLiquid placement still activates water through a
separate optional placement callback. Native water ticks bypass the legacy Java
fluid-state adapter, whose falling-state conversion discarded the native depth.
Other fluid adapters have not been declared equivalent by this water corpus.

The retained unsealed note scenes additionally exposed a false block replacement
on each note power-history change. Native note power is actor-only; replacing its
permutation incorrectly wakes dormant water above it. The circuit publication
path now permits actor-only consumer output, preserving event cancellation and
the post-publication callback without sending neighbour mutations. All twelve
water traces and both original unsealed note traces now pass strict per-frame
state comparisons, alongside note events/load/packets and bell packet/actor/queue
tests (entity 3.637 s, session 3.915 s, block 1.962 s). Subsequent physical-water
and full shared checks are recorded separately; this is not full fluid parity.

### Bell save/load evidence

Sixteen additional owned original-BDS scenes save a bell at ring ages 0, 5, 24
and 25, then load it with or without power already present, at both source
parities (bedrock_1_26_51_bell_loads.json, adamant-redstone-bds-fbk250yc).
The stopped immutable LevelDB/WAL readback verifies all 1,656 input/per-frame
actor snapshots against the corresponding API permutation. Native loads retain
Ringing, Ticks and Direction. Initial positive scene power can ring the loaded
bell again and flip its saved toggle_bit; unlike the note consumer, this actor
does not suppress that initial positive input. Unpowered loads continue the
saved animation, and later scripted power edges exercise repeated ringing.
The ordinary replay compares all 160 frames, both source positions, both bell
positions and every saved actor field. It passes with the broader entity
container/ejection/TNT/water selection (78.637 s); the full bell lifecycle row
remains open.

The full block package passes (119.776 s), as do player (24.291 s), item
(3.842 s) and structure readback (0.615 s). That first broader run exposed the
old note-save expectation and fluid tests whose physical setup now correctly
reserves an earlier pending activation. The note assertions now enforce native
transient power. Queue-clock and schedule-only tests install dormant static
water, or reserve their explicit tick before physical placement; their original
delay, identity, geometry and dispatch assertions remain unchanged. Native-only
extension boundaries retain their existing positional collision/SideClosed
contract through the shared mixed-boundary aperture calculation. The focused
follow-up passes world (3.163 s), session (5.457 s), entity (3.769 s). The next
complete world/session runs pass (14.093 s / 6.951 s), and the root server build
succeeds. These results precede the player-use bell changes below.

### Bell player-use direction and wire evidence

Sixty-four owned native player scenes cover all four attachments, all four
permutation directions and approaches from all four horizontal sides. There
are forty accepted and twenty-four rejected interactions. The corpus retains
2,928 verified saved actor frames and all 272 position-associated native
packets (bedrock_1_26_51_bell_use.json, adamant-redstone-bds-v3rllv8y).
The audit client can hold an explicit look rotation before interacting; every
accepted fixture verifies the actual server-side yaw in the native before-event.
An earlier discovery capture kept the client's default northward idle yaw and
is not used as this directional oracle. Native after-events occur only for the
accepted uses, so rejected uses correctly have no after-event.

Client BellBlock::use at 0x2a5f6c0 selects the player's horizontal look direction
and applies attachment-specific axis gates at 0x2a5f9ff..0x2a5fa3a. Standing
bells accept their facing axis, hanging bells either axis, side/multiple bells
the perpendicular axis. The swing direction comes from the opposite-direction
table before the actor ring call at 0x2a5facd, not directly from the packet's
clicked face. Every accepted use flips toggle_bit. Its actual wire publication
is state flags 3, one actor compound at Ticks zero, then sound. A rejected use
confirms the unchanged permutation with flags 3 and no new actor compound.
This differs from the separately verified redstone actor-pass duplicate sequence.

The implementation now applies those direction gates and toggles at the common
ring entry point. Manual sound can wait behind the bounded state/actor queue,
and player rejection sends only the state confirmation to that player. The
strict replay retains all native actor fields and packets, with both ordinary
and split viewer flushing. It passes (session 9.113 s) alongside bell load,
redstone, note, water and fluid-clock regressions. A backlog regression then
demonstrated that replacing a pending bell state or receiving a rejected click
could erase its unsent sound and animation. State coalescing now retains queued
effects, and rejection preserves the earlier pending actor/effects without
creating a new actor notification. The regression retains both accepted sounds
after the final state/actor behind 400 ordinary updates. It failed before the
fix (zero actors/sounds) and passes afterwards, together with all native bell
player/redstone/load/packet and piston queue selections (session 7.006 s,
entity 54.083 s, block 55.646 s, world 2.235 s, player 3.716 s). This does not
close projectile, support, hearing/vibration or broader bell lifecycle work.

### Bell arrow collisions, native shapes and split entity phases

Sixty-four uninstrumented BDS arrow scenes cover all bell attachments,
directions and horizontal approaches, with both actual input parities. Each
retains eighty frames of the full bell permutation, saved actor and arrow
position/velocity/AABB (5,120 actor snapshots). The accepted capture records the
actual native spawn position separately from the requested position: BDS can
move an arrow placed inside an adjacent support before its first physics step.
bedrock_1_26_51_bell_projectiles.json uses adamant-redstone-bds-36hne4ff. All
sixty-four arrows hit the bell, including directions rejected for player use.
Their ring follows the bell's facing rather than the impact face. The native
handler at client 0x2a61e30 has no player-use axis gate and excludes the two wind
charge types, whose activating-attack route remains separate and open.

The original BellBlock::getVisualShape at 0x2a5fbe0 was executed for all sixteen
attachment/direction combinations, with host callbacks supplying only native
state-reader values. The original collision default's virtual dispatch at
0x3182a80 was additionally executed for 48 translated cases, with no per-state
collision override or block offset component supplied. Original arithmetic and
tables remain unchanged (client_1_26_51_bell_shapes.json). The old Java-style
multi-box model disagreed: native standing height is 13/16, hanging/side/multiple
outlines are single aggregate AABBs, and side/multiple axes differ. The corrected
model agrees with all sixteen original shapes and the live arrow collisions.
This default-path execution alone is not a claim about arbitrary custom native
collision overrides or all entity contacts.

The arrow comparison also exposed the distinction between normal-tick button
contacts and ProjectileSystem movement. Moving the entire arrow tick after
blocks fixed the bell's animation origin but broke the retained wooden-button
wire timing. The kernel now samples the arrow's earlier button contact before
the circuit/block pass while running its movement/collision afterwards, through
a bounded once-per-GT pre-block callback. Bell animation therefore starts at
Ticks zero on impact, while the existing 180 button projectile traces retain
their exact source and wire timing. This was checked as a shared dependency,
not by loosening the button assertions or adding a bell-specific tick delay.

Twenty-eight further native free-flight scenes cover seven launch vectors,
two heights and both input parities (bedrock_1_26_51_arrow_flight.json,
adamant-redstone-bds-b8bqvhv7). Every one of their 64 frames is retained. Arrows
trace the preceding velocity, then apply drag/gravity for the next step; position
addition, drag and gravity each round as binary32. The ordinary replay requires
exact position and velocity equality, in addition to its AABB comparisons.
The original double arithmetic accumulated visible test differences and has
been replaced for native arrows. No broader projectile-fluid/acceleration
equivalence is inferred from these air scenes.

An independent packet capture at separated six-block scene spacing retains
another 5,120 actor frames and all 320 bell packets, with actual native birth
positions (bedrock_1_26_51_bell_projectile_packets.json,
adamant-redstone-bds-fj0xgaib). Setup/collision states use flags 3, one actor
snapshot, then sound; the redstone duplicate sequence is not used here. The
strict physical-arrow replay compares every actor field and every packet at
the bell position, with normal and split viewer flushing. The full native bell,
arrow motion, arrow/button and pre/post-block selection passes entity (6.127 s),
session (6.183 s), block (3.263 s) and world (1.953 s). A complete eight-package
run and root build follow; their result is not assumed from this focused pass.

### Bell support lifecycle and remaining packet diagnostic

Another 128 original-BDS scenes cover all four attachments and directions,
both input parities, support removal/replacement, adding a second side support,
two ordered mutations in one transaction, and deliberately unsupported command
states. Each retains 64 frames of six cells
(bedrock_1_26_51_bell_supports.json, adamant-redstone-bds-zo7x9xp5). A separate
connected-client capture preserves all 416 associated packets
(bedrock_1_26_51_bell_support_packets.json, adamant-redstone-bds-fboi3t1d).

The state fix now processes actual loaded neighbour mutations synchronously,
ignores synthetic self-notifications and does not replay deferred notifications
created before the bell existed. Unsupported command bells remain until a real
neighbour change, matching native behavior. Adding the opposite side support
converts side to multiple attachment; the next ordered removal can then select
the remaining wall and reverse facing. The ordinary
TestBellSupportStatesMatchBDS retains all 128 traces and every cell/state check.
It passes alongside the existing bell/note/water/arrow/native-button selection
(session 12.067 s, block 6.426 s, entity 7.342 s, world 2.331 s).

At the 2026-10-29 checkpoint, wire parity for these support mutations was still
unfinished. The queue then
collapses intermediate same-position permutations and actor notifications;
removing a bell sends air with flags 2 where the captured native update uses 3.
The strict packet diagnostic preserves these mismatches without loosening their
assertions. It is explicitly separate from the completed state regression and
is enabled with:

```text
go test -tags redstone_bell_support_audit ./server/session -run '^TestBellSupportUpdatesMatchNativePackets$' -count=1
```

This command was expected to fail at that checkpoint. It was rerun before
publication (session 4.664 s) and still exposes the recorded discrepancies.
The user paused the audit and requested commits/publication; this diagnostic
records the next work rather than claiming the support packet path is fixed.
The resumed fix and ordinary regression below supersede this opt-in diagnostic.

### Publication checkpoint on 2026-10-29

The completed core snapshot passes the full eight-package check: block
116.765 s, entity 453.236 s, world 17.802 s, player 22.283 s, session 11.710 s,
item 3.805 s and structure readback 0.254 s; inventory has no tests. That full
run precedes the small bell-support state fix above, whose new ordinary
128-scene regression and related shared selection also pass. The root server
build succeeds after the support change, retaining the root entry point for
`go run .`. The final complete block/session/world/player check also passes:
117.719 s / 9.681 s / 14.963 s / 23.489 s. This checks the published support
state fix as well as the shared code; the opt-in packet diagnostic remains an
explicitly documented failure, not part of a claim of completed wire parity.

The implementation/evidence snapshot is committed in `79bed5c9`; the verified
support state transitions, retained native captures and unfinished wire
diagnostic are committed separately in `bc2467f9`. Private operator/ban data
and the temporary gameplay log are excluded from this publication.

The effort estimate is now approximately 70%, increased from the prior 65%
planning checkpoint. It describes work performed, not proven global parity:
all 22 matrix rows still retain open coverage. The audit remains paused after
the user's request; creating and sending this checkpoint does not resume it.

### Resumed bell support packet audit on 2026-10-31

The original strict diagnostic reproduced the support publication differences
without changing the retained BDS corpus. Support-driven attachment changes now
publish their block state independently of the actor notification. Actor data
waits until the enclosing world callback ends, so two consecutive surviving
permutations publish both states before both actor snapshots even if a viewer
flushes between source mutations. Removing the bell seals pending support
states/actors before the air update and retains native flags 3. The transient
publication markers reset on load, ring and actor tick; saved actor NBT is
unchanged.

TestBellSupportUpdatesMatchNativePackets is now an ordinary regression without
an opt-in build tag. It compares all 416 native packets in all 128 scenes with
normal flushing, split flushing, and a 400-position backlog. No native fields,
duplicates or intermediate updates were removed from its comparison. The full
bell/moving-actor selection passes (session 6.202 s), including existing player,
arrow and redstone ring sequences. Additional queue tests exercise 140 real
support mutations with and without final removal across the 128-block batch
boundary, plus a flush between the removal marker and foreground replacement
(session 0.431 s). Pending per-position history is bounded, and each flush now
counts actual block packets rather than assuming one packet per position.
Selection, packet queuing and retaining partial entries now share the pending
block mutex. The outgoing packet queue is non-blocking and bounded; no encoding
or network I/O runs under that mutex. This avoids losing an unsent support or
removal marker to a concurrent newer update during a detached-map flush.

The full shared check passes block 131.857 s, session 13.457 s, world 15.614 s
and player 18.707 s. The precise bell/arrow/button/water/piston entity selection
also passes (33.468 s). A broader preliminary entity filter additionally ran
TestBredFrogLaysFrogspawnOnNearbyWater and failed once (one frog instead of two);
twenty isolated unchanged-baseline repetitions pass (5.779 s). That isolated
baseline success does not prove the cause of the preliminary failure.
The stronger comparison uses an untouched detached worktree at the published
`e5eea7aa` checkpoint: the same frog test fails 4/100 repetitions (44.814 s),
versus 1/40 on the current tree (10.947 s). Its assertion and AI implementation
are unchanged by this support fix; this is a reproduced pre-existing failure,
not a green full-entity-suite claim. After the final queue mutex change, the
complete session package passes again (11.270 s), and the root server builds.

The next inspected client path was BellBlock::_onHitByActivatingAttack at
`0x2a62040`. It rejects an already-ringing actor at `0x2a620d8` before calling
the ring body at `0x2a621e3`. The shared Go wind-charge interaction initially
had no bell case. The native capture and fix are recorded in the next section.

This closes the retained support-state/packet diagnostic, not the full bell
coverage row. Wider placement eligibility, broader wind-charge/attack, hearing and
vibration, chunk lifecycle and the other outstanding matrix requirements remain
open. The effort estimate remains approximately 70% of the full audit.

### Bell activating attacks and actual breeze projectiles

Four independent original-BDS corpora retain 64 scenes each, all four bell
attachments/directions, both input parities and single/repeated hits ten ticks
apart. Each stores 80 frames and 5,120 original saved bell actor snapshots:

- bedrock_1_26_51_bell_wind_projectiles.json: player wind charges, owned root
  adamant-redstone-bds-ces7svv3;
- bedrock_1_26_51_bell_wind_packets.json: independent connected-client capture,
  320 bell packets, adamant-redstone-bds-cmlwehb1;
- bedrock_1_26_51_bell_breeze_projectiles.json: actual breeze-fired charges,
  adamant-redstone-bds-yu1t7zxr;
- bedrock_1_26_51_bell_breeze_packets.json: independent connected-client capture
  of actual breeze-fired charges, 320 bell packets, adamant-redstone-bds-3pg4dodb.

The native breeze projectile is not summonable; the first direct-spawn pilot
was rejected by BDS and is not used as an oracle. Instead, unmodified native
breezes shoot at iron golems. The probes retain each of the 96 original birth
bodies and breeze owner identities, hold those real actors in air, and use
explicit teleport/shoot inputs to control the subsequent bell impact. No entity
definition is replaced. The resulting evidence measures controlled projectile
and bell behavior, not natural breeze targeting or launch accuracy.

All scenes ring once, along the bell's facing, without the player-use axis
restriction. The second hit while already ringing produces no additional toggle,
animation reset or bell notification. In the native breeze corpus, twelve
impacts hit the front support rather than the bell itself; their bursts still
activate the adjacent bell. The strict replay retains those hit cells rather
than rewriting them as direct bell collisions.

The shared burst interaction now calls Bell.ActivateByAttack, preserving the
inspected idle gate and facing. Both charge types run their collision/burst pass
after block actors, keeping the first ringing snapshot at Ticks zero. Their
measured native body is 0.3125 blocks wide/high with its bottom at actor Y;
the old player box was smaller, and the old breeze box was additionally shifted
downward. Player wind charge drag is now zero rather than 0.01, matching the
retained flight frames. Impact removes both native charge types in that same
collision pass; the previous implementation retained them until the next GT.

The new entity replay checks every bell state/actor frame, projectile presence,
the available projectile motion/AABB frames and the translated original breeze
birth AABBs. Both 64-scene entity corpora pass together (1.252 s). The generalized
session replay also checks all actor fields and all 640 actual bell packets with
normal and split flushing, alongside the existing player-use, arrow, redstone
and support corpora (8.209 s). Existing shared bell/arrow/button/water/piston and
breeze AI selection passes after the player-charge change (53.918 s); the final
shared check after the breeze geometry/phase fix is recorded separately below.

The final complete shared check passes block 96.856 s, session 13.578 s, world
13.793 s and player 17.175 s. The focused entity selection including both wind
charge corpora, arrows/buttons, water, pistons, breeze AI and Bedrock identifier
aliases also passes (38.545 s). The root server builds after all changes.
All four newly retained corpora are complete and preserve the original binary
hash, per-shot inputs and read-only structure-journal evidence.

Broader burst search geometry/occlusion and multi-block interaction ordering,
natural breeze flight/targeting, other activating attacks, bell hearing and
vibration, wider placement/support eligibility and chunk lifecycle remain open.
These captures do not prove complete projectile or bell parity. The subsequent
switch-grid/ray comparison below extends the footprint and ordering evidence.

### Wind burst ray selection, switch activation and affected-set order

The old burst implementation visited the union of three neighbour cubes in Go
map order. It did not activate levers or buttons. The replacement follows the
client Explosion::explode boundary-ray loop at `0x1134b44..0x1134e75`, preserving
binary32 normalization, strength, stepping and the non-air cost. Each burst
consumes exactly 1,352 native float inputs, one for every boundary cell of a
16-cubed direction grid. The block-hit origin moves 0.25 blocks out along the
hit face, as WindBurstOnHitSubcomponent::doOnHitEffect at `0x235ad90` does;
the original client constant at `0xe61e140` is 0.25. The measured native powers
are binary32 1.2 for player charges and 3.0 for breeze charges.

The original loop runs locally with entry/exit adapters bypassing unrelated
explosion setup, entity effects and publication. World-cell reads and affected
insertions are supplied callbacks; all loop instructions and constants are
unchanged. The original affected-set insertion at `0x1137d60`, rehash at
`0x119edb0` and bucket-vector helper at `0x76e00` execute independently, with only
allocation/free and scalar ceilf supplied by the host. Their output verifies
the shared BlockPos hash/list rules, including duplicates, negative/large
coordinates and 8/64/512 bucket growth boundaries through 600 distinct keys.
Production now uses that traversal instead of randomized Go map iteration.

The complete uninstrumented BDS evidence is:

- bedrock_1_26_51_wind_switch_grids.json: 64 scenes, four directions, both
  parities, a stone-wall control, and lever/stone/wooden/blackstone-button grids;
  each records 49 cells over all 56 frames, including button release. Owned root
  adamant-redstone-bds-3chgyxkk.
- bedrock_1_26_51_breeze_switch_grids.json: sixteen independent grids fed by
  actual breeze-fired charges, preserving original birth/owner data and the
  controlled teleport/shoot inputs; root adamant-redstone-bds-2lu8ffvq.
- client_1_26_51_wind_bursts.json and client_1_26_51_breeze_wind_bursts.json:
  executed original ray and collection functions with the recorded world/RNG
  inputs. All 64+16 footprints match the independent BDS grid states.

Read-only level-RNG snapshots at bounded Script checkpoints preserve the full
before/after transition, verified by original client draws on copies. That
interval includes unrelated world draws; it is not itself an explosion scope.
One breeze window has 1,574 words rather than the common 1,592. No words or
frames were removed to make those totals uniform. Additional hardware traces
of fresh owned BDS children resolve the exact ray inputs:

- bedrock_1_26_51_breeze_switch_grids_hardware.json: sixteen original explosion
  entries, native power 3.0 and all 1,352 observed ray words per entry;
  adamant-redstone-bds-by0dvi1x.
- bedrock_1_26_51_wind_switch_grids_hardware.json: eight player-charge entry
  controls, power 1.2, native resistance limit zero, and the same 1,352-word
  contract; adamant-redstone-bds-2ii6ri72.

The BDS explosion entry at `0x795690` is located independently from matching
client entry and radius-check instructions. Every scoped copied input stream
also matches the actual first 1,352 observed level draws after that entry.
Both charge types begin those inputs 80 words into the held interval in these
scenes. Debug registers are restored before detach; no target instructions or
definitions are replaced. Hardware-observed captures remain distinguished from
the uninstrumented grids.

Go replay now checks the complete 80 grids and all button release frames,
and independently compares exact affected-position order to original client
instructions. Button activating attacks use the native null actor argument;
levers use their existing state/event path. The ray inputs stay on the shared
world RNG, and unobserved extra ray draws fail replay. The focused native set
passes entity 4.387 s and world 0.247 s before adding the final player-entry
integrity check. The full shared suite also passes block 98.263 s, session
13.914 s, world 13.911 s and player 17.289 s; the related entity selection,
including bell packet dependencies, arrows, pistons and breeze AI, passes
40.441 s. The root server builds after the implementation change.
After adding both hardware-entry integrity regressions, the complete focused
ray/grid/input/order checks pass again (entity 2.611 s, world 0.259 s).

This establishes the tested ray/state/collection paths, not complete wind
interaction parity. Remaining work includes liquid and partial-geometry
boundaries, other interactable materials, mixed-device mutation/publication
order, sound/vibration ownership and packet fields, cancellation, whole-world
RNG ordering (including constructor/shoot draws outside the retained burst
scope) and chunk lifecycle. All broader audit matrix requirements remain
open, and the full-audit effort estimate remains approximately 70%.

### Wind material diagnostic and publication checkpoint on 2026-10-31

The next complete original-BDS capture,
`bedrock_1_26_51_wind_materials.json`, retains 176 scenes: 22 materials/forms,
both initial open/lit states, both powered states and both spawn parities.
Each scene includes the four original setup permutations, 56 complete frames,
the real player wind projectile input and its block-hit event. The owned root
is `adamant-redstone-bds-_d0i0rmt`; the capture script is
`scripts/redstone_bds_wind_material_probe.py`.

This diagnostic is unfinished and deliberately preserves the failures. Native
powered doors, trapdoors and gates retain their initial state when hit, whereas
the existing Go interaction toggles them. Initially powered command placement
also exposes an earlier initial edge difference, including iron controls.
Lit candles expose a nil-owner panic through ordinary player activation;
the comparison catches and reports that panic rather than dropping the scene.
The pre-fix run fails 64 of 176 scenes and reports eight candle hit panics.
Campfire, soul-campfire and fire controls retain their native state.

The inspected client activating-attack handlers independently contain a
positive consumer-strength gate for doors at `0x978a8d1..0x978a916`, trapdoors
at `0x4033846..0x4033884` and gates at `0x7ac4d1f..0x7ac4d64`. Their circuit
lifecycle, initial sampling and publication still need comparison before a
complete fix. This checkpoint does not claim those material paths are fixed.

The diagnostic validates redundant SDK direction aliases before comparing
canonical states. It retains the raw upper-door open bit in the corpus while
using the independently verified upper-half network representation for visible
state comparison. No failing scenes or frames are skipped. Run it explicitly:

```text
go test -tags redstone_wind_material_audit ./server/entity -run '^TestWindBurstMaterialsMatchBDS$' -count=1
```

This command is expected to fail at this publication checkpoint. The opt-in
tag keeps the newly captured open audit separate from the passing regression
suite for the completed bell, projectile, ray and switch changes above.
The user requested commits and publication; the audit remains active and its
full effort estimate remains approximately 70%, with all 22 matrix rows open.

The final pre-publication bell/wind/switch/queue/order selection passes session
8.636 s, entity 4.578 s and world 0.310 s. The root server builds again with
`go build -o .tmp/adamant-redstone-audit.exe .`; the `go run .` entry point is
retained. Python probe syntax and `git diff --check` also pass. This final
selection supplements the complete shared-package checks recorded above;
it is not a claim that the unfinished material diagnostic passes.

The completed implementation and native regressions are committed in
`a727e33f`; the unfinished material capture and opt-in diagnostic are retained
separately in `de6d45f9`. Private operator/ban data and the temporary gameplay
log are excluded from these commits.

### Resumed barrier consumers and wind materials

The former 176-scene opt-in material diagnostic now passes as an ordinary
regression. Its build tag is removed. Doors, trapdoors and gates now have
world-owned consumers on the shared two-GT circuit clock; their visible open
bit seeds their initial component value, and the first input notification is
ignored. Manual and activating-attack changes do not overwrite the electrical
history. Wind handlers consult evaluated consumer strength, ignore upper door
hits and iron controls, and preserve gate facing independently of ownership.
Lit candles and candle cakes use a dedicated owner-independent extinguish
handler, avoiding the previous nil-user panic and held-item/enchantment path.

`scripts/redstone_bds_barrier_probe.py` captures another 192 original-BDS
scenes in `bedrock_1_26_51_barrier_pulses.json` (owned root
`adamant-redstone-bds-ooa7adyq`). Three door families, three trapdoor families
and two gate families each have both initial states, both initial power states,
both absolute source parities and lower/upper/split input controls. Every scene
retains all 108 frames, one-, two-, three-, four- and eight-GT pulse inputs,
another late transition and a command edit of the live open bit. Split feeds
keep the independent upper input steady while pulsing the lower. Go compares
all 41,472 recorded cell states, including upper unused bits and the complete
initially powered interval. The existing iron-door test now allows the initial
consumer notification before testing a subsequent rising edge, matching the
native distinction instead of expecting command placement to act as that edge.

The inspected handlers at DoorBlock::_onRedstoneUpdate (`0x9789080`),
TrapDoorBlock::_onRedstoneUpdate (`0x4032090`) and
FenceGateBlock::_onRedstoneUpdate (`0x7ac3a30`) return on the event's first-time
flag. Native setup seeds their input from open state. The already retained
activating-attack handlers independently gate on positive consumer strength.
Door input publication combines both halves while leaving their scene
strengths independent. A native consumer publication callback lets an upper
door notification mutate its lower target after cancellation is checked,
without first publishing a fictitious upper open-state change.

A fresh connected-client capture from
`scripts/redstone_bds_wind_material_packet_probe.py` retains all 176 independent
scenes and their original packets in
`bedrock_1_26_51_wind_material_effects.json` (owned root
`adamant-redstone-bds-skwpruz8`). An explicit Script checkpoint at setup+10 GT
separates setup packets from effects; projectile spawn is at setup+20/21 GT.
Packet receipt times delimit that scope, not simulation tick order. Original
setup packets and the checkpoint remain in the corpus. The ordinary session
test compares all 168 activating-attack packets over 160 scenes, with both
normal and split flushing, alongside every sampled raw block state. Sixteen
campfire/soul-campfire controls remain separately identified: their raw packets
include four periodic actor refreshes after the checkpoint. Those refreshes are
retained, but their independent actor clock is outside this packet comparison;
the material-state regression checks their unchanged state.

The wire evidence corrects a previous audit assumption. BDS retains the upper
door's unused open bit even when it is true, and republishes that unchanged
permutation when the lower half toggles. It does not always send an upper
open_bit=false. The old registry normalization and its incorrect unit-test
expectation are replaced by raw, reversible network hashes. Both material
comparators now check the original upper bits directly; they no longer
normalize them away. The packet comparison retains the exact original IDs.
The stronger evidence therefore supersedes the earlier upper-half claim in
this report, without rewriting the native captures to match it.

Opening a door now changes its lower permutation and republishes the retained
upper permutation, with native flags 3 and lower-before-upper ordering. The
sound follows both states and uses the old lower permutation's hash. Trapdoor
and gate attack sounds instead use their new permutation's hash. Candle
attacks use extinguish.candle at integer block coordinates, after the state
update, with the native empty entity identifier and ExtraData=-1. The pending
queue retains these dependencies and effects through its existing bounded
publication path, including split flushing. Ordinary non-attack candle use and
splash paths are not changed by this dedicated handler.

The upper collision model now reads the lower half's opening, following the
inspected DoorBlock::_isDoorToggled (`0x9789b60`) call from the native visual
shape body at `0x97896c0`. AI door reads and ground path costs use that same
lower opening rather than the retained upper bit. Eight additional integration
controls cover both disagreement directions for wooden/copper doors, compare
upper/lower collision shapes and path costs, and exercise an upper-half AI
interaction while retaining its original permutation.

Command fill is a separate lifecycle event. Native onFillBlock (`0x978ba10`)
copies the lower open bit to its matching partner when placing/editing the
command state. An explicit command-fill callback now handles that distinction;
ordinary permutation changes, oxidation, native toggles and actor publication
do not accidentally run the command callback. The pulse replay drives that
real callback on its recorded command changes. Actual setblock/fill/clone
integration tests also pass (builtin 0.487 s).

The native material/pulse/packet tests pass together before the final command
callback split (session 6.769 s, entity 5.891 s, block 0.350 s). After that split,
the related shared selection passes block 0.718 s, session 14.927 s, entity
43.234 s and world 0.293 s. The additional AI/path/material/pulse selection
passes after its final integration changes (entity 5.862 s).

The final complete shared check passes block 96.254 s, session 19.279 s,
world 13.610 s, player 17.421 s and item 1.368 s; inventory has no tests.
The root server builds after the final AI/path changes. Both new Python probes
parse successfully, the two new BDS corpora are complete, and diff whitespace
checks pass. This was the resumed working snapshot after `1e99e7e2`;
the paused publication below records its final implementation and evidence.

This extends the barrier and activating-attack evidence, not full parity.
Remaining work includes analog/conducted input changes, actual player placement
and powered use, broader command property changes, invalid/mixed door halves,
hinge and direction geometry, native save/load component initialization,
campfire actor publication timing, vibration ownership, cancellation and
multi-device/backlog matrices. Broader wind liquids/geometry, natural breeze
flight and whole-world RNG/lifecycle remain open. The full audit remains active
at approximately 70% effort; all 22 matrix rows retain open work.

### Barrier signal changes and independent attack inputs

Three further complete, independently captured original-BDS corpora extend the
barrier comparison. All retain the inspected BDS SHA-256
`76d547f82e02c18d0986c30b47132c9cc4171d0f2df1c00649e50ff35788b321`:

- `bedrock_1_26_51_barrier_signals.json`: 192 scenes from
  `scripts/redstone_bds_barrier_signal_probe.py`, root
  `adamant-redstone-bds-rj5xs5m5`. Real source blocks and dust lines change
  strength while remaining positive, switch fully off/on, and follow explicit
  command closures while powered. Three door, three trapdoor and two gate
  families each have both initial states, input heights, source parities and
  direct-dust/stone-bridge/glass-bridge controls. Glass supports isolate raised
  dust from downward conduction. Every scene retains 120 frames, both target
  cells, the nearest dust strength and its bridge/support, plus all initial
  input geometry and fifteen original command mutations. Go checks all 92,160
  cell states. In the tested controls, dust changes 13/11 directly or 14/12 at
  the bridge; stone conducts the change, while glass does not. The native
  barriers can reopen on a positive-to-positive change after a command closure.
  Treating their consumer history as only a powered boolean would miss this.
- `bedrock_1_26_51_wind_barrier_inputs.json`: 192 scenes from
  `scripts/redstone_bds_wind_barrier_input_probe.py`, root
  `adamant-redstone-bds-c4lzdo4e`. These retain real projectile impacts, 56
  complete frames and separately placed lower/upper/split sources, rather than
  only the earlier powered floor. All 57,344 recorded cell states match. A
  positive upper door consumer does not by itself reject an attack on the
  lower half whose own consumer is zero; the gate reads that component, not
  the combined visible door opening or immediate physical adjacency.
- `bedrock_1_26_51_barrier_packets.json`: 192 fresh connected-client scenes
  from `scripts/redstone_bds_barrier_packet_probe.py`, root
  `adamant-redstone-bds-azy8r85t`. They preserve every target state frame,
  original setup packet, the explicit setup+10-GT scope checkpoint, original
  command inputs, and all 3512 subsequent target state/sound packets.

The signal/pulse replay passes together before publication changes (entity
12.488 s), and both material/independent-input projectile corpora pass together
(6.276 s). These are direct native state checks; no material, frame, positive
strength change or upper open bit is filtered out.

### Barrier redstone publication and sound origin

The fresh 3512-packet comparison initially fails every scene in both normal
and split flushing. Redstone-triggered sounds precede the pending block updates
in the native stream. The preceding wind corpus instead places its sound
after the state updates. The former shared Go path imposed the latter order on
redstone notifications too. Trapdoor/gate circuit packets additionally used
flags 2 and the generic sound entity identifier instead of native flags 3 and
the empty identifier. Command edits had separate primary/partner flag and
ordering differences.

Circuit publication now retains its mutation snapshots until the enclosing
callback completes. The world is still mutated before its sound callback,
following the native setOpen operation; only packet publication is ordered.
The sound uses the existing immediate bounded outgoing queue; it does not wait
for the per-block background flush. Both halves of a door remain in
lower-before-upper order. The readiness gate makes this hold even
when a viewer attempts a split flush while receiving a block callback.
The new viewer assertion also checks that sound listeners already observe the
mutated barrier, preventing an apparent wire fix that instead plays sound
against the old world state. Wind/ordinary door interaction retains its
separate after-state publication path.

Command-fill snapshots now explicitly request the native primary flags 3.
A changed upper partner uses flags 2 and depends on the lower update; it is not
sent if that unused bit was already equal. This is separate from a circuit
notification republishing an unchanged upper permutation with flags 3.
The new optional command-fill updater prepares only the outgoing snapshot;
the existing fill callback retains the actual partner mutation.

The first publication correction leaves 48 door origin mismatches across the
two flush modes. Native sound stays at the notifying position: an upper-half
consumer produces sound at upper Y+0.5, even though the changed open bit is in
the lower block. The common helper now retains that original position rather
than moving sound down with its mutation target. Sound block data still uses
the old lower permutation for doors and the new permutation for gates/trapdoors.

All original 3512 redstone packets and 168 activating-attack packets now match
without dropping fields or modifying their original IDs, including both
flushing modes. All four state/input corpora and the door AI integration
controls pass together with these packet tests (session 13.623 s, entity
14.994 s). Adding the post-mutation sound-listener assertion and the shared
bell/moving-block/queue selection also passes (session 19.607 s). The final
immediate-sound path retains that complete selection (19.554 s), and the full
session package passes again (24.389 s).

The complete shared check passes block 106.881 s, session 30.192 s, world
16.013 s, player 20.151 s and item 1.435 s; inventory has no tests. The related
entity selection passes 72.113 s, including both wind cohorts, all barrier
corpora, arrows, bells, pistons, ground paths and breeze AI. Actual
setblock/fill/clone integration tests pass again (builtin 0.598 s). The root
server builds after the final sound publication change; all three added Python
probes parse and diff whitespace checks pass. A full entity-suite pass is not
claimed: the reproduced, unchanged frog breeding flake remains as recorded
above.

This proves the retained state frames and per-scene packet order/fields, not
the exact network delivery tick. Original receipt timestamps show a separation
between redstone sounds and state delivery, but receipt time alone is not an
authoritative simulation-tick label. An ordered native tick marker or direct
sender-phase observation is still required for that clock comparison.
Actual player use/placement, broader conductor/source combinations,
multi-device ordering, cancellation, persistence and all other open matrix
requirements also remain outstanding. The effort estimate remains approximately
70%; the paused publication below supersedes this resumed working status.

### Native packet phase markers and paused publication checkpoint

Two complete original-BDS captures now bound packet delivery relative to the
Script interval, using received packet sequence rather than wall-clock gaps:

- `bedrock_1_26_51_barrier_clock.json`: 32 independent redstone scenes, 108
  frames each, 216 ordered before/after markers and 604 effect packets;
  owned root `adamant-redstone-bds-srge33hb`.
- `bedrock_1_26_51_wind_barrier_clock.json`: 32 independent real-projectile
  scenes, 108 frames each, 216 ordered markers and 56 effect packets;
  owned root `adamant-redstone-bds-kg4fg7_4`. Iron controls retain no attack
  effects. The original impact events, inputs and raw state frames remain.

The probes are `scripts/redstone_bds_barrier_clock_probe.py` and
`scripts/redstone_bds_wind_barrier_clock_probe.py`. The loopback-only client now
records monotonically increasing receive sequence numbers and the original
text packets. BDS wraps Script messages in rawtext objects; the capture decodes
their derived markers while retaining those original fields. Completion requires
the actual final marker, so a delayed last network flush cannot silently remove
the end of the trace. Both corpora retain the same inspected original BDS hash.

`scripts/redstone_barrier_clock_analysis.py` independently matches every effect
to the original state transition and native network hash, preserving the upper
door's unused bit and validating SDK direction aliases. It checks all packet
fields and records the surrounding original markers. For N, the observed
mutation tick (the explicit input tick for commands), the retained intervals are:

| Effects | Count | Previous marker | Following marker |
|---|---:|---|---|
| Circuit sound | 220 | N-1 after inputs | N before inputs |
| Circuit lower/upper state | 340 | N after inputs | N+1 before inputs |
| Command state | 44 | N after inputs | N+1 before inputs |
| Wind state and sound | 56 | N after inputs | N+1 before inputs |

These are receive-order bounds around actual native Script ticks, not a
conversion from milliseconds or a claim about an unobserved lower-level sender
clock. The native Script frame already observes the changed world permutation.

The ordinary `TestBarrierPacketsRespectNativeScriptPhase` replays both corpora
with actual circuit updates, command inputs and real Go wind projectile
collisions. A stationary observer registers a deferred Script-like callback
before the ordinary block/circuit/late-projectile phases. That callback checks
every raw state frame and emits the two phase markers around the recorded
inputs. It also attempts an early flush. Both normal and split block viewers
must preserve the original packet/marker stream; only the explicitly warmed
setup window is excluded from packet comparison, on both sides. State frames
are checked throughout, including that window.

The initial phase comparison exposes 80 failures: wind state can escape before
the Script-like interval in either flush mode, and a split command mutation can
escape between its before/after markers. Readiness now covers wind/manual door,
trapdoor/gate attack and candle snapshots as well as circuit publication.
Command-fill snapshot preparation receives its owning transaction and creates
the same callback-completion gate; a changed upper partner shares that gate.
Actual world mutation, collision and electrical sampling remain in their
ordinary phases. Circuit sound stays immediate, while wind sound retains its
after-state order. This fixes premature publication without adding a fixed
whole-tick delay or changing the original native packet IDs.

The phase regression and both earlier packet corpora pass together (session
12.926 s). The final complete shared check passes block 98.488 s, session
27.407 s, world 13.931 s, player 17.451 s and item 1.257 s; inventory has no
tests. The related entity selection passes 56.344 s, and actual
setblock/fill/clone integration passes (builtin 0.597 s). The root server builds
after the final callback-gate change. All added clock Python sources parse and
diff whitespace checks pass. The previously documented full-entity-suite
limitation remains; this is not a claim that the unrelated frog flake is fixed.

The audit is now paused at the user's request, with approximately 70% of the
full effort completed. This remains a planning estimate, not proven global
parity: all 22 matrix rows retain open work. Exact lower-level sender timing,
unrestricted asynchronous network/background scheduling, backpressure and
multi-device lifecycle/cancellation still require further comparison, alongside
the outstanding gameplay, geometry, player-use and persistence matrices.
This publication records the completed fixes and reproducible evidence without
resuming the remaining audit. Private operator/ban data and the temporary
gameplay log are excluded.

The implementation/state regressions are committed in `1eb77585` and the
independent packet/phase capture tools, original corpora and regressions in
`af7ab67f`. Publication of this checkpoint does not resume the paused audit.
