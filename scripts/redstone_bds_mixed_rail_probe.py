#!/usr/bin/env python3
"""Capture rail-family barriers and active/inactive detector-rail sources."""
import json

from redstone_bds_powered_rail_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = []
    for block in ('golden_rail','activator_rail'):
        other = 'activator_rail' if block=='golden_rail' else 'golden_rail'
        for middle,powered in ((block,False),(other,False),('rail',False),('detector_rail',False),('detector_rail',True)):
            for phase in (0,1):
                cases.append(dict(block=block,axis='x',length=10,phase=phase,middle_block=middle,middle_powered=powered))
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
