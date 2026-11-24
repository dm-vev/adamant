<div align="center">

# Adamant

**High-performance Minecraft: Bedrock Edition server in Go — drop-in BDS alternative with Java 26.3 parity and clean extensibility.**

<p align="center"><a href=".github/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/dm-vev/adamant/ci.yml?branch=main&label=CI" alt="CI" height="20"></a> <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.27+" height="20"></a> <a href="docs/protocols.md"><img src="https://img.shields.io/badge/Bedrock-1.26.40%E2%80%931.26.66-62B47A" alt="Bedrock 1.26.40-1.26.66" height="20"></a> <a href="docs/protocols.md"><img src="https://img.shields.io/badge/protocols-2168%E2%80%932225-0284C7" alt="Protocols 2168-2225" height="20"></a> <a href="vanilla-reference.json"><img src="https://img.shields.io/badge/Java-26.3-E76F00" alt="Java 26.3" height="20"></a> <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-64748B" alt="License: MIT" height="20"></a></p>

[**Quick Start**](#quick-start) ·
[**Why Adamant?**](#why-adamant) ·
[**Benchmarks**](#benchmarks) ·
[**Architecture & SDK**](#architecture--sdk) ·
[**Documentation**](#documentation) ·
[**Contributing**](.github/CONTRIBUTING.md)

</div>

---

**Adamant** is an open-source, high-performance **Minecraft: Bedrock Edition server written in Go** — designed as a fast, extensible drop-in alternative to **Bedrock Dedicated Server (BDS)**. Clients across **1.26.40 – 1.26.66** connect over native **NetherNet (WebRTC)**, while world generation, structures, mob AI (`MobAI` & `Brain`), redstone, and survival mechanics are ported directly from vanilla **Java Edition 26.3** and official **BDS 1.26.44 – 1.26.66** references ([`vanilla-reference.json`](vanilla-reference.json)).

## Why Adamant?

| Capability | **Adamant** | Vanilla BDS | PocketMine-MP / Nukkit | Upstream Dragonfly |
|---|:---:|:---:|:---:|:---:|
| **Vanilla Overworld, Nether, End & Structures** | ✅ Java 26.3 | ✅ | ⚠️ Basic / plugins | ⚠️ Basic Overworld |
| **Full Vanilla Mob AI (`MobAI` + `Brain`, Raids, Villagers)** | ✅ | ✅ | ❌ | ❌ |
| **Redstone, Crafting, Loot & Survival Parity** | ✅ | ✅ | ⚠️ Partial | ⚠️ Partial |
| **Multi-Core Regions, Parallel AI & Crowd Batching** | ✅ | ⚠️ Limited | ❌ Single-tick bottlenecks | ⚠️ Single-world loop |
| **Multi-Version Bedrock Protocol (`2168` – `2225`)** | ✅ Built-in | ❌ Single version | ⚠️ Via plugins | ❌ Single version |
| **Typed Zero-Allocation Plugin SDK** | ✅ Go (`api/`) | ❌ Closed C++ | PHP / Java | Go (core-coupled) |
| **Single Static Binary (`MIT`, Linux / Windows / macOS)** | ✅ | ❌ Proprietary | ❌ Runtime required | ✅ |

## Benchmarks

Measured on AMD Ryzen 7 8700F (`Go 1.27.3`, 20 TPS = `50 ms` tick budget):

| Workload | Result | Notes |
|---|---|---|
| **1,003 dense mobs in one area** (`RADIUS=5`) | **`7.77 ms` p50** (`11.27 ms` p99) | Parallel `Prefetch`, 64-shard `sharedPathCache`, 4×4 sub-cell broadphase, `0 GC` |
| **1,678 mobs across 4 clusters** (`REGIONS=1`) | **`12.71 ms` p50** | 4×4 checkerboard parallel world regions (~25% of 50 ms tick budget) |
| **100-player crowd packet flush** | **`163.5 µs`** (`1.02 GB/s`, `2 allocs`) | Sync-flushed RFC 1951 `Flate` segments in `BatchCache` (**2.82×** faster) |
| **Pure-Go `bedrock.Snappy` codec** | **`1.61 GB/s`** decompress / **`698 MB/s`** compress | Zero-cgo `24 KiB` chunk in `35.2 µs`; `1.85 KiB` batch in `1.15 µs` |
| **Java 26.3 Overworld generation** (`16×16`) | **`55.9 chunks/s`** (`17.9 ms/chunk`, 2 workers) | Single-pass 4-color DAG decoration (**-78%** memory & allocations) |
| **ECS & EventBus hot path** | **`0.20 ns`** `Bag.Get` · **`9.3 ns`** `Bus` emit | `0 B/op`, `0 allocs/op`, zero reflection |

## Quick start

**Docker** (`linux/amd64` & `linux/arm64`):

```shell
docker run -d --name adamant \
  -p 19132:19132/tcp -p 19132:19132/udp \
  -v adamant-data:/data \
  ghcr.io/dm-vev/adamant:latest
```

**Prebuilt binaries:** grab the latest archive for Linux, Windows, or macOS from [**Releases**](https://github.com/dm-vev/adamant/releases) and run `./adamant`.

**Build from source** (requires **Go 1.27+**):

```shell
git clone https://github.com/dm-vev/adamant
cd adamant
go run .
```

- Generates default [`config.toml`](config.toml) on first launch and listens on `:19132` (NetherNet).
- Accepts Bedrock **1.26.40 – 1.26.66** clients simultaneously out of the box (`Protocols = ["all"]`, `PrimaryProtocol = "latest"`):

| Protocol | Bedrock versions | Pipeline |
|---|---|---|
| `2225` | `1.26.60` – `1.26.66` | Primary model (zero-conversion wire fast path) |
| `2193` | `1.26.50` – `1.26.52` | Wire & block/item translation (`c2225`) |
| `2169` | `1.26.45` | Multi-step wire, biome & block/item translation (`c2193` → `c2225`) |
| `2168` | `1.26.40` – `1.26.44` | Multi-step wire, biome & block/item translation (`c2169` → `c2193` → `c2225`) |

- Pass `-headless` for non-interactive systemd/Docker deployments; stop cleanly with `Ctrl+C` or `stop`.

## Status

| Subsystem | State | Highlights |
|---|:---:|---|
| Protocol & networking | ✅ | Multiprotocol `2168`–`2225` (`1.26.40`–`1.26.66`), native NetherNet (WebRTC), `Flate` & `Snappy` crowd batching |
| Registries & persistence | ✅ | Per-protocol BDS registries (up to 2,080 items, 23,900+ block states, 138 entities, 89 biomes), LevelDB (`mcdb`) |
| World generation | 🟢 | Java 26.3 Overworld, Nether & End, structures, single-pass 4-color DAG decoration |
| Mob AI & spawning | 🟢 | Java `MobAI` + `Brain` for all species, riding, raids, villagers, `NaturalSpawner` |
| Blocks, items & redstone | 🟢 | All vanilla blocks (except 5 Education blocks), BDS crafting, Java smelting/loot, [redstone](docs/redstone-audit.md) |
| Commands, security & ops | 🟢 | Java 26.3 `execute` & scoreboards, RBAC, bans, backups, [anti-cheat](docs/anticheat.md), Prometheus `/metrics` |

**Known gaps:** 5 Education Edition blocks are unimplemented; Bedrock lacks banner map markers and a few Java-only Dragon effects; natural mob counts on generated terrain have not yet been cross-counted live against a running Java server ([`docs/verification.md`](docs/verification.md)).

## Architecture & SDK

Adamant isolates a data-oriented simulation core (`server/`) from wire adapters (`protocol/`, `net/`) and an external plugin SDK (`api/`, `api/bedrock/`), enforced in CI by architectural ratchets (`internal/archtest`):

- **Zero-alloc ECS & EventBus (`server/ecs`, `server/event`):** dense typed component bags (`Key[T]` / `Bag` with optional NBT persistence) on entities, players, chunks, and worlds; prioritized `event.Bus` (`Cancellable`) on `Server`, `World`/`Tx`, and `Player`.
- **Parallel simulation (`server/world`, `server/entity`):** `World.AddSystem(TickPhase)` with panic containment, `SetBlockRID` / `FillRID` volume ops, 4×4 parallel world `Regions`, multi-core AI/fluid/collision `Prefetch`, and `sharedPathCache`.
- **External SDK (`api`, `api/bedrock`):** `App` / `Plugin` lifecycle, player facets (`Movement`, `Combat`, `UI`, `Net`), custom blocks/entities/AI goals/menus/commands, Overworld hooks (`overworld.Hooks`), and Bedrock `PacketInterceptor` — without touching core packages.

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

Runtime settings are documented in [`config.toml`](config.toml). Deep-dive guides live in [`docs/`](docs):

- [`docs/protocols.md`](docs/protocols.md) — Bedrock multiprotocol (`2168`–`2225`), version selection, and wire translation
- [`docs/anticheat.md`](docs/anticheat.md) — movement, combat, interaction, and timer checks (`[AntiCheat]`)
- [`docs/admin.md`](docs/admin.md) — RBAC permissions, bans, online backups, audit log, and HTTPS Remote Admin API
- [`docs/identity.md`](docs/identity.md) — Xbox Live authentication, `IdentityKey`, and `server-key.pem`
- [`docs/operations.md`](docs/operations.md) — Prometheus `/metrics`, `/livez` & `/readyz` probes, `pprof`, and logging
- [`docs/protocol-boundary.md`](docs/protocol-boundary.md) — core/adapter boundaries and R1–R5 architectural ratchets
- [`docs/redstone-audit.md`](docs/redstone-audit.md) & [`docs/verification.md`](docs/verification.md) — BDS & Java 26.3 parity verification

## Development & contributing

```shell
go build ./...
go test ./...
go test ./internal/archtest   # boundary, mutation, wait, API, entity-layer & func-length ratchets
```

| Path | Contents |
|---|---|
| `main.go`, `internal/app` | Entrypoint, `config.toml` loader, logging, metrics, and service lifecycle |
| `api`, `api/bedrock` | External plugin SDK and Bedrock packet interception / raw packet helpers |
| `server/ecs`, `server/event` | Zero-allocation component bags and prioritized typed event bus |
| `server/world`, `server/entity` | Parallel world regions, LevelDB, Java 26.3 worldgen, `MobAI` & `Brain` |
| `server/player`, `server/block`, `server/item`, `server/cmd` | Player facets & anti-cheat, block/item mechanics, Java 26.3 commands |
| `protocol/bedrock`, `net` | Bedrock session/wire adapter and stdlib-only `net` transport & compression module |
| `internal/archtest`, `cmd` | Architectural ratchets (R1–R5, API snapshot) and BDS/Java parity audit tools |

Contributions, bug reports, and parity fixes are welcome — see [**CONTRIBUTING.md**](.github/CONTRIBUTING.md) and open [**Issues**](https://github.com/dm-vev/adamant/issues). If Adamant helps your server or Go game-server research, **consider leaving a ⭐ on GitHub**!

## License

Released under the [MIT License](LICENSE). Originally forked from [Dragonfly](https://github.com/df-mc/dragonfly) and [gophertunnel](https://github.com/Sandertv/gophertunnel), with Java reference mechanics via [MCP-Reborn](https://github.com/Hexeption/MCP-Reborn). *Not an official Minecraft product; not approved by or associated with Mojang or Microsoft.*
