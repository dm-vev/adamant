#!/usr/bin/env python3
"""Execute the client moving-block contact-shape function with supplied shapes.

Original _getWrappedBlockCollisionShapes instructions compute both query cells
and world AABBs. Host callbacks provide a parent actor, direction, wrapped-block
shape and scalar floorf; no live game process is modified.
"""

import argparse
import ctypes as C
from ctypes import wintypes as W
import hashlib
import itertools
import json
import math
from pathlib import Path
import struct

from redstone_native_probe import ClientPE
from redstone_native_lightning_search_probe import NativeSearch, BASE


class NativePistonShapes(NativeSearch):
    def __init__(self, pe):
        self.pe, self.keep, self.committed = pe, [], set()
        self.kernel = C.WinDLL("kernel32", use_last_error=True)
        self.kernel.VirtualAlloc.argtypes = [C.c_void_p, C.c_size_t, W.DWORD, W.DWORD]
        self.kernel.VirtualAlloc.restype = C.c_void_p
        self.kernel.VirtualProtect.argtypes = [C.c_void_p, C.c_size_t, W.DWORD, C.POINTER(W.DWORD)]
        self.kernel.VirtualFree.argtypes = [C.c_void_p, C.c_size_t, W.DWORD]
        self.address = self.kernel.VirtualAlloc(BASE, 0x12000000, 0x2000, 1)
        if self.address != BASE:
            raise RuntimeError("preferred client range unavailable")
        # Map constants before code: the loader changes protection page-wide.
        for rva, size in ((0x117DA080, 8), (0xE5A7060, 4), (0xE6022E0, 16)):
            self.map(rva, pe.read(rva, size))
        self.map(0x11DFEE60, struct.pack("<Q", 1234))
        self.map(0x11DFEE90, struct.pack("<Q", 5678))
        self.map(0x11E01638, struct.pack("<Q", 9999))
        self.map(0x4ADD8B0, pe.read(0x4ADD8B0, 0x505), executable=True)
        self.map(0xE585660, pe.read(0xE585660, 2), executable=True)
        self.map(0xF0FBAA8, struct.pack("<Q", BASE + 0xE585660))
        self.source, self.source_vt = self.buffer(16), self.buffer(0x30)
        self.parent, self.parent_vt = self.buffer(0x120), self.buffer(0x90)
        self.moving = self.buffer(0x130)
        self.wrapped, self.wrapped_type, self.wrapped_vt = self.buffer(0x70), self.buffer(0xF0), self.buffer(0x30)
        self.base_block, self.base_type = self.buffer(0x70), self.buffer(0xF0)
        self.direction = self.buffer(12)
        self.output = self.buffer(48)
        for obj, vt in ((self.source, self.source_vt), (self.parent, self.parent_vt), (self.wrapped_type, self.wrapped_vt)):
            self.put(obj, 0, "Q", C.addressof(vt))
        self.put(self.wrapped, 0x68, "Q", C.addressof(self.wrapped_type))
        self.put(self.wrapped_type, 0xE0, "Q", 42)
        self.put(self.base_block, 0x68, "Q", C.addressof(self.base_type))
        self.put(self.base_type, 0xE0, "Q", 1234)
        get_block = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p], lambda *_: C.addressof(self.base_block))
        get_actor = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p], lambda *_: C.addressof(self.parent) if self.case["parent"] else 0)
        is_type = self.callback(C.c_bool, [C.c_void_p, C.c_ubyte], lambda _, kind: kind == 18)
        shape = self.callback(C.c_void_p, [C.c_void_p] * 6, self.get_shape)
        for table, slot, callback in ((self.source_vt, 0x10, get_block), (self.source_vt, 0x18, get_actor),
                                       (self.parent_vt, 0x80, is_type), (self.wrapped_vt, 0x20, shape)):
            self.put(table, slot, "Q", C.cast(callback, C.c_void_p).value)
        get_direction = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p], lambda *_: C.addressof(self.direction))
        floor = self.callback(C.c_float, [C.c_float], lambda value: math.floor(value))
        self.bridge(0x4ADC560, C.cast(get_direction, C.c_void_p).value)
        self.bridge(0xE5A6A90, C.cast(floor, C.c_void_p).value)
        self.function = C.WINFUNCTYPE(C.c_void_p, C.c_void_p, C.c_void_p, C.c_void_p)(BASE + 0x4ADD8B0)

    def head_collisions(self):
        # The original function initializes three static tables through MSVC
        # thread guards. Supply only the guard bookkeeping; original table
        # construction and AABB constructor instructions remain untouched.
        self.map(0x10000000, bytes.fromhex("65488b042558000000c3"), executable=True)
        tls_array = C.WINFUNCTYPE(C.c_void_p)(BASE + 0x10000000)()
        tls_zero = C.c_void_p.from_address(tls_array).value if tls_array else 0
        if not tls_zero or C.c_int32.from_address(tls_zero + 4).value == 2147483647:
            raise RuntimeError("no usable current-thread static TLS slot")
        self.map(0x11D040C8, struct.pack("<I", 0))
        self.map(0x11E581B0, bytes(0x1E0))
        for rva in (0x11E58240, 0x11E582E0, 0x11E58380):
            self.map(rva, struct.pack("<i", 2147483647))
        begin = self.callback(None, [C.c_void_p], lambda pointer: C.c_int32.from_address(pointer).__setattr__("value", -1))
        end = self.callback(None, [C.c_void_p], lambda pointer: C.c_int32.from_address(pointer).__setattr__("value", -2147483648))
        self.bridge(0xE024890, C.cast(begin, C.c_void_p).value)
        self.bridge(0xE024950, C.cast(end, C.c_void_p).value)
        self.map(0x3327C80, self.pe.read(0x3327C80, 0x5F), executable=True)
        self.map(0x6EB1020, self.pe.read(0x6EB1020, 0x68A), executable=True)
        function = C.WINFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_void_p, C.c_ubyte)(BASE + 0x6EB1020)
        buffers = [self.buffer(24) for _ in range(3)]
        result = []
        for face, native_face in enumerate((0, 1, 3, 2, 5, 4)):
            function(*[C.addressof(buffer) for buffer in buffers], native_face)
            result.append(dict(face=face, native_face=native_face,
                               boxes=[list(struct.unpack("<6f", buffer)) for buffer in buffers]))
        return result

    def head_world_collisions(self):
        self.map(0x6EB0AC0, self.pe.read(0x6EB0AC0, 0x50B), executable=True)
        # The source supplies a block-state reader and matching rear actor.
        legacy, legacy_vt, block = self.buffer(0x340), self.buffer(0x270), self.buffer(0x128)
        sentinel, state, getter, getter_vt, meta = [self.buffer(n) for n in (0x30, 8, 0x10, 8, 0x10)]
        entries = self.buffer(16)
        self.put(legacy, 0, "Q", C.addressof(legacy_vt))
        self.put(legacy, 0x1D8, "Q", C.addressof(sentinel))
        self.put(sentinel, 8, "Q", C.addressof(sentinel))
        self.put(sentinel, 0x19, "B", 1)
        self.put(legacy, 0x320, "QQ", C.addressof(entries), C.addressof(entries) + 16)
        self.put(entries, 0, "Q", C.addressof(getter))
        self.put(getter, 0, "QQ", C.addressof(getter_vt), C.addressof(meta))
        self.put(meta, 8, "Q", C.addressof(state))
        self.map(0x11E2EDF0, struct.pack("<Q", C.addressof(state)))
        self.put(block, 0x68, "Q", C.addressof(legacy))
        def read_state(_, output, typ, bits):
            C.memmove(output, struct.pack("<iI", self.case["native_face"], 1), 8)
            return output
        def second_part(_, source, position, output):
            head = self.pos(position)
            direction = [(0,-1,0),(0,1,0),(0,0,-1),(0,0,1),(-1,0,0),(1,0,0)][self.case["face"]]
            C.memmove(output, struct.pack("<iii", *[head[i] - direction[i] for i in range(3)]), 12)
            return True
        read = self.callback(C.c_void_p, [C.c_void_p] * 4, read_state)
        second = self.callback(C.c_bool, [C.c_void_p] * 4, second_part)
        self.put(getter_vt, 0, "Q", C.cast(read, C.c_void_p).value)
        self.put(legacy_vt, 0x258, "Q", C.cast(second, C.c_void_p).value)
        direction_table = [(0,-1,0),(0,1,0),(0,0,1),(0,0,-1),(1,0,0),(-1,0,0)]
        self.map(0x11E583C0, struct.pack("<18i", *[v for point in direction_table for v in point]))
        function = C.WINFUNCTYPE(C.c_bool, *([C.c_void_p] * 7))(BASE + 0x6EB0AC0)
        position, vector, boxes = self.buffer(12), self.buffer(24), self.buffer(72)
        cases = []
        for face, parent, (last, progress), anchor in itertools.product(
                range(6), (False, True), ((0, .5), (.5, 1), (1, .5), (.5, 0)), (0, 1000000, -1000000, 29999980)):
            self.case = dict(face=face, native_face=(0,1,3,2,5,4)[face], parent=parent,
                             last_progress=last, progress=progress, position=[anchor, 65, anchor])
            self.put(position, 0, "iii", *self.case["position"])
            self.put(self.parent, 0xF4, "ff", progress, last)
            start = C.addressof(boxes)
            self.put(vector, 0, "QQQ", start, start, start + 72)
            accepted = function(C.addressof(legacy), C.addressof(block), C.addressof(self.source),
                                C.addressof(position), 0, C.addressof(vector), 0)
            _, end, _ = struct.unpack_from("<QQQ", vector)
            count = (end - start) // 24
            if count not in (0, 3) or bool(accepted) != parent:
                raise RuntimeError("unexpected original head collision result")
            cases.append({**self.case, "boxes": [list(struct.unpack_from("<6f", boxes, n*24)) for n in range(count)]})
        return cases

    def get_shape(self, _, output, block, source, position, flags):
        cell = list(self.pos(position))
        self.cells.append(cell)
        values = [C.c_float(cell[i % 3] + self.case["shape"][i]).value for i in range(6)]
        C.memmove(output, struct.pack("<6f", *values), 24)
        return output

    def run(self, case):
        self.case, self.cells = case, []
        self.put(self.moving, 8, "iii", *case["position"])
        self.put(self.moving, 0xF0, "Q", C.addressof(self.wrapped))
        self.put(self.moving, 0x110, "iii", 1, 65, 1)
        self.put(self.moving, 0x11C, "B", case["expanding"])
        self.put(self.parent, 0xF4, "ff", case["progress"], case["last_progress"])
        self.put(self.direction, 0, "iii", *[(0,-1,0),(0,1,0),(0,0,-1),(0,0,1),(-1,0,0),(1,0,0)][case["face"]])
        self.function(C.addressof(self.moving), C.addressof(self.output), C.addressof(self.source))
        return {**case, "cells": self.cells, "boxes": [list(struct.unpack_from("<6f", self.output, n * 24)) for n in range(2)]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected image")
    native, cases = NativePistonShapes(pe), []
    try:
        for face, expanding, parent, (last, progress), anchor, shape in itertools.product(
                range(6), (False, True), (False, True), ((0, .5), (.5, 1), (1, .5), (.5, 0)),
                (0, 1000000, -1000000, 29999980),
                ((0,0,0,1,1,1), (.0625,0,.0625,.9375,.9375,.9375), (0,.5,0,1,1,1))):
            cases.append(native.run(dict(face=face, expanding=expanding, parent=parent,
                                         last_progress=last, progress=progress,
                                         position=[anchor, 65, anchor], shape=shape)))
        heads = native.head_collisions()
        head_world = native.head_world_collisions()
    finally:
        native.close()
    args.output.write_text(json.dumps({"complete": True, "reference": __doc__, "client_sha256": digest,
        "function": {"rva": "0x4add8b0", "size": 0x505, "sha256": hashlib.sha256(pe.read(0x4ADD8B0, 0x505)).hexdigest()},
        "head_function": {"rva": "0x6eb1020", "size": 0x68A, "sha256": hashlib.sha256(pe.read(0x6EB1020, 0x68A)).hexdigest()},
        "head_collisions": heads,
        "head_world_function": {"rva": "0x6eb0ac0", "size": 0x50B, "sha256": hashlib.sha256(pe.read(0x6EB0AC0, 0x50B)).hexdigest()},
        "head_world_collisions": head_world,
        "scenarios": cases}, separators=(",", ":")) + "\n", encoding="utf-8")
    print(json.dumps({"complete": True, "cases": len(cases)}))
