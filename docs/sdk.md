# Adamant Plugin SDK

Adamant provides a modular, zero-reflection Go plugin SDK under `github.com/dm-vev/adamant/sdk/*`. Instead of a monolithic API package, each domain lives in its own focused package built on top of `sdk/plugin`.

For runnable end-to-end projects, see [`examples/hub`](../examples/hub) and [`examples/minigame`](../examples/minigame).

---

## Package Overview

| Package | Purpose |
|---|---|
| `sdk/plugin` | `App`, `Plugin`/`Func`/`NewFunc`, scoped `*plugin.Context` with LIFO `OnDisable` cleanup, dependency ordering (`UseOrdered`), hot-reload (`Reload`), typed service registry (`Provide`/`Service`), atomic JSON/TOML storage |
| `sdk/event` | Prioritized typed event subscriptions (`OnServer`, `OnWorld`, `OnPlayer`, `NewBus`, `Emit`) with automatic unsubscribe on disable |
| `sdk/ecs` | Typed ECS `Key[T]` components & NBT `Codec` persistence across entities, players, chunks, and worlds |
| `sdk/schedule` | World tick `System`, `After`/`Every` tick tasks, and off-thread `Async[T]` tasks with `.ThenWorld(...)` marshalling and auto-cancel |
| `sdk/cmd` | Declarative `Register`, strongly-typed `RegisterTyped`, and fluent autocomplete `Group` / `Sub` command builders |
| `sdk/perm` | Standalone hierarchical RBAC `Store` with wildcards (`*`, `shop.*`), explicit negation (`-shop.admin`), group inheritance, and JSON persistence |
| `sdk/admin` | Server bans/IP bans, operators, whitelist management, checkpoint saves, and offline player data access (`LoadOffline`/`SaveOffline`/`EditOffline`) |
| `sdk/block` | Declarative custom `Define` blocks with custom hitboxes, light, hardness, drops, XP, callbacks, and fast `SetRID`/`FillRID` placement |
| `sdk/item` | Custom `Define` items with capabilities, resource-pack textures, and fluent `Stack(...)` builder (`Name`, `Lore`, `Enchant`, `Unbreakable`, `Value`) |
| `sdk/recipe` | `Shaped`, `Shapeless`, `Smithing`, `Brewing`, `Dynamic`, `Stonecutter`, plus furnace `Smelting`/`Blasting`/`Smoking`/`Campfire` and `RegisterFuel` |
| `sdk/mob` | Custom entity `Define` types with `NewGoal`, sensor/memory/activity `NewBrain`, equipment, loot drops, XP, metadata patches, and NBT `Codec` |
| `sdk/npc` | Skinned human NPCs (`AddPlayer` without tab-list clutter) with head tracking (`LookAtPlayers`), equipment, damage immunity, click callbacks, and visibility control |
| `sdk/menu` | Virtual `Chest`, `DoubleChest`, `Hopper`, `Entity`, and `Paged` container GUIs with per-slot locks and click/change/close callbacks |
| `sdk/form` | Fluent Bedrock `NewMenu`, `NewModal`, and reflection-free `NewCustom` builders with typed `Values` |
| `sdk/hud` | `NewScoreboard`, `NewBossBar`, `NewTitle`, `NewDialogue`, `ActionBar`/`Popup`/`Tip`/`Toast`/`Broadcast`, sounds/particles, `Hologram`, HUD visibility & input locks, entity vanish, nametag/scoretag |
| `sdk/worldgen` | Unified Overworld/Nether/End `Attach`/`Clear` generation hooks, custom biomes, immutable `Structure` templates, and `.mcstructure` file I/O |
| `sdk/worlds` | Dynamic `Open`/`Manager` lifecycle, checkpoint-safe `Transfer`, `ForceChunk` tickets, game rules/time/weather/difficulty/scoreboard, and chunk-indexed `RegionIndex` zones |
| `sdk/bedrock` | Multi-protocol packet interception (`InterceptScoped`, `ForProtocols`, `SendPlayerVersioned`), Camera/Fog/`Transfer`/`AnimateEntity`, resource packs, and runtime `NewPackBuilder` |
| `sdk/sdktest` | Fast in-memory `NewServer`, `NewApp`, `NewWorld`, and `SpawnPlayer` harness for `-race`-clean plugin tests |

---

## Plugin Lifecycle, Dependencies & Storage

Every plugin receives a scoped `*plugin.Context` when enabled. Subscriptions, scheduled tasks, async workers, chunk tickets, dynamic worlds, NPCs, and packet interceptors registered with `ctx` are automatically cleaned up in reverse order when `app.Disable()` runs.

