<!--suppress ALL -->

<div align="center">

# Adamant

**A Minecraft: Bedrock Edition server in Go, built for full vanilla parity.**

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![Bedrock](https://img.shields.io/badge/Bedrock-1.26.52-62B47A)](vanilla-reference.json)
[![Protocol](https://img.shields.io/badge/protocol-2193-blue)](vanilla-reference.json)
[![Java reference](https://img.shields.io/badge/Java%20reference-26.3-E76F00)](vanilla-reference.json)
[![License: MIT](https://img.shields.io/badge/license-MIT-lightgrey)](LICENSE)

[Goal](#goal-full-vanilla-parity) ·
[Status](#status) ·
[Features](#whats-implemented) ·
[Getting started](#getting-started) ·
[Configuration](#configuration) ·
[Development](#development) ·
[Verification notes](docs/verification.md)

</div>

---

Adamant is a Minecraft: Bedrock Edition server written in Go, forked from [Dragonfly](https://github.com/df-mc/dragonfly). Bedrock clients connect to it as they would to Bedrock Dedicated Server (BDS), while the world behaves as the vanilla game does. Terrain, structures, mobs, blocks, redstone and survival rules are ported from the game's own sources rather than approximated.

> [!IMPORTANT]
> Adamant aims for **full vanilla parity** but has not reached it yet. Large parts of the game already behave as vanilla does; the [status table](#status) and [known gaps](#known-gaps) list what is complete, partial or missing.

## Goal: full vanilla parity

Vanilla parity means that a world played on Adamant is indistinguishable from the same world played on vanilla Minecraft. The same seed should give the same terrain, mobs should make the same decisions, machines should keep the same timing, and saves should load in vanilla.

Adamant uses two pinned references:

| Reference | Version | Used for |
|---|---|---|
| Java Edition sources ([MCP-Reborn](https://github.com/Hexeption/MCP-Reborn)) | **26.3** | Gameplay and generation: terrain, features, structures, mob AI, blocks, items, survival logic |
| Bedrock Dedicated Server | **1.26.52.3** (build 51798919) | Wire protocol, block and item registries, biome definitions, NBT and world format |

Exact revisions and checksums are recorded in [`vanilla-reference.json`](vanilla-reference.json).

These rules follow from that:

- **Port behaviour, don't imitate it.** Logic is translated from the reference sources, including their random-number streams, tick ordering and edge cases, instead of being reimplemented to look similar.
- **Prove it against the reference.** Behaviour is checked against executed Java fixtures, captures from the pinned BDS and regression tests. Values produced by the implementation under test are never taken as the expected result. [`docs/verification.md`](docs/verification.md) records each check and what it does and does not establish.
- **Stay native on the wire and on disk.** Clients see exactly what BDS would send, and worlds are saved in the native Bedrock LevelDB format with BDS's NBT layout.
- **Keep it vanilla.** Adamant implements the game itself. Data packs and scripting add-ons are out of scope.

## Status

| Area | Status | Notes |
|---|:---:|---|
| Protocol and transport | ✅ | Protocol 2193 via gophertunnel v1.62.0; NetherNet by default, RakNet optional |
| Registries | ✅ | All 2,076 items, 22,079 block states (1,477 blocks) and 89 biomes match BDS |
| Terrain generation | 🟢 | Java 26.3 noise, climate, biomes and aquifers in the Overworld, Nether and End |
| Features and structures | 🟢 | Configured features and nearly all structures; see [World generation](#world-generation) |
| Mob AI | 🟢 | Java's goal and brain AI drives every mob species; mobs ride and steer mobs |
| Redstone | 🟢 | Shared circuit timing checked against BDS; [audit coverage and remaining gaps](docs/redstone-audit.md) |
| Blocks and items | 🟡 | Most mechanics ported; some registered states are still placeholders |
| Commands | 🟢 | Java 26.3 command system with selectors, `execute` and scoreboards |
| Persistence | ✅ | Native LevelDB worlds, BDS NBT, background chunk saving |
| Natural spawning and ecology | 🟡 | Spawn tables and patrols in place; the full spawn cycle is still being verified |

✅ complete · 🟢 ported, gaps remain · 🟡 partial

## What's implemented

### Networking and server

- Bedrock protocol 2193 (client 1.26.52) on gophertunnel v1.62.0.
- Native NetherNet transport (HTTP signalling and WebRTC), with RakNet available for older or custom clients.
- A persistent P-384 server identity that survives restarts.
- An admin console and server CLI, a Nukkit-style whitelist and RakNet Query.
- Concurrency and input hardening throughout sessions, inventories, containers and packet validation.

### World generation

- **Generation kernel:** a port of Java 26.3's random streams, noise, terrain splines, climate lookup, biome zoom, aquifers, carvers, material rules and feature placement. These are checked against the executed Java runtime.
- **Features:** ores, trees and saplings, vegetation, lakes, springs, geodes, dripstone, basalt deltas, fungi, fallen trees, multiface growth and more. Saplings, fungi and bone meal grow the same configured features in the live world.
- **Overworld structures:**
  - strongholds and mineshafts
  - desert pyramids, jungle temples, swamp huts and igloos
  - shipwrecks, ocean ruins and ocean monuments
  - woodland mansions, buried treasure and ruined portals
  - jigsaw structures: villages, pillager outposts, ancient cities, trail ruins and trial chambers
- **Nether:** fortresses, bastion remnants, nether fossils and ruined portals.
- **The End:** the main island, outer islands, end cities, gateways and the full dragon fight, including respawning.
- **Structure state:** loot, spawner flags and structure mobs (with their Java spawn initialisation) survive saves as Java keeps them.
- **Asynchronous generator:** generation runs on parallel worker goroutines. It has `fast-cores` and `many-cores` profiles and prioritises the chunks nearest each player. On a 16-thread desktop CPU it generates 64–100 Overworld chunks per second and stays at 20 TPS during fast flight.

### Mobs

- **AI frameworks:** Java's two mob AI frameworks are ported: the goal and target selectors (`MobAI`) and the memory-and-activity `Brain`, with its sensors and behaviours. Navigation, pathfinding, move/look/jump controls and body rotation follow Java as well.
- **Brain-driven mobs:** villagers, wardens, piglins and piglin brutes, hoglins and zoglins, breezes, creakings, copper golems, allays, sniffers, camels, axolotls, frogs, tadpoles, goats, armadillos and baby happy ghasts.
- **Goal-driven mobs:** every other mob runs its Java goals. This covers zombies and their variants, skeletons, creepers, spiders, endermen, slimes, blazes, ghasts, illagers and ravagers, witches, guardians, drowned, phantoms, shulkers and the wither. It also covers farm animals, horses and llamas, wolves, cats, foxes, bees, parrots, pandas, polar bears, dolphins, turtles, fish, squids, bats, striders, golems and wandering traders.
- **Villagers:** professions taken from job sites, daily schedules, sleeping in beds, farming and bread making, trading and restocking, gossip, breeding, panic, iron golem summoning and raid behaviour.
- **Riding:** a mob riding another mob steers it with its own goals, as Java's `getNavigation` and `getMoveControl` do. This covers chicken jockeys, spider and strider jockeys, drowned on zombie nautili, zombie horsemen, ravager riders in raids and baby piglins riding baby hoglins.
- **Encounters:**
  - Raids, pillager patrols and wandering traders with their llamas.
  - Warden anger, sonic booms, digging and emerging.
  - Creaking hearts.
  - Trial spawners and vaults.
- **Breeding, taming, panic, tempting, item pickup, mob conversions and the death and loot pipeline**, including entity cramming and fall damage.

### Blocks and mechanics

- **Redstone:** repeaters, comparators, observers, pistons, droppers, dispensers, the crafter, rails, buttons, plates, tripwires, bells, lamps, doors and trapdoors. Native Bedrock timing checks and remaining differences are recorded in the [redstone audit](docs/redstone-audit.md).
- **Sculk:** sensors (including calibrated sensors), shriekers and catalysts, with vibration selection, occlusion and persistence.
- **Other blocks:** conduits, cauldrons, powder snow and freezing, pointed dripstone, amethyst and dripleaves, lightning rods, suspicious sand and gravel, scaffolding, plant and crop growth.
- **Structure tools:** structure blocks, jigsaws and Bedrock `.mcstructure` templates.
- **Fluids and ticks:** Java fluid identity, and scheduled block and fluid ticks saved as native BDS records.

### Items, crafting and loot

- Every BDS recipe from the 1.26.52 crafting data:
  - shaped, shapeless and user-data shapeless recipes
  - smithing transforms
  - crafter crafting
  - the `limited_crafting` game rule
- Java loot tables resolved onto Bedrock items, with independent loot random streams that survive restarts.
- The 26.3 enchantment set with Java's selection rules, plus Trial Chambers effects and potions.
- Spears and other Java 26.x combat items.
- Maps with native persistence, scaling and locking.

### Worlds and rules

- The Java 26.3 game rule registry and its subsystems, including spawning, drops, portals, precipitation and world borders.
- Nether portals found through the POI index, and End transit with Java's player and non-player transitions.
- Weather and lightning.
- Scoreboards and operators.
- Background chunk saving, saved seeds that are authoritative, and entity and generator state that survive restarts.

### Commands

- **Java 26.3 command system:** coordinates, target selectors, `execute` with all its subcommands (`as`, `at`, `in`, `on`, `positioned`, `rotated`, `facing`, `align`, `anchored`, `summon`, `if`/`unless`, `store`), scoreboards, and `data`, `give`, `tp`, `kill`, `effect`, `xp`, `tag`, `place`, `rotate`, `spectate`, `clear`, `tick`.
- **Server administration:** `help`, `list`, `kick`, `gamemode`, `time`, `status`, `gc`, `whitelist` and `stop`.

## Known gaps

Parity work is ongoing. These are the main known differences from vanilla:

- Some registered block states are still `unknownBlock` placeholders, and some implemented blocks have incomplete behaviour.
- The full natural-spawning cycle and long-run mob ecology have not been verified end to end.
- The skeleton horse trap is simplified, and converting mobs do not yet keep their passengers.
- Lighting is not always recomputed after block edits, items sink in water, and magma blocks do not burn mobs.
- The warden's sonic boom is reduced by Protection.
- How some features render on a real Bedrock client, such as the resource-pack knowledge book, is unverified.

Each claim of parity in this repository is backed by a recorded check; see [`docs/verification.md`](docs/verification.md).

## Getting started

Requirements: **Go 1.26** or newer.

```shell
git clone https://github.com/dm-vev/adamant
cd adamant
go run .
```

Run from the repository root; `config.toml` is read from the current working directory. Stop the server with `Ctrl+C` or the console command `stop`. Use `go run . -headless` to run without the interactive console; termination signals still shut the server down cleanly.

Connect with Bedrock 1.26.52 to the address in `config.toml` (`:19132` by default).

## Configuration

Settings live in `config.toml`.

| Section | Keys |
|---|---|
| `Network` | `Address` (default `:19132`), `Transport` (`nethernet` default, or `raknet`), `IdentityFile` (default `server-key.pem`), `EnableQuery` |
| `Server` | `Name`, `AuthEnabled`, `DisableJoinQuitMessages`, `MuteEmoteChat`, `ReconnectPolicy` |
| `World` | `Seed`, `SaveData`, `Folder`, `GeneratorProfile` (`auto`, `fast-cores`, `many-cores`), `GeneratorWorkers`, `GeneratorQueueSize` |
| `Players` | `MaxCount`, `MaximumChunkRadius`, `SaveData`, `Folder` |
| `Resources` | `AutoBuildPack`, `Folder`, `Required` |
| `Whitelist` | `Enabled`, `File` (default `white-list.txt`), `Reason` |

**Transport.** NetherNet serves HTTP signalling on `Network.Address` over TCP and uses WebRTC over UDP for gameplay. Behind NAT or a firewall, the negotiated ICE connectivity must be allowed; opening only the old RakNet UDP port is not enough. The endpoint is plain HTTP, so terminate TLS at a reverse proxy if you need it. RakNet must be selected explicitly; the server never falls back to it.

**Identity.** `IdentityFile` holds the server's private key. Back it up privately and keep it across restarts. A corrupt key stops startup instead of being replaced.

**Seed.** `World.Seed` applies only when a world is created. After that, the seed in `level.dat` is authoritative.

> [!WARNING]
> Adamant versions before the seed fix wrote a timestamp to `level.dat.RandomSeed`. Back up such worlds and restore their original generation seed before upgrading. See [World seed and saved settings](docs/verification.md#world-seed-and-saved-settings).

**Generator.** `GeneratorProfile = "auto"` picks `many-cores` on hosts with 24 or more logical CPUs. `GeneratorQueueSize` defaults to four times `GeneratorWorkers`. If the logs warn `world generator queue saturated`, raise both values.

**Whitelist.** Enable it under `[Whitelist]`, then manage it with `whitelist add <player>`, `whitelist remove <player>` and `whitelist list`.

**Query.** Query requires `Network.Transport = "raknet"` and `Network.EnableQuery = true`. It is disabled by default and not served over NetherNet.

## Development

```shell
go build ./...
go test ./...
make lint
```

### Repository layout

| Path | Contents |
|---|---|
| `server/world` | World, chunks, ticking, scheduled updates, persistence and the generation API |
| `server/world/generator/advanced` | Overworld, Nether and End generators and their structures |
| `server/internal/vanilla` | Java generation kernel: random streams, noise, splines, climate and features |
| `server/internal/gamedata` | Block, tag and resource tables taken from the references |
| `server/entity` | Entities, the `MobAI` and `Brain` frameworks, and every mob's AI |
| `server/block`, `server/item` | Block and item behaviour |
| `server/player`, `server/session` | Players and the Bedrock network session |
| `server/cmd` | Command system and built-in commands |
| `cmd/` | Tools for comparing against BDS and auditing registries |

### Contribution workflow

- **Reference first.** Every behaviour change starts from the Java 26.3 source or a BDS 1.26.52.3 capture, and includes a reproducible scenario against it.
- **Regression tests.** Fix root causes and add a test that fails without the fix.
- **Honest claims.** Record what a check establishes, and what it does not, in [`docs/verification.md`](docs/verification.md).
- **Dragonfly conventions.** Adamant keeps Dragonfly's package structure and Go API style; upstream API docs are at [pkg.go.dev](https://pkg.go.dev/github.com/df-mc/dragonfly/server).

The registry comparison harness, which runs the pinned BDS next to Adamant and diffs their registries, is described in [Native registry comparison](docs/verification.md#native-registry-comparison).

To add a language or translate command descriptions, see [Command description localization](docs/localization.md).

## Contributing

Issues and pull requests are welcome. Keep changes focused, and state the edition and version that your reference behaviour comes from. Never replace a reference expectation with a value generated by the implementation under test.

## Acknowledgements

Adamant builds on [Dragonfly](https://github.com/df-mc/dragonfly) by the df-mc maintainers and community, and on [gophertunnel](https://github.com/Sandertv/gophertunnel). Java references come from [MCP-Reborn](https://github.com/Hexeption/MCP-Reborn).

## License

Adamant is released under the [MIT License](LICENSE), inherited from Dragonfly.

*Not an official Minecraft product. Not approved by or associated with Mojang or Microsoft.*
