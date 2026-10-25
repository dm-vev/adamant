# Bedrock redstone audit

Status: **paused at the user's request on 2026-10-25; audit incomplete**.
This report tracks the full redstone implementation,
not just the fixes already made. An existing Go test or a matching happy-path
delay is not evidence of complete Bedrock equivalence.

The requested reference is the disassembled **Windows client 1.26.51.1**. Its
image SHA-256 is
`8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5`.
Unmodified Windows BDS 1.26.51.1 supplies additional world-level observations.
The repository's Java gameplay baseline does not override the requested
Bedrock reference for this audit.

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
pulses at different times. Analog values are sampled once per game tick,
separately from direct electrical input, after circuit evaluation. The first
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
| Circuit cadence and ordering | Shared clock and capacitor cache/evaluation traversal installed; native registration, bucket growth and chunk vectors verified in 26 heap/world cases. Open: complete publication traversal, other groups, startup/restart, first placement, simultaneous changes, priorities, sound and native unload/reload ordering |
| Source/consumer/conductor graph | Open: weak/strong activation, one-solid-block transfer, face filtering, nonconductors, attenuation, removal and independent circuits |
| Redstone wire | Transport phases and 48 basic network traces verified. Open: horizontal shape, directional output, native vertical/material matrices, other component connections, feedback, cancellation and reload |
| Repeaters | Side-source filter, pulse pipeline and delay setup corrected; fixed/random evaluator traces and 96 world timing/lock/train/delay traces match. Open: powered placement, native load/pulse persistence, source graph and cross-chunk ordering |
| Comparators | Arithmetic, independent lit/variant state, side-source filter, tested rear analog precedence, configured mode/direction and shared clock corrected; 8,704 arithmetic cases, 78 settled source/priority cases, 60 pulse traces, 32 mode traces and 40 rotation traces match. Open: complete source/material graph, all analog blocks, broader interaction/property/publication lifecycle, output-change events and block actor persistence |
| Redstone torches | Native counter, shared clock and registration traversal installed; 34 one-torch traces, 36 ascending networks, 62 common-support traces, 60 placement traces, 24 rotation/replacement traces, 60 settled face/conduction cases and 3,776 algorithm cases match. Immediate placement inversion and premature direction refresh fixed. Open: broader player interactions, full source/material/attenuation graph, replacement registration order in larger graphs, dependency lifecycle, native sound/publication order, restart/unload/reload and broader cancellation |
| Observers | 38 face/phase/retrigger traces, 28 output traces, 12 lifecycle traces and 18 movement callback windows match. Open: notification-only changes, larger chains, powered placement/load, unload/reload and complete piston timing |
| Pistons and sticky pistons | Nonadjacent power bug fixed. Open all direction/source combinations, conduction, timing, short pulses, push/pull limits, adhesion, block actors and moving entities |
| Levers and buttons | Open: mounting faces, weak/strong output, press/release duration, repeated activation, arrows/projectiles, support and reload |
| Pressure plates and tripwire | Open: entity filters, weighting, update interval, release, hook scans, disarming and support |
| Daylight detectors and lightning rods | Open: analog calculation, inverted mode, dimension/weather/time changes, pulse timing and conduction |
| Sculk sensors/calibrated sensors/shriekers | Open: detection/activation/cooldown phases, frequency filtering, output, waterlogging, paired events and persistence |
| Lamps and copper bulbs | Lamp source/consumer phase corrected in observer/wire matrices. Open: broader retrigger/cancellation/load cases. Copper-bulb toggle, comparator output and persistence remain open |
| Doors/trapdoors/fence gates | Open: direct/conducted input, upper/lower halves, manual changes while powered, transition events and all materials |
| Powered/detector/activator rails | Open: scan range, slopes, branching, entity activation, analog outputs and minecart effects |
| Dispensers/droppers/crafters | Open: edge detection, delay/retrigger, source direction, inventory side effects, craft scheduling and block actors |
| Hoppers | Open: lock input faces/conduction, cooldown and item-transfer ordering, minecart interactions and reload |
| TNT/note blocks/bells | Open: triggering edge, conducted sources, scheduled action, payload and repeated input |
| Command/structure blocks | Open: trigger/cycle timing, modes, conditionals, powered-state persistence and cross-chunk execution |
| Analog-output blocks | Open: containers/trapped chests, furnaces/campfires, cake/candle cake, cauldrons, composters, bookshelves, jukeboxes, lecterns, item frames, anchors, portal frames, skulls and creaking hearts |
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
- Overall redstone parity has **not** yet been established. Further gameplay
  changes are paused; completed work is being committed and published at the
  user's request. The coverage matrix above is the resume checklist.
- Before publication, the committed server was launched with `go run .` in
  an isolated temporary worktree. A loopback RakNet listener used an ephemeral
  port, world/player saving and resource building were disabled, and the
  console `stop` command closed the server with exit code zero. Existing worlds
  and server configuration were not used. Log: `.tmp/go-run-server-smoke.log`.
