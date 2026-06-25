# Spec: Dragonfly Upstream Sync — Port 47 Commits to Adamant

## Objective

Sync Adamant (dm-vev/adamant) with Dragonfly (df-mc/dragonfly) upstream by porting 47 of the 48 commits that landed upstream between 2026-04-27 and 2026-06-24. Adamant is a faithful fork focused on vanilla parity; the goal is to absorb upstream improvements while preserving Adamant's own vanilla-parity work.

**Excluded:** PR #1095 (basic redstone components) — 24 files, architectural conflict with Adamant's own redstone refactor. To be handled in a separate effort.

**Included:** 47 commits covering protocol updates, bug fixes, new blocks/items, block registry refactor, view layers, optimizations, and behavioral parity.

## Tech Stack

- Go 1.24.0 → 1.26.0 (bump required)
- gophertunnel v1.55.0 → v1.57.0
- Minecraft Bedrock protocol 1.26.0 → 1.26.30
- No build tools — standard `go build`/`go test`
- 22 existing test files in the repo

## Success Criteria

1. `go build ./...` compiles with zero errors on Go 1.26
2. All existing tests pass: `go test ./...`
3. Minecraft protocol version supports 1.26.30
4. All 47 ported commits have corresponding changes in Adamant
5. No regressions in Adamant's vanilla-parity features (blocks, items, behaviors)
6. `go vet ./...` passes clean
7. Adamant's own changes (258 unique files) are preserved

## Out of Scope

- PR #1095 (redstone components) — deferred to separate effort
- CI/CD workflow files (4 commits) — Adamant has no CI
- Contributor list updates (4 commits) — cosmetic
- Rewriting Adamant's own vanilla implementations to match upstream's approach
- Uploading NBT binary files that differ between forks (handled case-by-case)

## Commit Inventory (47 commits, 7 waves)

### Wave 1: Foundation — Go bump + dependencies (3 commits)
| Hash | PR | Description |
|------|-----|-------------|
| 32be9264 | #1226 | Bump Go to 1.26.0 |
| 5903cc1c | #1222 | Updated for Minecraft 1.26.20 |
| 0f8cf2e6 | #1268 | Updated for Minecraft 1.26.30 |

### Wave 2: Bug fixes — low conflict risk (10 commits)
| Hash | PR | Description |
|------|-----|-------------|
| d365dd67 | #1203 | Fix panic in world.entitiesWithin (slices.Clone) |
| c6806019 | #1218 | Implement String() for block |
| 2098749d | #1217 | Fix liquid-removable blocks dropping items in lava |
| 477b8168 | #1276 | Fix panic when sessionless player logs |
| 1746dd7a | #1278 | Guard nil session handle in metadata |
| faa406df | #1239 | Fix empty nametag metadata |
| a6feaf55 | #1240 | Fix incorrect player prone eye height |
| b4a4d553 | — | Revert incorrect eye height change |
| 716292c7 | #1269 | Always read full level.dat file |
| 8be43f37 | #1277 | Add missing leveldat fields |

### Wave 3: Deadlock/lock fixes (2 commits)
| Hash | PR | Description |
|------|-----|-------------|
| 3e26ebd3 | #1232 | Fix debug drawer deadlock by queueing mutations |
| ebcebd40 | #1235 | Avoid holding HUD lock while sending HUD packets |

### Wave 4: New blocks & items (4 commits)
| Hash | PR | Description |
|------|-----|-------------|
| a733711e | #1229 | Implement Cobweb block |
| 73b3ec7a | #1230 | Implement Honey Bottle item |
| 5fb6a0e7 | #1234 | Implement Infested Blocks (5 block types) |
| bee6061e | #1246 | Implement Piercing enchantment |

### Wave 5: Block registry refactor (1 commit, 31 files)
| Hash | PR | Description |
|------|-----|-------------|
| 26206ab4 | #1171 | Make block registry instance-scoped |