```go
package main

import (
	"github.com/dm-vev/adamant/sdk/perm"
	"github.com/dm-vev/adamant/sdk/plugin"
)

type cfg struct {
	MOTD string `toml:"motd"`
}

func main() {
	// Start server srv first (see examples/hub/main.go).
	app := plugin.New(srv, plugin.WithDataDir("plugins"))
	defer app.Disable()

	permsPlugin := plugin.NewFunc("perm", func(ctx *plugin.Context) {
		store := perm.New(ctx.Server())
		store.GrantGroup("default", "hub.join")
		plugin.Provide(ctx.App(), store)
	})

	hubPlugin := plugin.NewFunc("hub", func(ctx *plugin.Context) {
		c, err := plugin.LoadOrDefaultTOML(ctx, "config.toml", cfg{MOTD: "Welcome"})
		if err != nil {
			ctx.Log().Error("load config", "err", err)
			return
		}
		store := plugin.MustService[*perm.Store](ctx.App())
		ctx.Log().Info("hub enabled", "motd", c.MOTD, "perms", store != nil)
	},
		plugin.WithDepends("perm"),
		plugin.WithOnReload(func(ctx *plugin.Context) error {
			_, err := plugin.LoadTOML[cfg](ctx, "config.toml")
			return err
		}),
	)

	if err := app.UseOrdered(hubPlugin, permsPlugin); err != nil {
		panic(err)
	}
}
```

Use `plugin.SaveJSON` / `plugin.LoadJSON` or `plugin.SaveTOML` / `plugin.LoadTOML` for atomic file persistence inside each plugin's sandboxed `DataDir()` (paths escaping `DataDir()` are rejected).

---

## Threading Model: `world.Tx` vs `schedule.Async`

World state is owned exclusively by its world goroutine and accessed inside `*world.Tx`:

- **World tick callbacks & events** already run inside a valid `*world.Tx`. Never perform blocking network, disk, or database I/O inside a transaction or event listener.
- **Periodic / delayed game logic:** use `schedule.System`, `schedule.After`, `schedule.Every`, `schedule.AfterDuration`, or `schedule.EveryDuration`.
- **Database / HTTP / CPU work:** run off-thread with `schedule.Async(ctx, workFn)` and deliver the result safely back to a world transaction with `.ThenWorld(w, func(tx *world.Tx, val T, err error) { ... })`. If the plugin disables before completion, the work context is cancelled automatically and `.ThenWorld` is suppressed.

```go
schedule.Async(ctx, func(bgCtx context.Context) (Profile, error) {
	return db.LoadProfile(bgCtx, playerUUID)
}).ThenWorld(w, func(tx *world.Tx, prof Profile, err error) {
	if err == nil {
		if ent, ok := handle.Entity(tx); ok {
			p := ent.(*player.Player)
			hud.ActionBar(p, "Loaded rank: "+prof.Rank)
		}
	}
})
```

---

## Events (`sdk/event`)

Attach handlers at `event.Lowest`, `Low`, `Normal`, `High`, `Highest`, or `Monitor`:

- `event.OnServer[E](ctx, prio, fn)` — server-wide bus (`JoinEvent`, `PreLoginEvent`, custom events)
- `event.OnWorld[E](ctx, w, prio, fn)` — world-scoped bus (`WorldTickEvent`, `BlockChangeEvent`, `ExplosionEvent`, `WeatherChangeEvent`, `ProjectileHitEvent`, `EntityDamageEvent`, `EntityDeathEvent`, `ChunkLoadEvent`/`ChunkUnloadEvent`, plus forwarded player events in that world)
- `event.OnPlayer[E](ctx, p, prio, fn)` — per-player bus (`QuitEvent`, `MoveEvent`, `TeleportEvent`, `ChatEvent`, `BlockBreakEvent`, `BlockPlaceEvent`, `AttackEntityEvent`, `HurtEvent`, `DeathEvent`, `RespawnEvent`, `ItemUseEvent`, `ItemUseOnBlockEvent`, `ItemUseOnEntityEvent`, `ItemPickupEvent`, `ItemConsumeEvent`, `DropEvent`, `HealEvent`, `FoodLossEvent`, `ExperienceGainEvent`, `HeldSlotChangeEvent`, `CommandEvent`, `ToggleSprintEvent`, `ToggleSneakEvent`, `WorldChangeEvent`, `SignEditEvent`, `PunchAirEvent`)

---

## Custom Content & Entities

