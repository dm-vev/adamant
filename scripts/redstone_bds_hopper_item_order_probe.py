#!/usr/bin/env python3
"""Capture native hopper slot order and simultaneous dropped-stack selection."""
import json

from redstone_bds_hopper_item_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    diamond = lambda n: dict(id='minecraft:diamond', count=n)
    dirt = lambda n: dict(id='minecraft:dirt', count=n)
    layouts = [
        [None, diamond(60), None, dirt(64), None],
        [diamond(63), None, diamond(60), None, dirt(64)],
    ]
    cases = [dict(initial=0, initial_slots=slots, count=64,
                  profile=f'holes{index}/extra{time}', dx=0, dy=1.,
                  destination=destination, locked=False, phase=0,
                  extra=[dict(time=time, id='minecraft:dirt', count=3)])
             for index, slots in enumerate(layouts)
             for destination in (False, True) for time in (40, 44)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
