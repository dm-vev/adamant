#!/usr/bin/env python3
"""Capture ascending/descending and mismatched-axis powered rail topology."""
import json

from redstone_bds_powered_rail_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(block=block, axis=axis, length=10, phase=phase, topology=topology)
             for block in ('golden_rail', 'activator_rail') for axis in ('x', 'z')
             for topology in ('flat', 'rise', 'fall', 'wrong_axis') for phase in (0, 1)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
