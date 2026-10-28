#!/usr/bin/env python3
"""Capture live flag edits around the rail circuit publication tick."""
import json

from redstone_bds_powered_rail_lifecycle_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(block=block,mode='flag',powered=False,phase=phase,edit_offset=offset,flag=flag)
             for block in ('golden_rail','activator_rail') for phase in (0,1)
             for offset in (0,1,2,3) for flag in (False,True)]
    script = SCRIPT.replace('t===40+phase','t===56+c.scenario.edit_offset+phase')
    script = script.replace('case "flag":place(c,target,false,1);','case "flag":place(c,target,c.scenario.flag,1);')
    capture(args,script.replace('CASES',json.dumps(cases)),len(cases))
