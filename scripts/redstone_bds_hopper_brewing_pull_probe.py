#!/usr/bin/env python3
"""Capture bottom extraction from brewing slots into hopper inventory holes."""
import json

from redstone_bds_hopper_slot_pull_probe import pull_script
from redstone_bds_timing_probe import arguments, capture


def brewing_pull_cases():
    stack = lambda name, n: dict(id='minecraft:' + name, count=n)
    bottle, potion, reagent = stack('glass_bottle', 4), stack('potion', 1), stack('nether_wart', 4)
    stone = stack('stone', 64)
    profiles = (
        ('bottles', [reagent, bottle, bottle, bottle, None]),
        ('potions', [reagent, potion, potion, potion, None]),
        ('mixed', [reagent, bottle, potion, bottle, None]),
        ('ingredient_only', [reagent, None, None, None, None]),
    )
    destinations = (
        ('empty', [None] * 5),
        ('earlier_hole', [None, stack('glass_bottle', 60), stone, stone, stone]),
        ('matching_first', [stack('glass_bottle', 60), None, stone, stone, stone]),
        ('one_hole', [stone, stone, None, stone, stone]),
        ('full', [stone] * 5),
    )
    return [dict(kind='hopper', face=5, target='brewing_stand', fill=profile + '_' + pattern,
                 phase=0, pull=True, initial_source=receiver, initial_target=upper)
            for profile, upper in profiles for pattern, receiver in destinations]


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = brewing_pull_cases()
    capture(args, pull_script().replace('CASES', json.dumps(cases)), len(cases))
