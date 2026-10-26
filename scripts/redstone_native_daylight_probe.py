#!/usr/bin/env python3
"""Retain inspected daylight instruction/data evidence, without executing callbacks."""

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
    constants = {hex(rva): struct.unpack("<f", client.read(rva, 4))[0] for rva in
                 (0xE7FEF84, 0xE7E14EC, 0xE6022B0, 0xE6AE484, 0xE6AE488,
                  0xE6022D0, 0xE6022AC, 0xE6AE454, 0xE5C6370, 0xE7E8804)}
    # The constructor writes its first four bounds as a vector followed by
    # the last two floats as an immediate qword. This is data inspection.
    bounds = list(struct.unpack("<4f", client.read(0xE779850, 16)))
    bounds.extend(struct.unpack("<2f", client.read(0x977EAE3, 8)))
    if bounds != [0, 0, 0, 1, .375, 1]:
        raise RuntimeError("inspected constructor bounds changed")
    spans = {}
    for name, rva, size in (
        ("DaylightDetectorBlock constructor bounds", 0x977EACD, 0x25),
        ("DaylightDetectorBlockActor::tick", 0x4ABFE70, 0x11D),
        ("DaylightDetectorBlock::use inspected span", 0x977F250, 0x3C0),
        ("DaylightDetectorBlock::updateSignalStrength", 0x977F7C0, 0x39B),
        ("Dimension::getTimeOfDay", 0x23122E0, 0xC6),
        ("Dimension::tick darkening inspected span", 0x230B0DE, 0x127),
        ("Scene graph update callback inspected span", 0x11A8580, 0x7F),
        ("BaseCircuitComponent::getStrength", 0x66D76D0, 0x5),
        ("BaseCircuitComponent::setStrength", 0x66D76E0, 0x5),
    ):
        spans[name] = {"rva": hex(rva), "size": size, "sha256": hashlib.sha256(client.read(rva, size)).hexdigest()}
    output = {"complete": True, "reference": "inspected client instructions/data; callbacks not executed",
              "client_sha256": digest, "constants": constants, "bounds": bounds, "spans": spans,
              "sine_table_rva": "0x11d6973c", "sine_table": "runtime initialized; not extracted or executed"}
    args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(args.output), "complete": True}))
