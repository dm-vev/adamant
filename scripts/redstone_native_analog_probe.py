#!/usr/bin/env python3
"""Execute the inspected campfire analog virtual methods from the local client."""

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
        raise RuntimeError("client differs from the inspected 1.26.51.1 image")
    records = {}
    # CampfireBlock inherits both constant-zero BlockType virtual methods.
    # These call-free three-byte methods ignore all arguments. Inspecting the
    # actual vtable is essential: absence of a named override is insufficient.
    for name, offset in (("hasComparatorSignal", 0x2D0), ("getComparatorSignal", 0x2D8)):
        address = struct.unpack("<Q", client.read(0xE86F390 + offset, 8))[0] - 0x140000000
        if address != 0x76C20:
            raise RuntimeError("campfire virtual method differs from disassembly")
        code = client.read(address, 3)
        if hashlib.sha256(code).hexdigest() != "251447ee91a9067dcd6ab96703133f617565974cd6c4819021760c4688c91abf":
            raise RuntimeError("constant-zero function differs from disassembly")
        evaluate = Evaluator(code)
        try:
            result = int(evaluate(ctypes.create_string_buffer(1)))
        finally:
            evaluate.close()
        records[name] = {"rva": hex(address), "sha256": hashlib.sha256(code).hexdigest(), "result": result}
    output = {"complete": True, "client_sha256": image_hash,
              "reference": "executed client CampfireBlock vtable methods; no world callbacks executed",
              "campfire_vtable_rva": "0xe86f390", "methods": records}
    args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
    print(json.dumps(output))
