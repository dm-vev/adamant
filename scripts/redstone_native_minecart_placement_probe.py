#!/usr/bin/env python3
"""Execute original client MinecartItem::_useOn up to its spawn request.

Supplied callbacks report a rail, no obstructing actors and a server world.
The spawn boundary records the original coordinates and returns false, so
this isolates request construction rather than creating or ticking an actor.
No live process or on-disk binary is modified.
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


class NativeMinecartPlacement(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        self.map(0xE5C6370, pe.read(0xE5C6370, 4))
        self.map(0x24A05D0, pe.read(0x24A05D0, 0x2C0), executable=True)
        self.actor, self.owner = self.buffer(0x220), self.buffer(16)
        self.region, self.region_vt = self.buffer(8), self.buffer(0x68)
        self.level, self.level_vt = self.buffer(8), self.buffer(0x9F0)
        self.cart_source, self.cart_source_vt = self.buffer(8), self.buffer(0x100)
        self.rail = self.buffer(8)
        self.cart_item, self.stack, self.result = self.buffer(0x220), self.buffer(0x100), self.buffer(0x80)
        for obj, vt in ((self.region, self.region_vt), (self.level, self.level_vt), (self.cart_source, self.cart_source_vt)):
            self.put(obj, 0, 'Q', C.addressof(vt))
        self.put(self.actor, 0x1C8, 'Q', C.addressof(self.region))
        self.put(self.actor, 0x1D0, 'Q', C.addressof(self.owner))
        self.put(self.actor, 0x1D8, 'Q', C.addressof(self.level))
        self.put(self.owner, 8, 'i', 1)
        source = self.callback(C.c_void_p, [C.c_void_p], lambda _: C.addressof(self.cart_source))
        get_block = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p], lambda *_: C.addressof(self.rail))
        server = self.callback(C.c_bool, [C.c_void_p], lambda _: False)
        is_rail = self.callback(C.c_bool, [C.c_void_p], lambda _: True)

        def fetch(_source, result, _excluded, query, _players, _removed):
            self.query = list(struct.unpack('<6f', C.string_at(query, 24)))
            C.memset(result, 0, 16)
            return result

        def spawn(kind, _actor, _source, position, _stack):
            self.request = dict(kind=kind, position=list(struct.unpack('<3f', C.string_at(position, 12))))
            return False

        fetch_cb = self.callback(C.c_void_p, [C.c_void_p] * 4 + [C.c_ubyte] * 2, fetch)
        spawn_cb = self.callback(C.c_bool, [C.c_int, C.c_void_p, C.c_void_p, C.c_void_p, C.c_void_p], spawn)
        self.put(self.region_vt, 0x60, 'Q', C.cast(source, C.c_void_p).value)
        self.put(self.cart_source_vt, 0x10, 'Q', C.cast(get_block, C.c_void_p).value)
        self.put(self.cart_source_vt, 0xF8, 'Q', C.cast(fetch_cb, C.c_void_p).value)
        self.put(self.level_vt, 0x9E8, 'Q', C.cast(server, C.c_void_p).value)
        self.bridge(0x2A55A20, C.cast(is_rail, C.c_void_p).value)
        self.bridge(0x24A0890, C.cast(spawn_cb, C.c_void_p).value)
        self.function = C.WINFUNCTYPE(C.c_void_p, C.c_void_p, C.c_void_p, C.c_void_p, C.c_void_p,
                                     C.c_void_p, C.c_ubyte, C.c_int, C.c_void_p)(BASE + 0x24A05D0)

    def run(self, pos, kind):
        self.put(self.cart_item, 0x218, 'i', kind)
        self.request, self.query = None, None
        block_pos = (C.c_int32 * 3)(*pos)
        hit = (C.c_float * 3)(.5, .5, .5)
        self.function(C.addressof(self.cart_item), C.addressof(self.result), C.addressof(self.stack),
                      C.addressof(self.actor), C.addressof(block_pos), 1, 0, C.addressof(hit))
        if self.request is None or self.query is None or self.request['kind'] != kind:
            raise RuntimeError('original placement did not reach the supplied spawn boundary')
        return dict(block_position=pos, query=self.query, **self.request)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise ValueError('client differs from the inspected image')
    native = NativeMinecartPlacement(pe)
    try:
        cases = [native.run([x, y, z], kind) for kind in (0, 1, 3, 4, 5)
                 for x, y, z in itertools.product((0, -17, 16777215, -29999999), (0, 65, 319), (0, -17))]
    finally:
        native.close()
    args.output.write_text(json.dumps(dict(complete=True, reference=__doc__, client_sha256=digest,
                                          function_rva='0x24a05d0', cases=cases)), encoding='utf-8')
    print(json.dumps(dict(complete=True, cases=len(cases), output=str(args.output))))
