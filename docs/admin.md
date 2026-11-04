# Administration: permissions, bans, backups, remote admin, audit

How to run a server's administration: who may run which command and do what in the world, who may join, how the
world is backed up and restored, how to administer it over HTTP, and what is recorded of all of it. Everything here
is set in `config.toml`, under `[Permissions]`, `[Bans]`, `[Backup]`, `[Audit]`, `[RemoteAdmin]` and `[Whitelist]`.

The lists are keyed as `[Server] IdentityKey` says: by Xbox account (`xuid`) with sign-in on, by name otherwise. A
player in a command is a name or `xuid:<XUID>`. See [`identity.md`](identity.md) for how names resolve to accounts.

## Permission levels

A command needs a **level**, as Java Edition numbers them. A source runs a command if its level is at least the
command's; a source below it is told `Unknown command`, as Java tells it, and the client of a player does not list
the command.

| Level | Name | Commands (Java 26.3's levels; Adamant's own commands at the level of their nearest Java relative) |
|---|---|---|
| 0 | all | `about`, `help`, `list`, `me`, `msg`, `random`, `status`, `teammsg`, `trigger` |
| 1 | moderator | none by default |
| 2 | game master | `attribute`, `bossbar`, `clear`, `clone`, `damage`, `data`, `defaultgamemode`, `difficulty`, `effect`, `enchant`, `execute`, `experience`, `fill`, `fillbiome`, `forceload`, `gamemode`, `gamerule`, `give`, `item`, `kill`, `locate`, `loot`, `particle`, `place`, `playsound`, `recipe`, `return`, `ride`, `rotate`, `say`, `scoreboard`, `seed`, `setblock`, `setworldspawn`, `spawnpoint`, `spectate`, `spreadplayers`, `stopsound`, `summon`, `tag`, `team`, `tellraw`, `time`, `title`, `tp`, `version`, `weather`, `worldborder` |
| 3 | admin | `ban`, `ban-ip`, `banlist`, `deop`, `kick`, `netstat`, `op`, `pardon`, `pardon-ip`, `permission`, `setidletimeout`, `tempban`, `tempban-ip`, `tick`, `transfer`, `whitelist` |
| 4 | owner | `backup`, `gc`, `save`, `save-all`, `save-off`, `save-on`, `stop` |

Game-master blocks (command blocks, structure blocks, jigsaw blocks, command-block minecarts) are edited in creative
from level 2 on, as Java's `canUseGameMasterBlocks`.

### Who has which level

| Source | Level |
|---|---|
| A player in `ops.json` | its entry's `level`, or `[Permissions] OperatorLevel` (4) if the entry has none |
| Any other player | `[Permissions] DefaultLevel` (0) |
| The console | 4 |
| A command block, a command-block minecart | `[Permissions] CommandBlockLevel` (2) |
| A client of the remote admin API | the level of its token |
| `/execute as …`, `execute store` | the level of the source that ran it |
| Code that embeds the server, with a source of its own | 4, unless the source implements `cmd.Leveled` |

`[Permissions.Commands]` sets the level of any command by its name or an alias, such as `kick = 2` or `version = 0`. A
name no command registered is a warning naming the closest command as the server starts, and stops the start with
`[Server] StrictConfig = true`.

### Operators and `ops.json`

`ops.json` is Java's file, keyed as the other lists are:

```json
[
  "alex",
  {"xuid": "2535412345678901", "name": "Steve"},
  {"xuid": "2535498765432109", "name": "Mod", "level": 3},
  {"name": "builder", "level": 2, "bypassesPlayerLimit": true}
]
```

- An entry without `level` follows `OperatorLevel`: lowering the key lowers every such operator at once. A level is
  written only when set, by `/op <player> <level>` or by hand.
- `bypassesPlayerLimit` lets the operator join a full server (`[Players] MaxCount`). The check of a full server runs
  once a client logged in and was allowed, so that the server knows who it is.
- An operator of any level skips the whitelist, and is exempt from the anti-cheat with `ExemptOperators = true`.

Commands:

| Command | Does |
|---|---|
| `/op <player>` | makes an operator of `OperatorLevel`, with no level of its own |
| `/op <player> <level>` | makes an operator of that level, or changes an operator's level ("Steve is now at level 2.") |
| `/deop <player>` | takes an operator off `ops.json` |
| `/permission list` | the operators with their levels, and the roles of `permissions.json` |
| `/permission reload` | reads `ops.json` and `permissions.json` again and refreshes every player online |

