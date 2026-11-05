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
