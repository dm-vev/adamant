# Operations: logs, metrics and health

How to watch a running server: what it logs and how, the metrics it serves, and the probes that tell whether it
is live and ready. Everything here is set in `config.toml`, under `[Server] LogLevel`, `[Log]` and `[Monitoring]`.

## Logs

Logs go to **stderr**. Chat goes to **stdout**, as text, in either format, so a pipe of the logs reads stderr alone.

### Levels and components

`[Server] LogLevel` is the level of every component: `"debug"`, `"info"` (the default), `"warn"` or `"error"`, in
any case. `[Log.Levels]` overrides it for a component; an empty value takes `LogLevel`.

| Key of `[Log.Levels]` | Component | What logs through it |
|---|---|---|
| `Server` | `server` | the start and close of the server, the config, the lists of admins, the identity of players |
| `World` | `world` | the ticking of the worlds, their chunks, their data beside the chunks, game code in a world |
| `Generator` | `generator` | the generator workers, their backlog, structures |
| `Save` | `save` | checkpoints, the writes of chunks, the world provider |
| `Session` | `session` | the sessions of players: packets, forms, chunks sent |
| `Network` | `network` | listeners, logins, NetherNet, Xbox Live, LAN, ACME, the proxy, query |
| `Player` | `player` | players |
| `Commands` | `commands` | the console, and the commands logged under the game rule `log_admin_commands` |
| `AntiCheat` | `anticheat` | the anti-cheat |

> [!NOTE]
> Until the parts of the server take the loggers of their components (the commit that moves their call sites is
> still to land), almost every line is of the `server` component: `Server` sets the level of nearly all, and the
> other keys change only the lines already routed: the commands of `log_admin_commands`.

### Formats

`[Log] Format` is `"text"` (the default) or `"json"`.

- **Text** is the lines the server always wrote, byte for byte: `2006/01/02 15:04:05 LEVEL msg k=v`. The component is
  not written.
- **JSON** is one object a line, as Go's `slog.JSONHandler` writes it: `time`, `level`, `msg`, then `component`, then
  the attributes. A stack of a panic is a string, so a panic is one line. In JSON, the lines of Go's `log` package and
  of code that logs through `slog.Default()` are JSON too, of the component `default`.

```json
{"time":"2026-11-04T12:00:00.123456789Z","level":"WARN","msg":"TPS dropped below threshold.","component":"server","dimension":"overworld","tps":18.7}
```

### Rate limit

`[Log] RateLimit = 100` caps the lines of each component at 100 a second, in bursts of 1,000. The lines past it are
dropped, counted in `adamant_log_lines_dropped_total`, and summed up at most every 10 seconds:

```
WARN Log lines dropped over the rate limit. component=world dropped=3900 since=2026-11-04T12:00:00.000Z
```

It is a backstop against floods, such as a corrupt region that logs every block it fails to read, beside the limits
of single lines the server always had (faults, TPS, overload, the generator backlog). `0` turns it off, as for a
session debugged at debug level on a busy server. The default text is the same as before as long as no component
floods.

### Lines that changed

Against the program before `[Log]` and `[Monitoring]`, with the default config:

- the line of what is monitored: `pprof and metrics on localhost:6060` is now
  `INFO Monitoring served. addr=127.0.0.1:6060 paths="/metrics /livez /readyz /healthz /debug/pprof/"`;
- the lines of game code that went to the process default now carry the attributes of their loggers: those of
  blocks and items (missing entity factories, loot tables, the over-fill of a container) a `dimension`, and the line
  of `log_admin_commands` the `name`, `uuid`, `data-uuid` and `raddr` of the player;
- under a flood, the summaries of the rate limit.

## Monitoring

`[Monitoring]` serves, over HTTP on one address:

