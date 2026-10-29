#!/usr/bin/env python3
"""Capture actual hopper pushes across lock/unlock at both input parities."""
import json

from redstone_bds_dropper_container_probe import SCRIPT
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


def transfer_script():
    script = replace_once(SCRIPT, 'if(t===60||t===76){set(c.source,t===60?',
                          'if(t===60+c.scenario.input_phase||t===76+c.scenario.input_phase){set(c.source,t===60+c.scenario.input_phase?')
    script = replace_once(script, 'kind:t===60?"power":"off"',
                          'kind:t===60+c.scenario.input_phase?"power":"off"')
    script = replace_once(script, 'c.changes.push({tick:system.currentTick,kind:"load"});',
                          'c.load_control={source:slots(c.base),target:slots(c.target)};'
                          'c.changes.push({tick:system.currentTick,kind:"load"});')
    script = replace_once(script, 'c.samples[40].items[0]?.count!==4',
                          'c.load_control?.source[0]?.count!==4')
    return script


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    script = transfer_script()
    cases = [dict(kind='hopper', face=face, target='barrel', fill=fill, phase=0, input_phase=phase)
             for face in (0, 2, 3, 4, 5) for fill in ('empty', 'full', 'partial') for phase in (0, 1)]
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
