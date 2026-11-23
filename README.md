<div align="center">

# Adamant

**High-performance Minecraft: Bedrock Edition server in Go, built for vanilla parity and clean extensibility.**

[![Go](https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![Bedrock](https://img.shields.io/badge/Bedrock-1.26.52%20%E2%80%93%201.26.61-62B47A)](docs/protocols.md)
[![Protocol](https://img.shields.io/badge/protocol-2193%20%7C%202225-blue)](docs/protocols.md)
[![Java reference](https://img.shields.io/badge/Java%20reference-26.3-E76F00)](vanilla-reference.json)
[![License: MIT](https://img.shields.io/badge/license-MIT-lightgrey)](LICENSE)

[Quick start](#quick-start) ·
[Status](#status) ·
[Architecture & SDK](#architecture--sdk) ·
[Documentation](#documentation) ·
[Development](#development)

</div>

---

Adamant is a Minecraft: Bedrock Edition server in Go (originally forked from [Dragonfly](https://github.com/df-mc/dragonfly)). Clients connect over native NetherNet or RakNet as they would to Bedrock Dedicated Server, while world generation, mob AI, redstone, and survival rules are ported directly from vanilla references:

| Reference | Version | Scope |
|---|---|---|
| Java Edition ([MCP-Reborn](https://github.com/Hexeption/MCP-Reborn)) | **26.3** | World generation, structures, mob AI (`MobAI` & `Brain`), blocks, items, commands, survival |
| Bedrock Dedicated Server | **1.26.52.3** | Wire protocol, 2,076 items, 22,079 block states, 89 biomes, LevelDB & NBT layout |

Exact reference checksums are pinned in [`vanilla-reference.json`](vanilla-reference.json); parity checks are documented in [`docs/verification.md`](docs/verification.md).

## Quick start

Requires **Go 1.27+** (`toolchain go1.27.3`).

```shell
git clone https://github.com/dm-vev/adamant
cd adamant
go run .
```

- Reads [`config.toml`](config.toml) from the working directory (generates defaults on first run) and listens on `:19132`.
- Supports Bedrock **1.26.50 – 1.26.61** (protocols `2193` and `2225`, automatic packet translation).
- Pass `-headless` to disable the interactive console; stop cleanly with `Ctrl+C` or `stop`.

## Status

| Area | State | Summary |
|---|:---:|---|
| Protocol & networking | ✅ | Protocols 2193 & 2225, NetherNet (WebRTC) + RakNet, `Flate` & `Snappy` crowd batching |
| Registries & persistence | ✅ | Full BDS item/block/biome registries, native LevelDB worlds, background chunk saving |
| World generation | 🟢 | Java 26.3 Overworld, Nether & End, structures, single-pass 4-color DAG decoration |
| Mob AI & spawning | 🟢 | Java `MobAI` + `Brain` for all species, riding, raids, villagers, `NaturalSpawner` |
| Blocks, items & redstone | 🟢 | All vanilla blocks (except 5 Education blocks), BDS crafting, Java smelting/loot, [redstone](docs/redstone-audit.md) |
| Commands & admin | 🟢 | Java 26.3 commands (`execute`, selectors, scoreboards), RBAC, bans, backups, anti-cheat |

**Known gaps:** 5 Education Edition blocks are unimplemented; Bedrock lacks banner map markers and a few Java-only Dragon effects; natural mob counts on generated terrain have not yet been cross-counted live on a Java server.

## Architecture & SDK

The engine separates a lean, data-oriented simulation core (`server/`) from protocol adapters (`protocol/`, `net/`) and an external plugin SDK (`api/`, `api/bedrock/`):

- **Zero-alloc ECS & EventBus (`server/ecs`, `server/event`):** dense typed component bags (`Key[T]` / `Bag` with optional NBT persistence) on entities, players, chunks, and worlds; zero-reflection prioritized `event.Bus` (`Cancellable`) on `Server`, `World`/`Tx`, and `Player`.
- **Parallel world & crowd AI (`server/world`, `server/entity`):** `World.AddSystem(TickPhase)` with panic containment, `SetBlockRID` / `FillRID` volume ops, 4×4 checkerboard parallel world `Regions`, multi-core `Prefetch` (fluids, collisions, melee/ranged/brain A* paths), 64-shard `sharedPathCache`, and 4×4 sub-cell parallel broadphase collisions.
- **High-density network pipeline (`net/bedrock`, `protocol/bedrock`):** pre-framed `netconn.Shared` broadcasts, single-lock `Conn.WriteBatch` session flushing, composable RFC 1951 sync-flushed DEFLATE segments in `BatchCache` (eliminating `N-1` crowd recompression), and a zero-dependency pure-Go `bedrock.Snappy` codec.
- **External SDK (`api`, `api/bedrock`):** `App` / `Plugin` lifecycle, player facets (`Movement`, `Combat`, `UI`, `Net`), custom blocks, entities, AI goals, menus, commands, Overworld generation hooks (`overworld.Hooks`), and Bedrock `PacketInterceptor` / raw packet injection — without modifying core packages.

```go
app := api.New(srv).Use(api.FuncPlugin{
	OnEnable: func(app *api.App) {
		api.On(app.Events(), api.PriorityNormal, func(ev *player.PlayerJoinEvent) {
			ev.Player.Message("Welcome to Adamant!")
		})
	},
})
defer app.Close()
```

## Documentation

All runtime settings live in [`config.toml`](config.toml). Detailed subsystem guides are in [`docs/`](docs):

- [`docs/protocols.md`](docs/protocols.md) — supported Bedrock versions, NetherNet signalling/WebRTC, and RakNet
- [`docs/anticheat.md`](docs/anticheat.md) — movement, combat, interaction, and timer checks (`[AntiCheat]`)
- [`docs/admin.md`](docs/admin.md) — permission levels, roles, bans, online backups, audit log, and HTTPS Remote Admin API
- [`docs/identity.md`](docs/identity.md) — Xbox Live authentication, `IdentityKey` (`xuid` / `name`), and `server-key.pem`
- [`docs/operations.md`](docs/operations.md) — Prometheus `/metrics`, `/livez` & `/readyz` probes, `pprof`, and structured logging
- [`docs/protocol-boundary.md`](docs/protocol-boundary.md) — core/adapter architecture and R1–R5 boundary ratchets
- [`docs/redstone-audit.md`](docs/redstone-audit.md) & [`docs/verification.md`](docs/verification.md) — BDS & Java 26.3 parity verification
- [`docs/localization.md`](docs/localization.md) — command localization

## Development

```shell
go build ./...
go test ./...
go test ./internal/archtest   # boundary, mutation, wait, API, entity-layer & func-length ratchets
make lint                     # golangci-lint v2.15.0
```

| Path | Contents |
|---|---|
| `main.go`, `internal/app` | Entrypoint, `config.toml` loader, logging, metrics, and service lifecycle |
| `api`, `api/bedrock` | External plugin SDK and Bedrock packet interception / raw packet helpers |
| `server/ecs`, `server/event` | Zero-allocation component bags and prioritized typed event bus |
| `server/world` | World, chunks, tick systems, 4×4 parallel regions, LevelDB (`mcdb`), and generators (`generator/advanced`) |
| `server/entity` | Entities, `MobAI` & `Brain` frameworks, parallel prefetch, `sharedPathCache`, and mob behaviours |
| `server/player` | Players, zero-allocation facets, typed events, and anti-cheat |
| `server/block`, `server/item`, `server/cmd` | Block/item registries & mechanics, Java 26.3 command engine |
| `protocol/bedrock`, `net` | Bedrock adapter (`session`, `netconn`, `query`) and stdlib-only `net` transport/compression module |
| `internal/archtest`, `cmd` | Architectural ratchets (R1–R5, API snapshot) and standalone BDS/Java audit tools |

## License

Released under the [MIT License](LICENSE). Originally built on [Dragonfly](https://github.com/df-mc/dragonfly) and [gophertunnel](https://github.com/Sandertv/gophertunnel), with Java reference sources via [MCP-Reborn](https://github.com/Hexeption/MCP-Reborn). *Not an official Minecraft product; not approved by or associated with Mojang or Microsoft.*
