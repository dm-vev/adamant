#!/usr/bin/env python3
"""Record native door survival drops, entity spawn events and per-tick item counts."""
from redstone_bds_door_support_probe import CASES as SUPPORT_CASES, capture
from redstone_bds_timing_probe import arguments


CASES = [dict(c, drops=drops) for c in SUPPORT_CASES for drops in (False, True)]


if __name__ == '__main__':
    capture(arguments(), CASES)