| Key | Default | Meaning |
|---|---|---|
| `Address` | `"localhost:6060"` | host:port; `""` serves nothing |
| `Health` | `true` | `/livez`, `/readyz`, `/healthz` |
| `Metrics` | `true` | `/metrics`, the text format of Prometheus |
| `Pprof` | `true` | the Go profiles under `/debug/pprof/` |
| `Token` | `""` | if set, `/metrics` and `/debug/pprof/` need `Authorization: Bearer <Token>` |
| `LiveTimeout` | `"2m"` | how long a tick may take before `/livez` fails; more than a minute |
| `ReadyTickAge` | `"5s"` | how long ago a world may have last ticked for `/readyz`; a second or more |

The probes never need the token: kubelets and load balancers probe without credentials, and the bodies name worlds
and listeners, never players. **Profiles off this machine need a token**: an address other than `localhost`,
`127.0.0.0/8` or `::1` (an empty host, as `":6060"`, binds every interface) with `Pprof = true` and no `Token` stops
the server at start, as the profiles show the memory and the command line of the process. Metrics served off this
machine without a token are warned of. The token sits in `config.toml` beside `Proxy.Secret`: keep the file
private.

If the address cannot be listened on, the server logs an error and runs without the monitor. The monitor serves from
before the worlds are opened, so that `/livez` answers while they load, and stops when the server closes, freeing its
port.

### Embedding

