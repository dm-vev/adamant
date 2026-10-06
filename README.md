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

Custom `MissingStateReconstructor` implementations must preserve explicit properties and assign a lower `ReconstructionOrder` to derived-state dependencies. Same-order neighbours may be read for base properties, not unresolved derived geometry. Ray callers now choose `trace.RayShape` or `trace.CollisionShape` explicitly.

### Generated entities and block data

`world.Generator.GenerateChunk` now returns `world.GenerationResult` in addition to filling the supplied chunk. Return newly constructed worldless entity handles, absolute-position block-entity NBT, and initial block updates whose `Tick` values are relative delays. The world takes ownership; generation workers must not retain mutable result data or enter world transactions. Block-only generators return the zero value.

The world installs this data once on its owner goroutine before publishing or saving the column. Shutdown also consumes in-flight results, and read-only worlds close their entities without saving columns. Dimension generators, rather than the world layer, supply gateway destinations and banner data.

Runtime verification under the race detector covered synchronous and background generation, an End crystal with a beam target, gateway NBT, disk reopening without a generator, and a generated water update that flowed on its third tick. Regression tests also cover shutdown while a generation worker is still running. Flat generation fills biomes through the dimension's inclusive upper boundary, including Overworld Y=319.

### Java 26.3 generation kernel

`server/world/generator/advanced/internal/java26` ports Java's legacy and Xoroshiro random sources, float32 noise stacks, blended terrain noise, terrain splines, climate search, three-dimensional biome zoom, Overworld density graph, noodle caves, ore-vein fields and aquifers. The climate tree contains all 7,594 Java parameter records and uses Java's 19-child search tree; biome identities are translated separately to Bedrock registry IDs.

The Overworld cutover is still in progress: the verified kernel does not yet replace the default generator's complete terrain, surfaces, structures and feature pipeline. These checks are not a claim of complete generation or gameplay parity.

Verification against executed Java 26.3 classes:

- 707 random/noise comparisons cover both random algorithms, Gaussian sampling, normal and legacy Nether noise, simplex noise, blended terrain, biome-seed hashing and ridge folding.
- 16,538 terrain-spline samples match the Java results exactly.
- 280 Overworld router fields across 35 sampled positions agree within `1e-6`; the largest observed absolute difference was `7.450580596923828e-9`.
- 35 biome-zoom probes agree, including negative coordinates.
- Fifteen complete Overworld density/aquifer chunks across seeds `0`, `1` and `-1` match all **1,474,560** material decisions and fluid-update flags. This excludes surface rules, structures, carvers and placed features.

Reference executables, decompiled sources and temporary oracle programs are not committed. A PDB may only supply RVAs for an executable with the matching CodeView GUID and age; the supplied 26.51.1 symbols do not match the pinned 1.26.52.3 executable.

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
