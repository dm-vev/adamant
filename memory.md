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
