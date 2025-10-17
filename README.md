<!--suppress ALL -->

# Adamant

Adamant is a Go-based Minecraft: Bedrock Edition server, forked from df-mc/dragonfly. The goal is to reach feature parity with PocketMine‑MP (PMMP) while keeping Dragonfly’s performance and developer‑friendly design.

Upstream: https://github.com/df-mc/dragonfly

## What’s New in Adamant
- **Fully asynchronous world generator:** generation tasks now run in parallel across worker goroutines, drastically improving chunk generation speed and reducing main-thread load.
- **PM‑style world generation**: pmgen overworld with biomes (e.g., Swamp) and ore population; configurable seed, worker count and queue size.
- **Built‑in admin commands + server CLI**: `help`, `list`, `kick`, `gamemode`, `time`, `chat`, `gc`, `status`, `stop`, `about`, `whitelist`.
- **Bedrock Query support**: server status, players, MOTD and plugins exposed to query clients (integrated via `server/query_adapter.go`).
- **Whitelist system**: TOML‑backed whitelist with built‑in commands to add/remove/list entries; toggle enforcement via config (`[Whitelist]`).
- **New items and vanilla features**: early stage of implementing core Bedrock mechanics — starting with fishing rods and Nether portals.

## Project Goal
Achieve a PMMP‑like feature list, prioritising gameplay parity and admin ergonomics while keeping clean Go APIs for plugin and feature work.

## Getting Started
Requirements: **Go 1.23+**

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
- `Server.Name`: server name in the list; `AuthEnabled`, `DisableJoinQuitMessages`, `MuteEmoteChat`.
- `World`: `Seed`, `SaveData`, `GeneratorWorkers`, `GeneratorQueueSize`, `Folder`.

  The asynchronous generator defaults to `GeneratorQueueSize = GeneratorWorkers * 4`. Under
  extreme preloading you may need to raise both values to keep the queue from saturating; watch
  the server logs for `world generator queue saturated` warnings. Profile the generator first if
  it is limited by LevelDB or other I/O to ensure additional workers do not become the bottleneck.
- `Players`: `MaxCount`, `MaximumChunkRadius`, `SaveData`, `Folder`.
- `Resources`: `AutoBuildPack`, `Folder`, `Required`.
- `Whitelist`: `Enabled`, `File` (default `whitelist.toml`).

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
