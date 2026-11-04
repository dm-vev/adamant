# How the server tells its players apart

The server keys its players, in the whitelist, the operators, the bans and the data it keeps of them, either by
their **Xbox account** or by their **name**. `[Server] IdentityKey` in `config.toml` chooses:

| Value | Players are keyed by | Needs |
|---|---|---|
| `auto` (default) | `xuid` if `AuthEnabled = true`, `name` otherwise | – |
| `xuid` | their Xbox account (XUID) | `AuthEnabled = true`; the start fails otherwise |
| `name` | their name, trimmed and lower-cased | – (with `AuthEnabled = true` the start warns) |

The value is trimmed and read in any case; anything else fails the start, as a typo must not change who is an
operator. The start logs the mode chosen. A `server.Server` built in code with the zero `Config` keys by name;
set `Config.IdentityKey = identity.XUID` for accounts. The package `server/identity` holds the modes, the keys
(`identity.Key`), the normalisation of names and the UUID of a name.

## Keyed by Xbox account (`xuid`)

- **Who is let in.** A login Xbox Live did not vouch for, or with no XUID, is refused with "This server needs an
  Xbox Live account." The server refuses it itself, whatever the listener let in. Xbox Live vouches for a login it
  signed, or for a player a proxy holding the secret of the links says it signed.
- **The lists** hold accounts. A player who renames their gamertag keeps their entries, and whoever takes the old
  name gets none of them. At each login the entries of an account take its current name, and the server logs
  `xuid:2535412345678901 renamed: Steve → Bob.`
