# Protocol boundary

This document records the plan for separating Adamant's game logic and server from the network protocols and transports they are served over, and how the separation is kept from regressing. It is a plan: each phase states what is done when it is finished, and [verification.md](verification.md) records the checks as they are made.

## Goal

Game logic and the server must not depend on any network protocol or transport. Transports (RakNet, NetherNet, …) and protocols (Bedrock Edition today, Java Edition later) become pluggable: a protocol adapter is written against the game's own types, and the game never learns which adapter a player is connected through. Two rules hold in every phase: Bedrock clients receive byte-identical output, and the module path stays `github.com/df-mc/dragonfly`.

## Layers and directories

- `net/`: protocol and transport libraries (Bedrock protocol 2193, wire, login, text, NetherNet, Java framing, nbt, xbox and others), a separate module, `github.com/dm-vev/adamant/net`. No game code.
- `server/`: the game core: worlds, players, blocks, items, entities, commands and the server loop. It depends on no protocol. Neutral contracts between the game and its clients live in `server/client`, and neutral structured text in `server/text`.
- `protocol/<name>/`: protocol adapters inside the main module. `protocol/bedrock` receives today's `server/session`, `server/netconn`, `server/listener_nethernet.go` and `server/xbox.go`; `protocol/java` comes later. An adapter may import the game and `net/`. The game never imports an adapter, and adapters never import each other.
- `main.go` and `cmd/`: the composition root and tools. They are unrestricted.

Until phase 3 moves them, `server/session` and `server/netconn` are the Bedrock adapter and are exempt from the import rules below; R4 applies to them as adapter code.

## Dependency rules

