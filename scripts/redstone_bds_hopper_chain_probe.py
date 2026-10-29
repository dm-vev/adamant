#!/usr/bin/env python3
"""Capture two-hop native hopper transfers across chunk boundaries and actor order."""
import json

from redstone_bds_hopper_transfer_pulse_probe import transfer_script
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    script = transfer_script()
    script = replace_once(script, 'const target=base.map',
                          'base[0]+=scenario.offset;base[2]+=scenario.offset;const target=base.map')
    script = replace_once(script, 'if(t===60+c.scenario.input_phase||t===76+c.scenario.input_phase)', 'if(false)')
    script = replace_once(script, 'set(base,scenario.kind+\' ["facing_direction"=\'+scenario.face+\']\');',
                          'if(!scenario.reverse)set(base,scenario.kind+\' ["facing_direction"=\'+scenario.face+\']\');')
    script = replace_once(script, 'if(scenario.face!==1)set([target[0],target[1]-1,target[2]],"stone");',
                          'const destination=[target[0],target[1]-1,target[2]];set(destination,"barrel");')
    script = replace_once(script, 'set(target,scenario.target);',
                          'set(target,scenario.target);if(scenario.reverse)set(base,scenario.kind+\' ["facing_direction"=\'+scenario.face+\']\');')
    script = replace_once(script, 'if(scenario.target==="hopper")', 'if(false)')
    script = replace_once(script, 'cells.push({scenario,base,target,source,lock,',
                          'cells.push({scenario,base,target,source,destination,lock,')
    script = replace_once(script, 'target_items:slots(c.target),',
                          'target_items:slots(c.target),destination:read(c.destination),destination_items:slots(c.destination),')
    script = replace_once(script, 'new ItemStack("minecraft:diamond",4)', 'new ItemStack("minecraft:diamond",c.scenario.count)')
    script = replace_once(script, 'c.load_control?.source[0]?.count!==4', 'c.load_control?.source[0]?.count!==c.scenario.count')
    cases = [dict(kind='hopper', target='hopper', face=face, count=count, fill='empty',
                  offset=offset, reverse=reverse, phase=0)
             for face in (0, 2, 3, 4, 5) for count in (1, 4, 16)
             for offset in (0, 15) for reverse in (False, True)]
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
