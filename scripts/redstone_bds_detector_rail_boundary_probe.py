#!/usr/bin/env python3
"""Capture detector-rail settled selection at minecart AABB boundaries."""
import json

from redstone_bds_detector_rail_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(entity=entity, stacks=([[0, 'minecraft:diamond', 64]] if entity == 'minecraft:chest_minecart' else []),
                  phase=0, z=z)
             for entity in ('minecraft:minecart', 'minecraft:chest_minecart', 'minecraft:command_block_minecart')
             for z in (-.31, -.291, -.29, -.289, -.28, .1, .19, .2, .5, .8, .81, .9, 1.28, 1.289, 1.29, 1.291, 1.31)]
    script = SCRIPT.replace('cmd(\'setblock 0 65 0 unpowered_comparator',
                            'for(const z of [-2,-1,1,2])cmd(`setblock -1 65 ${z} rail ["rail_direction"=0]`);\n cmd(\'setblock 0 65 0 unpowered_comparator')
    script = script.replace('y:65.0625,z:.5', 'y:65.0625,z:c.z')
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
