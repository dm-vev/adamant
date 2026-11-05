# The anti-cheat

The game checks what the clients of players ask of them: their moves, breaks, places, attacks, item uses and
menus, and the rate of their ticks. The checks live in the game (`server/player`, with their settings, levels and
math in `server/player/anticheat`), not in a protocol adapter: an adapter hands the game each tick of its client as
a `client.Move` and the actions through `client.Controllable`, and applies the corrections the game makes through
`client.Self.CorrectMove`, the block resends and `Disconnect`. Every check is configured in `config.toml`, under
`[AntiCheat]`; a file without the table, or without some of its keys, runs every check with its defaults.

Only what a client asks is checked. `Player.Move`, `BreakBlock`, `AttackEntity` and the rest stay the API of
plugins and the server, unchecked but for the reach they always checked. A player made without a server, whose
`player.Config.AntiCheat` is nil, is not checked at all.

## The checks

| Check | Detects | Default | Reference |
|---|---|---|---|
| `MovedTooQuickly` | a move no player makes in a tick: farther than 10 blocks (17 gliding) beyond the velocity the server knows, with the impulses it gave | setback | Java's "moved too quickly"; off with the game rule `player_movement_check` false, or gliding with `elytra_movement_check` false |
| `Speed` | horizontal speed beyond the envelope of Java's travel for the state of the player, with 30% and 0.05 of slack | log | Java's `LivingEntity.travel` |
| `Fly` | hovering: more than 80 moves in a row that do not descend by 0.03125 with only air around (640 under Slow Falling); a flight the game mode does not allow | setback to where the player last stood, after the blocks below were resent | Java's "floating too long", which kicks |
| `Ascend` | rising beyond a jump, a step, a climb, a liquid, a bounce or an impulse; the up-blips of NoFall | log | Java's jump and gravity |
| `NoClip` | a move into blocks the player was not in, 0.1 deep | the first violation resends the blocks and logs; setback from the next | Java's `isEntityCollidingWithAnythingNew` |
| `Phase` | a move through what blocks its path, though it starts and ends clear | log | Java's "moved wrongly" |
| `EntityReach` | attacks and interactions beyond the reach: from the eyes to the box of the target, 3 blocks and 3 of buffer (5 and 3 in creative) | cancel | Java's `Player.canInteractWithEntity` |
| `BlockReach` | breaking, placing and using blocks beyond 8 blocks from the eyes to their centre (14 in creative) | cancel | about Bedrock's pick range × 1.5, as before the anti-cheat |
| `BreakTime` | a break finished before 70% of its time; an instant break of a block that does not break in a tick, in survival | cancel: the block is resent | Java's 0.7; BDS's server-authoritative block breaking |
| `Place` | places beyond 20 a second (a burst of 10); clicks on faces the eyes are behind; clicks outside the block, which are refused and no violation | log | Java's 1.0000001 bound of the point clicked |
| `Attack` | attacks beyond 20 a second (a burst of 10); more than one living target in a tick | log | – |
| `AttackAim` | an attack off every aim of the client (its look, its interaction ray, its camera) whose ray misses the box of the target and whose cosine to it is below 0.85; a target the eyes cannot see past full blocks | log | BDS's `player-movement-action-direction-threshold` |
| `FastUse` | items without a cooldown, as snowballs, used more often than every 4 ticks (a burst of 2) | log | Java's right-click delay |
| `Timer` | a client that runs its ticks faster than real time | throttle: its moves wait for a later tick | BDS's input tick policy |
| `Menu` | a menu out of reach: the block within 4.5 + 4 blocks of the eyes (5 + 4 in creative), the entity within 3 + 4 (5 + 4); more than 64 menu requests in a tick | cancel: the menu is closed, the requests refused | Java's `stillValid` |

The movement checks run in this order on every move a client sends, moved or not: `Timer`, `MovedTooQuickly`,
then, outside the grace periods, `NoClip`, `Speed`, `Fly`, `Ascend` and `Phase`. The first that sets back stops
the others; those that only log go on.

## How a check acts

Every check has the same seven keys:

