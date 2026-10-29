#!/usr/bin/env python3
"""Execute original hopper query shrink/body tests and container-type getters.

One supplied actor bypasses BlockSource's chunk/type prefilter. Instructions
through the original geometric accept branch are unchanged. At that branch a
probe-only marker replaces item transfer and jumps to the original loop tail.
This measures geometry only, not successful inventory insertion or chunk order.
No live process or on-disk client is modified.
"""
import argparse
import ctypes as C
import hashlib
import itertools
import json
from pathlib import Path
import struct

from redstone_native_probe import ClientPE
from redstone_native_hopper_shapes_probe import NativeHopperShapes
from redstone_native_lightning_search_probe import BASE


class NativePickup(NativeHopperShapes):
    def __init__(self, pe):
        super().__init__(pe)
        self.actor, self.actor_box, self.actors, self.accepted = [self.buffer(n) for n in (0x228, 24, 8, 1)]
        self.put(self.actor, 0x220, 'Q', C.addressof(self.actor_box))
        self.put(self.actors, 0, 'Q', C.addressof(self.actor))

        def fetch(_source, result, _kind, _query, _excluded, cache):
            self.put(self.accepted, 0, 'B', 0)
            C.memmove(result, struct.pack('<QQ', 1, C.addressof(self.actors)), 16)
            C.memset(cache, 0, 56)
            return result

        callback = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p, C.c_uint32, C.c_void_p, C.c_void_p, C.c_void_p], fetch)
        self.put(self.source_vt, 0xF0, 'Q', C.cast(callback, C.c_void_p).value)
        self.marker = b'\x48\xb8' + struct.pack('<Q', C.addressof(self.accepted)) + b'\xc6\x00\x01\xe9' + struct.pack('<i', 0x2061370 - (0x206125E + 18))
        self.map(0x206125E, self.marker, executable=True)

    def intersects(self, position, body):
        self.above_container, self.queries = False, []
        self.put(self.hopper, 9, 'B', 0)
        self.put(self.actor_box, 0, '6f', *body)
        point = (C.c_float * 3)(*position)
        result = self.query_function(C.addressof(self.hopper), C.addressof(self.source), C.addressof(self.block), C.addressof(point))
        if result:
            raise RuntimeError('geometry-only probe unexpectedly transferred an item')
        return bool(self.accepted.raw[0])


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise ValueError('client differs from inspected image')
    native = NativePickup(pe)
    f32 = lambda x: C.c_float(x).value
    cases, flags = [], []
    try:
        for anchor, y in itertools.product((0, -17, 1000000, 16777215, -29999999, 29999999), (65, 319)):
            position = [anchor, y, anchor]
            # Query arithmetic is evaluated by the original instructions for
            # each input; these centres merely select supplied test bodies.
            for axis, side, offset in itertools.product(range(3), (0, 1), (-.1251, -.125, -.1249, -.0001, 0, .0001, .1249, .125, .1251)):
                center = [f32(anchor + .5), f32(y + 1.3125), f32(anchor + .5)]
                boundary = (y + (.625 if side == 0 else 2)) if axis == 1 else anchor + side
                center[axis] = f32(boundary + offset)
                body = [f32(v - .125) for v in center] + [f32(v + .125) for v in center]
                cases.append(dict(position=position, body=body, intersects=native.intersects(position, body)))
        tables = dict(barrel=0xE843340, blast_furnace=0xE845EE0, brewing_stand=0xE86C5C0,
                      chest=0xEA426F0, copper_chest=0xE9CD030, dispenser=0xEA6D7B0,
                      dropper=0xEA6F9E0, ender_chest=0xE9FC1A0, furnace=0xE9FFE00,
                      hopper=0xE999F50, lectern=0xE881CF0, shulker_box=0xEAAB8F0,
                      smoker=0xEAAD650, crafter=0xE9CF650, chiseled_bookshelf=0xEA42B50,
                      composter=0xEA45070, decorated_pot=0xEA6C700, jukebox=0xE99C0A0)
        for name, table in tables.items():
            rva = struct.unpack('<Q', pe.read(table + 0xB8, 8))[0] - BASE
            code = pe.read(rva, 3)
            if code not in (bytes.fromhex('b001c3'), bytes.fromhex('31c0c3')):
                raise ValueError(f'{name}: nonconstant container getter {rva:x}: {code.hex()}')
            native.map(rva, code, executable=True)
            value = C.WINFUNCTYPE(C.c_bool, C.c_void_p)(BASE + rva)(C.addressof(native.above_type))
            flags.append(dict(block='minecraft:' + name, vtable=hex(table), getter=hex(rva), code=code.hex(), container=value))
    finally:
        native.close()
    functions = [dict(rva=hex(rva), size=size, sha256=hashlib.sha256(pe.read(rva, size)).hexdigest())
                 for rva, size in ((0x2060EB0, 0x672), (0x2062B80, 0x137))]
    args.output.write_text(json.dumps(dict(complete=True, client_sha256=digest, reference=__doc__, functions=functions,
                                         marker_rva='0x206125e', loop_tail='0x2061370', scenarios=cases, container_flags=flags), separators=(',', ':')) + '\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, scenarios=len(cases), container_flags=len(flags))))
