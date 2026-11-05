# Bedrock protocols

Adamant speaks two protocols of Bedrock Edition:

| Protocol | Versions of the game | Notes |
|---|---|---|
| 2193 | 1.26.50, 1.26.51, 1.26.52 | reaches the session through the step in `net/bedrock/proto/convert/c2225` |
| 2225 | 1.26.60, 1.26.61 | the model: the session is compiled against its packets |

Two keys of `[Network]` in `config.toml` choose what a server does with them:

- `Protocols` lists the protocols clients may join with. `"all"` is every protocol this build supports and is the default. A list of versions (`["1.26.50"]`) allows those alone. A client of a protocol that is not allowed is refused.
- `PrimaryProtocol` is the protocol the server speaks: the packets and fields it uses are those of that protocol, and clients of the other protocols get them converted. `"latest"` is the newest of `Protocols` that this build's session speaks, and is the default; an empty value means the same. A version pins the primary.

## The newest protocol is the primary

The defaults, `Protocols = ["all"]` and `PrimaryProtocol = "latest"`, follow the build. A build that adds a newer protocol allows it and speaks it, with no change to `config.toml`. So 1.26.60 is the primary today and 1.26.50 clients join through conversion.

A config written before that names versions. `Protocols = ["1.26.50"]` keeps allowing 1.26.50 alone, and `PrimaryProtocol = "1.26.50"` keeps speaking it, but the clients of newer protocols are refused. The server says so once as it starts: an info line with the protocols allowed and the primary, and a warning for each of

- `Protocols` leaving out a supported protocol newer than every one it lists: set `Protocols = ["all"]`;
- `PrimaryProtocol` pinning a protocol older than the newest allowed one that the build speaks: set `PrimaryProtocol = "latest"`.

With no `Protocols`, a pinned `PrimaryProtocol` is the only protocol allowed, as it was; with neither, both default as above. `StrictConfig` reports the same problems the server refuses to start on (an unknown version, a primary that is not one of `Protocols`, a primary newer than this build's session speaks).

## What clients are told

Whatever shows a version to a client that looks the server up names the primary:

- the status of the NetherNet listener (the server list entry and its message of the day);
- the LAN discovery answer;
- the Xbox Live session properties (the Friends tab);
- the `version` of a query.

When the primary is not the newest protocol allowed (a pinned primary), the server shows the older version and still lets clients of the newer protocols join.

A client that cannot join is refused with a `PlayStatus`: `LoginFailedClient` (outdated client) if its protocol is older than the oldest allowed or between two allowed ones, `LoginFailedServer` (outdated server) if it is newer than every one allowed. The packet holds no version, so the client's own message does the telling.

## Adding a protocol

A build that adds a protocol adds it to `versions.All()` and, once its content is served, to `versions.Configurable()` (`protocol/bedrock/internal/versions`). If the session is compiled against it, `versions.Model()` and `versions.NewestPrimary()` move to it; until the session speaks it as its primary, `"latest"` stays on the newest protocol the session does speak. The goldens of `protocol/bedrock/session/testdata/golden` are recorded for a primary and a client protocol each: the top directory is primary 2193 and clients of 2193, and `primary2225/` holds primary 2225 with clients of 2193 and 2225, the setup of the default config.
