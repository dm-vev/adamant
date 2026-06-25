# Dragonfly Upstream Sync — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-driven-development + spec-driven-quality-loop to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port 47 upstream Dragonfly commits into Adamant, preserving Adamant's vanilla-parity work.

**Architecture:** Sequential waves by dependency — foundation first (Go/protocol), then bug fixes, then new features, then refactor, then behavioral parity. Each wave builds on the previous. Within each wave, independent tasks run in parallel via subagents.

**Tech Stack:** Go 1.26, gophertunnel v1.57.0, Minecraft Bedrock 1.26.30

## Global Constraints

- Go version: 1.26.0 (bump from 1.24.0)
- gophertunnel: v1.57.0 (bump from v1.55.0)
- Module path: `github.com/df-mc/dragonfly` (unchanged)
- Exclude PR #1095 (redstone components)
- Preserve all Adamant-unique files (258 files)
- Verification: `go build ./... && go test ./... && go vet ./...`
- Conflict resolution: keep Adamant's vanilla-parity additions, merge upstream improvements

---

## Wave 1: Foundation — Go Bump + Protocol Updates

### Task 01: Bump Go to 1.26.0

**Files:**
- Modify: `go.mod` (change `go 1.24.0` → `go 1.26.0`, remove toolchain line)
- Modify: `go.sum` (update checksums)

**Source commit:** 32be9264

- [ ] **Step 1:** Read current `go.mod` — note module path and Go version
- [ ] **Step 2:** Change `go 1.24.0` to `go 1.26.0` in go.mod. Remove `toolchain go1.24.4` line.
- [ ] **Step 3:** Run `go mod tidy` to update go.sum
- [ ] **Step 4:** Run `go build ./...` — must compile
- [ ] **Step 5:** Commit: `chore: bump Go to 1.26.0 (#1226)`

### Task 02: Update for Minecraft 1.26.20

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `server/item/recipe/recipe.go`, `server/item/recipe/vanilla.go`
- Modify: `server/player/debug/shape.go`
- Modify: `server/session/handler_enchanting.go`, `server/session/player.go`
- Replace (binary): `server/item/creative/creative_items.nbt`, `server/item/recipe/crafting_data.nbt`, `server/item/recipe/furnace_data.nbt` (may be deleted), `server/item/recipe/potion_data.nbt`, `server/world/block_states.nbt`, `server/world/vanilla_items.nbt`

**Source commit:** 5903cc1c

- [ ] **Step 1:** Read upstream commit `git show 5903cc1c` for exact diffs
- [ ] **Step 2:** Apply go.mod/go.sum changes (gophertunnel v1.55.0 → newer version matching upstream)
- [ ] **Step 3:** Apply recipe.go changes (remove furnace_data references if upstream did)
- [ ] **Step 4:** Apply vanilla.go changes
- [ ] **Step 5:** Apply player/debug/shape.go changes
- [ ] **Step 6:** Apply session/handler_enchanting.go changes
- [ ] **Step 7:** Apply session/player.go changes — CAREFUL: Adamant may have its own changes here. Merge, don't overwrite.
- [ ] **Step 8:** Copy binary NBT files from upstream — verify they exist in upstream at this commit
- [ ] **Step 9:** Run `go build ./...` — must compile
- [ ] **Step 10:** Run `go test ./...` — all tests pass
- [ ] **Step 11:** Commit: `feat: updated for Minecraft 1.26.20 (#1222)`

### Task 03: Update for Minecraft 1.26.30

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `server/player/bossbar/colour.go`
- Modify: `server/player/debug/shape.go`
- Modify: `server/session/player.go`, `server/session/text.go`, `server/session/world.go`
- Modify: `server/world/biome/register.go`
- Create: `server/world/biome/sulfur_caves.go`
- Modify: `server/world/chunk/chunk.go`
- Replace (binary): `server/item/creative/creative_items.nbt`, `server/item/recipe/crafting_data.nbt`, `server/world/block_states.nbt`, `server/world/vanilla_items.nbt`

**Source commit:** 0f8cf2e6

