#!/usr/bin/env python3
"""Execute original moving-block contact with explicit fetched-actor inputs.

The original body chooses displacement and slime motion. The fetched candidate,
component tags and wrapped cube are inputs. The move callback records requests
without resolving or applying them; this is not an actor-physics simulation.
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


class NativePistonContact(NativePistonShapes):
    def __init__(self, pe):
        self.callback_errors = []
        super().__init__(pe)
        for rva in (0xE5C6370, 0xE6AE530, 0xE75D7C4):
            self.map(rva, pe.read(rva, 4))
        self.map(0x4ADC6A0, pe.read(0x4ADC6A0, 0xF0E), executable=True)
        self.region_vt, self.type_vt = self.buffer(0x160), self.buffer(0x1E8)
        self.wrapped_type = self.buffer(0x180)
        self.put(self.source, 0, 'Q', C.addressof(self.region_vt))
        self.put(self.wrapped_type, 0, 'Q', C.addressof(self.type_vt))
        self.put(self.wrapped_type, 0xE0, 'Q', 42)
        self.put(self.wrapped, 0x68, 'Q', C.addressof(self.wrapped_type))
        self.level, self.level_vt = self.buffer(8), self.buffer(0x9F0)
        self.put(self.level, 0, 'Q', C.addressof(self.level_vt))
        self.actor, self.state, self.shape = self.buffer(0x230), self.buffer(36), self.buffer(32)
        self.registry, self.bucket, self.entry = self.buffer(0x78), self.buffer(8), self.buffer(32)
        self.tag, self.sparse, self.sparse_pages = self.buffer(0x20), self.buffer(8192), self.buffer(8)
        self.put(self.actor, 0x10, 'QI', C.addressof(self.registry), 0)
        self.put(self.actor, 0x218, 'QQ', C.addressof(self.state), C.addressof(self.shape))
        self.put(self.registry, 0x48, 'QQ', C.addressof(self.bucket), C.addressof(self.bucket) + 8)
        self.put(self.registry, 0x68, 'QQ', C.addressof(self.entry), C.addressof(self.entry) + 32)
        self.put(self.entry, 0, 'qI', -1, 0x72DDE456)
        self.put(self.entry, 0x10, 'Q', C.addressof(self.tag))
        self.put(self.sparse_pages, 0, 'Q', C.addressof(self.sparse))
        self.put(self.tag, 8, 'QQ', C.addressof(self.sparse_pages), C.addressof(self.sparse_pages) + 8)
        self.actors, self.unique_id = self.buffer(8), self.buffer(8)
        self.put(self.actors, 0, 'Q', C.addressof(self.actor))
        self.put(self.unique_id, 0, 'Q', 1)
        def fetch(_source, out, _except, _box, _a, _b):
            included = self.case.get('fetched', [True, True])[self.query_count]
            self.query_count += 1
            C.memmove(out, struct.pack('<QQ', int(included), C.addressof(self.actors)), 16)
            return out
        def copy(out, _unused, begin, count):
            values = self.buffer(count * 8 or 8)
            C.memmove(values, begin, count * 8)
            C.memmove(out, struct.pack('<QQQ', C.addressof(values), C.addressof(values) + count * 8, C.addressof(values) + count * 8), 24)
            return out
        def move(_piston, _actor, delta):
            self.requests.append(list(struct.unpack('<3f', C.string_at(delta, 12))))
        def insert(vector, position, value):
            begin, end, _ = struct.unpack('<3Q', C.string_at(vector, 24))
            size, offset = end - begin, (position or 0) - begin
            values = self.buffer(size + 8)
            if offset:
                C.memmove(values, begin, offset)
            C.memmove(C.addressof(values) + offset, value, 8)
            if size > offset:
                C.memmove(C.addressof(values) + offset + 8, begin + offset, size - offset)
            C.memmove(vector, struct.pack('<3Q', C.addressof(values), C.addressof(values) + size + 8, C.addressof(values) + size + 8), 24)
            return C.addressof(values) + offset
        shape = self.callback(C.c_void_p, [C.c_void_p] * 6, self.get_shape)
        callbacks = (
            (self.region_vt, 0x10, self.callback(C.c_void_p, [C.c_void_p] * 2, lambda *_: C.addressof(self.base_block))),
            (self.region_vt, 0x18, self.callback(C.c_void_p, [C.c_void_p] * 2, lambda *_: C.addressof(self.parent))),
            (self.region_vt, 0xF8, self.callback(C.c_void_p, [C.c_void_p] * 4 + [C.c_bool] * 2, fetch)),
            (self.region_vt, 0x158, self.callback(C.c_void_p, [C.c_void_p], lambda _: C.addressof(self.level))),
            (self.level_vt, 0x9E8, self.callback(C.c_bool, [C.c_void_p], lambda _: False)),
            (self.type_vt, 0x20, shape),
            (self.type_vt, 0x1E0, self.callback(C.c_bool, [C.c_void_p] * 2, lambda *_: False)),
        )
        for table, offset, callback in callbacks:
            self.put(table, offset, 'Q', C.cast(callback, C.c_void_p).value)
        for rva, callback in (
            (0x1184890, self.callback(C.c_void_p, [C.c_void_p, C.c_void_p, C.c_void_p, C.c_uint64], copy)),
            (0x4ADDE80, self.callback(None, [C.c_void_p] * 3, move)),
            (0x19F30D0, self.callback(C.c_void_p, [C.c_void_p], lambda _: C.addressof(self.unique_id))),
            (0xA4CF0, self.callback(C.c_void_p, [C.c_void_p] * 3, insert)),
            (0xE021350, self.callback(None, [C.c_void_p, C.c_uint64], lambda *_: None)),
        ):
            self.bridge(rva, C.cast(callback, C.c_void_p).value)
        self.contact = C.WINFUNCTYPE(None, *([C.c_void_p] * 3))(BASE + 0x4ADC6A0)

    def callback(self, result, args, fn):
        def checked(*values):
            try:
                return fn(*values)
            except BaseException as error:
                self.callback_errors.append(repr(error))
                return None if result is None else 0
        return super().callback(result, args, checked)

    def contact_case(self, case):
        self.case, self.cells, self.requests, self.query_count = case, [], [], 0
        self.put(self.moving, 8, '3i', *case['position'])
        self.put(self.moving, 0xF0, 'Q', C.addressof(self.wrapped))
        self.put(self.moving, 0x110, '3i', 1, 65, 1)
        self.put(self.moving, 0x11C, 'B', case['expanding'])
        self.put(self.parent, 0xF4, 'ff', case['progress'], case['last_progress'])
        self.put(self.parent, 0xFF, 'BB', 1 if case['expanding'] else 3, 1 if case['expanding'] else 3)
        self.put(self.direction, 0, '3i', *case['direction'])
        self.put(self.wrapped_type, 0x134, 'I', int(case['slime']))
        self.put(self.state, 0, '3f', *case['origin'])
        self.put(self.state, 24, '3f', .125, -.0784, -.25)
        half = case['width'] / 2
        p = case['origin']
        self.put(self.shape, 0, '8f', p[0] - half, p[1], p[2] - half, p[0] + half, p[1] + 1.975, p[2] + half, case['width'], 1.975)
        self.contact(C.addressof(self.moving), C.addressof(self.parent), C.addressof(self.source))
        if self.callback_errors:
            raise RuntimeError(self.callback_errors)
        return {**case, 'requests': self.requests, 'motion': list(struct.unpack_from('<3f', self.state, 24))}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    native, cases = NativePistonContact(pe), []
    try:
        for face, (last, progress), width, slime, offset in itertools.product(
                (2, 3, 4, 5), ((1, 1), (1, .5), (.5, 0)), (.25, .5), (False, True), (-.25, 0, .25, .5, .75, 1, 1.25)):
            direction = ((0, 0, -1), (0, 0, 1), (-1, 0, 0), (1, 0, 0))[face - 2]
            p = [8.5 + direction[0] * offset, 65, 8.5 + direction[2] * offset]
            cases.append(native.contact_case(dict(face=face, direction=direction, expanding=False, parent=True,
                last_progress=last, progress=progress, position=[8, 65, 8], shape=[0, 0, 0, 1, 1, 1], origin=p, width=width, slime=slime)))
        for face, (last, progress), width, slime, lateral in itertools.product(
                (2, 3, 4, 5), ((1, 1), (1, .5), (.5, 0)), (.25, .5), (False, True), (-.625, -.5, -.499, 0, .499, .5, .625)):
            direction = ((0, 0, -1), (0, 0, 1), (-1, 0, 0), (1, 0, 0))[face - 2]
            p = [8.5 + direction[0] * last, 66, 8.5 + direction[2] * last]
            p[0 if face < 4 else 2] += lateral
            cases.append(native.contact_case(dict(face=face, direction=direction, expanding=False, parent=True,
                last_progress=last, progress=progress, position=[8, 65, 8], shape=[0, 0, 0, 1, 1, 1], origin=p,
                width=width, slime=slime, fetched=[True, False])))
    finally:
        native.close()
    args.output.write_text(json.dumps(dict(complete=True, reference=__doc__, client_sha256=digest,
        function=dict(rva='0x4adc6a0', size=0xF0E, sha256=hashlib.sha256(pe.read(0x4ADC6A0, 0xF0E)).hexdigest()),
        scenarios=cases), separators=(',', ':')) + '\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, cases=len(cases))))
