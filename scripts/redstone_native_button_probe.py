#!/usr/bin/env python3
"""Read inspected button geometry and instruction constants from the local client.

This records native data/disassembly evidence; it does not execute the methods
or supply a world-level trident/player interaction trace.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct

from redstone_native_probe import ClientPE


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    client = ClientPE(args.client)
    image_hash = hashlib.sha256(client.data).hexdigest()
    if image_hash != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from the inspected 1.26.51.1 image")
    tables = [0xE870580, 0xE870598, 0xE8705B0, 0xE8705C8, 0xE8705E0, 0xE8705F8]
    columns = [struct.unpack("<6f", client.read(rva, 24)) for rva in tables]
    immediate = lambda rva: struct.unpack("<I", client.read(rva, 4))[0]
    # Actual instruction immediates: collision guard and both actor queries.
    actors = {}
    for name, collision, query, expected in (
        ("minecraft:arrow", 0x3193177, 0x3194EE9, 0xC00050),
        ("minecraft:thrown_trident", 0x319318E, 0x3194F2C, 0xC00049),
    ):
        if immediate(collision) != expected or immediate(query) != expected:
            raise RuntimeError("inspected button actor constants changed")
        actors[name] = {"actor_type": expected, "collision_immediate_rva": hex(collision), "query_immediate_rva": hex(query)}
    delays = {"wood": immediate(0x3192D0C), "stone": immediate(0x3192D11)}
    if delays != {"wood": 30, "stone": 20} or [immediate(0x3195016), immediate(0x319501B)] != [30, 20]:
        raise RuntimeError("inspected press/recheck constants changed")
    spans = {}
    for name, rva, size in (
        ("ButtonBlock::getCollisionShape", 0x3193120, 0x30E),
        ("ButtonBlock::entityInside", 0x3195570, 0x16D),
        ("ButtonBlock::_checkPressed inspected span", 0x3194B00, 0x600),
    ):
        spans[name] = {"rva": hex(rva), "size": size, "sha256": hashlib.sha256(client.read(rva, size)).hexdigest()}
    output = {"complete": True, "reference": "inspected client data tables and instruction immediates; no methods executed",
              "client_sha256": image_hash, "box_table_rvas": [hex(x) for x in tables],
              "bounds": list(zip(*columns)), "colliding_actors": actors, "press_ticks": delays, "spans": spans}
    args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(args.output), "complete": True, "box_count": len(output["bounds"])}))
