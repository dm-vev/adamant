#!/usr/bin/env python3
"""Retain native setblock/fill mode behavior with existing and invalid doors."""
from redstone_bds_door_command_probe import CONTEXTS, capture
from redstone_bds_timing_probe import arguments


CASES = [dict(block=b, command=command, context=context, top=top, direction=0,
              open=False, hinge=False, mode=mode)
         for b in ('wooden_door', 'copper_door', 'iron_door')
         for command, modes in (('setblock', ('replace', 'keep', 'destroy')),
                                ('fill', ('replace', 'keep', 'destroy', 'hollow', 'outline')))
         for mode in modes for context in CONTEXTS for top in (False, True)]


if __name__ == '__main__':
    capture(arguments(), CASES)