No one gives a level above their own, or changes or removes an operator above their own level: a level-3 admin
makes no level-4 operator.

`config.toml` is read only at start. Edit `ops.json` or `permissions.json` by hand, then `/permission reload`.

## Roles

Beside the level of its commands, a player has a **role**, as Bedrock Dedicated Server's `permissions.json` gives
it. The role decides what the player may do in the world, and the server holds it to that whatever its client shows:
a refused action is silent, and the block, slot or entity is shown again as it is.

| Role | Build, mine | Doors and switches | Open containers | Attack players, mobs | Operator commands, teleport (client bits) | Client shown |
|---|---|---|---|---|---|---|
| `visitor` | – | – | – | – | – | Visitor |
| `member` | yes | yes | yes | yes | – | Member |
| operator (in `ops.json`) | yes | yes | yes | yes | from level 2 | Operator |
| `custom` | as listed | as listed | as listed | as listed | as listed (the commands still follow the level) | Custom |

A player's role is its entry in `permissions.json`, if any; else operator, for an operator; else `[Permissions]
DefaultRole` (`"member"`, or `"visitor"`). An operator whose role is set explicitly keeps the commands of its level.

The client is shown the role as its permission level (StartGame and the abilities), the command permission level
`Any` at level 0 and `GameDirectors` from level 1 on (as BDS shows its operators), and the ability bits of its game
mode as far as its role allows them.

`permissions.json`, beside `ops.json`, is BDS's file:

```json
[
  {"permission": "visitor", "xuid": "2535412345678901"},
  {"permission": "custom", "xuid": "2535498765432109", "abilities": ["build", "mine", "doors_and_switches"]},
  {"permission": "member", "name": "guest"}
]
```

The abilities are `build`, `mine`, `doors_and_switches`, `open_containers`, `attack_players`, `attack_mobs`,
`operator_commands` and `teleport`. BDS's `"operator"` entries move to `ops.json` at start, the file before kept as
`permissions.json.before-roles`.

Roles are changed with:

- **`/permission set <player> <visitor|member|custom|default> [abilities...]`** (level 3): `default` takes the
  entry off, so that the player has the role of an operator or `DefaultRole` again.
- **The player list of the game**, as on BDS: an operator of `[Permissions] RoleLevel` (3) or more may change the
  permission of a player below its own level. Operator is `/op` without a level (from level 3, and only if
  `OperatorLevel` is not above the asker's); visitor, member and custom are written to `permissions.json`, and an
  operator given one of them is taken off `ops.json`, as BDS's list does. A request not granted shows the player
  again as it is. Each request is recorded in the audit log.

## Files

All of them are beside the whitelist (`[Whitelist] File`, `white-list.txt` by default), unless a key gives an
absolute path.

| File | What | Written |
|---|---|---|
| `white-list.txt` | the whitelist: `xuid:<n> <name>` or a name, a line each | `/whitelist` |
| `ops.json` | operators, with `level` and `bypassesPlayerLimit` | `/op`, `/deop` |
| `permissions.json` | roles, BDS's format | `/permission set`, the player list |
| `banned-players.json` | bans of players (`players`) and of addresses (`ips`) | the ban commands, expiries |
| `known-players.json` | the accounts a server keyed by XUID has seen, by name | at each login |
| `audit.log`, `audit-<time>.log` | the audit log and its rotated files | always, if `[Audit] Enabled` |
| `admin-tokens.txt` | the tokens of the remote admin API, mode 0600 | by hand; made at the first start that serves the API |
| `*.before-xuid`, `permissions.json.before-roles` | copies made once by the migrations | once |

Each list is written atomically: a temporary file, fsync, rename, then fsync of the folder.

## Bans

| Command (level 3) | Output |
|---|---|
| `/ban <player> [reason]` | `Banned Griefer (xuid:…): griefing`; over a ban for a time, `… is now banned for ever` |
| `/tempban <player> <duration> [reason]` | `Banned Griefer until 2026-11-11 at 14:20:31 UTC: griefing`; over a ban, `The ban of Griefer now ends on …` |
| `/ban-ip <address\|network\|player> [reason]` | `Banned IP 203.0.113.7: alts`, then `This ban affects 2 player(s): Alt1, Alt2` |
| `/tempban-ip <address\|network\|player> <duration> [reason]` | as `/ban-ip`, with `until …` |
| `/pardon <player>`, `/pardon-ip <address\|network>` | `Unbanned Griefer`, `Unbanned IP 203.0.113.7` |
| `/banlist [players\|ips]` | `There are 3 ban(s):`, then `Griefer (xuid:…) was banned by Steve (xuid:…): griefing (until …)` |

- **Durations** are Go's units plus `d` and `w`, in any combination: `90m`, `12h`, `7d`, `1w2d`, `1d12h30m`; a minute
  at least, and at most `[Bans] MaxDuration` (`"0s"`: no cap).
- **Expiry** is by the clock: an expired ban stops applying at once, and is taken off the file at the next login it
  would have refused, at `/banlist`, hourly and at start. Each expiry is in the audit log.
- **Players not online.** A name seen before bans the account last seen with it; on a server keyed by XUID, a name
  never seen is held and binds to the first account that joins with it, which is refused, keeping the ban's expiry.
- **Addresses.** An IPv4 address bans itself; an IPv6 address bans its network of `[Bans] IPv6Prefix` bits (64: one
  subscriber); a typed network, such as `203.0.113.0/24`, bans the network. A loopback address is banned only by a
  ban of it alone, never by a network. `/ban-ip <player>` bans the address the player is online from.
- **Proxies and relays.** A player of an Adamant proxy is banned by the address the proxy signed into its token, the
  client's, not the proxy's: a ban of the proxy's own address refuses no one. NetherNet over TURN gives the client's
  public address. On a LAN the client's private address is what is banned.
- **Texts.** A banned player is refused at login, and kicked if online, with Java's texts, which `[Bans]` sets:

  | Key | Default |
  |---|---|
  | `Message` | `You are banned from this server.` |
  | `IPMessage` | `Your IP address is banned from this server.` |
  | `ReasonMessage` | `\nReason: {reason}` (after the message, if there is a reason or a `DefaultReason`) |
  | `DefaultReason` | `Banned by an operator.` |
  | `ExpiresMessage` | `\nYour ban will be removed on {expires}` (if the ban expires) |
  | `TimeFormat` | `2006-01-02 at 15:04:05 MST`, a layout of Go's package `time`, in the server's time zone |

  Each text may hold `{reason}`, `{source}`, `{created}` and `{expires}`. A `config.toml` written before these
  defaults keeps the one-line texts it sets.

`/ban` does not deop: a banned operator stays in `ops.json` and is refused, since bans are checked first.

## Whitelist

`[Whitelist] Enabled` turns it on. `Enforce = true` (Java's `enforce-whitelist`) kicks the players online it does not
hold, operators excepted, as it turns on (`/whitelist on`) or reloads (`/whitelist reload`). Off by default.

## Backups

A backup is a copy of the world made while the server runs, with no world waiting for it: the database is copied
with its tables hard-linked and its journals copied up to their length, so that the copy holds the world as a crash
at that instant would leave it, every checkpoint whole or absent. Each copy is verified before it counts.

### Settings

| Key of `[Backup]` | Default | What |
|---|---|---|
| `Enabled` | `true` | makes a backup every `Interval`; `/backup` works either way |
| `Folder` | `"backups"` | where backups go; it must not lie inside the world folder |
| `Format` | `"folder"` | `"folder"`: a world that opens as it is, its unchanged tables shared by hard links with the world and the other backups (same disk); `"zip"`: a `.mcworld` archive |
| `Interval` | `"6h"` | from the end of one backup to the start of the next; the first is due one interval after start; `"0s"` schedules none |
| `Keep` | `10` | how many backups are kept |
| `MaxAge` | `"0s"` | backups older than this are deleted (`"0s"`: none for its age); the newest is never deleted |
| `MinFreeSpace` | `"1GiB"` | a backup that would leave less free on the disk of `Folder` is refused |
| `SaveFirst` | `true` | saves a checkpoint first, so that the backup holds the world as of that moment |
| `Verify` | `"record"` | `"record"` opens the copy and reads its checkpoint; `"full"` also reads every key; `"off"` does not verify |

A scheduled backup is skipped while saving is off (`/save-off`); `/backup` makes one anyway. Retention runs after
each backup, over the folders and archives that hold a readable `backup.json`: files the server did not write are
never deleted.

### Layout

```
backups/
  2026-11-04T14-00-00Z-autosave/
    backup.json        name, time, reason, who, format, the checkpoint it holds, files, bytes, verification
    world/             level.dat, level.dat_old, levelname.txt, db/…, packs
    admin/             ops.json, permissions.json, banned-players.json, white-list.txt, known-players.json
  2026-11-04T20-00-00Z-manual.mcworld   (Format = "zip": the world at the root, adamant-backup.json, adamant-admin/)
  .hold/               the copy /save hold prepares (not a backup)