- **Names held for an account.** `/op Steve` for a name no account is known by adds an entry held by the name:
  the first Xbox account that joins as Steve gets it (as BDS's `allowlist.json` does), and the server logs
  `Bound the operator entry "steve" to xuid:2535412345678901 (Steve).` A warning lists the operators and the
  whitelist entries still held by name at each start.
- **Data.** The record of a player and its spawn points are keyed by the UUID of the login, which Xbox Live signs
  and which follows from the account: as before.
- **Known players.** `known-players.json`, beside `ops.json`, keeps each account that joined: the name and the
  UUID it last joined with, and when. A name is the account that last joined with it, as a gamertag belongs to
  one account at a time. The commands find accounts by it; the console completes its names.

## Keyed by name (`name`)

- **Who is let in.** Anyone can join under any name, an operator's included: nothing checks who a client is. Use
  it only on a network you trust, or behind a proxy that signs players in. Offline servers should have no
  operators.
- **The lists** hold names, compared trimmed and lower-cased, as they always have; their files keep their bytes.
  An entry of an account, which a server keyed by XUID wrote, matches the name its player last had and is written
  back unchanged.
- **Data.** The record of a player and its spawn points are keyed by the UUID of its name
  (`identity.OfflineUUID`: version 3 of the MD5 of `OfflinePlayer:` and the name lower-cased, Java's offline
  rule), whatever UUID its client sends. A client that sends the UUID of another player does not get that
  player's data, and a client that sends a new UUID at each login keeps its own. The client keeps its UUID for its
  entity and the player list. A record the server kept by the client's UUID before, of the same name, moves to the
  name at the first login, with its spawn point.
- **One name, one player.** A client that logs in under the name of a player online, in any case, is held to the
  reconnect policy: `kick-existing` (the default of `config.toml`) disconnects the player online, `kick-joining` refuses the new
  client. With `kick-existing`, anyone can kick a player by joining under its name; they could already join as
  that player. `kick-joining` keeps the first connected, at the price of making a player whose connection dropped
  wait out the dead session.
- **XUIDs** are what the clients claim. `Player.XUID`, `Server.PlayerByXUID` and the player list show them as
  sent; they match no entry of an account.

## What the admin types

`/op`, `/deop`, `/ban`, `/pardon` and `/whitelist add|remove` take a name or `xuid:<XUID>`. On a server keyed by
XUID, a name added is, in order: the player online with that name; the account that last joined with it; or
else the name, held for the first account that joins with it. A name removed is the account of the player online
with that name (which must be on the list if the list holds another account of that name); the one account of the
list whose player last had the name; the account that last joined with it; or the name held. Several accounts of
one name are an error that names each, with `xuid:`. On a server keyed by name, `xuid:<XUID>` cannot be added,
and names are names. The commands print what they found:

    Made Steve (xuid:2535412345678901) a server operator.
    Steve has not joined this server: the first Xbox account that joins as Steve becomes an operator. Name the
    account with xuid:<XUID> instead.

`/banlist` and `/whitelist list` show `Steve (xuid:2535412345678901)` for an account and `steve (held by name)`
for a held name. Names that begin with `xuid:` or `java:` are refused at login.

## The files

| File | Name entry | Account entry |
|---|---|---|
| `white-list.txt` | `steve` | `xuid:2535412345678901 Steve` |
| `white-list.toml` | `"steve"` in `players` | `"xuid:2535412345678901 Steve"` in `players` |
| `ops.json` | `"steve"` | `{"xuid": "2535412345678901", "name": "Steve"}` |
| `banned-players.json` | `{"name": "steve", "reason": …}` | `{"xuid": "2535412345678901", "name": "Steve", "reason": …}` |
| `known-players.json` | – | `{"xuid": …, "name": …, "uuid": …, "lastSeen": …}` |

The lists are written atomically. A list that holds names alone writes the bytes it always wrote.

**Older servers.** An older server refuses to start with an `ops.json` that holds an account, matches no account
line of the whitelist (it fails closed) and keeps the bans of accounts by the name they hold.

## The first start keyed by XUID

The first start of a server keyed by XUID, which the lack of `known-players.json` tells, moves the names on the
lists to accounts:

1. The known players are seeded from the saved records of the players that hold an XUID. A name two records give
   to two accounts is ambiguous.
2. Each list file that holds a name is kept as `<file>.before-xuid`, unless such a copy exists.
3. A name exactly one account has becomes the entry of that account; an ambiguous name stays held by name, with
   a warning naming the accounts; an unknown name stays held.
4. The lists are written, then `known-players.json`, which marks the move done.

The report reads `Moved 12 of 15 names on the lists to Xbox accounts; 3 stay held by name until they join: …`.
Records written before this version hold no XUID (the provider never saved it), so on a server that upgrades,
the names stay held until each player joins, which binds them. If a server ever ran with `AuthEnabled = false`,
the XUIDs of its records are what clients claimed: review the lists after the move. To roll back, stop the server
and copy the `*.before-xuid` files back.

## Release notes

- On a server keyed by name, records and spawn points are now kept by the UUID of the name. A record kept by the
  client's UUID moves at the first login, if it is of the same name. Entities that remember a player, such as
  tamed pets, villager gossip and liked players of allays, keep the UUID of its client: a client that sent a new
  UUID at each login had pets that already forgot it; one that sent the same keeps them.
- On a server with sign-in on, `auto` now keys the lists by Xbox account, and the first start moves them.
- The operator checks of commands, game master blocks and abilities go by the player online, so an entity of a
  plugin named after an operator is not one.
- `cmd.SetOperatorChecker`, `cmd.IsOperator` and `client.Game.IsOperator` (since replaced by
  `client.Game.PermissionsOf`, the level and role of a player) take a UUID;
  `session.IdentityOf` takes whether Xbox Live vouched for the login; netconn's `Config.Allow` gains it.

## Later work

- A hybrid offline mode that verifies the Xbox tokens it is offered and keys verified players by XUID.
- Looking a gamertag up on Xbox Live for `/op` of a name no account is known by.
- Keys of Java Edition accounts (`java:<uuid>`), which `identity.Parse` reserves.
- Comparing names in one Unicode form (NFC).
