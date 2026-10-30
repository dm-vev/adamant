#!/usr/bin/env python3
"""Record inspected client tripwire geometry, scan bounds and tick constants.

This reads client data/instruction immediates with an image hash guard; it does
not execute callbacks. Original-BDS traces verify the corresponding world paths.
"""

import argparse
import ctypes
import hashlib
import json
from pathlib import Path
import struct

from redstone_native_probe import ClientPE, Evaluator


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    client = ClientPE(args.client)
    image_hash = hashlib.sha256(client.data).hexdigest()
    if image_hash != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected image")
    tables = [struct.unpack("<4f", client.read(rva, 16)) for rva in (0xE779850, 0xE8B3340, 0xE8B3350)]
    # Non-suspended shape writes four table floats then two instruction
    # immediates. Suspended shapes write x=0, four table floats, then z=1.
    grounded_tail = struct.unpack("<2f", client.read(0x4034F42, 8))
    boxes = {"grounded": [*tables[0], *grounded_tail],
             "suspended_free": [0, *tables[1], 1],
             "suspended_attached": [0, *tables[2], 1]}
    delays = {name: struct.unpack("<I", client.read(rva, 4))[0]
              for name, rva in (("string", 0x403789B), ("hook", 0x403607D))}
    limits = {"source_scan": client.read(0x40350AD, 1)[0],
              "hook_loop_end_exclusive": client.read(0x4035C08, 1)[0]}
    if delays != {"string": 10, "hook": 10} or limits != {"source_scan": 41, "hook_loop_end_exclusive": 42}:
        raise RuntimeError("inspected native query/delay constants changed")
    collision_rva = struct.unpack("<Q", client.read(0xE8B4960 + 0x20, 8))[0] - 0x140000000
    if collision_rva != 0xE1D20:
        raise RuntimeError("inspected tripwire collision virtual method changed")
    code = client.read(collision_rva, 0x12)
    evaluate = Evaluator(code)
    try:
        result = (ctypes.c_float * 6)(*[123.0] * 6)
        call = ctypes.WINFUNCTYPE(ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p)(evaluate.address)
        if call(None, ctypes.addressof(result)) != ctypes.addressof(result):
            raise RuntimeError("native collision method returned another result buffer")
        collision = {"rva": hex(collision_rva), "sha256": hashlib.sha256(code).hexdigest(), "bounds": list(result)}
    finally:
        evaluate.close()
    spans = {name: {"rva": hex(rva), "size": size, "sha256": hashlib.sha256(client.read(rva, size)).hexdigest()}
             for name, rva, size in (("visual_shape", 0x4034DA0, 0x247),
                                    ("source_update", 0x4034FF0, 0x31C),
                                    ("hook_calculate", 0x4035830, 0xEF5),
                                    ("contact_check", 0x4036F00, 0x9D7),
                                    ("player_destroy", 0x4035320, 0x47A),
                                    ("hook_sound", 0x4038B70, 0x4C8))}
    output = {"complete": True, "client_sha256": image_hash, "bounds": boxes,
              "recheck_ticks": delays, "limits": limits, "spans": spans,
              "collision": collision,
              "reference": "executed constant-empty collision getter; inspected geometry/delay/scan constants; no world callbacks executed"}
    args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
    print(json.dumps(output))
