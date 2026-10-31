#!/usr/bin/env python3
"""Capture wind barrier packet delivery between original-BDS Script markers."""
from redstone_bds_barrier_clock_probe import capture_clock
from redstone_bds_timing_probe import arguments

if __name__ == '__main__':
    capture_clock(arguments(), wind=True)