### Wave 6: View layers + world improvements (4 commits)
| Hash | PR | Description |
|------|-----|-------------|
| b9a408ac | #1223 | Add per-viewer view layers |
| ad2d193a | #1237 | Add DefaultSpawn() to generators, fix superflat spawn |
| d7434283 | #1252 | Add RuntimeIDToHash to block registry |
| 16359459 | #1242 | Allow compression in config |

### Wave 7: Behavioral parity + optimizations (11 commits)
| Hash | PR | Description |
|------|-----|-------------|
| 4b16847f | #1135 | Reduce durability on attacking living entity |
| fe5d7a0f | #1236 | Fix fortune enchantment for iron/gold ore drops |
| 0121d71e | #1243 | Simplify stair FaceSolid closed side check |
| db2d6a4e | #1117 | Apply incompatible enchantments option |
| 7869803a | #1244 | Align fence/pane collision with vanilla |
| ff14ddcd | #1192 | Optimize light calculations |
| 77713f94 | #1258 | Add missing light emission to smelter blocks |
| 536e50da | #1261 | Stack golden apple absorption, clear on depletion |
| 62274835 | #1262 | Add BlockIntersects/BBoxIntersects to cube/trace |
| bb1a7a77 | #1264 | Add Clone methods to chunk, subchunk, palette |
| c08da572 | #1266 | Fix bone meal duplication glitch |

### Wave 7b: Bonemeal particles + crossbow fixes (4 commits)
| Hash | PR | Description |
|------|-----|-------------|
| 64d40fd8 | #1267 | Implement bonemeal huge growth particles |
| 83a8f57c | #1270 | Always set crossbow critical flag |
| 7c304285 | #1272 | Fix crossbow charging animation without projectile |
| a2f61753 | #1245 | Add gocritic to golangci-lint config |

## Boundaries

### Always do
- Run `go build ./...` after each wave
- Run `go test ./...` after each wave
- Run `go vet ./...` after each wave
- Preserve Adamant's own changes — do NOT overwrite with upstream version
- Resolve conflicts manually, keeping Adamant's vanilla-parity additions
- Commit after each task with descriptive message

### Ask first
- Changing NBT binary files (block_states.nbt, creative_items.nbt, etc.)
- Modifying go.mod beyond what upstream did
- Any change to Adamant's unique files (258 files not in upstream)

### Never do
- Delete Adamant's vanilla blocks/items that upstream doesn't have
- Overwrite Adamant's optimized world ticking/chunk serialization
- Touch redstone implementation (#1095 is excluded)
- Force-push to main
- Delete existing tests

## Conflict Zones

69 .go files are touched by BOTH Adamant and upstream. High-risk files:

| File | Upstream PRs | Adamant changes | Risk |
|------|-------------|-----------------|------|
| server/block/register.go | #1218, #1229, #1234 | 6 commits | HIGH |
| server/block/hash.go | #1218, #1229, #1234 | 6 commits | HIGH |
| server/player/player.go | #1135, #1223, #1240, #1261, #1272 | 5 commits | HIGH |
| server/session/world.go | #1095(excl), #1223, #1267 | 1 commit | MEDIUM |
| server/world/world.go | #1171, #1203, #1223 | 2 commits | HIGH |
| server/item/stack.go | #1117, #1246 | 1 commit | MEDIUM |
| server/item/item.go | #1117, #1272 | 1 commit | MEDIUM |
| server/world/chunk/light.go | #1192 | 1 commit | MEDIUM |
| server/conf.go | #1171, #1242 | 0 direct | MEDIUM |

## Testing Strategy

1. **After each task:** `go build ./...` must compile
2. **After each wave:** `go test ./...` — all existing tests must pass
3. **After each wave:** `go vet ./...` — clean
4. **New tests:** Add tests for new features (cobweb, honey bottle, piercing, infested blocks) following existing test patterns in `server/block/*_test.go`
5. **Manual verification:** Protocol version check, block registration check
