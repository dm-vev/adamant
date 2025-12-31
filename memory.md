## Codex Intercall Memory
- Made client-originated movement suppression atomic and per-move to avoid races and lost server corrections.
- Drained debug-shape add queue and removed shapes without holding locks to prevent deadlocks and channel races.
- Made debug shape IDs atomically initialized to prevent concurrent ShapeID races.
- Snapshotted chat subscribers to avoid lock contention and guarded closed chats; fixed scoreboard write counts and limits.
- Added atomic world chunk/entity counters and a dedicated weather RNG to eliminate map length races and out-of-band weather RNG contention.
- Fixed scoreboard state tracking to remove stale entries and corrected lightning height checks to use X/Z coordinates.
- Added locks and snapshotting for creative items/groups and recipe registries to prevent concurrent access races.
- Added registry locks for items, enchantments, effects, and biomes; rebuilt biome runtime caches safely and replaced unsafe hash encoding with deterministic byte writes.
- Guarded player lookups by name/XUID with locks to prevent map races and removed unnecessary allocations; fixed lightning entity height comparisons.
- Guarded whitelist enabled flag with locks to avoid races during allow checks.
- Fixed @p target selection to pick the true nearest player without sorting.
- Made held slot access atomic and captured inventory slot callbacks to remove concurrent update races.
- Hardened query token caching and playerdb persistence by pruning tokens, validating UUIDs, and guarding corrupt inventory slots.
- Guarded session chunk radius with atomic access and clamped client values to safe bounds to prevent races and overflowed view distances.
- Tracked placeholder chunks after provider load errors to avoid mutating untracked columns and losing world changes.

- Hardened AI scheduler shutdown, ensured brain in-flight resets on panic, cleared inventory state/handlers on close, and constrained slice deletes to comparable types.
- Validated item stack request result slots, creative item IDs, and anvil rename indices to avoid out-of-range panics.
- Normalized debug shape updates to last-op-wins pending maps with Nop guards to avoid add/remove ordering races and blocked queues.
- Reused existing entity runtime IDs for all entities and backfilled runtime maps to prevent duplicate IDs and stale lookups.
- Validated client block faces and loom banner pattern IDs to avoid panics; defaulted corrupted banner pattern NBT safely; guarded scoreboard removal for Nop sessions.
- Reworked pathfinding node storage to use stable pointers and heap fixes, preventing invalid heap references during path expansion.
- Fixed instant-break checks to use a baseline haste multiplier and corrected lightning fire spread offsets.
- Hardened trace traversal and recipe/command/custom block registries against external mutation and zero-length ray crashes.

- Bounded pmgen population concurrency to prevent goroutine growth and fixed paletted storage equality for empty or nil palettes.
- Guarded scoreboard line removal to avoid panics when removing a non-existent line.
- Fixed chest unpairing to use fresh viewer locks/maps for cloned inventories, keeping callbacks in sync after separation.
- Clamped query HostPort to safe bounds to avoid wrapping invalid ports.
- Guarded session shutdown and chunk-radius handling when loaders or handles are uninitialised to prevent nil-pointer panics during early disconnects.
- Hardened NBT item damage encoding to avoid panics on unexpected numeric types and preserve existing damage tags.
- Guarded scoreboard sends for Nop/uninitialised sessions to prevent nil pointer panics.
- Avoided inventory merge deadlocks with ordered locks and cloned slot snapshots.
- Moved command origin tracking to an atomic session field to avoid races and keep command output correlated with the latest request.
- Hardened explosion randomness by serializing shared rand sources and deduplicated affected blocks to avoid duplicate break/drop processing.
- Hardened inspect_palette candle-cake scanning to avoid prefix slice panics and use EOF-safe decoding.
- Guarded XP drop randomization against invalid ranges to avoid rand panics.
- Ensured sessions always close background workers by closing connections during shutdown to prevent goroutine leaks after packet read errors.
- Avoided panics when encoding/decoding item custom NBT data by skipping malformed values and filtering lore entries safely.
- Fixed grindstone experience calculation to return values in the correct inclusive range and added guards against invalid ranges.
- Preserved barrel open state and brewing stand slot flags during NBT decode to keep block entity state aligned with stored runtime IDs.
- Hardened session form handling with Nop/handler guards and marshal error logging; validate transfer ports before sending.- Resolved translation parameter handling to honor Translation/translation arguments and avoid panics on nested translations.
- Guarded player item cooldowns with a mutex to prevent concurrent map access races.
- Made session held-slot pointers atomic and initialized before inventory callbacks to prevent racey nil dereferences during inventory transactions.

- Guarded NPC dialogue state with mutex-protected pointers to avoid races between session updates and network handlers.
- Prevented item stack lore slices from sharing backing arrays with callers to avoid accidental mutation and race risks.

- Hardened book NBT decode/encode paths to clamp page counts and sizes and skip malformed page entries.
- Guarded skin, cape, and animation constructors against invalid dimensions and overflowed allocations by returning empty values.

- Ignored nil errors in command output collection to prevent panics when rendering command output.
- Hardened resource pack building to return errors instead of panicking, adding logging and safe version parsing.

- Guarded crossbow quick-charge tick math against invalid levels and cloned stack custom data/unbreakable flags based on the new item type.

- Validated item stack request container lookups and slot updates to reject invalid containers and avoid nil dereferences during inventory actions.

- Avoided holding the session list lock while sending player list packets by snapshotting sessions before I/O.
- Clamped persisted and loaded held-slot values to hotbar bounds to avoid invalid inventory access from corrupted data.
- Detached query provider player-name slices to avoid concurrent mutation races during query responses.

- Avoided ender chest slot updates after container close by checking open state and position inside the transaction.
- Preserved quoted command arguments by splitting ExecuteLine once and parsing args consistently for hooks.

- Defaulted query logging to slog.Default when a nil logger is supplied to avoid nil-pointer panics in RakNet network setup.

- Added internal locking to scoreboard state access to eliminate data races during concurrent updates and reads.
- Validated inbound skin, cape, and animation payload dimensions and lengths, copying buffers to avoid malformed data panics.
- Guarded enchanting random selection against invalid weights/values to avoid IntN panics on custom enchantables.

- Resolved missing client cache blob hashes even when payloads are unavailable to avoid stalling chunk delivery.
- Guarded explosion knockback and orb attraction math against zero-length vectors to prevent NaNs.
- Avoided deleting unrelated entity runtime IDs when removing sessions by checking mapping presence before delete.
- Corrected Nether/Overworld coordinate scaling to use floor division for negative positions during portal travel.

- Avoided sending invalid sound events for wax/scrape/lightning effects and unknown music discs, preventing crashes and stray packets.
- Defaulted invalid item categories to a safe label instead of panicking during component builds.
- Logged and skipped failed entity identifier serialization rather than panicking during session setup.
- Hardened book editing methods against invalid indices and oversized text, and made skin/cape/animation At return zero colors for out-of-bounds access.
