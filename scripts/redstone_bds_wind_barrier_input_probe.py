#!/usr/bin/env python3
"""Observe wind attacks with independent lower and upper barrier consumers."""
import json

from redstone_bds_barrier_probe import barrier_cases
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture
from redstone_bds_wind_material_probe import SCRIPT


def barrier_input_script(cases):
    script = SCRIPT.replace('CASES', json.dumps(cases))
    script = replace_once(script,
        'cmd(`setblock ${base.join(" ")} stone`);cmd(`setblock ${target[0]} 64 ${target[2]} ${scenario.powered?"redstone_block":"stone"}`);',
        'cmd(`setblock ${base.join(" ")} stone`);'
        'const lower=[target[0]+1,65,target[2]],upper=[target[0]+1,66,target[2]];'
        'const sources=scenario.feed==="lower"?[lower]:scenario.feed==="upper"?[upper]:[lower,upper];'
        'for(const p of sources)cmd(`setblock ${p.join(" ")} ${scenario.powered?"redstone_block":"air"}`);')
    script = replace_once(script,
        'const positions=[base,target,[target[0],66,target[2]],[target[0],64,target[2]]];',
        'const positions=[base,target,[target[0],66,target[2]],[target[0],64,target[2]],...sources];')
    return replace_once(script, 'cells.push({scenario,base,target,positions,',
        'cells.push({scenario,base,target,positions,sources,')


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = barrier_cases()
    capture(args, barrier_input_script(cases), len(cases))