The monitor is the package `server/monitor`, so a program that builds `server.Config` itself, instead of running
`main.go`, adds it, and the remote admin API, as services of the server (`server.Config.Services`). `monitor.UserConfig`
is the `[Monitoring]` table, with the keys and defaults of the table above (`monitor.DefaultConfig()`), and its
`Check` is run by `config.toml` reading as for the other tables; a program that sets the fields by hand calls `Check`
itself. The API is `remoteadmin.UserConfig` (see [admin.md](admin.md#lifecycle-and-embedding)).

```go
mon := monitor.DefaultConfig()
mon.Monitoring.Address = "127.0.0.1:6060" // Token, Pprof, ... are fields of the same struct
admin := remoteadmin.DefaultConfig()
admin.RemoteAdmin.Enabled, admin.RemoteAdmin.Address = true, "127.0.0.1:19140"

conf.Services = append(conf.Services,
	mon.Service(monitor.Options{Log: log}),
	admin.Service(remoteadmin.ListenOptions{Dir: adminDir, Log: log}),
)
srv := conf.New()
srv.Listen() // serves both
// ...
_ = srv.Close() // closes both: their ports are free, their goroutines ended
```

`Server.Listen` starts the services before the network listeners, and `Server.Close` closes them last, so the same
program may close the server and start another on the same ports, as `TestEmbedderRestartsServices` does. The
monitor serves as `Listen` starts it, after the worlds loaded. To have `/livez` answer while they load, as `main.go`
does, call `Serve` on the service before `conf.New()`:

```go
svc := mon.Service(monitor.Options{Log: log})
svc.Serve() // /livez passes and /readyz fails as starting, until Listen hands the server over
conf.Services = append(conf.Services, svc)
```

An address that cannot be listened on is logged (`Monitoring not served: ...`) and is no error: the game runs without
the monitor. `Service.Addr` is the address it serves on, nil while it does not. A service may be started again after
`Close`, for another server.

## Health

| Path | Passes when | Fails means |
|---|---|---|
| `/livez` (and `/healthz`) | no world's owner has been busy with a tick, from when the tick was asked for, longer than `LiveTimeout` | the process is wedged: restart it |
| `/readyz` | every check below passes | do not send players here |

The checks of `/readyz`:

- `server/listening`: the server listens and is not closing (`starting` before, `closing` from the start of a close);
- `listener/<i>`: the accept loop of each listener runs;
- `world/<dimension>/ticked`: each world ended a tick within `ReadyTickAge`;
- `world/<dimension>/failed`: no world reported a fatal fault;
- `checkpoint`: while saving is on, the last checkpoint that wrote everything, or the start before the first, is no
  older than two intervals of `SaveInterval` and a minute (11 minutes at the default 5); with `save off` or
  `SaveInterval = "0s"`, it passes.

TPS is not checked: a slow server is still the place of its players. A server closing is not ready, and is live
throughout, so that an orchestrator does not kill it while it saves. The probes wait for no world owner: they answer
most of all when the owners are stuck.

The answers are plain text: `ok` and 200, or 503 and a line for each check, `[+]name` or `[-]name why`; `?verbose`
lists the checks of a pass too.

```
$ curl -s localhost:6060/readyz?verbose
[+]server/listening
[+]listener/0
[+]world/overworld/ticked
[+]world/nether/ticked
[+]world/end/ticked
[+]checkpoint
ok
```

`LiveTimeout` stays above a minute, when the watchdog of each world logs the stacks of all goroutines for a tick
still running (`A tick has run for a long time`), so that the stacks are in the log before a probe fails. An
orchestrator adds its own `failureThreshold × periodSeconds`.

### Kubernetes

The probes must reach the address: bind it to the pod, and set a `Token`, which `Pprof` needs off localhost.

```toml
[Monitoring]
  Address = ":6060"
  Token = "a long random string"
```

```yaml
livenessProbe:
  httpGet: {path: /livez, port: 6060}
  periodSeconds: 10
  failureThreshold: 3
readinessProbe:
  httpGet: {path: /readyz, port: 6060}
  periodSeconds: 10
  failureThreshold: 3
startupProbe:
  httpGet: {path: /livez, port: 6060}
  periodSeconds: 5
  failureThreshold: 60
```

## Memory

Set `GOMEMLIMIT` to about 80% of the memory the process is given (of the container, or of the machine if it has it
to itself): `GOMEMLIMIT=6400MiB` for 8 GiB. Go's collector lets the heap in use grow to twice the live heap
(`GOGC` 100) before it collects, and a server that holds loaded columns has a live heap that is big and changes
slowly. In a load test of up to 100 players in the town and of explorers who load new columns without end, the RSS
of the process ran at 1.6 to 2.1 times its live heap: 602 MiB of live heap, 1.66 GB in use and 1.8 GB of RSS
for 50 players in the town at view distance 32. The collector costs 1 to 10% of the CPU there and pauses less than
a millisecond, so making it work earlier near the limit costs little, and a process that the system kills for
using more than it was given loses what it did not save.

The limit is a soft one: near it the collector runs more often, and a heap that is live and above it is collected
all the time, so size the memory by what is live, and leave the other 20% to what Go does not count (the stacks,
the buffers of the network and of the world's database).

What is live is about 150 to 163 KiB for each loaded column at view distance 32: the blocks of it (44 KiB), the
chunk payloads kept for the next player who sees it (32 to 34), the scheduled ticks of the leaves its generation
left (32), what the generator keeps and the light, and the players themselves, about 1.5 MiB each. The loaded
columns are what the views of the players cover together (`adamant_world_chunks_loaded`): the players of one town at
view 32 share about 3,700 of them, 600 MiB; every player who explores alone loads about 3,200 more. A smaller view
distance gives each player fewer columns, but each column then costs more of the players' own memory: at view 12
it was 296 KiB.

## Metrics

`/metrics` writes the text format 0.0.4 of Prometheus: the metrics of the network stack, of the logs and of the
server, sorted by name. A scrape reads atomics and takes no lock a world owner holds.

### Of the network (already there)

`adamant_nethernet_*` (datagrams, bytes, offers, connections, send batches), `adamant_bedrock_*` (packets and bytes by
ID, batches, rate limits), `adamant_login*` and `adamant_packets_undecodable_total`, `adamant_session_*` (flushes,
congestion), `adamant_sessions` and `adamant_replication_delay_seconds`.

### Of the logs

| Metric | Type | Labels |
|---|---|---|
| `adamant_log_lines_dropped_total` | counter | `component` |

### Of each world

Labelled `dimension` (`overworld`, `nether`, `end`).

| Metric | Type | Meaning |
|---|---|---|
| `adamant_world_tick_seconds` | histogram | time of each tick; bounds 1, 2.5, 5, 10, 25, **50**, 100, 250 ms and 1 s |
| `adamant_world_ticks_total` | counter | ticks |
| `adamant_world_ticks_skipped_total` | counter | ticks a world too far behind skipped |
| `adamant_world_tps` | gauge | ticks a second over the last 20 |
| `adamant_world_mspt` | gauge | mean time of the last 100 ticks, in seconds |
| `adamant_world_owner_busy_nanoseconds_total` | counter | nanoseconds the owner of the world ran transactions (ticks, the work of players and loaders); its rate over a second divided by 1e9 is the share of the owner in use, which the ticks alone do not tell |
| `adamant_world_transactions_queued` | gauge | transactions waiting for the owner; a player's actions answer only once those before them ran |
| `adamant_world_frozen` | gauge | 1 while `/tick freeze` holds the world |
| `adamant_world_entities`, `adamant_world_chunks_loaded` | gauge | entities, columns loaded |
| `adamant_world_chunks_read_total`, `_chunk_load_errors_total`, `_chunks_unloaded_total`, `_chunks_written_total` | counter | columns read from the provider, failed to read, unloaded, written |
| `adamant_world_generation_queue`, `_generation_workers_busy` | gauge | columns waiting for a generator worker, workers generating |
| `adamant_world_generation_seconds` | histogram | time to generate a column; 1 ms to 5 s |
| `adamant_world_generation_failures_total` | counter | generations that panicked |
| `adamant_world_generator_caches_released_total` | counter | times the generator released the caches it keeps of the chunks around the ones it generated, after 30 s without generating a chunk: hundreds of megabytes at a view of 32 chunks |
| `adamant_world_generation_backlog_total` | counter | columns queued past the size of the generation queue |
| `adamant_world_faults_total` | counter | panics of game code recovered, by `kind` (`entity`, `player`, `block_entity`, `scheduled_tick`, `random_tick`, `neighbour_update`, `subsystem`, `transaction`, `save`, `generation`, `fatal`) |
| `adamant_world_quarantined` | gauge | offenders quarantined after their faults |

The share of ticks on time is `adamant_world_tick_seconds_bucket{le="0.05"} / adamant_world_tick_seconds_count`.

### Of the server

| Metric | Type | Labels |
|---|---|---|
| `adamant_checkpoint_seconds` | histogram, 10 ms to 30 s | `phase`: `capture` (on the world owners), `write` |
| `adamant_checkpoints_total`, `adamant_checkpoint_failures_total` | counter | `reason`: `autosave`, `save-all`, `close` |
| `adamant_checkpoint_last_success_timestamp_seconds` | gauge | – |
| `adamant_checkpoint_chunks`, `adamant_checkpoint_players` | gauge, of the last checkpoint | – |
| `adamant_players_online`, `adamant_players_max` (0 for no limit) | gauge | – |
| `adamant_player_joins_total`, `adamant_player_leaves_total` | counter | – |
| `adamant_listener_up` | gauge | `listener` (its index), `kind` (its type) |
| `adamant_build_info` | gauge, 1 | `version`, `minecraft`, `go`, `commit` |
| `adamant_start_time_seconds` | gauge | – |

### Of the Go runtime

Five samples of `runtime/metrics`, named as the Go collector of Prometheus names them: `go_goroutines`,
`go_sched_gomaxprocs_threads`, `go_memory_classes_heap_objects_bytes`, `go_memory_classes_total_bytes` and
`go_gc_cycles_total_gc_cycles_total`. Reading them stops no goroutine.

There are no metrics of single players or chunks, whose labels would have no bound: `/status` and the logs cover
them.

### Alerts worth having

```yaml
- alert: AdamantTicksLate
  expr: 1 - rate(adamant_world_tick_seconds_bucket{le="0.05"}[5m]) / rate(adamant_world_tick_seconds_count[5m]) > 0.1
  for: 10m
- alert: AdamantCheckpointFailing
  expr: increase(adamant_checkpoint_failures_total[30m]) > 0
- alert: AdamantFaults
  expr: increase(adamant_world_faults_total[10m]) > 0
- alert: AdamantLogsDropped
  expr: increase(adamant_log_lines_dropped_total[10m]) > 0
```
