#!/usr/bin/env python3
"""Execute client cylinder arithmetic, candidate sorting and rod selection.

The original client instruction bodies run in an isolated Python process.
World access is supplied by synthetic palette, chunk-height, rain and biome
callbacks. This verifies selection/control flow, not native world simulation.
"""

import argparse
import ctypes as C
from ctypes import wintypes as W
import hashlib
import itertools
import json
from pathlib import Path
import random
import struct

from redstone_native_probe import ClientPE

BASE = 0x140000000
SPANS = {
    "paletted cylinder query": (0x5502750, 0x270),
    "candidate sort": (0x2D9D600, 0xAB8),
    "rod selection": (0x32CBD00, 0x1DC),
    "rod group predicate": (0x32F9750, 0x7A),
    "on-disk indirect dispatch": (0xE585660, 2),
}


class NativeSearch:
    def __init__(self, pe):
        self.pe, self.keep, self.committed = pe, [], set()
        self.kernel = C.WinDLL("kernel32", use_last_error=True)
        self.kernel.VirtualAlloc.argtypes = [C.c_void_p, C.c_size_t, W.DWORD, W.DWORD]
        self.kernel.VirtualAlloc.restype = C.c_void_p
        self.kernel.VirtualProtect.argtypes = [C.c_void_p, C.c_size_t, W.DWORD, C.POINTER(W.DWORD)]
        self.kernel.VirtualFree.argtypes = [C.c_void_p, C.c_size_t, W.DWORD]
        self.address = self.kernel.VirtualAlloc(BASE, 0x12000000, 0x2000, 1)
        if self.address != BASE:
            raise RuntimeError("preferred client range unavailable in this Python process")
        for rva, size in SPANS.values():
            self.map(rva, pe.read(rva, size), executable=True)
        self.map(0x117DA080, pe.read(0x117DA080, 8))
        self.map(0xF0FBAA8, pe.read(0xF0FBAA8, 8))
        if pe.read(0xE585660, 2) != b"\xff\xe0":
            raise RuntimeError("unexpected on-disk indirect dispatcher")
        # Only byte movement is supplied by the host C runtime. Native sort,
        # comparisons and partitioning remain untouched.
        self.crt = C.CDLL("msvcrt")
        self.bridge(0xE5A62C0, C.cast(self.crt.memmove, C.c_void_p).value)
        self.source, self.vtable = self.buffer(0x80), self.buffer(0x160)
        self.dimension, self.biome = self.buffer(0x200), self.buffer(0x100)
        self.storage, self.storage_vtable = self.buffer(0x240), self.buffer(0xB0)
        self.palette = (C.c_void_p * 2)()
        self.keep.append(self.palette)
        self.records = self.buffer(24 * 4096)
        self.vector = self.buffer(24)
        self.put(self.source, 0, "Q", C.addressof(self.vtable))
        self.put(self.source, 0x38, "hh", 320, -64)
        self.put(self.storage, 0, "Q", C.addressof(self.storage_vtable))
        self.hashes = [self.buffer(8) for _ in range(8)]
        self.group = (C.c_void_p * 8)(*[C.addressof(x) for x in self.hashes])
        self.keep.append(self.group)
        self.blocks = []
        for i in range(9):
            typ, block = self.buffer(0xF0), self.buffer(0x70)
            self.put(typ, 0xE0, "Q", i + 1)
            self.put(block, 0x68, "Q", C.addressof(typ))
            self.blocks.append(C.addressof(block))
            if i < 8:
                self.put(self.hashes[i], 0, "Q", i + 1)
        self.map(0x11DF9CC0, struct.pack("<QQQ", C.addressof(self.group), C.addressof(self.group) + 64, C.addressof(self.group) + 64))
        self.palette[0] = self.blocks[8]
        self.snapshot = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p], self.get_snapshot)
        self.put(self.storage_vtable, 0xA8, "Q", C.cast(self.snapshot, C.c_void_p).value)
        self.cylinder = C.WINFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_void_p, C.c_uint,
                                     C.c_uint, C.c_void_p, C.c_void_p)(BASE + 0x5502750)
        self.select = C.WINFUNCTYPE(C.c_void_p, C.c_void_p, C.c_void_p, C.c_void_p)(BASE + 0x32CBD00)
        self.sort = C.WINFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_size_t)(BASE + 0x2D9D600)
        self.fetch = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p, C.c_void_p, C.c_uint, C.c_uint, C.c_void_p], self.fetch_blocks)
        self.bridge(0x2D8D970, C.cast(self.fetch, C.c_void_p).value)
        self.chunk = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p], self.get_chunk)
        self.get_dimension = self.callback(C.c_void_p, [C.c_void_p], lambda _: C.addressof(self.dimension))
        self.put(self.vtable, 0x148, "Q", C.cast(self.chunk, C.c_void_p).value)
        self.put(self.vtable, 0x128, "Q", C.cast(self.get_dimension, C.c_void_p).value)
        self.get_biome = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p], lambda *_: C.addressof(self.biome))
        self.precip = self.callback(C.c_bool, [C.c_void_p, C.c_void_p, C.c_void_p], self.precipitating)
        self.temperature = self.callback(C.c_float, [C.c_void_p, C.c_void_p, C.c_void_p], self.get_temperature)
        for rva, callback in ((0x2D8CDD0, self.get_biome), (0x32AB1E0, self.precip), (0x687BDA0, self.temperature)):
            self.bridge(rva, C.cast(callback, C.c_void_p).value)
        self.map(0xE6AE4DC, pe.read(0xE6AE4DC, 4))

    def map(self, rva, data, executable=False):
        start, end = rva & ~4095, (rva + len(data) + 4095) & ~4095
        for page in range(start, end, 4096):
            if page not in self.committed:
                if not self.kernel.VirtualAlloc(BASE + page, 4096, 0x1000, 4):
                    raise C.WinError(C.get_last_error())
                self.committed.add(page)
            old = W.DWORD()
            if not self.kernel.VirtualProtect(BASE + page, 4096, 4, C.byref(old)):
                raise C.WinError(C.get_last_error())
        C.memmove(BASE + rva, data, len(data))
        if executable:
            old = W.DWORD()
            if not self.kernel.VirtualProtect(BASE + start, end - start, 0x20, C.byref(old)):
                raise C.WinError(C.get_last_error())

    def bridge(self, rva, address):
        self.map(rva, b"\x48\xb8" + struct.pack("<Q", address) + b"\xff\xe0", executable=True)

    def buffer(self, size):
        value = C.create_string_buffer(size)
        self.keep.append(value)
        return value

    @staticmethod
    def put(buffer, offset, fmt, *values):
        struct.pack_into("<" + fmt, buffer, offset, *values)

    def callback(self, result, args, fn):
        value = C.WINFUNCTYPE(result, *args)(fn)
        self.keep.append(value)
        return value

    @staticmethod
    def pos(pointer):
        return struct.unpack("<iii", C.string_at(pointer, 12))

    def get_snapshot(self, _, output):
        C.memmove(output, struct.pack("<QQ", 2, C.addressof(self.palette)), 16)
        return output

    def fetch_blocks(self, source, output, center, radius, height, predicate):
        self.arguments = {"center": self.pos(center), "radius": radius, "height": height}
        begin = C.addressof(self.records)
        self.put(self.vector, 0, "QQQ", begin, begin, begin + len(self.records))
        for candidate in sorted(self.case["candidates"], key=lambda c: (c["pos"][0] >> 4, c["pos"][2] >> 4,
                                c["pos"][1] >> 4, c["pos"][0] & 15, c["pos"][2] & 15, c["pos"][1] & 15)):
            if not candidate.get("loaded", True):
                continue
            x, y, z = candidate["pos"]
            if not -64 <= y < 320:
                continue
            self.palette[1] = self.blocks[candidate.get("material", 0)]
            C.memset(C.addressof(self.storage) + 8, 0, 512)
            index = ((x & 15) << 8) | ((z & 15) << 4) | (y & 15)
            self.put(self.storage, 8 + (index // 32) * 4, "I", 1 << (index % 32))
            origin = (C.c_int * 3)(x & ~15, y & ~15, z & ~15)
            self.cylinder(C.addressof(self.storage), C.addressof(origin), center, radius, height,
                          predicate, C.addressof(self.vector))
        _, end, _ = struct.unpack_from("<QQQ", self.vector)
        C.memmove(source + 0x40, struct.pack("<QQQ", begin, end, begin + len(self.records)), 24)
        C.memmove(output, struct.pack("<QQ", begin, (end - begin) // 24), 16)
        return output

    def get_chunk(self, _, pos):
        key = tuple(struct.unpack("<ii", C.string_at(pos, 8)))
        return C.addressof(self.chunks[key]) if key in self.chunks else 0

    def candidate_above(self, pointer):
        x, y, z = self.pos(pointer)
        return next(c for c in self.case["candidates"] if c["pos"] == [x, y - 1, z])

    def precipitating(self, _weather, _source, pos):
        return self.case.get("raining", True) and self.candidate_above(pos).get("precipitation", True)

    def get_temperature(self, _biome, _source, pos):
        return self.candidate_above(pos).get("temperature", 0.8)

    def run(self, case):
        self.case, self.chunks = case, {}
        for c in case["candidates"]:
            if not c.get("loaded", True):
                continue
            x, y, z = c["pos"]
            key = (x >> 4, z >> 4)
            if key not in self.chunks:
                self.chunks[key] = C.create_string_buffer(0xE00)
            if c.get("covered", False):
                self.put(self.chunks[key], 0xB80 + ((z & 15) << 4 | (x & 15)) * 2, "h", y + 2 + 64)
        center = (C.c_int * 3)(*case["center"])
        result = C.create_string_buffer(16)
        self.select(C.addressof(result), C.addressof(center), C.addressof(self.source))
        count = (struct.unpack_from("<Q", self.source, 0x48)[0] - C.addressof(self.records)) // 24
        order = [list(struct.unpack_from("<iiiI", self.records, i * 24 + 8)) for i in range(count)]
        if self.arguments != {"center": tuple(case["center"]), "radius": 64, "height": 64}:
            raise RuntimeError("native selector query changed")
        return {**case, "selected": list(self.pos(C.addressof(result))) if result[12] != b"\0" else None,
                "candidate_order": order}

    def close(self):
        if self.address:
            self.kernel.VirtualFree(self.address, 0, 0x8000)
            self.address = None

    def run_sort(self, distances, ideal):
        records = C.create_string_buffer(max(24, len(distances) * 24))
        for i, distance in enumerate(distances):
            self.put(records, i * 24, "QiiiI", 0, i, 0, 0, distance)
        begin = C.addressof(records)
        self.sort(begin, begin + len(distances) * 24, ideal)
        return {"distances": distances, "ideal": ideal,
                "order": [struct.unpack_from("<i", records, i * 24 + 8)[0] for i in range(len(distances))]}


def scenarios():
    center = [0, 100, 0]
    for dx, dy, dz in itertools.product((-65, -64, -48, -1, 0, 1, 48, 64, 65), (-65, -64, 0, 64, 65), (-65, -64, -48, -1, 0, 1, 48, 64, 65)):
        yield {"name": "cylinder boundary", "center": center, "candidates": [{"pos": [dx, 100 + dy, dz]}]}
    for material, covered, precipitation, temperature, loaded in itertools.product(range(9), (False, True), (False, True), (0.15, 0.15000002, 0.8), (False, True)):
        yield {"name": "eligibility and fallback", "center": center, "candidates": [
            {"pos": [1, 100, 0], "material": material, "covered": covered, "precipitation": precipitation,
             "temperature": temperature, "loaded": loaded}, {"pos": [16, 100, 0]}]}
    yield {"name": "clear weather", "center": center, "raining": False, "candidates": [{"pos": [1, 100, 0]}]}
    rng = random.Random(2651)
    for count in (2, 8, 32, 33, 64, 129):
        for _ in range(8):
            positions = rng.sample(list(itertools.product(range(-10, 11), range(-10, 11))), count)
            yield {"name": "distance sort and ties", "center": center,
                   "candidates": [{"pos": [x, 100, z], "covered": rng.randrange(3) == 0} for x, z in positions]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected image")
    native = NativeSearch(pe)
    try:
        cases = [native.run(case) for case in scenarios()]
        rng = random.Random(65164)
        sort_cases = []
        for count, heap in itertools.product((0, 1, 2, 8, 32, 33, 40, 41, 64, 129, 512, 2048), (False, True)):
            for distances in ([0] * count, list(range(count)), list(reversed(range(count))),
                              [rng.randrange(8) for _ in range(count)],
                              [rng.randrange(0x100000000) for _ in range(count)]):
                sort_cases.append(native.run_sort(distances, 0 if heap else count))
    finally:
        native.close()
    output = {"complete": True, "client_sha256": digest,
              "reference": "executed client cylinder query, group predicate, sort and selector; synthetic world callbacks",
              "supplied_callbacks": ["palette snapshot", "loaded chunk height", "precipitation", "biome temperature",
                                     "chunk enumeration feeding actual paletted query", "host CRT byte movement"],
              "spans": {name: {"rva": hex(rva), "size": size, "sha256": hashlib.sha256(pe.read(rva, size)).hexdigest()}
                        for name, (rva, size) in SPANS.items()}, "cases": cases, "sort_cases": sort_cases}
    args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(args.output), "complete": True, "cases": len(cases)}))