- [ ] **Step 1:** Read upstream commit `git show 0f8cf2e6` for exact diffs
- [ ] **Step 2:** Apply go.mod/go.sum changes
- [ ] **Step 3:** Create `server/world/biome/sulfur_caves.go` — copy from upstream
- [ ] **Step 4:** Apply biome/register.go changes (register new biome)
- [ ] **Step 5:** Apply bossbar/colour.go changes
- [ ] **Step 6:** Apply player/debug/shape.go changes — merge with changes from Task 02
- [ ] **Step 7:** Apply session/player.go, text.go, world.go changes — merge with Adamant's changes
- [ ] **Step 8:** Apply chunk/chunk.go changes
- [ ] **Step 9:** Copy binary NBT files from upstream
- [ ] **Step 10:** Run `go build ./...` — must compile
- [ ] **Step 11:** Run `go test ./...` — all tests pass
- [ ] **Step 12:** Commit: `feat: updated for Minecraft 1.26.30 (#1268)`

---

## Wave 2: Bug Fixes (10 commits, mostly independent)

### Task 04: Fix panic in world.entitiesWithin

**Files:** Modify: `server/world/world.go`
**Source:** d365dd67 (#1203)

- [ ] **Step 1:** `git show d365dd67` — apply slices.Clone fix to entitiesWithin
- [ ] **Step 2:** `go build ./...` — compile
- [ ] **Step 3:** `go test ./...` — pass
- [ ] **Step 4:** Commit: `fix: use slices.Clone in world.entitiesWithin (#1203)`

### Task 05: Implement String() for block

**Files:** Create: `server/block/string.go`, Modify: `server/block/hash.go`, `server/block/register.go`
**Source:** c6806019 (#1218)

- [ ] **Step 1:** `git show c6806019` — copy string.go, apply hash.go and register.go changes
- [ ] **Step 2:** CAREFUL: register.go and hash.go are high-conflict files. Merge with Adamant's entries.
- [ ] **Step 3:** `go build ./...` — compile
- [ ] **Step 4:** `go test ./...` — pass
- [ ] **Step 5:** Commit: `feat: implement String() for block (#1218)`

### Task 06: Fix liquid-removable blocks dropping items in lava

**Files:** Modify: `server/block/lava.go`, `server/block/liquid.go`, `server/block/water.go`, `server/world/block.go`
**Source:** 2098749d (#1217)

- [ ] **Step 1:** `git show 2098749d` — apply liquid drop fix
- [ ] **Step 2:** `go build ./...` — compile
- [ ] **Step 3:** `go test ./...` — pass
- [ ] **Step 4:** Commit: `fix: liquid-removable blocks no longer drop items in lava (#1217)`

### Task 07: Fix sessionless player panic

**Files:** Modify: `server/session/session.go`
**Source:** 477b8168 (#1276)

- [ ] **Step 1:** `git show 477b8168` — apply nil check fix
- [ ] **Step 2:** `go build ./...` — compile
- [ ] **Step 3:** `go test ./...` — pass
- [ ] **Step 4:** Commit: `fix: panic when sessionless player logs (#1276)`

### Task 08: Guard nil session handle in metadata

**Files:** Modify: `server/session/entity_metadata.go`
**Source:** 1746dd7a (#1278)

- [ ] **Step 1:** `git show 1746dd7a` — apply nil guard
- [ ] **Step 2:** `go build ./...` — compile
- [ ] **Step 3:** `go test ./...` — pass
- [ ] **Step 4:** Commit: `fix: guard nil session handle in metadata (#1278)`

### Task 09: Fix empty nametag metadata

**Files:** Modify: `server/session/entity_metadata.go`
**Source:** faa406df (#1239)

- [ ] **Step 1:** `git show faa406df` — apply nametag fix
- [ ] **Step 2:** Merge with Task 08 changes (same file)
- [ ] **Step 3:** `go build ./...` — compile
- [ ] **Step 4:** `go test ./...` — pass
- [ ] **Step 5:** Commit: `fix: empty nametag metadata (#1239)`

### Task 10: Fix player prone eye height

**Files:** Modify: `server/player/player.go`
**Source:** a6feaf55 (#1240) + b4a4d553 (revert)

- [ ] **Step 1:** `git show a6feaf55` — apply eye height fix
- [ ] **Step 2:** `git show b4a4d553` — apply revert of incorrect change (net effect: both together)
- [ ] **Step 3:** CAREFUL: player.go is high-conflict. Merge with Adamant's changes.
- [ ] **Step 4:** `go build ./...` — compile
- [ ] **Step 5:** `go test ./...` — pass
- [ ] **Step 6:** Commit: `fix: player prone eye height (#1240)`

### Task 11: Always read full level.dat file

**Files:** Modify: `server/world/mcdb/leveldat/level_dat.go`
**Source:** 716292c7 (#1269)

- [ ] **Step 1:** `git show 716292c7` — apply full-file read fix
- [ ] **Step 2:** `go build ./...` — compile
- [ ] **Step 3:** `go test ./...` — pass
- [ ] **Step 4:** Commit: `fix: always read full level.dat file (#1269)`

### Task 12: Add missing leveldat fields

**Files:** Modify: `server/world/mcdb/leveldat/data.go`
**Source:** 8be43f37 (#1277)

- [ ] **Step 1:** `git show 8be43f37` — add missing struct fields
- [ ] **Step 2:** `go build ./...` — compile
- [ ] **Step 3:** `go test ./...` — pass
- [ ] **Step 4:** Commit: `feat: add missing leveldat fields (#1277)`

---

## Wave 3: Deadlock/Lock Fixes

### Task 13: Fix debug drawer deadlock

**Files:** Modify: `server/player/debug/shape.go`, `server/session/player.go`, `server/session/session.go`
**Source:** 3e26ebd3 (#1232)

- [ ] **Step 1:** `git show 3e26ebd3` — apply queueing mutations fix
- [ ] **Step 2:** Merge with Adamant's session.go changes
- [ ] **Step 3:** `go build ./...` — compile
- [ ] **Step 4:** `go test ./...` — pass
- [ ] **Step 5:** Commit: `fix: debug drawer deadlock by queueing mutations (#1232)`

### Task 14: Avoid HUD lock during packet send

**Files:** Modify: `server/session/player.go`
**Source:** ebcebd40 (#1235)

- [ ] **Step 1:** `git show ebcebd40` — apply HUD lock fix
- [ ] **Step 2:** Merge with Task 13 changes (same file)
- [ ] **Step 3:** `go build ./...` — compile
- [ ] **Step 4:** `go test ./...` — pass
- [ ] **Step 5:** Commit: `fix: avoid holding HUD lock while sending packets (#1235)`

---

## Wave 4: New Blocks & Items

### Task 15: Implement Cobweb block

**Files:** Create: `server/block/cobweb.go`, Modify: `server/block/break_info.go`, `server/block/hash.go`, `server/block/register.go`, `server/item/sword.go`
**Source:** a733711e (#1229)

- [ ] **Step 1:** `git show a733711e` — copy cobweb.go, apply register/hash/break_info changes
- [ ] **Step 2:** Merge with Adamant's register.go and hash.go (high-conflict)
- [ ] **Step 3:** Apply sword.go change (cobweb breaks faster with sword)
- [ ] **Step 4:** `go build ./...` — compile
- [ ] **Step 5:** Write test: `server/block/cobweb_test.go` — test break time with/without sword
- [ ] **Step 6:** `go test ./...` — pass
- [ ] **Step 7:** Commit: `feat: implement cobweb block (#1229)`

### Task 16: Implement Honey Bottle

**Files:** Create: `server/item/honey_bottle.go`, Modify: `server/item/register.go`
**Source:** 73b3ec7a (#1230)

- [ ] **Step 1:** `git show 73b3ec7a` — copy honey_bottle.go, apply register.go changes
- [ ] **Step 2:** `go build ./...` — compile
- [ ] **Step 3:** Write test: `server/item/honey_bottle_test.go` — test poison cure behavior
- [ ] **Step 4:** `go test ./...` — pass
- [ ] **Step 5:** Commit: `feat: implement honey bottle (#1230)`

### Task 17: Implement Infested Blocks

**Files:** Create: `server/block/infested_cobblestone.go`, `server/block/infested_deepslate.go`, `server/block/infested_stone.go`, `server/block/infested_stone_bricks.go`, Modify: `server/block/block.go`, `server/block/hash.go`, `server/block/register.go`
**Source:** 5fb6a0e7 (#1234)

- [ ] **Step 1:** `git show 5fb6a0e7` — copy 4 infested block files, apply block.go/hash.go/register.go changes
- [ ] **Step 2:** Merge with Adamant's register.go and hash.go
- [ ] **Step 3:** `go build ./...` — compile
- [ ] **Step 4:** Write test: `server/block/infested_test.go` — test silverfish spawn on break
- [ ] **Step 5:** `go test ./...` — pass
- [ ] **Step 6:** Commit: `feat: implement infested blocks (#1234)`

### Task 18: Implement Piercing enchantment

**Files:** Create: `server/item/enchantment/piercing.go`, Modify: `server/entity/projectile.go`, `server/entity/register.go`, `server/item/bow.go`, `server/item/crossbow.go`, `server/item/enchantment/multishot.go`, `server/item/enchantment/register.go`, `server/item/stack.go`, `server/world/entity.go`
**Source:** bee6061e (#1246)

- [ ] **Step 1:** `git show bee6061e` — copy piercing.go, apply all modifications
- [ ] **Step 2:** Merge with Adamant's stack.go changes
- [ ] **Step 3:** `go build ./...` — compile
- [ ] **Step 4:** Write test: `server/item/enchantment/piercing_test.go` — test projectile penetration
- [ ] **Step 5:** `go test ./...` — pass
- [ ] **Step 6:** Commit: `feat: implement piercing enchantment (#1246)`

---

## Wave 5: Block Registry Refactor

### Task 19: Make block registry instance-scoped

**Files (31 files):** `server/conf.go`, `server/internal/packbuilder/blocks.go`, `server/internal/packbuilder/resource_pack.go`, `server/player/player.go`, `server/server.go`, `server/session/handler_inventory_transaction.go`, `server/session/handler_mob_equipment.go`, `server/session/handler_player_auth_input.go`, `server/session/player.go`, `server/session/session.go`, `server/session/world.go`, `server/world/block.go`, `server/world/block_registry.go`, `server/world/block_state.go`, `server/world/chunk/block_registry.go`, `server/world/chunk/chunk.go`, `server/world/chunk/decode.go`, `server/world/chunk/encode.go`, `server/world/chunk/encoding.go`, `server/world/chunk/light.go`, `server/world/chunk/light_area.go`, `server/world/chunk/light_type.go`, `server/world/chunk/sub_chunk.go`, `server/world/conf.go`, `server/world/generator/flat.go`, `server/world/mcdb/conf.go`, `server/world/mcdb/db.go`, `server/world/network_block_hash.go`, `server/world/tick.go`, `server/world/tx.go`, `server/world/world.go`
**Source:** 26206ab4 (#1171)

This is the highest-risk task. 31 files, many also changed by Adamant.

- [ ] **Step 1:** Read the full upstream diff: `git show 26206ab4 --stat` then `git show 26206ab4` for each file
- [ ] **Step 2:** Create `server/world/block_registry.go` and `server/world/network_block_hash.go` and `server/world/chunk/block_registry.go` from upstream
- [ ] **Step 3:** Apply changes to `server/world/block.go`, `server/world/block_state.go` — merge with Adamant
- [ ] **Step 4:** Apply changes to chunk package (chunk.go, decode.go, encode.go, encoding.go, light.go, light_area.go, light_type.go, sub_chunk.go) — merge with Adamant's chunk optimizations
- [ ] **Step 5:** Apply changes to session package (player.go, session.go, world.go, handlers) — merge with Adamant's session changes
- [ ] **Step 6:** Apply changes to `server/world/world.go`, `server/world/tx.go`, `server/world/tick.go` — merge with Adamant's world ticking optimizations
- [ ] **Step 7:** Apply changes to `server/conf.go`, `server/server.go`, `server/world/conf.go` — merge
- [ ] **Step 8:** Apply changes to `server/player/player.go` — merge with Adamant's changes
- [ ] **Step 9:** Apply changes to packbuilder, mcdb, generator files
- [ ] **Step 10:** `go build ./...` — MUST compile. Fix any issues.
- [ ] **Step 11:** `go test ./...` — all tests pass
- [ ] **Step 12:** `go vet ./...` — clean
- [ ] **Step 13:** Commit: `refactor: make block registry instance-scoped (#1171)`

---

## Wave 6: View Layers + World Improvements

### Task 20: Add per-viewer view layers

**Files:** Create: `server/session/view_layer.go`, `server/world/view_layer.go`, `server/world/visibility_level.go`, Modify: `server/player/player.go`, `server/session/session.go`, `server/session/session_list.go`, `server/session/world.go`, `server/world/world.go`
**Source:** b9a408ac (#1223)

- [ ] **Step 1:** `git show b9a408ac` — copy 3 new files, apply modifications
- [ ] **Step 2:** Merge with Adamant's player.go, session.go, world.go (high-conflict)
- [ ] **Step 3:** `go build ./...` — compile
- [ ] **Step 4:** `go test ./...` — pass
- [ ] **Step 5:** Commit: `feat: add per-viewer view layers (#1223)`

### Task 21: Add DefaultSpawn to generators

**Files:** Modify: `server/world/conf.go`, `server/world/generator.go`, `server/world/generator/flat.go`
**Source:** ad2d193a (#1237)

- [ ] **Step 1:** `git show ad2d193a` — apply DefaultSpawn method and superflat fix
- [ ] **Step 2:** `go build ./...` — compile
- [ ] **Step 3:** `go test ./...` — pass
- [ ] **Step 4:** Commit: `feat: add DefaultSpawn to generators, fix superflat spawn (#1237)`

### Task 22: Add RuntimeIDToHash to block registry

**Files:** Modify: `server/world/block_registry.go`
**Source:** d7434283 (#1252)

- [ ] **Step 1:** `git show d7434283` — add RuntimeIDToHash method
- [ ] **Step 2:** Depends on Task 19 (block_registry.go created there)
- [ ] **Step 3:** `go build ./...` — compile
- [ ] **Step 4:** `go test ./...` — pass
- [ ] **Step 5:** Commit: `feat: add RuntimeIDToHash to block registry (#1252)`

### Task 23: Allow compression in config

**Files:** Modify: `server/conf.go`, `server/listener.go`
**Source:** 16359459 (#1242)

- [ ] **Step 1:** `git show 16359459` — add compression config option
- [ ] **Step 2:** `go build ./...` — compile
- [ ] **Step 3:** `go test ./...` — pass
- [ ] **Step 4:** Commit: `feat: allow compression in config (#1242)`

---

## Wave 7: Behavioral Parity + Optimizations

### Task 24: Reduce durability on attacking living entity

**Files:** Modify: `server/player/player.go`
**Source:** 4b16847f (#1135)

- [ ] **Step 1:** `git show 4b16847f` — apply durability reduction
- [ ] **Step 2:** Merge with Adamant's player.go
- [ ] **Step 3:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 4:** Commit: `fix: reduce durability on attacking living entity (#1135)`

### Task 25: Fix fortune enchantment for iron/gold ore

**Files:** Modify: `server/block/gold_ore.go`, `server/block/iron_ore.go`
**Source:** fe5d7a0f (#1236)

- [ ] **Step 1:** `git show fe5d7a0f` — apply fortune drop fix
- [ ] **Step 2:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 3:** Commit: `fix: fortune enchantment for iron/gold ore drops (#1236)`

### Task 26: Simplify stair FaceSolid check

**Files:** Modify: `server/block/model/stair.go`
**Source:** 0121d71e (#1243)

- [ ] **Step 1:** `git show 0121d71e` — simplify closed side check
- [ ] **Step 2:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 3:** Commit: `refactor: simplify stair FaceSolid check (#1243)`

### Task 27: Apply incompatible enchantments option

**Files:** Modify: `server/internal/nbtconv/read.go`, `server/item/stack.go`
**Source:** db2d6a4e (#1117)

- [ ] **Step 1:** `git show db2d6a4e` — add incompatible enchantment option
- [ ] **Step 2:** Merge with Adamant's stack.go
- [ ] **Step 3:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 4:** Commit: `feat: apply incompatible enchantments option (#1117)`

### Task 28: Align fence/pane collision with vanilla

**Files:** Modify: `server/block/model/fence.go`, `server/block/model/thin.go`
**Source:** 7869803a (#1244)

- [ ] **Step 1:** `git show 7869803a` — align collision boxes
- [ ] **Step 2:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 3:** Commit: `fix: align fence/pane collision with vanilla (#1244)`

### Task 29: Optimize light calculations

**Files:** Modify: `server/world/chunk/light.go`, `server/world/chunk/light_area.go`
**Source:** ff14ddcd (#1192)

- [ ] **Step 1:** `git show ff14ddcd` — apply light optimization
- [ ] **Step 2:** Merge with Adamant's light.go changes (if any)
- [ ] **Step 3:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 4:** Commit: `perf: optimize light calculations (#1192)`

### Task 30: Add light emission to smelter blocks

**Files:** Modify: `server/block/blast_furnace.go`, `server/block/furnace.go`, `server/block/smoker.go`
**Source:** 77713f94 (#1258)

- [ ] **Step 1:** `git show 77713f94` — add light emission levels
- [ ] **Step 2:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 3:** Commit: `feat: add light emission to smelter blocks (#1258)`

### Task 31: Stack golden apple absorption

**Files:** Modify: `server/item/golden_apple.go`, `server/item/item.go`, `server/player/player.go`
**Source:** 536e50da (#1261)

- [ ] **Step 1:** `git show 536e50da` — apply absorption stacking + clear on depletion
- [ ] **Step 2:** Merge with Adamant's player.go and item.go
- [ ] **Step 3:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 4:** Commit: `feat: stack golden apple absorption (#1261)`

### Task 32: Add BlockIntersects/BBoxIntersects to cube/trace

**Files:** Create: `server/block/cube/trace/bbox.go`, `server/block/cube/trace/block.go`
**Source:** 62274835 (#1262)

- [ ] **Step 1:** `git show 62274835` — copy both files
- [ ] **Step 2:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 3:** Commit: `feat: add BlockIntersects/BBoxIntersects to cube/trace (#1262)`

### Task 33: Add Clone methods to chunk types

**Files:** Modify: `server/world/chunk/chunk.go`, `server/world/chunk/palette.go`, `server/world/chunk/paletted_storage.go`, `server/world/chunk/sub_chunk.go`
**Source:** bb1a7a77 (#1264)

- [ ] **Step 1:** `git show bb1a7a77` — add Clone() methods
- [ ] **Step 2:** Merge with Adamant's chunk.go changes
- [ ] **Step 3:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 4:** Commit: `feat: add Clone methods to chunk, subchunk, palette (#1264)`

### Task 34: Fix bone meal duplication glitch

**Files:** Modify: `server/block/grass.go`
**Source:** c08da572 (#1266)

- [ ] **Step 1:** `git show c08da572` — fix duplication glitch
- [ ] **Step 2:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 3:** Commit: `fix: bone meal duplication glitch (#1266)`

### Task 35: Implement bonemeal huge growth particles

**Files:** Modify: 14 block files (beetroot_seeds, carrot, cocoa_bean, double_flower, fern, flower, grass, kelp, melon_seeds, pink_petals, potato, pumpkin_seeds, sea_pickle, short_grass, sugar_cane, wheat_seeds), `server/item/bone_meal.go`, `server/session/world.go`, `server/world/particle/block.go`
**Source:** 64d40fd8 (#1267)

- [ ] **Step 1:** `git show 64d40fd8` — apply particle additions to all block files
- [ ] **Step 2:** Apply bone_meal.go and session/world.go changes — merge with Adamant
- [ ] **Step 3:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 4:** Commit: `feat: implement bonemeal huge growth particles (#1267)`

### Task 36: Fix crossbow critical flag + charging animation

**Files:** Modify: `server/item/crossbow.go`, `server/item/item.go`, `server/player/player.go`
**Source:** 83a8f57c (#1270) + 7c304285 (#1272)

- [ ] **Step 1:** `git show 83a8f57c` — always set critical flag
- [ ] **Step 2:** `git show 7c304285` — fix charging animation without projectile
- [ ] **Step 3:** Merge both changes, merge with Adamant's player.go and item.go
- [ ] **Step 4:** `go build ./...`, `go test ./...` — pass
- [ ] **Step 5:** Commit: `fix: crossbow critical flag and charging animation (#1270, #1272)`

### Task 37: Add gocritic to lint config

**Files:** Modify: `.golangci.yml` or equivalent
**Source:** a2f61753 (#1245)

- [ ] **Step 1:** `git show a2f61753` — add gocritic linter config
- [ ] **Step 2:** If Adamant has no lint config, create one matching upstream
- [ ] **Step 3:** Commit: `chore: add gocritic to golangci-lint config (#1245)`

---

## Verification Checklist (after all waves)

- [ ] `go build ./...` — zero errors
- [ ] `go test ./...` — all tests pass
- [ ] `go vet ./...` — clean
- [ ] Minecraft protocol version is 1.26.30
- [ ] gophertunnel is v1.57.0
- [ ] Go version is 1.26.0 in go.mod
- [ ] No Adamant-unique files deleted (258 files preserved)
- [ ] New tests exist for: cobweb, honey bottle, infested blocks, piercing
- [ ] No redstone #1095 changes included
- [ ] All 47 commits have corresponding changes
