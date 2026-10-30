#!/usr/bin/env python3
"""Execute DetectorRailBlock::getComparatorSignal with an empty typed fetch.

Original instructions construct the query bounds and choose the actor-family
filter. The supplied fetch only records that query and returns no candidates.
This does not execute native spatial selection or container arithmetic.
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


class NativeDetectorQuery(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        self.map(0xE6AE454, pe.read(0xE6AE454, 4))
        self.map(0xE6AE47C, pe.read(0xE6AE47C, 4))
        self.map(0x9783E50, pe.read(0x9783E50, 0x25D), executable=True)
        self.rail_source, self.rail_source_vt = self.buffer(8), self.buffer(0xF8)
        self.put(self.rail_source, 0, 'Q', C.addressof(self.rail_source_vt))
        self.error = None

        def fetch(_source, result, kind, query, excluded, _predicate):
            try:
                self.query = dict(kind=kind, bounds=list(struct.unpack('<6f', C.string_at(query, 24))),
                                  excluded=excluded or 0)
                C.memmove(result, struct.pack('<QQ', 0, 0), 16)
            except Exception as error:
                self.error = str(error)
            return result

        callback = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p, C.c_uint32, C.c_void_p, C.c_void_p, C.c_void_p], fetch)
        self.put(self.rail_source_vt, 0xF0, 'Q', C.cast(callback, C.c_void_p).value)
        self.getter = C.WINFUNCTYPE(C.c_int, C.c_void_p, C.c_void_p, C.c_void_p, C.c_void_p, C.c_ubyte)(BASE + 0x9783E50)

    def run(self, pos):
        self.query = None
        result = self.getter(0, C.addressof(self.rail_source), C.addressof((C.c_int32 * 3)(*pos)), 0, 0)
        if self.error or self.query is None or result != 0:
            raise RuntimeError((self.error, self.query, result))
        return dict(position=pos, **self.query)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    native = NativeDetectorQuery(pe)
    try:
        cases = [native.run([x, y, z]) for x, z in itertools.product(
            (0, -1, 15, 16, -17, 16777215, 16777216, 29999999, -29999999), repeat=2)
            for y in (-64, 0, 65, 319)]
    finally:
        native.close()
    args.output.write_text(json.dumps(dict(complete=True, reference=__doc__, client_sha256=digest,
        function=dict(rva='0x9783e50', size=0x25D, sha256=hashlib.sha256(pe.read(0x9783E50, 0x25D)).hexdigest()),
        scenarios=cases), separators=(',', ':')) + '\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, cases=len(cases))))
