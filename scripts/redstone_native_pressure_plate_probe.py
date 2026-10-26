#!/usr/bin/env python3
"""Execute the inspected plate state conversions and read contact-box constants.

Only relocation-free scalar methods execute; entity/world queries are not
executed by this probe and require the separate original-BDS captures.
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
        raise RuntimeError("client differs from the inspected image")
    methods = {}
    for name, rva, size in (("encode_binary", 0x9952600, 8), ("decode_binary", 0x9952610, 14),
                            ("decode_weighted", 0x2687840, 3), ("binary_delay", 0x2A50930, 6),
                            ("weighted_delay", 0x2CAE60, 6)):
        code = client.read(rva, size)
        evaluator = Evaluator(code)
        try:
            call = ctypes.WINFUNCTYPE(ctypes.c_int, ctypes.c_void_p, ctypes.c_int)(evaluator.address)
            cases = [[i, call(None, i)] for i in range(-1, 17)]
        finally:
            evaluator.close()
        methods[name] = {"rva": hex(rva), "sha256": hashlib.sha256(code).hexdigest(), "cases": cases}
    constants = {name: {"rva": hex(rva), "value": struct.unpack("<f", client.read(rva, 4))[0]}
                 for name, rva in (("one", 0xE5A7060), ("negative_inset", 0xE842590),
                                   ("height", 0xE61E140), ("inset", 0xE6ABFC4),
                                   ("untyped_query_min_expansion", 0xE6022D4),
                                   ("untyped_query_max_expansion", 0xE6022A8))}
    spans = {name: {"rva": hex(rva), "size": size, "sha256": hashlib.sha256(client.read(rva, size)).hexdigest()}
             for name, rva, size in (("sensitive_box", 0x2A51C30, 0xBE),
                                    ("binary_entity_query", 0x9952620, 0x21B),
                                    ("weighted_entity_query", 0x2DA5E20, 0xD8),
                                    ("typed_query_chunk_range", 0x2D85184, 0x74),
                                    ("untyped_query_chunk_range", 0x2D84717, 0xB0))}
    output = {"complete": True, "client_sha256": image_hash, "methods": methods,
              "constants": constants, "inspected_spans": spans,
              "reference": "executed scalar conversions/delays; inspected sensitive-box constants and query spans"}
    args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
    print(json.dumps({"complete": True, "methods": len(methods), "output": str(args.output)}))
