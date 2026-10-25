#!/usr/bin/env python3
"""Compare command-driven delay changes with the Script API permutation path."""

from redstone_bds_timing_probe import arguments, capture
from redstone_bds_repeater_state_probe import script


if __name__ == "__main__":
    capture(arguments(), script(command_delay=True, delay_only=True), 8)
