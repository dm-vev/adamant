## Codex Intercall Memory
- Made client-originated movement suppression atomic and per-move to avoid races and lost server corrections.
- Drained debug-shape add queue and removed shapes without holding locks to prevent deadlocks and channel races.
- Made debug shape IDs atomically initialized to prevent concurrent ShapeID races.
- Snapshotted chat subscribers to avoid lock contention and guarded closed chats; fixed scoreboard write counts and limits.
- Added atomic world chunk/entity counters and a dedicated weather RNG to eliminate map length races and out-of-band weather RNG contention.
- Fixed scoreboard state tracking to remove stale entries and corrected lightning height checks to use X/Z coordinates.
- Added locks and snapshotting for creative items/groups and recipe registries to prevent concurrent access races.
- Added registry locks for items, enchantments, effects, and biomes; rebuilt biome runtime caches safely and replaced unsafe hash encoding with deterministic byte writes.
