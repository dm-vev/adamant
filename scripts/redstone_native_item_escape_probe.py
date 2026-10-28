#!/usr/bin/env python3
"""Execute original Actor::pushOutOfBlocks with supplied world/actor inputs.

World block flags, collision shapes, actor status/depenetration bits and height
offset are explicit inputs supplied by callbacks. The original function selects
the escape direction/motion and status transitions. This does not establish a
vanilla material's flags or the later actor-physics caller policy.
"""

import argparse
import ctypes as C
import hashlib
import itertools
import json
from pathlib import Path
import struct

from redstone_native_probe import ClientPE
from redstone_native_piston_shapes_probe import NativePistonShapes
from redstone_native_lightning_search_probe import BASE


class NativeItemEscape(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        for rva, size in ((0xE6258C8, 4), (0xE7CA4C0, 8), (0xE7D0280, 84)):
            self.map(rva, pe.read(rva, size))
        self.map(0x1A1E250, pe.read(0x1A1E250, 0x8BC), executable=True)
        self.actor, self.context, self.context_vt, self.ref = [self.buffer(n) for n in (0x228, 0x70, 0x68, 16)]
        self.state_vector, self.body = self.buffer(36), self.buffer(32)
        self.registry = self.buffer(0x78)
        self.buckets = (C.c_int64 * 8)(*([-1]*8))
        self.keep.append(self.buckets)
        self.put(self.registry, 0x48, "QQ", C.addressof(self.buckets), C.addressof(self.buckets)+64)
        self.put(self.actor, 0x10, "Q", C.addressof(self.registry))
        self.put(self.actor, 0x1C8, "QQ", C.addressof(self.context), C.addressof(self.ref))
        self.put(self.ref, 8, "ii", 100, 100)
        self.put(self.actor, 0x218, "QQ", C.addressof(self.state_vector), C.addressof(self.body))
        self.put(self.context, 0, "Q", C.addressof(self.context_vt))
        get_source = self.callback(C.c_void_p, [C.c_void_p], lambda _: C.addressof(self.source))
        self.put(self.context_vt, 0x60, "Q", C.cast(get_source, C.c_void_p).value)
        self.types, self.blocks = [], []
        for solid in (False, True):
            typ, block = self.buffer(0x170), self.buffer(0x70)
            self.put(typ, 0x164, "B", 4 if solid else 0)
            self.put(block, 0x68, "Q", C.addressof(typ))
            self.types.append(typ)
            self.blocks.append(block)
        self.current_type, self.current_block, self.current_vt = self.buffer(0x170), self.buffer(0x70), self.buffer(0x38)
        self.put(self.current_type, 0, "Q", C.addressof(self.current_vt))
        self.put(self.current_block, 0x68, "Q", C.addressof(self.current_type))
        get_block = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p], self.read_block)
        single = self.callback(C.c_void_p, [C.c_void_p]*6, self.single_shape)
        multi = self.callback(C.c_bool, [C.c_void_p]*7, self.multiple_shapes)
        self.put(self.source_vt, 0x10, "Q", C.cast(get_block, C.c_void_p).value)
        self.put(self.current_vt, 0x20, "Q", C.cast(single, C.c_void_p).value)
        self.put(self.current_vt, 0x30, "Q", C.cast(multi, C.c_void_p).value)
        set_bit = self.callback(None, [C.c_void_p, C.c_int, C.c_bool], lambda _, bit, value: self.bits.__setitem__(bit, value))
        get_bit = self.callback(C.c_bool, [C.c_void_p, C.c_int], lambda _, bit: self.bits.get(bit, False))
        status = self.callback(None, [C.c_void_p, C.c_int, C.c_bool], lambda _, bit, value: self.status.__setitem__(bit, value))
        offset = self.callback(C.c_float, [C.c_void_p], lambda _: self.case["height_offset"])
        release = self.callback(None, [C.c_void_p], lambda _: None)
        for rva, callback in ((0x253A220,set_bit),(0x253A120,get_bit),(0x1E14890,status),(0x253CEE0,offset),(0xE021350,release)):
            self.bridge(rva, C.cast(callback, C.c_void_p).value)
        self.function = C.WINFUNCTYPE(None, C.c_void_p, C.c_void_p)(BASE + 0x1A1E250)

    def read_block(self, _, position):
        pos = self.pos(position)
        if pos == (0,65,0):
            return C.addressof(self.current_block)
        delta = (pos[0],pos[1]-65,pos[2])
        directions = ((0,-1,0),(0,1,0),(0,0,-1),(0,0,1),(-1,0,0),(1,0,0))
        solid = delta in directions and bool(self.case["solid_neighbours"] & (1 << directions.index(delta)))
        return C.addressof(self.blocks[int(solid)])

    def single_shape(self, _, output, block, source, position, flags):
        C.memmove(output, struct.pack("<6f", *self.case["single_shape"]), 24)
        return output

    def multiple_shapes(self, _, block, source, position, query, output, flags):
        count = len(self.case["shapes"])
        start = C.addressof(self.shape_buffer)
        C.memmove(output, struct.pack("<QQQ", start, start+count*24, start+count*24), 24)
        return True

    def run(self, case):
        self.case, self.bits, self.status = case, {}, {}
        self.put(self.current_type, 0x164, "B", 4 if case["solid"] else 0)
        self.put(self.current_type, 0x130, "Q", case["material_bits"])
        self.put(self.body, 0, "6f", *case["body"])
        self.put(self.state_vector, 0, "9f", *case["point"], 0,0,0, *case["velocity"])
        point = (C.c_float * 3)(*case["point"])
        self.shape_buffer = C.create_string_buffer(struct.pack("<"+"f"*len(case["shapes"])*6, *[v for b in case["shapes"] for v in b]) or bytes(24))
        self.function(C.addressof(self.actor), C.addressof(point))
        return {**case, "motion": struct.unpack_from("<3f", self.state_vector, 24),
                "depenetration_bits": self.bits, "status_flags": self.status}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected image")
    native, cases = NativeItemEscape(pe), []
    try:
        for neighbours, fractional, material, shapes in itertools.product(range(64), (.125,.5,.875), (0,1),
                ([], [[0,65,0,1,66,1]], [[0,65,0,.5,66,1],[.5,65,0,1,66,1]])):
            point = [fractional,65+fractional,fractional]
            body = [fractional-.125,65+fractional,fractional-.125,fractional+.125,65+fractional+.25,fractional+.125]
            cases.append(native.run(dict(solid=True,solid_neighbours=neighbours,material_bits=material,
                point=point,body=body,velocity=[0,0,0],height_offset=.125,single_shape=[0,65,0,1,66,1],shapes=shapes)))
        for neighbours in range(64):
            cases.append(native.run(dict(solid=False,solid_neighbours=neighbours,material_bits=0,point=[.5,65.25,.5],
                body=[.375,65.25,.375,.625,65.5,.625],velocity=[.1,.2,.3],height_offset=.125,
                single_shape=[0,65,0,1,66,1],shapes=[])))
    finally:
        native.close()
    args.output.write_text(json.dumps({"complete":True,"reference":__doc__,"client_sha256":digest,
        "function":{"rva":"0x1a1e250","size":0x8BC,"sha256":hashlib.sha256(pe.read(0x1A1E250,0x8BC)).hexdigest()},
        "scenarios":cases},separators=(",",":"))+"\n",encoding="utf-8")
    print(json.dumps({"complete":True,"cases":len(cases)}))
