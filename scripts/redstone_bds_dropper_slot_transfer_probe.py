#!/usr/bin/env python3
"""Capture dropper insertion before a later matching stack in a container."""
import json

from redstone_bds_hopper_slot_transfer_probe import slot_script
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    script = replace_once(slot_script(), 'if(false)', 'if(t===60||t===76)')
    diamond = lambda n: dict(id='minecraft:diamond', count=n)
    cases = []
    for target in ('barrel', 'chest', 'hopper'):
        size = 5 if target == 'hopper' else 27
        for face in range(6):
            for pattern, initial in (
                ('earlier_empty', [None, diamond(60)] + [None] * (size - 2)),
                ('matching_first', [diamond(60), None] + [None] * (size - 2)),
            ):
                cases.append(dict(kind='dropper', face=face, target=target, fill=pattern, phase=0,
                                  initial_source=[diamond(4)] + [None] * 8, initial_target=initial))
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
