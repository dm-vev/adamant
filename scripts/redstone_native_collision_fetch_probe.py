#!/usr/bin/env python3
"""Execute original BlockSource collision-fetch traversal with supplied chunks.

The original client calculates cell bounds and traversal order. Host callbacks
supply loaded subchunks and empty block collision shapes; world borders are
explicitly disabled through GetCollisionShapeInterface. No game is modified.
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


class NativeCollisionFetch(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        self.map(0xE5E9578, pe.read(0xE5E9578, 4))
        self.map(0x2D87730, pe.read(0x2D87730, 0xB94), executable=True)
        self.region, self.region_vt = self.buffer(0x40), self.buffer(0x148)
        self.chunk, self.subchunks = self.buffer(0x150), self.buffer(25 * 0x68)
        self.storage, self.storage_vt = self.buffer(8), self.buffer(0x20)
        self.context, self.context_vt = self.buffer(8), self.buffer(0x48)
        self.put(self.region, 0, 'Q', C.addressof(self.region_vt))
        self.put(self.region, 0x3A, 'h', -64)
        self.put(self.chunk, 0x140, 'QQ', C.addressof(self.subchunks), C.addressof(self.subchunks) + len(self.subchunks))
        for n in range(25):
            self.put(self.subchunks, n * 0x68 + 0x30, 'Q', C.addressof(self.storage))
        self.put(self.storage, 0, 'Q', C.addressof(self.storage_vt))
        self.put(self.context, 0, 'Q', C.addressof(self.context_vt))
        self.cells = []
        def collision(_typ, _block, _source, position, _query, _output, _context):
            self.cells.append(list(struct.unpack('<3i', C.string_at(position, 12))))
            return False
        callbacks = (
            (self.region_vt, 0x118, self.callback(C.c_int16, [C.c_void_p], lambda _: -64)),
            (self.region_vt, 0x120, self.callback(C.c_int16, [C.c_void_p], lambda _: 320)),
            (self.region_vt, 0x140, self.callback(C.c_void_p, [C.c_void_p] * 2, lambda *_: C.addressof(self.chunk))),
            (self.storage_vt, 0x18, self.callback(C.c_void_p, [C.c_void_p, C.c_uint16], lambda *_: C.addressof(self.wrapped))),
            (self.context_vt, 0x40, self.callback(C.c_bool, [C.c_void_p], lambda _: True)),
        )
        for table, slot, callback in callbacks:
            self.put(table, slot, 'Q', C.cast(callback, C.c_void_p).value)
        self.block_vt = self.buffer(0x38)
        self.put(self.wrapped_type, 0, 'Q', C.addressof(self.block_vt))
        callback = self.callback(C.c_bool, [C.c_void_p] * 7, collision)
        self.put(self.block_vt, 0x30, 'Q', C.cast(callback, C.c_void_p).value)
        self.fetch = C.WINFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_void_p, C.c_bool, C.c_void_p, C.c_void_p)(BASE + 0x2D87730)

    def run(self, bounds):
        self.cells = []
        query = (C.c_float * 6)(*bounds)
        entries = self.buffer(24 * 4096)
        output = (C.c_uint64 * 3)(C.addressof(entries), C.addressof(entries), C.addressof(entries) + len(entries))
        collector = (C.c_uint64 * 1)(C.addressof(output))
        temporary = (C.c_uint64 * 3)()
        self.fetch(C.addressof(self.region), C.addressof(collector), C.addressof(query), False, C.addressof(self.context), C.addressof(temporary))
        return dict(bounds=list(query), cells=self.cells)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    native = NativeCollisionFetch(pe)
    try:
        cases = [native.run([x, y, z, x + width, y + height, z + width])
                 for (x, z), y, width, height in itertools.product(
                     ((0, 0), (-1.25, -16), (8.25, 43.25), (1000000.25, -1000000.25), (29999980, 29999980)),
                     (-63, 0, 64.9216, 65, 319), (.25, .5, 1), (.25, 1.975))]
    finally:
        native.close()
    args.output.write_text(json.dumps(dict(complete=True, reference=__doc__, client_sha256=digest, world_range=[-64,320],
        function=dict(rva='0x2d87730', size=0xB94, sha256=hashlib.sha256(pe.read(0x2D87730, 0xB94)).hexdigest()),
        scenarios=cases), separators=(',', ':')) + '\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, cases=len(cases))))
