#!/usr/bin/env python3
"""Capture source-face overlap at the lower end of ten-rail slopes."""
import json

from redstone_bds_powered_rail_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(block='golden_rail',axis=axis,topology=topology,length=10,
                  phase=0,face=face,source_index=0 if topology=='rise' else 9)
             for axis in ('x','z') for topology in ('rise','fall') for face in range(6)]
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
