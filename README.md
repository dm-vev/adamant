<!--suppress ALL -->

# Adamant

Adamant is a Go-based Minecraft: Bedrock Edition server, forked from df-mc/dragonfly. Its goal is behavioural compatibility with a pinned modern Bedrock Dedicated Server (BDS) release while retaining native Go simulation. Full vanilla parity has **not** been reached.

Upstream: https://github.com/df-mc/dragonfly

## What’s New in Adamant
- **Modern protocol and transport:** gophertunnel v1.62.0 (protocol 2193), native NetherNet HTTP signaling/WebRTC, and a persistent server identity across restarts.
- **Fully asynchronous world generator:** generation tasks now run in parallel across worker goroutines, drastically improving chunk generation speed and reducing main-thread load.
- **PM‑style world generation**: pmgen overworld with biomes (e.g., Swamp) and ore population; configurable seed, worker count and queue size.
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

The reference is BDS **1.26.52.3**, build **51798919**. Archive and embedded item-registry hashes are recorded in the reference file. Protocol library version strings and successful connections alone do not establish gameplay parity.

Current gaps include modern world generation, mob coverage and block implementations. A registered block state can still be an `unknownBlock`; a non-placeholder implementation can also have incomplete behaviour.

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


Upstream docs: https://pkg.go.dev/github.com/df-mc/dragonfly/server

## Contributing
Contributions are welcome! Please open issues/PRs with focused changes, behaviour references and explicit edition/version information. Do not replace reference expectations with hashes generated by the implementation under test.

Thanks to the df‑mc maintainers and community for the excellent base.
