<!--suppress ALL -->

# Adamant

Adamant is a Go-based Minecraft: Bedrock Edition server, forked from df-mc/dragonfly. The goal is to reach feature parity with PocketMine‑MP (PMMP) while keeping Dragonfly’s performance and developer‑friendly design.

Upstream: https://github.com/df-mc/dragonfly

## What’s New in Adamant
- **Fully asynchronous world generator:** generation tasks now run in parallel across worker goroutines, drastically improving chunk generation speed and reducing main-thread load.
- **PM‑style world generation**: pmgen overworld with biomes (e.g., Swamp) and ore population; configurable seed, worker count and queue size.
- **Vanilla dimension generators**: new Overworld/Nether/End generators ported from the original Minecraft generation rules.
- **Built‑in admin commands + server CLI**: `help`, `list`, `kick`, `gamemode`, `time`, `chat`, `gc`, `status`, `stop`, `about`, `whitelist`.
- **Bedrock Query support**: server status, players, MOTD and plugins exposed to query clients (integrated via `server/query_adapter.go`).
- **Whitelist system**: Nukkit-style `white-list.txt` allowlist with legacy TOML fallback; built‑in `whitelist`/`allowlist` commands and config enforcement.
- **New items and vanilla features**: early stage of implementing core Bedrock mechanics — starting with fishing rods and Nether portals.
- **Stability and concurrency hardening**: locks/atomics across sessions, registries, world counters, whitelist checks, chunk radius, and command origin tracking; snapshotting to avoid hot locks and I/O under locks.
- **Inventory and container safety**: ordered locks for merge operations, guarded slot callbacks, validated stack requests/containers, hotbar slot clamping, and safer container-close handling.
- **Data validation and panic resistance**: checks for creative item IDs, anvil rename indices, banner patterns, item NBT damage/book pages, skin/cape dimensions, transfer ports, and query HostPort bounds.
- **World/AI correctness fixes**: bounded pmgen population concurrency, placeholder chunk tracking after load errors, stable pathfinding node storage, and hardened weather/explosion randomness.
- **Gameplay correctness**: fixes for lightning height/offsets, grindstone XP ranges, crossbow quick-charge ticks, instant-break haste baseline, XP drop ranges, chest unpairing, and barrel/brewing NBT state.
- **Query and persistence robustness**: token pruning, UUID validation, corruption guards for inventories, detached query player slices, and safer resource pack building.

## Project Goal
Achieve a PMMP‑like feature list, prioritising gameplay parity and admin ergonomics while keeping clean Go APIs for plugin and feature work.

## Getting Started
Requirements: **Go 1.26+**

Run from source:
```shell
git clone https://github.com/dm-vev/adamant
cd adamant
go run .
```

Stop the server with `Ctrl+C`.

### Configuration
Server settings are in `config.toml`.
- `Network.Address`: listen address and port (default `:19132`).
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
The Bedrock Query adapter publishes live status, player names, and MOTD to query clients. No extra setup needed.

## Development
Adamant tracks Dragonfly upstream and focuses on PMMP parity on top. The codebase keeps Dragonfly’s structure and API conventions to make contributing straightforward.

Upstream docs: https://pkg.go.dev/github.com/df-mc/dragonfly/server

## Contributing
Contributions are welcome! Please open issues/PRs with focused changes. If you’re proposing PMMP‑parity features, include examples and behaviour references. 

Thanks to the df‑mc maintainers and community for the excellent base.
