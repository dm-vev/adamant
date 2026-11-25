// Package api provides a high-level, protocol-neutral external developer SDK
// for building plugins on top of Adamant:
//
//   - Plugin lifecycle & App facade (App, Plugin, FuncPlugin)
//   - Typed prioritized events (On, OnServer, OnWorld, OnPlayer, Emit)
//   - Zero-alloc ECS components with NBT persistence for entities, players,
//     chunks, and worlds (NewComponent, WithComponentCodec, Get/Set/Update)
//   - Tick systems, delayed/repeating Scheduler, and bulk block runtime-ID edits
//   - Declarative custom blocks, custom items, and crafting/smithing/potion recipes
//   - Custom entities with MobAI goals, sensor/memory/activity Brain, equipment,
//     custom NBT, death drops/XP, and floating multi-line Holograms
//   - Virtual chest/double-chest/hopper menus with custom titles, PagedMenu,
//     Menu/Modal/CustomForm builders, and player UI helpers (scoreboards, boss
//     bars, titles, toasts, sounds, particles, NPC dialogues)
//   - Declarative, typed, and subcommand-group commands with client autocomplete
//   - Overworld, Nether, and End world generation hooks, custom biomes, and
//     reusable StructureTemplate placement
//   - Dynamic world creation/unloading, cross-world TransferPlayer, atomic
//     JSON/TOML Storage, hierarchical RBAC PermissionStore, and Economy ledger
//
// For Bedrock-specific multi-protocol packet interception, versioned raw packet
// injection, and resource pack loading, see package
// github.com/df-mc/dragonfly/api/bedrock.
package api
