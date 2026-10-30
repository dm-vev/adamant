#!/usr/bin/env python3
"""Capture sided hopper insertion of valid brewing ingredients, fuel and bottles.

Fuel powder may be converted by the native block actor into brewing fuel;
ordinary inventory-only item totals are therefore not a conservation invariant
for those scenes. The full source/target slots and block flags are retained.
"""
import json

from redstone_bds_hopper_slot_transfer_probe import slot_script
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(kind='hopper', face=face, target='brewing_stand', fill=pattern, phase=0,
                  initial_source=[dict(id='minecraft:diamond', count=4), dict(id='minecraft:' + item, count=count)] + [None] * 3,
                  initial_target=[None] * 5)
             for face in (0, 2, 3, 4, 5)
             for pattern, item, count in (
                 ('reagent', 'nether_wart', 4), ('fuel', 'blaze_powder', 4),
                 ('potion', 'potion', 1), ('bottle', 'glass_bottle', 4))]
    capture(args, slot_script().replace('CASES', json.dumps(cases)), len(cases))
