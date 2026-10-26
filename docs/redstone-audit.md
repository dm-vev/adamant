# Bedrock redstone audit

Status: **paused on 2026-10-26 at the user's request; audit incomplete**.
This report tracks the full redstone implementation,
not just the fixes already made. An existing Go test or a matching happy-path
delay is not evidence of complete Bedrock equivalence.

The requested reference is the disassembled **Windows client 1.26.51.1**. Its
image SHA-256 is
`8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5`.
Unmodified Windows BDS 1.26.51.1 supplies additional world-level observations.
The repository's Java gameplay baseline does not override the requested
Bedrock reference for this audit.

The current effort estimate is approximately **60%** of the full audit. This is
a planning estimate, not a percentage of proven equivalence or a count of closed
matrix rows. Every row below retains its outstanding work. The published checkpoint
includes capture tooling, comparators/analog outputs, switches, pressure plates,
tripwire, daylight/live lighting, lightning rods and the tested sculk paths.
The implementation and native regression evidence are committed on `dev`.

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
client shuffle cases match. This does not close the sensor or circuit ordering
coverage rows.

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
support removal,
rotations/replacement during pending checks, explicit gaps followed by later
source changes, crossings, full weak/strong input/output/material matrices,
additional actor states/game modes, real kernel movement/contact phase, exact
touching boundaries, other sound/state combinations and save/unload/reload behavior.

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
separate scenarios. Hook vibration/game-event parity is also still open.

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

Still open: other event sources/frequencies and filter strengths, all strong/
weak output/material paths, fractional positions, broader occlusion and
listener selection/rejection, waterlogging, resonators, shriekers, producer
startup, phase replacement and native save/load/unload behavior.
The existing Java-derived listener/actor persistence is not treated as proof
of Bedrock equivalence. None of the 22 audit areas is declared complete here.

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
| Circuit cadence and ordering | Shared clock and capacitor traversal installed; registration/bucket growth/chunk vectors verified in 26 heap/world cases. Per-chunk actor shuffle matches 130 executed client cases; actor identity rechecks and insertion order corrected. Open: complete publication traversal, native load/generation order and shared RNG stream, other groups, startup/restart, simultaneous changes, priorities and sound |
| Source/consumer/conductor graph | 480 switch output/conduction cases match tested direct and one-stone-block paths. Open: broader weak/strong activation, other sources/materials, face filtering, nonconductors, attenuation, removal and independent circuits |
| Redstone wire | Transport phases and 48 basic network traces verified. Open: horizontal shape, directional output, native vertical/material matrices, other component connections, feedback, cancellation and reload |
| Repeaters | Side-source filter, pulse pipeline and delay setup corrected; fixed/random evaluator traces and 96 world timing/lock/train/delay traces match. Open: powered placement, native load/pulse persistence, source graph and cross-chunk ordering |
| Comparators | Arithmetic, independent lit/variant state, side-source filter, tested rear analog precedence, configured mode/direction, pending replacement output and shared clock corrected; 8,704 arithmetic cases, 78 settled source/priority cases, 60 pulse traces, 32 mode traces, 40 rotation traces and 48 publication-edit traces match. Open: complete source/material graph, all analog blocks, broader interaction/property/publication lifecycle, output-change events and block actor persistence |
| Redstone torches | Native counter, shared clock and registration traversal installed; 34 one-torch traces, 36 ascending networks, 62 common-support traces, 60 placement traces, 24 rotation/replacement traces, 60 settled face/conduction cases and 3,776 algorithm cases match. Immediate placement inversion and premature direction refresh fixed. Open: broader player interactions, full source/material/attenuation graph, replacement registration order in larger graphs, dependency lifecycle, native sound/publication order, restart/unload/reload and broader cancellation |
| Observers | 38 face/phase/retrigger traces, 28 output traces, 12 lifecycle traces and 18 movement callback windows match. Open: notification-only changes, larger chains, powered placement/load, unload/reload and complete piston timing |
| Pistons and sticky pistons | Nonadjacent power bug fixed. Open all direction/source combinations, conduction, timing, short pulses, push/pull limits, adhesion, block actors and moving entities |
| Levers and buttons | 196 source states, 480 direct/conducted output cases, 180 actual-arrow traces and 66 real player-use/retrigger traces match. Missing wooden-button activation/holding, projectile collision/contact phase and switch sound events corrected; six native click packets verify three sources. Open: trident world capture, multiple entities/boundaries, broader support/materials, full sound material/mount matrix, replacement and reload |
| Pressure plates and tripwire | Plate conversion/query fixes verified in 302 native traces and five executed scalar methods. Tripwire initialization, contact bounds, 40-string limit, disarming/recheck and neighbour callback scope corrected; 140 contact + 80 line traces match. Artificial carpet collision removed against an executed native getter. Another 58 actual player-cut traces and 230 hook sound packets match after adding the destruction callback and native sound ordering. Open: player modes/actor states, complete boundary/transition/contact integration, previously powered/disarmed cutting, gaps/crossings/support/materials, broader sound/game events and persistence |
| Daylight detectors and lightning rods | Detector clock, environment, initial/live roof lighting, body and producer registration corrected; 48,960 dimension/initialization source frames, 28,800 weather samples, 24 live roof traces, 16 source-removal/overlap traces, ten producer heap checkpoints and 12 player inversions with wire timing match. Rod hit/expiry/retrigger/oxidation/producer and tested support output match 100 real-bolt traces; 96 rod shapes match inspected bounds. Attraction geometry/order/selection matches 670 executed client cases with synthetic world inputs plus 120 native sort/heap cases; 26 actual BDS traps verify shared selector integration, bolt positions and pulse occurrence through tested rain occluders. Open: other lighting filters/liquid states and lifecycle, partial weather/fog, wider detector face/conduction/support/events/persistence; random weather scheduling, broader precipitation/climate/occluders, copper/projectile/interaction/events and lifecycle |
| Sculk sensors/calibrated sensors/shriekers | Missing lever/button events and calibrated disabled-output face corrected. Radius/phases/strengths, selected filters in four facings, all 16 wool colours and eight material controls match 120 complete traces (four include heap readback). Open: other event/filter/occlusion/output paths, waterlogging, resonance, shriekers, fractional positions, startup, network action phase and persistence |
| Lamps and copper bulbs | Lamp source/consumer phase corrected in observer/wire matrices. Bulb comparator output verified lit/unlit for all eight variants. Open: broader lamp retrigger/cancellation/load cases, bulb toggle timing/interaction and persistence |
| Doors/trapdoors/fence gates | Open: direct/conducted input, upper/lower halves, manual changes while powered, transition events and all materials |
| Powered/detector/activator rails | Open: scan range, slopes, branching, entity activation, analog outputs and minecart effects |
| Dispensers/droppers/crafters | Dispenser/dropper nine-slot container contract corrected and settled comparator fullness verified. Open: edge detection, delay/retrigger, source direction, inventory side effects, craft scheduling and block actors |
| Hoppers | Settled bookshelf transfers verified in 48 cases with two barrel controls. Open: lock input faces/conduction, individual cooldown/transfer ordering, all other containers and sides, minecart interactions and reload |
| TNT/note blocks/bells | Open: triggering edge, conducted sources, scheduled action, payload and repeated input |
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
