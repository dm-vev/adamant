#!/usr/bin/env python3
"""Retain inspected lightning callbacks, producer setup and rod shape data.

No world callback is executed by this script. The separate BDS probe exercises
real lightning entities and records the corresponding world output.
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
    digest = hashlib.sha256(client.data).hexdigest()
    if digest != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected 1.26.51.1 image")
    vtables = {}
    for name, rva in (("LightningRodBlock", 0xE8829D0), ("CopperLightningRodBlock", 0xE882DF0)):
        vtables[name] = {hex(slot): hex(struct.unpack("<Q", client.read(rva + slot, 8))[0] - 0x140000000)
                         for slot in (0x20, 0x50, 0x80, 0xA8)}
    columns = [struct.unpack("<6f", client.read(rva, 24)) for rva in
               (0xE886238, 0xE886220, 0xE886208, 0xE8861F0, 0xE8861D8, 0xE8861C0)]
    spans = {name: {"rva": hex(rva), "size": size,
                    "sha256": hashlib.sha256(client.read(rva, size)).hexdigest()}
             for name, rva, size in (
                 ("LightningRodBlock::tick", 0x3459110, 0x250),
                 ("LightningRodBlock::setupRedstoneComponent", 0x3459360, 0x2A0),
                 ("LightningRodBlock::onLightningHit", 0x3459C30, 0x3A1),
                 ("CopperLightningRodBlock::onLightningHit", 0x345A720, 0xAC),
                 ("LightningRodBlock::getVisualShape", 0x3459FE0, 0x1D8),
                 ("LightningBolt::normalTick", 0x95966B0, 0xDB0),
                 ("LevelChunk::tickImpl rod-position conversion", 0x32C8F33, 0x42),
                 ("SkeletonHorseTrapGoal::tick selector dispatch", 0xCCA23BC, 0x9D),
                 ("LevelChunk::getTopRainBlockPos", 0x32DCBF0, 0x3F9),
                 ("BlockType::getCollisionShape visual dispatch", 0x3182B7B, 0x1B))}
    output = {"complete": True, "client_sha256": digest, "vtables": vtables,
              "visual_bounds_by_facing": list(zip(*columns)), "spans": spans,
              "reference": "inspected instructions/data; no world callbacks executed"}
    args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(args.output), "complete": True}))
