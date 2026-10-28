#!/usr/bin/env python3
"""Observe native cart dispensing with no rail, an obstruction or a lower rail."""
import json

from redstone_bds_dispenser_cart_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(item=kind,face=face,slope=False,track=track,phase=0)
             for kind in ('minecraft:minecart','minecraft:chest_minecart','minecraft:hopper_minecart',
                          'minecraft:tnt_minecart','minecraft:command_block_minecart')
             for face in (2,3,4,5) for track in ('none','blocked','below')]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
