#!/usr/bin/env python3
"""Measure first hopper push versus inventory load offset and placement parity."""
import json
import os

from redstone_bds_hopper_transfer_pulse_probe import transfer_script
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    phase = int(os.environ.get('ADAMANT_HOPPER_SETUP_PHASE', '0'))
    if phase not in (0, 1):
        raise ValueError('setup phase must be 0 or 1')
    script = transfer_script()
    script = replace_once(script, 'if(t===40)', 'if(t===c.scenario.load)')
    script = replace_once(script, 'if(t===60+c.scenario.input_phase||t===76+c.scenario.input_phase)', 'if(false)')
    if phase:
        script = replace_once(script, 'if(system.currentTick&1)return;', 'if(!(system.currentTick&1))return;')
    cases = [dict(kind='hopper', face=face, target='barrel', fill='empty', load=load, phase=phase)
             for face in (0, 2, 3, 4, 5) for load in range(40, 48)]
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
