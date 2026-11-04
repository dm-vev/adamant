# Block test fixtures

These are BDS captures and Java oracles that tests across the repository read.

Fixtures of 100 KiB or more are stored zstd-compressed as `x.json.zst`. Tests still name `x.json` and read it
through `internal/testfixture`, which falls back to `x.json.zst` when `x.json` is absent. To inspect one, run
`zstd -d x.json.zst`. A decompressed copy takes precedence, so delete it again before committing.

A new or recaptured large fixture should be compressed before it is committed:

	zstd -19 --rm x.json

Go recorders should write through `testfixture.Write`, which compresses from 100 KiB.

`bedrock_1_26_51_piston_movability.json` stays uncompressed: `scripts/redstone_bds_entity_physics_probe.py`
reads it directly.
