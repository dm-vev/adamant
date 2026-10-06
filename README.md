<!--suppress ALL -->

# Adamant

Adamant is a Go-based Minecraft: Bedrock Edition server, forked from df-mc/dragonfly. Its goal is behavioural compatibility with a pinned modern Bedrock Dedicated Server (BDS) release while retaining native Go simulation. Full vanilla parity has **not** been reached.

Upstream: https://github.com/df-mc/dragonfly

## What’s New in Adamant
- **Modern protocol and transport:** gophertunnel v1.62.0 (protocol 2193), native NetherNet HTTP signaling/WebRTC, and a persistent server identity across restarts.
- **Fully asynchronous world generator:** generation tasks now run in parallel across worker goroutines, drastically improving chunk generation speed and reducing main-thread load.
- **Optional PM-style generator:** pmgen remains available separately; it is not the default dimension generator.
- **Legacy dimension generators**: Overworld/Nether/End use partial Java 1.12-based algorithms. They are not modern Bedrock world generation.
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
Reproduce the gameplay, generation, persistence and network behaviour of the BDS release pinned in [`vanilla-reference.json`](vanilla-reference.json). BDS is the behavioural oracle; Java sources are supplementary references, not a substitute for checking edition differences.

The reference is BDS **1.26.52.3**, build **51798919**. Archive, embedded item-registry and block-palette hashes are recorded in the reference file. Protocol library version strings and successful connections alone do not establish gameplay parity.

Current gaps include modern world generation, mob coverage and block implementations. A registered block state can still be an `unknownBlock`; a non-placeholder implementation can also have incomplete behaviour.

### Block states and migration

The embedded palette contains **22,079 persisted states across 1,477 block identifiers**, captured from the pinned BDS. This is registry coverage, not implementation coverage.

- Panes, bars, fences and tripwire persist their four cardinal connections. Stairs persist straight, inner-left/right and outer-left/right corners.
- Old disk states retain explicitly supplied properties. Missing derived properties are reconstructed from loaded neighbours; unresolved boundary states survive save/reload without forcing neighbouring chunk generation. Stair geometry resolves before dependent pane/fence connections.
- Generated chunks reconstruct the same derived properties before they are committed as complete states.
- Stair neighbour rules are checked against 112 BDS scenarios, including orientation, half and material-dependent corner suppression. Physical corner collision and projectile/ray shape are separate: captured BDS arrows use the straight stair outline even at corners. Explosion and firework exposure retain their existing collision-shape behaviour.
- State lookup distinguishes property names, types and string boundaries; invalid supplied properties are not silently replaced with defaults.

Custom `MissingStateReconstructor` implementations must preserve explicit properties and assign a lower `ReconstructionOrder` to derived-state dependencies. Same-order neighbours may be read for base properties, not unresolved derived geometry. Ray callers now choose `trace.RayShape` or `trace.CollisionShape` explicitly.

### Modern generation numerical kernel

`server/world/generator/advanced/internal/bedrock` implements seeded float32 noise, eight-block Voronoi biome cells, cached climate columns, terrain splines and column controls, blended terrain noise, preliminary surface heights, native-ordered biome lookup, Overworld cave density and aquifers. The default dimension generators still use the legacy algorithms; these components do not yet replace their terrain, caves, fluids, surfaces or features.

Readable Java 26.3 sources generated with [MCP-Reborn](https://github.com/Hexeption/MCP-Reborn) supplied the spline shapes and Overworld climate rules. Bedrock-specific noise precision, quantization, spatial interpolation and tree ordering were checked separately. The packed tree contains 7,594 parameter records and 9,113 nodes, ordered by executing the pinned BDS tree builder rather than approximating its unstable sort.

Verification:
- 30,000 noise evaluations, 900 cell-index overflow cases and 100,000 biome-cell selections match the executed BDS routines. Overflow follows the native SSE conversion, including non-finite noise results; it is not replaced with Java coordinate wrapping.
- 49,614 spline evaluations agree between Java and the BDS evaluator using the same MCP-provided shape data.
- 7,740 native tree-search queries agree, including history-sensitive interval boundaries.
- 17,856 sampled biome observations across five seeds agree with the live BDS API. This is sampled climate coverage, not complete world-generation parity.
- 50,000 aquifer queries agree bit-for-bit with the native BDS fluid-level and barrier routines across five seeds, negative coordinates, world-border coordinates and five prescribed preliminary surfaces. This verifies the fluid component with initialized noise/spline inputs, not the complete terrain pipeline. The Bedrock implementation selects three nearest centres and retains its own pressure and update thresholds.
- 10,000 terrain columns and preliminary surface heights agree with the native routines using the same initialized noise and MCP spline data. Another 5,000 queries agree with native terrain-surface and aquifer sampling connected together.
- 32,790 blended terrain samples agree bit-for-bit with the native three-dimensional smeared-Perlin evaluator across six seeds, negative Y, subcell coordinates and far cell-index overflow. The immutable 16/16/8 octave stacks allocate nothing while sampling.
- 50,595 cave-density queries agree bit-for-bit with the native routine using initialized noise inputs, including the surface/underground transition, negative and world-border coordinates, and non-finite upstream densities. The harness bridges `powf` to host libm; Go's widened pillar cube agrees in this corpus but is not universally bit-identical to that library.
- Aquifer workers cache the bounded 27×19 preliminary-surface window instead of resampling it at every fluid centre. Hot climate, Voronoi, spline, terrain-column, cave and aquifer sampling allocates zero objects after initialization; aquifer chunk-cache reset also allocates nothing. Native-backed regression tests pass under the Go race detector.

The source revisions, reference executable hash and packed-data checksum are pinned in `vanilla-reference.json`. Decompiled game sources and reference executables are not redistributed with the kernel.

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
