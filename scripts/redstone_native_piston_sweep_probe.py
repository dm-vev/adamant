#!/usr/bin/env python3
"""Execute original client AABB clipping and swept movement on authored inputs.

This isolates movement/depenetration arithmetic from actor state, world shape
collection and caller-selected penetration limits. No movement implementation
is supplied by Python, and no live process is modified.
"""

import argparse
import ctypes as C
import hashlib
import itertools
import json
from pathlib import Path
import random
import struct

from redstone_native_probe import ClientPE
from redstone_native_piston_shapes_probe import NativePistonShapes
from redstone_native_lightning_search_probe import BASE


class NativePistonSweep(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        for rva, size in ((0xE5C6380, 16), (0xE69DC98, 4), (0xE5E9578, 4), (0xE780AC0, 8), (0xE6258C8, 4), (0xE6D7D88,12), (0xE6D8608,12)):
            self.map(rva, pe.read(rva, size))
        self.map(0x3327D80, pe.read(0x3327D80, 0x463), executable=True)
        self.map(0x82A4890, pe.read(0x82A4890, 0x334), executable=True)
        self.map(0x9027730, pe.read(0x9027730, 0x7B), executable=True)
        self.clip = C.WINFUNCTYPE(C.c_void_p, *([C.c_void_p] * 4))(BASE + 0x3327D80)
        self.sweep = C.WINFUNCTYPE(C.c_void_p, *([C.c_void_p] * 5))(BASE + 0x82A4890)
        self.configure = C.WINFUNCTYPE(None, *([C.c_void_p] * 3))(BASE + 0x9027730)

    def policy(self, bits, one_way_blocks, minimum, override):
        dep, request = C.create_string_buffer(56), C.create_string_buffer(152)
        struct.pack_into("<I3f",dep,0,bits,*minimum)
        struct.pack_into("<QQ",dep,16,0,24 if one_way_blocks else 0)
        if override is not None:
            struct.pack_into("<3f?",dep,40,*override,True)
        self.configure(0,C.addressof(dep),C.addressof(request))
        return dict(bits=bits,one_way_blocks=one_way_blocks,minimum=minimum,override=override,
                    limits=struct.unpack_from("<3f",request,72))

    def run(self, case):
        # These buffers need live only until the original calls return.
        body = (C.c_float * 6)(*case["body"])
        moves = (C.c_float * max(3, len(case["moves"]) * 3))(*[v for point in case["moves"] for v in point])
        obstacles = (C.c_float * max(6, len(case["obstacles"]) * 6))(*[v for box in case["obstacles"] for v in box])
        span = (C.c_uint64 * 2)(len(case["moves"]), C.addressof(moves))
        begin = C.addressof(obstacles)
        vector = (C.c_uint64 * 3)(begin, begin + len(case["obstacles"]) * 24, begin + len(case["obstacles"]) * 24)
        limits = (C.c_float * 3)(*case["limits"])
        result, clip_result = C.create_string_buffer(40), C.create_string_buffer(32)
        self.sweep(C.addressof(result), C.addressof(span), C.addressof(body), C.addressof(vector), C.addressof(limits))
        values = struct.unpack("<10f", result)
        clips = []
        if case["moves"]:
            for n in range(len(case["obstacles"])):
                self.clip(C.addressof(clip_result), begin + n * 24, C.addressof(body), C.addressof(moves))
                values_clip = struct.unpack("<7fi", clip_result)
                clips.append(dict(penetration=values_clip[0], clipped=values_clip[1:4], depenetrating=values_clip[4:7], axis=values_clip[7]))
        return {**case, "move": values[:3], "penetration": values[3], "shape": values[4:], "clips": clips}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected image")
    native, cases, policies = NativePistonSweep(pe), [], []
    try:
        for anchor, size, face, gap, speed, limit in itertools.product(
                ((0,0,0), (.5,65,.5), (1000000,65,-1000000)), ((1,1,1), (.25,.25,.25), (.5,1.975,.5)),
                range(6), (-.5, 0, .5, 1), (0, .25, 1), (0, .25, 1)):
            direction = [(0,-1,0),(0,1,0),(0,0,-1),(0,0,1),(-1,0,0),(1,0,0)][face]
            body = [*anchor, *[anchor[i]+size[i] for i in range(3)]]
            low = [anchor[i]+direction[i]*gap for i in range(3)]
            obstacle = [*low, *[v+1 for v in low]]
            cases.append(native.run(dict(body=body, moves=[[speed*v for v in direction]], obstacles=[obstacle], limits=[limit]*3)))
        rng = random.Random(2651)
        for _ in range(128):
            obstacles = []
            for _ in range(rng.randrange(5)):
                low = [rng.randrange(-4, 5)*.25 for _ in range(3)]
                obstacles.append([*low, *[v+rng.choice((.25, .5, 1)) for v in low]])
            moves = [[rng.randrange(-4, 5)*.25 for _ in range(3)] for _ in range(rng.randrange(4))]
            cases.append(native.run(dict(body=[0,0,0,.5,1.975,.5], moves=moves, obstacles=obstacles, limits=[rng.choice((0,.25,1)) for _ in range(3)])))
        for bits, one_way, minimum, override in itertools.product(range(32),(False,True),
                ((0,0,0),(.25,.5,1.5),(2,.125,.75)),(None,(.25,1.5,.5),(2,.25,.125))):
            policies.append(native.policy(bits,one_way,minimum,override))
    finally:
        native.close()
    args.output.write_text(json.dumps({"complete": True, "reference": __doc__, "client_sha256": digest,
        "functions": [{"rva": hex(rva), "size": size, "sha256": hashlib.sha256(pe.read(rva, size)).hexdigest()}
                      for rva, size in ((0x3327D80,0x463),(0x82A4890,0x334),(0x9027730,0x7B))],
        "policies":policies,"scenarios": cases}, separators=(",", ":")) + "\n", encoding="utf-8")
    print(json.dumps({"complete": True, "cases": len(cases)}))