```

> [!WARNING]
> A `folder` backup shares the inodes of the world's tables. Nothing writes a table in place, but a tool that edits
> or `chmod`s files under `backups/` changes the live world's too: treat the folder as read-only. A `folder` backup is
> on the same disk as the world: it protects against griefing, corruption and bad updates, not against losing the
> disk. Sync `backups/` off the host, or use `Format = "zip"` with `Folder` on another disk.

### Commands

| Command (level 4) | Does |
|---|---|
| `/backup` | `Backing up...`, then, once done: `Backup 2026-11-04T14-00-00Z-manual done: 2.0 GiB, 1043 files (1012 linked), 0.19 s, checkpoint 1312, verified (record).` |
| `/backup list` | the backups, newest first |
| `/backup status` | whether one runs, the last, the last failure, the next due |
| `/backup verify <name> [full]` | verifies a backup, by its record or reading every key |
| `/save hold` | BDS's: prepares a copy of the world in `<Folder>/.hold/<world>/` and answers `Saving...` at once |
| `/save query` | BDS's: `A previous save has not been completed.` while it is prepared; then a line naming the hold folder, `Data saved. Files are now ready to be copied.` and the files, `world/db/000005.ldb:2097152, world/level.dat:1234, …` |
| `/save resume` | BDS's: deletes the hold folder, `Changes to the world are resumed.` |

`/save hold` is for hosting panels that back a server up through its console. Unlike BDS, the files to copy are not
those of the live world folder but of the copy in the hold folder, which does not change: copy them from there, as
the paths are relative to it; the lengths are their sizes. A hold left by a stop is deleted at the next start.

> [!CAUTION]
> `/save-off`, `/save-all`, then a copy of the world folder is **not** a safe backup on this server: the database
> keeps compacting and deleting its files, and quit saves, map IDs, spawn points and structure templates are still
> written. Use `/backup` or `/save hold`.

What a backup holds: the last checkpoint (taken first with `SaveFirst`), plus what was written outside checkpoints
since (the saves of players who quit, map IDs, spawn points, structure templates). See
[Save checkpoint](verification.md#save-checkpoint).

### Restore

This procedure is followed, for a backup of each format, by `TestRestoreProcedure` (`server`): a backup of Alice with
10 diamonds, then a chest of them saved after it, restored to Alice with her 10 diamonds and no chest.

1. **Pick the backup**: `/backup list`, or `ls backups/`. Verify it on the running server: `/backup verify <name>
   full`.
2. **Stop the server** (`/stop`), and wait until it has closed.
3. **Keep the current world**: `mv world world.before-restore`. Do not delete it until the restored server has run.
4. **Copy the backup into place.**
   - `folder`: `cp -a backups/<name>/world ./world`. Copy rather than move, so that the backup stays.
   - `zip`: `mkdir world && cd world && unzip ../backups/<name>.mcworld -x 'adamant-*'`.
5. **Optionally restore the lists**, from `backups/<name>/admin/` (or `adamant-admin/` in the archive). Without
   this, the bans, operators and whitelist changes made since the backup stay, which is usually what is wanted: a
   rollback is of the world, not of the moderation.
6. **The players.** With the world and the players saved (`SaveData` in both tables, as by default), the players
   are in the world's database and roll back with it: no player keeps items the world lost. An old `players/`
   folder is read only for players without a record in the database.
7. **Start.** The log says `Opened the world at its last checkpoint.` with the checkpoint of the backup. A warning
   that `level.dat` is older than the checkpoint is harmless: its settings are taken from the checkpoint.

Players online during the backup are back where they were at its checkpoint.

## Remote admin

An HTTP/JSON API to run admin commands and read the lists, for panels and scripts. It is **off by default**, and
listens on its own address, apart from `[Monitoring]`: monitoring is read-only and often exposed to a scraper's
network, the API is full control.

### Settings

| Key of `[RemoteAdmin]` | Default | What |
|---|---|---|
| `Enabled` | `false` | serves the API |
| `Address` | `"localhost:19140"` | where it listens; off this machine (`localhost`, `127.0.0.0/8`, `::1`; an empty host is not) `CertFile` and `KeyFile` are needed |
| `CertFile`, `KeyFile` | `""` | the certificate and key of TLS 1.2 or newer, in PEM: both, or neither; loaded as the file is read |
| `AllowPlaintext` | `false` | serves without TLS off this machine anyway, warning at every start: tokens cross the network in clear |
| `AllowedHosts` | `[]` | host names a request may name in `Host`, besides the host of `Address` and the loopback names; others get 421 |
| `CommandTimeout` | `"10s"` | how long a request waits for the output of its command, 1s to 5m |
| `MaxFailures` | `5` | failed sign-ins an address gets a minute before it is refused for a minute |
| `TokenFile` | `"admin-tokens.txt"` | the tokens, beside `ops.json` if relative |
| `TrustedProxies` | `[]` | addresses and networks whose `X-Forwarded-For` is believed |

### Tokens

`admin-tokens.txt` holds one token a line, `<name> <level> <token>`, with `#` comments:

```
# name  level  token
owner   4      <40 letters and digits>
panel   3      <another token of 32 characters or more>
```

- The first start that serves the API makes the file, mode 0600, with one token `owner` of level 4 from
  `crypto/rand`, and logs `Remote admin token written to admin-tokens.txt.` **The token is never logged.**
- A file its group or others may read is warned of as `config.toml` is read, and refused with `StrictConfig`.
- A token under 32 characters, a level out of 0 to 4 or a name twice is an error of the file.
- The file is read again when it changes, checked at most every 5 seconds: add, revoke or rotate tokens without a
  restart.
- Only `Authorization: Bearer <token>` is read: no query parameter, no cookie.

### Endpoints

| Method and path | Level | Answer |
|---|---|---|
| `POST /v1/command` | the token's | `{"command": "ban Griefer griefing", "wait": "10s"}` → `{"output": [{"text": "...", "error": false}], "pending": false}` |
| `GET /v1/status` | 0 | version, players, max players, uptime, records the audit log dropped |
| `GET /v1/players` | 2 | name, XUID, UUID, dimension, level, role; the address for tokens of level 3 or more |
| `GET /v1/operators`, `/v1/permissions`, `/v1/bans`, `/v1/ip-bans`, `/v1/whitelist` | 3 | the entries of the lists |

