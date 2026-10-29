#!/usr/bin/env python3
"""Capture hopper pickup underneath empty containers and ordinary solid blocks."""
import json

from redstone_bds_hopper_item_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    # A container blocks world pickup even while empty; a solid block alone
    # does not. Hopper-above is reserved for a multi-actor inventory capture.
    blocks = ('stone', 'glass', 'chest', 'ender_chest', 'barrel', 'furnace',
              'blast_furnace', 'smoker', 'brewing_stand', 'dispenser', 'dropper',
              'crafter', 'shulker_box', 'chiseled_bookshelf', 'composter',
              'decorated_pot', 'jukebox', 'lectern')
    cases = [dict(initial=0, count=3, profile='cover/' + name,
                  above='minecraft:' + name, dx=0, dy=.7, destination=destination,
                  locked=False, phase=0)
             for name in blocks for destination in (False, True)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
