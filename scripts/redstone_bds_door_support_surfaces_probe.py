#!/usr/bin/env python3
"""Compare native door survival on full, partial and transparent support surfaces."""
from redstone_bds_door_support_probe import capture
from redstone_bds_timing_probe import arguments


SURFACES = [dict(id=name) for name in (
    'stone', 'glass', 'oak_leaves', 'slime', 'honey_block', 'packed_ice',
    'farmland', 'grass_path', 'soul_sand', 'soul_soil', 'oak_fence',
    'cobblestone_wall', 'glass_pane', 'white_carpet', 'oak_double_slab')]
SURFACES += [dict(id='oak_slab', states={'minecraft:vertical_half':half}) for half in ('bottom','top')]
SURFACES += [dict(id='oak_stairs', states={'upside_down_bit':top,'weirdo_direction':0}) for top in (False,True)]
SURFACES += [dict(id='snow_layer', states={'height':height,'covered_bit':False}) for height in (0,6,7)]
CASES = [dict(block=block, action='surface', phase=phase, support=surface, foundation=True)
         for block in ('wooden_door', 'copper_door', 'iron_door')
         for surface in SURFACES for phase in (0,1)]


if __name__ == '__main__':
    capture(arguments(), CASES)
