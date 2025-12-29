## Codex Intercall Memory
- Made client-originated movement suppression atomic and per-move to avoid races and lost server corrections.
- Drained debug-shape add queue and removed shapes without holding locks to prevent deadlocks and channel races.
