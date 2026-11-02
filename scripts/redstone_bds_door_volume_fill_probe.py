#!/usr/bin/env python3
"""Record native door fill batches, automatic upper cells and mode ordering."""
from redstone_bds_door_command_probe import capture
from redstone_bds_timing_probe import arguments


CASES = [dict(block=b, command='fill', context='clear', top=False, direction=0,
              open=False, hinge=False, mode=mode, size=size)
         for b in ('wooden_door', 'copper_door', 'iron_door')
         for mode in ('replace', 'keep', 'destroy', 'hollow', 'outline')
         for size in ((1, 4, 1), (2, 1, 2), (3, 3, 3))]


if __name__ == '__main__':
    capture(arguments(), CASES)
