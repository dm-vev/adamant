#!/usr/bin/env python3
"""Record native survival after direct API copper variant replacements."""
from redstone_bds_door_support_probe import capture
from redstone_bds_timing_probe import arguments


CASES = [dict(block=b, replacement=r, action='variant', phase=0, top=top)
         for b, r in (('copper_door', 'exposed_copper_door'),
                      ('copper_door', 'waxed_copper_door'),
                      ('waxed_copper_door', 'copper_door'))
         for top in (False, True)]


if __name__ == '__main__':
    capture(arguments(), CASES)
