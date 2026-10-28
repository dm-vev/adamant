#!/usr/bin/env python3
"""Observe the command-block cart's native one-slot analog container."""
import json

from redstone_bds_detector_rail_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(entity='minecraft:command_block_minecart', phase=0, stacks=stacks)
             for stacks in ([], [[0, 'minecraft:diamond', 1]], [[0, 'minecraft:diamond', 64]],
                            [[0, 'minecraft:ender_pearl', 16]], [[0, 'minecraft:shield', 1]])]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
