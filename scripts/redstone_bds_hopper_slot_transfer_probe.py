#!/usr/bin/env python3
"""Capture hopper source-slot selection and destination holes for real transfers."""
import json

from redstone_bds_dropper_container_probe import SCRIPT
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


def slot_script():
    script = replace_once(SCRIPT, 'if(t===60||t===76)', 'if(false)')
    script = replace_once(script,
        'inv(c.base).setItem(0,new ItemStack("minecraft:diamond",4));const target=inv(c.target);\n'
        '   if(c.scenario.fill!=="empty")for(let i=0;i<target.size;i++)target.setItem(i,new ItemStack("minecraft:diamond",64-(c.scenario.fill==="partial"&&i===target.size-1?1:0)));',
        'for(const [pos,items] of [[c.base,c.scenario.initial_source],[c.target,c.scenario.initial_target]]){\n'
        '    const inventory=inv(pos);if(items.length!==inventory.size)throw new Error("initial slot count mismatch");\n'
        '    for(let i=0;i<items.length;i++){const s=items[i];if(s)inventory.setItem(i,new ItemStack(s.id,s.count));}\n'
        '   }c.load_control={source:slots(c.base),target:slots(c.target)};')
    script = replace_once(script, 'if(c.samples[40].items[0]?.count!==4)', 'if(!c.load_control)')
    return script


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    script = slot_script()
    diamond = lambda n: dict(id='minecraft:diamond', count=n)
    dirt = lambda n: dict(id='minecraft:dirt', count=n)
    cases = []
    for target in ('barrel', 'chest', 'hopper'):
        size = 5 if target == 'hopper' else 27
        full = [diamond(64) for _ in range(size)]
        patterns = (
            ('blocked_first', [diamond(4), dirt(4), None, None, None], full[:-1] + [dirt(60)]),
            ('blocked_after_hole', [None, diamond(4), dirt(4), None, None], full[:-1] + [dirt(60)]),
            ('earlier_empty', [diamond(4), dirt(4), None, None, None], [None, diamond(60)] + full[2:]),
            ('different_first', [dirt(4), diamond(4), None, None, None], [None] * size),
        )
        for face in (0, 2, 3, 4, 5):
            for pattern, source, destination in patterns:
                cases.append(dict(kind='hopper', face=face, target=target, fill=pattern, phase=0,
                                  initial_source=source, initial_target=destination))
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
