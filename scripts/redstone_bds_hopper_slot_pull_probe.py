#!/usr/bin/env python3
"""Capture pulling past rejected source stacks into hopper inventory holes."""
import json

from redstone_bds_hopper_slot_transfer_probe import slot_script
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


def pull_script():
    script = replace_once(slot_script(),
        'const target=base.map((x,j)=>x+v[j]),source=base.map((x,j)=>x-v[j]);',
        'const target=[base[0],base[1]+1,base[2]],source=[base[0]-1,base[1],base[2]];')
    script = replace_once(script,
        'if(scenario.face!==1)set([target[0],target[1]-1,target[2]],"stone");', '')
    return script


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    script = pull_script()
    diamond = lambda n: dict(id='minecraft:diamond', count=n)
    dirt = lambda n: dict(id='minecraft:dirt', count=n)
    cases = []
    for target in ('barrel', 'chest', 'hopper'):
        size = 5 if target == 'hopper' else 27
        full = [diamond(64) for _ in range(5)]
        patterns = (
            ('blocked_first', [diamond(4), dirt(4)] + [None] * (size - 2), full[:-1] + [dirt(60)]),
            ('blocked_after_hole', [None, diamond(4), dirt(4)] + [None] * (size - 3), full[:-1] + [dirt(60)]),
            ('earlier_empty', [diamond(4), dirt(4)] + [None] * (size - 2), [None, diamond(60)] + full[2:]),
            ('different_first', [dirt(4), diamond(4)] + [None] * (size - 2), [None] * 5),
        )
        for pattern, upper, receiver in patterns:
            # Fixture source/target names refer to base/upper positions, not
            # the direction in which items flow in these pull scenes.
            cases.append(dict(kind='hopper', face=5, target=target, fill=pattern, phase=0,
                              pull=True, initial_source=receiver, initial_target=upper))
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