| Key | Meaning |
|---|---|
| `Enabled` | the check runs |
| `Action` | what the check does once the violation level passed `ActAt`: `log`; `setback` (the movement checks; `Timer` too); `cancel` (the action checks; for `Ascend`, the move is applied but does not reset the fall distance, NoFall's counter); `throttle` (`Timer` only); `kick` |
| `ActAt` | the violation level past which `Action` applies: 0 applies it at the first violation |
| `KickAt` | the level at which the player is kicked whatever `Action` is; 0 never kicks. A `KickAt` at most `ActAt`, with an action other than `kick`, is warned of at start |
| `KickMessage` | the reason a kick shows, with § codes; empty for the default of the check, Java's English texts: "Flying is not enabled on this server" for `Fly`, "Invalid move player packet received" for the movement checks, "Attempted to attack an invalid entity" for `EntityReach`, `Attack` and `AttackAim`, "Kicked by the anti-cheat" for the others |
| `Decay` | the violation level lost each second |
| `ExemptGameModes` | the game modes the check skips, named as `/gamemode` takes them. By default spectators skip the movement checks, and creative players `Phase`, as Java leaves them |

A violation runs a pipeline:

1. An exempt player is not judged: the check or `[AntiCheat] Enabled` is off, its game mode is exempt,
   `ExemptOperators` is set and it is an operator, it is listed in `ExemptPlayers` (names, or `xuid:<XUID>`, as the
   admin lists take them), `server.Config.AntiCheatExempt` (the hook a permission system answers through, with
   nodes such as `adamant.anticheat.bypass.<check>`) exempts it, or a plugin called `Player.ExemptAntiCheat`.
2. `Handler.HandleViolation` sees it: a plugin may ignore it, or change its action.
3. The level of the check grows by one, less what decayed since the last.
4. It is logged at Info to the logger of the player, which carries its name, UUID and address: the first of a check
   at once, then a summary every `LogInterval` ticks with the count and the highest level.
5. Past `ActAt`, the action applies; at `KickAt`, the player is kicked.

`Player.AntiCheatLevel` and `Player.ResetAntiCheat` read and reset the levels. An action a plugin asks of a player
from a handler of a checked action of its client is not checked again; one it asks from the handler of an action
that is not checked, such as chat, while the client's packet is handled, is: `ExemptAntiCheat` exempts it.

## The keys

```toml
[AntiCheat]
  Enabled = true              # master switch
  ExemptOperators = false     # operators skip every check
  ExemptPlayers = []          # names or "xuid:<XUID>" that skip every check
  LogInterval = 20            # ticks between two log lines of a check of a player
  MaxLatencyTicks = 40        # the round trip the latency windows assume at most: 2 s

  [AntiCheat.Grace]           # ticks the movement checks skip moves for after...
    Join = 60                 # ...the first move, and a loading screen
    Teleport = 10             # ...a teleport, from its acknowledgement, and a move by the server, a change of scale
    WorldChange = 40          # ...a change of world
    Respawn = 20              # ...a respawn
    Dismount = 10             # ...getting off what the player rode
    GameMode = 10             # ...a change of game mode or of flight speeds
    Impulse = 10              # added to the round trip an impulse lasts
```

The settings of each check beyond the seven keys, with their defaults:

| Check | Keys |
|---|---|
| `MovedTooQuickly` | `Limit = 10.0` blocks a tick beyond the velocity (Java's √100), `LimitGliding = 17.32` (√300) |
| `Speed` | `Tolerance = 0.3` (the part allowed beyond the envelope), `Epsilon = 0.05` blocks a tick |
| `Fly` | `HoverTicks = 80`, `HoverDescent = 0.03125`, `ResendSupport = true` (resend the blocks below, once every 200 ticks at most) |
| `Ascend` | `Epsilon = 0.05`, `StepHeight = 0.6`, `GroundMargin = 0.02`, `GroundDepth = 0.25` |
| `NoClip` | `MinDepth = 0.1` blocks, `ResendFirst = true` |
| `Phase` | `Tolerance = 0.25` blocks (Java's √0.0625) |
| `EntityReach` | `Measure = "box"` (or `"position"`, the reach before the anti-cheat), `Survival = 3.0`, `Creative = 5.0`, `Buffer = 3.0` |
| `BlockReach` | `Measure = "centre"` (or `"box"`), `Survival = 8.0`, `Creative = 14.0`, `Buffer = 0.0`; Java's rule is `"box"`, 4.5, 5.0 and 1.0. Off, the reach to blocks is not checked at all |
| `BreakTime` | `Tolerance = 0.7`, `Delay = false` (break once the time is served instead of refusing, Java's delayed destroy), `InstantPerSecond = 20.0`, `InstantBurst = 5` |
| `Place` | `PerSecond = 20.0`, `Burst = 10`, `FaceEpsilon = 0.5` blocks, `LineOfSight = false` |
| `Attack` | `PerSecond = 20.0`, `Burst = 10`, `TargetsPerTick = 1` |
| `AttackAim` | `MinCos = 0.85`, `HitboxMargin = 0.4` blocks, `LineOfSight = true` |
| `FastUse` | `MinTicks = 4`, `Burst = 2` |
| `Timer` | `Tolerance = 10` ticks ahead, `MaxBalance = 40` ticks a client may make up, `MaxQueue = 40` ticks a move may wait |
| `Menu` | `Buffer = 4.0` blocks, `RequestsPerTick = 64` |

A value the game cannot run stops the server at start with the key it is in: an action a check does not take, an
unknown game mode or measure, a negative number, a `BreakTime.Tolerance` outside (0, 1], a `MinCos` outside
[−1, 1], a `Limit` of 0. Actions and measures are read trimmed and in any case.

## The movement envelopes

`Speed` and `Ascend` hold a move to an upper bound of what Java's physics lets a player do in a tick from the state
the server knows of it (`anticheat.HorizontalAllowance`, `anticheat.VerticalAllowance`):

- **Horizontally:** what the move carries of the one before, kept by the friction of where that began (the block
  below times 0.91 on the ground, 0.91 in the air, 0.96 at most in a liquid); what it gains, the ground
  acceleration of its speed (`Speed()`, sprinting and effects included, held over the round trip) on the ground or
  a tick after it, 0.026 in the air, 0.1 at least in a liquid; a sprint-jump's 0.2; and the impulses pending. A
  client starts sprinting in the move it says so: the envelope grants a sprint the player may start. As a block's
  friction may differ between a client and the server (honey's is 0.6 in Java, 0.8 in the game), the envelope also
  grants what blocks of the default friction allow.
- **Vertically:** in the air, what gravity leaves of the move before, `(vy − 0.08) × 0.98` (0.01 under Slow
  Falling), and what a liquid's drag left of it just out of one; held up, a jump (0.42 × the jump factor of honey,
  0.1 a level of Jump Boost), a step of 0.6, a climb of 0.2, a rise of 0.7 in a liquid and 1.8 on an upward bubble
  column, the bounce of slime and beds; the lift of Levitation; and the impulses pending. A move that ends on the
  ground may step up, as Java steps up a falling player that lands.
- **Standing.** Whether a client could jump is whether it stood on the ground before the move. The game's own
  `onGround` is the box below the feet, 0.05 deep, and holds for blocks both agree on; a Bedrock client's may not
  always agree with the server's (it may stand 0.125 above a single layer of snow the server lets it sink through),
  and its position is a float32 that can lie a hair off the edge of a block. So a client also stands where its box,
  grown by `GroundMargin` to its sides, touches a block, and where a block is within `GroundDepth` below it if its
  last move did not rise or it said it collided below in it (the `VerticalCollision` flag of its input). A client
  that claims the ground in mid-air, farther from the blocks than that, or after a jump, has none.
- A move carries what physics allowed of the one before at most, its envelope with no tolerance: a move past the
  envelope that was only logged does not lift the next one, and the tolerance does not compound.

Sprinting on stone is bounded by 0.286 blocks a tick, Java's sprint is 0.2806. With the defaults, the bound a move
is held to is 1.5 times a sprint: `Speed` notices a sprint 1.6 times as fast on most moves, and a `Tolerance` of 0.2
notices 1.5 times.

**Impulses.** The velocities the server gives a client player (knockback, explosions, riptide, wind charges, slime
and bed bounces) are kept for the round trip to its client, from its latency, capped by `MaxLatencyTicks`, and
`Grace.Impulse` ticks on top: the envelopes and `MovedTooQuickly` add the largest pending. The speed of the player
and its Jump Boost and Levitation are held over the same window after they fell.

## The timer

`Timer` counts the moves of a client against the time that went by: the most of the wall clock at the arrival of a
move and of the ticks of the player since its first move, or its last change of world. A client may make up
`MaxBalance` ticks it fell behind, as after a stall of its connection, and run `Tolerance` ahead. Beyond, with
`throttle`, the game returns `MoveDeferred` and the adapter hands the input again, with what came after it, in a
later tick: a stall of 3 s and its burst have their last moves wait a few ticks, with no violation. A move that
waited longer than `Tolerance` ticks counts as a violation, once an episode; one that waited longer than
`MaxQueue` is dropped and the player set back. A client 1.25 times as fast is throttled and logged within 10 s.

## False positives

The checks come with suites of legitimate traces that must not violate any of them
(`TestAntiCheatFalsePositiveSuites`): Java's travel, ported apart from the envelopes against the blocks of a world
(walking, sprinting and sprint-jumping on stone, ice, blue ice under trapdoors, packed ice, slime, honey and soul
sand; swimming; bubble columns; ladders and scaffolding; cobweb and powder snow; Levitation and Slow Falling;
sneaking off an edge; crawling through a gap), the falls of the game's own physics onto stone, water, slime and hay,
pushes (knockback, explosions, wind charges, riptide at latencies up to 1 s, pistons), teleports, a change of game
mode in mid-air, vehicles, a lagging server, jitter and stalls.

The jumps of Bedrock clients have a suite of their own (`TestBedrockJumpsAreLegitimate`): the positions are
float32 eyes, as a client sends them, with and without its `VerticalCollision` flag; standing jumps, sprint-jumps
and repeated sprint-jumps that land and take off at once, on stone, slabs, farmland, dirt paths, soul sand, snow,
carpets, honey, slime and ice; a staircase; auto-jumps and sprint-jumps up a block; jumps from the edge of a
block and over a gap; and a client that stands 0.125 above a layer of snow the server sinks through. The cheats
that jump in mid-air or from 0.3 above the floor are still flagged (`TestJumpCheatsAreStillFlagged`).

What they cannot hold is Bedrock's physics, which is not Java's: its own sprint-jump and swimming. That
is why `Speed`, `Ascend` and `Phase` only log by default, and why `NoClip` resends before it sets back. Recorded
traces of real Bedrock clients would calibrate them before any is tightened.

Other risks and their answers:

- **Desync of blocks** between a client and the server, ghost blocks and doors toggled at latency: `NoClip` resends
  the blocks entered, `Fly` the blocks below, before they set back.
- **Latency:** the latency windows of impulses and effects, Java's buffers of reach, the grace after teleports
  counted from their acknowledgement.
- **Chunks the server lacks:** a world still generating, and the empty stand-in of a chunk that failed to load.
  `NoClip`, `Fly` and `Phase` judge a move against the blocks, so they wait while a chunk the box of the player
  touches, before or after the move and a block around, is not loaded, not generated or a stand-in: the move is
  not judged, the floating count starts again, and no setback puts the player back there. A client on blocks the
  server lacks has the ground (`Ascend`, `Speed`). No check loads a chunk to look at it: that would wait for its
  generation on the tick of the world.
- **Server lag:** the movement checks count client ticks, not server ticks. The timer reads the wall clock at the
  arrival of a move, and credits a client with the ticks the server was late, from the time between its last ticks
  (up to 10 s): the tolerance, the balance and `MaxQueue` are that many ticks longer, since a server that freezes
  for 3 s hands the 60 inputs of a client at once, however late their arrival is stamped. The break time, the rate
  of attacks, places, uses and instant breaks are read from the time the inputs arrived, not from when the server
  handled them: a break of 0.8 s that a server ticking every second handles in one tick is not cancelled.
- **Throttling** after a long stall is a short freeze, BDS's behaviour, never a setback unless the client stays
  `MaxQueue` ticks ahead.

## Corrections

A setback (`client.Self.CorrectMove`) puts the client back where the server has its player, or, for `Fly`, where it
last stood, which its viewers see too; the Bedrock adapter sends a teleport and waits for its acknowledgement, and a
setback starts no grace. Refused breaks, places and uses resend the blocks; a refused flight resends the abilities.
Rewound corrections (`CorrectPlayerMovePrediction`) are not used: StartGame keeps a rewind history of 0.

## BDS and Java

| BDS 1.26.52.3 key | Here |
|---|---|
| `server-authoritative-movement-strict` | no single key: the tolerances of `Speed`, `Ascend` and `NoClip` |
| `server-authoritative-dismount-strict` | n/a: vehicles are driven by the server; `Grace.Dismount` |
| `server-authoritative-entity-interactions-strict` | `EntityReach.Buffer`, `AttackAim.LineOfSight` |
| `player-position-acceptance-threshold` | the `Epsilon`s of the envelopes and `NoClip.MinDepth` |
| `player-movement-action-direction-threshold` | `AttackAim.MinCos` |
| `server-authoritative-block-breaking` | `BreakTime.Enabled`; StartGame keeps server-authoritative breaking on |
| `server-authoritative-block-breaking-pick-range-scalar` | `BlockReach.Survival`, `Creative` |
| `player-rewind-history-size-ticks` | not used; `Timer.MaxBalance` forgives the same 2 s of lag |
| the input tick policy | `Timer`, action `throttle` |

| Java 26.3 | Here |
|---|---|
| "moved too quickly" | `MovedTooQuickly`, with the game rules `player_movement_check` and `elytra_movement_check` |
| "moved wrongly" | `Phase` |
| `isEntityCollidingWithAnythingNew` | `NoClip` |
| "floating too long" | `Fly`, which sets back where Java kicks |
| `canInteractWithEntity`, `canInteractWithBlock` | `EntityReach`, `BlockReach` with `Measure = "box"` |
| `ServerPlayerGameMode` break progress | `BreakTime`, refusing as BDS does, or delaying (`Delay`) as Java does |
| `stillValid` | `Menu` |
| the 1.0000001 bound of `handleUseItemOn` | `Place` |