Every change goes through `/v1/command`, with the same permission check, audit and output as a typed command: the
token's level bounds the command, and one above it answers `Unknown command`, as a player is told. Output a command
sends later (`/backup`, `/save-all`, `/backup verify`) is waited for up to `wait`; `pending: true` says it did not
come. Errors are `{"error": "...", "code": "..."}`: 401, 403 (an endpoint above the token's level), 404, 405, 413 (a
body over 64 KiB), 421 (an unknown `Host`), 429 with `Retry-After`, 503 (more than 8 requests in flight).

```shell
TOKEN=$(awk '$1 == "owner" {print $3}' admin-tokens.txt)
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:19140/v1/status
curl -s -H "Authorization: Bearer $TOKEN" -d '{"command": "backup", "wait": "60s"}' http://localhost:19140/v1/command
```

### Lifecycle and embedding

The API is a service of the server (`server.Config.Services`): `Server.Listen` starts it, and `Server.Close` closes
it once the players left and the worlds closed, before the audit log closes. When `Close` returns, its port is free
and its goroutines ended, so a program that embeds the server may close it and start another in the same process, on
the same address. A program that builds `server.Config` itself passes the API the same way:

```go
conf.Services = append(conf.Services, remoteConf.Service(remoteadmin.ListenOptions{Dir: adminDir, Log: log}))
srv := conf.New()
builtin.Register(srv)
srv.Listen()
// ...
_ = srv.Close() // stops the API too
```

`remoteadmin.UserConfig.Listen` still serves the API apart from the server, for a program that closes the
`Listener` itself. An API that cannot listen is logged (`Service not started: remote admin: ...`), and the server runs
without it.

### Threat model

