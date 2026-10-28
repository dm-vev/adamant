#!/usr/bin/env python3
"""Capture six source faces around flat and sloped native rail components."""
import json

from redstone_bds_powered_rail_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(block='golden_rail',axis=axis,topology=topology,length=1 if topology=='flat' else 3,
                  phase=0,face=face,source_index=0 if topology=='flat' else 1)
             for axis in ('x','z') for topology in ('flat','rise','fall') for face in range(6)]
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