| Rule | Files | Must not import |
| --- | --- | --- |
| R1: the game knows no protocol | `server/`, except `server/session` and `server/netconn` | anything below `github.com/dm-vev/adamant/net` except `net/nbt`; `server/session`, `server/netconn`; `protocol/` |
| R2: adapters stay apart | `protocol/<a>/` | `protocol/<b>` for any b other than a (the adapter's own subtree is fine) |
| R3: protocol libraries know no game | `net/` | `github.com/df-mc/dragonfly` and anything below it |

`boundary_test.go` in the repository root enforces them. `TestImportBoundary` walks the root module and the `net` module, skipping `.git`, every `testdata` directory and every other module (`fixes/`, `net/interop/`, `scripts/redstone_client/`). It parses the imports of every `.go` file, test files included, without type checking, and matches paths by whole segments. A violation is the pair `<file> <import path>`. `TestBoundaryRules` tests the rule function on its own.

The allowlist, `testdata/boundary/allowlist.txt`, is a ratchet. It lists the violations that remain and starts as exactly the violations that exist today. The test fails for a violation that is not listed, and also for a listed entry that is no longer a violation, so the list stays exact and can only shrink. Entries are only ever removed; adding one needs a justification in review. `go test -run TestImportBoundary -update-boundary .` rewrites the list to the current violations. The goal of phase 5 is an empty list.

## Adapters keep off the world

| Rule | Files | Must not |
| --- | --- | --- |
| R4: adapters do not change the world | adapter code: `protocol/`, and until phase 3 moves them `server/session` and `server/netconn`; test files excepted | change the state of the game or set off its effects (sounds, particles, animations, chat broadcasts): mutations go through `client.Controllable` and `server/menu` |

An adapter's client asks the game: through `client.Controllable`, through the operations of `server/menu`, and by submitting forms and dialogues. Effects count as mutations, so that every adapter does not have to set them off again: the game shows its viewers what they see.

`mutation_test.go` in the repository root enforces it. `TestAdaptersKeepOffTheWorld` loads the adapter packages with `golang.org/x/tools/go/packages`, type-checked, and resolves every selector of their files that are not tests, so that a method value counts as a use as much as a call does. A use breaks R4 if it is

1. a method of `*world.Tx` or `*world.World` that is not one of the reads the test lists (`worldReads`). The list holds reads, the scheduling of transactions and `Viewers`/`ReleaseViewers`; it fails closed, so a new method of either is forbidden until it is listed;
2. a function or method declared below `server/`, outside `server/client`, `server/menu` and the adapters, that takes a `*world.Tx`, other than the reads and entry points of `txEntries`: `(*world.EntityHandle).Entity`, every method of `*world.Loader`, `(block.Container).Inventory`, `(form.Form).SubmitJSON`, `(dialogue.Dialogue).Submit` and `(dialogue.Dialogue).Close`;
3. one of the state changers that take no transaction (`mutators`): the setters of `*inventory.Inventory` and `*inventory.Armour` and their `Close`, `(*world.EntityHandle).Close`, the writes and subscriptions of `*chat.Chat`, the trading of merchants, the inputs of vehicles, `(block.Crafter).NotifySlotState` and the experience and enchantment seed setters of `*player.Player`;
4. a method of an interface declared in an adapter that takes a `*world.Tx` or bears the name of one of those state changers (or `AddViewer`, `RemoveViewer`, `ResetExperience`, `UpdateCommandBlock`, `RideJump`): an interface the adapter asserts a value of the game to. Interfaces only the adapter implements, as the handlers of its packets, are left out (`ownInterfaces`);
5. a `View` method of `world.Viewer` called on a viewer: an effect the adapter shows other viewers, where the game should.

The methods of `client.Controllable` are allowed by construction, which is why the contract is to lose its raw setters. Reflection, `unsafe` and game state changed through an unlisted method without a transaction are not caught; review covers them. `TestMutationRules` tests the rules on a fixture of adapter code type-checked against the game and on the paths of adapter code.

The ratchet, `testdata/boundary/mutations.txt`, lists the uses that remain as `<file> <callee>` lines, one for each use, compared as a multiset. It works as the allowlist does: the test fails for a use that is not listed and for a listed entry that is no longer used, and `go test -run TestAdaptersKeepOffTheWorld -update-mutations .` rewrites it. It started with the 67 uses of the Bedrock session; phase 4 removes them, and from then on the test guards an empty list. Phase 3's move renames the files of its entries.

## The one neutral library

The game may import `net/nbt` (and the packages below it). It reads and writes NBT for the disk (little-endian) and the wire (big-endian and little-endian variants) and knows nothing of any protocol, so worlds, items and entities use it directly. It is the only package of `net/` the game may import.

## Decisions taken

- **Adapters live in `protocol/`**, one directory per protocol, inside the main module.
- **Text is structured in the game.** The game models text as structured text (`server/text`) and each adapter renders it: the Bedrock adapter to § formatting codes, the Java Edition adapter to text components. Strings that already carry § codes are read with their Bedrock meanings.
- **The module path stays `github.com/df-mc/dragonfly`.**
- **The key of the whitelist and of operators stays as it is today until Java Edition logins exist.** Today the key is the player name: the whitelist (`server/whitelist.go`), the operators list (`server/operators.go`) and the composition of the two (`server/whitelist_operators.go`) all look up the login's `DisplayName`, trimmed and lower-cased (`normalizeName`), and persist names only, not XUIDs or UUIDs. The admin store's bans match the address or the same `DisplayName`.
- **Identities carry platform IDs, such as the XUID, as optional attributes.** The neutral identity replacing `login.Identity` keeps the name as its key and holds such IDs beside it, so a later Java Edition login can add its own.
- **Bedrock output stays byte-identical**, guarded by golden packet snapshots in `server/session/testdata`, which are checked in every phase.

## Phases

0. **Safety net.** This boundary test; golden snapshots of Bedrock output; the structured-text foundation (`server/text`, with renderers in `protocol/bedrock/text` and `protocol/java/text`). Done: the test passes with today's violations allowed, the snapshots pass, and the renderers have tests.
1. **Client contract.** `server/client` holds the `Client` interface (the methods `Player` calls on its client: `world.Viewer` and 88 of its own, in groups) and the `Controllable` interface. `Player` holds a `client.Client` instead of a `*session.Session`; the `session.Nop` checks are replaced. Bedrock types leave the contract (`login.ClientData`, the container type byte, the boss bar colour byte, the dimension `int32`), `Diagnostics` and `NetStats` become neutral, text in the contract is structured, and a recording fake client serves tests. Done: `server/player` imports neither `server/session` nor `net/`.

   Phase 1 is done: nothing under `server/player`, tests included, imports `server/session`, `server/netconn` or `net/`, and the allowlist holds no `server/player` entry. `*session.Session` implements `client.Client`; a player without a client uses `client.Nop`, and `client/clienttest.Recorder` records what a player tells its client. `server/session` keeps `Controllable = client.Controllable` and `Diagnostics = client.Diagnostics` as aliases until phase 3 moves it, so its handlers and the handlers of plugins keep their signatures. Left for later phases:

   - Two duties of the client are game logic, marked "Transitional" on the contract, and move into the game in phase 4: `Client.Close` removes the player from its world and closes its entity handle, and the client that `Client.HandleInventories` hands the inventories to shows the viewers of the player the items it holds and the armour it wears.
   - `UnlockRecipes` keeps the recipes the adapter can map, so the unlocks a player keeps depend on its adapter (phases 4 and 5).
   - `StopSound` and `SpawnParticleIdentifier` take Bedrock sound names and particle identifiers (phase 5).
   - Forms and dialogues, and the name and score tags of entities, stay legacy strings, as items and signs hold them; only the text of the contract is structured.
   - `session.Nop` is still exported; passed as a `Config.Client` it would count as a client. It is unexported in phase 3.
2. **Neutral join.** `server.Listener` yields a join request (identity, profile, start, reject) instead of a `session.Conn`. The server hands over neutral spawn data, and the Bedrock adapter builds StartGame, the item and block entries, the dimension definitions and the game rules itself. A neutral identity replaces `login.Identity` in the whitelist, operators, login policy and admin store, and the global session list becomes a server-owned player registry. Done: the `server` package core imports no Bedrock package.
3. **Move Bedrock out.** Session, netconn, `listener_nethernet.go`, `xbox.go` and the Bedrock parts of status/query and the pack builder move to `protocol/bedrock`. `server.Config` loses its Bedrock fields; chunk network encoding, `ItemRuntimeID` and the biome definitions move to the adapter; the status shown to Java clients comes from the core. Done: nothing under `server/` imports `net/bedrock` or `protocol/`.
4. **Game logic out of the session.** A `server/menu` package, ported from the Java container menus (enchanting, anvil, grindstone, smithing, loom, stonecutter, beacon, merchant, crafting, crafter). Block-actor edits (signs, command, structure and jigsaw blocks, books, beacons) become `Player` methods with the validation in the game. Adapters stop mutating the world directly. Done: no world mutation in adapters, and the snapshots are unchanged.
5. **Remaining leaks.** Input locks, particles, structure enums, command parameter flags, versions and level.dat constants become game-owned. Done: the allowlist is empty.
6. **Proof.** A minimal Java Edition adapter (login, configuration, play, chunks, movement, chat) with no change to the game beyond neutral types. Done: a Java client joins, sees the world, moves and chats, while the boundary test still passes with an empty allowlist.