- **Blocks (`sdk/block`):** `block.Define("myplugin:pad", block.FullCube(), block.Light(12), block.OnInteract(...))`, register before world creation (`block.Register` or `block.RegisterIn`), stamp fast in bulk with `block.SetRID` / `block.FillRID`.
- **Items (`sdk/item`):** `item.Define("myplugin:wand", item.Cooldown(2*time.Second), item.OnUse(...))`; build rich stacks with `item.Stack(it).Name(...).Lore(...).Enchant(...).Unbreakable().Value(...).Build()`.
- **Recipes (`sdk/recipe`):** `Shaped`, `Shapeless`, `Smithing`, `Brewing`, `BrewingContainer`, `Dynamic`, `Stonecutter`, plus `RegisterCooking(Smelting(...), Blasting(...), Smoking(...), Campfire(...))` and `RegisterFuel`.
- **Custom Mobs (`sdk/mob`):** `mob.Define("myplugin:guard", mob.NetworkType("minecraft:zombie"), mob.MaxHealth(20), mob.WithBrain(brain), mob.WithGoal(1, goal), mob.WithEquipment(...), mob.Drops(...), mob.Codec(...))`.
- **Human NPCs (`sdk/npc`):** `npc.Spawn(ctx, tx, "Guide", npc.Skin(sk), npc.LookAtPlayers(10), npc.OnInteract(...), npc.OnAttack(...))`.

---

## UI, Worlds, WorldGen & Moderation

- **Virtual Menus (`sdk/menu`):** `menu.Chest`, `menu.DoubleChest`, `menu.Hopper`, `menu.Entity`, `menu.Paged` with `menu.ReadOnly()`, `menu.Slot(i, st, menu.Locked(), menu.SlotClick(...))`, and `SetSlot`/`SetItem` programmatic updates.
- **Forms (`sdk/form`):** `form.NewMenu`, `form.NewModal`, `form.NewCustom` with `Values.String`/`Bool`/`Float`/`Int`/`Index`.
- **HUD & Visibility (`sdk/hud`):** `NewScoreboard`, `NewBossBar`, `NewTitle`, `NewDialogue`, `Hologram` (with `HideFrom`/`ShowTo`), `HideHUD`/`ShowHUD`, `LockInput`/`UnlockInput`, `Vanish`/`Unvanish`, `SetNameTag`/`SetScoreTag`.
- **Worlds & Regions (`sdk/worlds`):** `worlds.Open` / `worlds.NewScopedManager`, checkpoint-safe `worlds.Transfer`, `ForceChunk`, `SetGameRule`/`SetTime`/`StartRain`/`SetDifficulty`/`Scoreboard`, and chunk-indexed `worlds.NewRegionIndex()` with `.TrackWorld(ctx, w, onEnter, onLeave)`.
- **WorldGen & `.mcstructure` (`sdk/worldgen`):** `worldgen.Attach(gen, Hooks{...})` / `worldgen.Clear(gen)` works uniformly for `*overworld.Overworld`, `*nether.Nether`, and `*end.End`; `NewBiome`, `NewStructure`, `LoadMCStructureFile` / `SaveMCStructureFile`.
- **Moderation & Offline Players (`sdk/admin`):** `BanPlayer`, `Ban`, `BanIP`, `PardonPlayer`, `Op`/`Deop`, `SetWhitelist`/`WhitelistAdd`, `SaveAll`, `LoadOffline`/`SaveOffline`/`EditOffline`.

---

## Bedrock Exclusives & Runtime Resource Packs (`sdk/bedrock`)

- **Packet Interception & Filtering:** `InterceptScoped(ctx, p, FuncInterceptor{OnIn: ..., OnOut: ...})`, `ForProtocols` / `ForProtocolRange`, `SendPlayerVersioned`.
- **Camera, Fog, Animations & Server Transfer:** `CameraFade`, `CameraSet`, `CameraClear`, `CameraFOV`, `CameraShake`, `SetFog`/`ClearFog`, `AnimateEntity`, and `Transfer(p, "play.example.com:19132")`.
- **Runtime Pack Generation:** `bedrock.NewPackBuilder("MyPack", "Assets").AddItemTexture(...).AddBlockTexture(...).AddSound(...).Build()` produces an in-memory `*resource.Pack` ready for `bedrock.WithPacks`.

---

## Testing Plugins (`sdk/sdktest`)

```go
func TestMyPlugin(t *testing.T) {
	app := sdktest.NewApp(t)
	w := sdktest.NewWorld(t)
	fp := sdktest.SpawnPlayer(t, w, "Alice")

	app.Use(myPlugin(w))
	fp.Run(func(p *player.Player, tx *world.Tx) {
		p.Message("hello")
	})
}
```

`sdktest` wires an isolated, silent in-memory server and synchronous world with automatic cleanup on `t.Cleanup`, making `-race` testing deterministic and fast.