| Threat | Mitigation |
|---|---|
| A token read from disk (a local user, a copy of the server folder, a support paste) | a file of its own, not `config.toml`, mode 0600, warned of or refused when wider; never logged; one token a client, revoked by editing the file |
| A token sniffed on the network | TLS required off this machine; on localhost by default |
| Brute force | tokens of 238 bits; per address, `MaxFailures` a minute, then every request of the address 429 for a minute without its token checked; 60 failures a minute in all block every address but loopback for a minute; failures audited, summarised once a minute per address |
| A timing oracle | SHA-256 of the token compared with that of every token in constant time, no early exit |
| CSRF and DNS rebinding from a page the admin visits | bearer header only, no cookies, no CORS; a `Host` not of the configured hosts is answered 421 |
| Escalation through commands | the token's level bounds every command; no one gives a level above their own |
| Escalation through command blocks | command blocks run at `CommandBlockLevel` (2) |
| A forged client address | `X-Forwarded-For` believed only from `TrustedProxies`, the right-most address no proxy holds |
| Denial of service | 8 requests in flight, header and body caps, timeouts (read 10 s, write `CommandTimeout` + 5 s); commands run in a world's transaction between ticks; no endpoint scans the world |
| Information disclosure | addresses only to tokens of level 3 or more; `/v1/status` shows what the server list shows |
| An insider | every command, read of a list and failed sign-in audited with the token's name; no endpoint edits the audit log |

There is no RCON: its password crosses the network in clear and it has no levels.

## Audit log

`audit.log`, beside `ops.json`, records who did what, when and where, one JSON object a line:

```json
{"time":"2026-11-04T14:20:31.123Z","actor":{"kind":"player","name":"Steve","key":"xuid:2535498765432109","level":4,"address":"203.0.113.9"},"where":{"dimension":"overworld","pos":[12.5,64,-3.2]},"action":"ban","command":"/ban Griefer griefing","result":"ok","output":["Banned Griefer (xuid:2535412345678901): griefing"]}
```

| Field | Meaning |
|---|---|
| `time` | UTC, to the millisecond |
| `actor` | `kind` (`player`, `console`, `remote`, `command_block`, `server`, `api`), `name`, `key` (`xuid:…` or the name), `level` at the time, `address` (left out with `RecordAddresses = false`), `token` for remote clients |
| `where` | dimension and position, for players and command blocks |
| `action` | the command (aliases resolved), or `role`, `ban-expired`, `read`, `auth-failed`, `audit-dropped` |
| `command` | the line as typed; for `role`, what the player list asked |
| `target` | the player or address, for list changes |
| `result` | `ok`, `error`, `denied` (level too low) or `refused` (a rule, such as a level above one's own) |
| `output` | the messages and errors, 4 KiB at most |

What is recorded:

- every command whose level is `[Audit] MinLevel` (2) or more, from any source, **denied attempts included**: a member
  trying `/op` is recorded;
- every command of a remote client, at any level, its reads of level 3 or more, and failed sign-ins (summarised);
- roles changed from the player list, granted or not;
- bans that expired.

Code that changes the lists through the methods of `server.Server` records what it does with `Server.Audit`; the
commands that call those methods are recorded once, as commands. Scheduled backups are logged (component `save`),
not audited; `/backup` is audited as a command.

**Writing.** No world waits for the log: records go through a buffer of 4,096, and a full buffer drops a record,
counts it, and writes `{"action":"audit-dropped","count":N}` before the next one (the count is also in
`/v1/status`). The file is synced once a second while written to, and at close.

| Key of `[Audit]` | Default | What |
|---|---|---|
| `Enabled` | `true` | writes the log |
| `File` | `"audit.log"` | the file, beside `ops.json` if relative |
| `MaxSize` | `"64MiB"` | rotates the file at this size, to `audit-2026-11-04T142031.123Z.log` |
| `Keep` | `10` | rotated files kept |
| `MinLevel` | `2` | the least level of the commands recorded; 0 records every command |
| `RecordAddresses` | `true` | records the addresses of players and remote clients |

With the game rule `log_admin_commands` on, operators online are shown the feedback of the commands of other
operators, and, with `[Permissions] BroadcastConsoleToOps` and `BroadcastRemoteToOps` (both on, as Java's
defaults), of the console's and the remote clients' as `[Console: …]`.
