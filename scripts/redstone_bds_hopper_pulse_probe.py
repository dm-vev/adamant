#!/usr/bin/env python3
"""Capture hopper lock publication for direct/conducted pulses on six faces."""
import json

from redstone_bds_dispenser_pulse_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(kind='hopper', face=face, conducted=conducted, phase=phase, edges=edges)
             for face in range(6) for conducted in (False, True) for phase in (0, 1)
             for edges in ([0, 1], [0, 2], [0, 4], [0, 4, 6, 10], [0, 4, 8, 12], [0, 4, 16, 20])]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
