#!/usr/bin/env python3
"""Capture powered/activator rails fed by wire and repeater components."""
import json

from redstone_bds_powered_rail_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(block=block, axis=axis, length=length, phase=phase, input=source)
             for block in ('golden_rail', 'activator_rail') for axis in ('x', 'z')
             for length in (1, 9) for phase in (0, 1) for source in ('wire', 'repeater')]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
