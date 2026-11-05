# BDS 1.26.61.1 actor captures

Step files for `TestBDSActorsProbe` (`net/bedrock/client/bds_actors_probe_test.go`)
and what they showed, read from the logs of the runs.

`villager3.steps`: four beds in a row, one facing each way, a villager beside
each, at midnight. For each villager that lay down BDS sent
`SetActorData [53 width]=1 [54 height]=0.3 flags2 +76 (Sleeping)`, a
`MoveActorDelta` with `ForceMove` to the position below, and the yaw and head yaw
below; no bed position (key 28) and no player flags (key 26). Waking up sent
`[53]=0.6 [54]=1.9` and flags2 without 76. A viewer arriving later was sent
`AddActor` with the box, the flag, the position and the yaw already.

| bed (foot to head) | head block | villager | yaw |
|---|---|---|---|
| north (dir 0)  | 3 100 2  | 3.5 100.6 2.875  | 180 |
| east (dir 1)   | 11 100 3 | 11.125 100.6 3.5 | 270 |
| south (dir 2)  | 17 100 4 | 17.5 100.6 4.125 | 0 |
| west (dir 3)   | 23 100 3 | 23.875 100.6 3.5 | 90 |

`misc.steps`: `replaceitem entity ... slot.weapon.mainhand` on a copper golem, a
zombie, a piglin, an allay, a fox, a panda and an iron golem. For each BDS sent
`MobEquipment` (runtime ID, item, slot 0, hotbar 0, window 0), and sent it again
after `AddActor` when the bot came back. A copper golem that moved an item out
of its hand sent `MobEquipment` with an empty stack.

`cows3.steps`, `cows4.steps`: feeding cows wheat. BDS ignored every
`InventoryTransaction` of `UseItemOnEntity` the bot sent (attack, bucket and
wheat), so nothing was recorded of the hearts.

`dispenser.steps`, `dispenser2.steps` (summoned projectiles and shulker boxes): dispensers facing south (a row at z 10,
another at z 18), each holding one item, powered one at a time by a redstone block
beside it, with BDS_LEVEL=1 so that the probe logs `LevelEvent`,
`LevelSoundEvent`, `SpawnParticleEffect` and `BlockEvent`. The launch point `L`
is 0.7 blocks out of the dispenser and 0.2 below its centre (x+0.5, y+0.3,
z+1.2 for a dispenser at x, y, z). ExtraData of a click is the network hash of the
block there; it is -1 when BDS sends no block. What BDS sent, in the order it
sent it:

| in the dispenser | LevelEvent | LevelSoundEvent |
|---|---|---|
| nothing | | `block.click.fail` at the dispenser's corner (x, y, z), hash of the dispenser |
| stone (ejected) | 2000 at L, data 7 | `block.click` at L, hash of the block at L |
| arrow | 1002 at L, data 78642; 2000 | `block.click` at L, -1 |
| egg, snowball, bottle o' enchanting, splash potion | 1002, data 78642; 2000 | none |
| wind charge | 1002, data 78642; 2000 | `block.click` at L, -1 (DisableRelativeVolume) |
| fire charge | 1008, data 78642; 2000 | none |
| firework rocket | 2000 | `block.click` at L, -1; the rocket plays `launch` itself |
| flint and steel, fire placed | 2000 | `block.click` at L, -1 |
| flint and steel, stone in front | 2000 | `block.click.fail` at L, -1 |
| bone meal on wheat | 2005 at the corner of the block in front, data 0; 2000 | `block.click` at L, -1 |
| bone meal on stone | 2000 | `block.click.fail` at L |
| shears (sheep or none), brush, shulker box (it is placed) | 2000 | none |
| water, lava bucket; glass bottle on water; bucket picking water up | 2000 | `block.click` at L |
| honeycomb on copper | 2030 at the centre of the block in front; 2000 | `block.click` at the corner of the block in front, -1 |
| glowstone on a respawn anchor | 2000 | `respawn_anchor.charge` at the centre of the anchor, -1; `block.click` at L, -1 (DisableRelativeVolume) |
| TNT | 1005 at L; 2000 | `block.click` at L, -1 |
| boat on water, spawn egg | 2000 | `block.click` at L, -1 |

A rocket, an arrow and a wind charge that were summoned by command made no
`block.click`: that of the table comes from the dispenser. BDS ejected
the carved pumpkin (with no golem pattern), the armour stand, the chest on a
donkey, the helmet for a zombie and the minecart it was given, with the click of an
ejected item (hash of the block in front), so it says nothing about them.
