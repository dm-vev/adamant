#!/usr/bin/env python3
"""Capture the first cart movement on both sides of dispenser chunk boundaries."""
import json

from redstone_bds_dispenser_cart_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(item=kind,face=face,slope=True,offset=offset,phase=0)
             for kind in ('minecraft:minecart','minecraft:chest_minecart')
             for face in (2,3,4,5) for offset in (0,1,7,14,15)]
    old = 'base=[64+i%2*128,65,64+Math.floor(i/2)*128]'
    new = 'base=[64+i%2*128+scenario.offset,65,64+Math.floor(i/2)*128+scenario.offset]'
    if old not in SCRIPT:
        raise ValueError('native dispenser base contract changed')
    capture(args, SCRIPT.replace(old,new).replace('CASES', json.dumps(cases)), len(cases))
