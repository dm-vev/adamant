#!/usr/bin/env python3
"""Capture hook permutation edits, supported rotations and explicit powered setup.

Uses the lifecycle circuit with an additional direct wire and south support, so
electrical output remains observable when the hook rotates to face north.
"""

import itertools
import json

from redstone_bds_hook_lifecycle_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


CASES = [{"mode": mode, "powered": powered, "filter": frequency, "phase": phase,
          "permutations": True}
         for mode, powered, frequency, phase in itertools.product(
             ("replace_on", "replace_off", "rotate_supported", "rotate_on",
              "edit_on", "edit_off", "edit_detached", "edit_idle"),
             (False, True), (9, 10), (0, 1))]


if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
