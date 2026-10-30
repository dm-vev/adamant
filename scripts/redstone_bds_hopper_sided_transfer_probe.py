#!/usr/bin/env python3
"""Capture actual hopper insertion into furnace inputs and fuel slots."""
import json

from redstone_bds_hopper_slot_transfer_probe import slot_script
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    stack = lambda name: dict(id='minecraft:' + name, count=4)
    patterns = (
        ('reject_then_fuel', ['diamond', 'coal']),
        ('reject_then_input', ['diamond', 'raw_iron']),
        ('input_then_fuel', ['raw_iron', 'coal']),
        ('fuel_then_input', ['coal', 'raw_iron']),
    )
    cases = [dict(kind='hopper', face=face, target=target, fill=pattern, phase=0,
                  initial_source=[stack(name) for name in items] + [None] * 3,
                  initial_target=[None] * 3)
             for target in ('furnace', 'blast_furnace', 'smoker')
             for face in (0, 2, 3, 4, 5) for pattern, items in patterns]
    capture(args, slot_script().replace('CASES', json.dumps(cases)), len(cases))
