#!/usr/bin/env python3
"""Capture native button/lever click packets through the local player client."""

from redstone_bds_switch_use_probe import capture_player_use
from redstone_bds_timing_probe import arguments


if __name__ == "__main__":
    cases = [{"block": block, "pattern": "repeat" if block == "lever" else "single",
              "facing": 1, "direction": "up_north_south", "request_phase": i & 1}
             for i, block in enumerate(("stone_button", "wooden_button", "lever"))]
    capture_player_use(arguments(), cases, require_parities=False)
