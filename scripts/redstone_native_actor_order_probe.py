#!/usr/bin/env python3
"""Execute the original client actor-vector shuffle with supplied random draws.

The inspected shuffle instructions are unmodified. An authored ABI wrapper
supplies the vector/level registers and exits at the original continuation;
only the RNG call is supplied. No native world or block actor is simulated.
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
from redstone_native_lightning_search_probe import NativeSearch, BASE


class NativeActorShuffle(NativeSearch):
    def __init__(self, pe):
        self.pe, self.keep, self.committed = pe, [], set()
        self.kernel = C.WinDLL("kernel32", use_last_error=True)
        self.kernel.VirtualAlloc.argtypes = [C.c_void_p, C.c_size_t, W.DWORD, W.DWORD]
        self.kernel.VirtualAlloc.restype = C.c_void_p
        self.kernel.VirtualProtect.argtypes = [C.c_void_p, C.c_size_t, W.DWORD, C.POINTER(W.DWORD)]
        self.kernel.VirtualFree.argtypes = [C.c_void_p, C.c_size_t, W.DWORD]
        self.address = self.kernel.VirtualAlloc(BASE, 0x12000000, 0x2000, 1)
        if self.address != BASE:
            raise RuntimeError("preferred client range unavailable in isolated Python")
        self.map(0x32CB7A6, pe.read(0x32CB7A6, 0x93), executable=True)
        # Original branches leave the shuffle at these two continuations.
        self.bridge(0x32CB839, BASE + 0x32CC000)
        self.bridge(0x32CB847, BASE + 0x32CC000)
        self.map(0x32CC000, bytes.fromhex("4883c4785b5f5e415c415d415e415f5dc3"), executable=True)
        self.level = self.buffer(0xA00)
        self.put(self.level, 0x9F8, "Q", C.addressof(self.level))
        self.random = self.callback(C.c_uint, [C.c_void_p, C.c_uint], self.next_draw)
        self.bridge(0x26DAE0, C.cast(self.random, C.c_void_p).value)
        # Win64 args: rcx=vector begin, rdx=count. Preserve native nonvolatile
        # registers and shadow/local space, then enter the untouched loop.
        wrapper = (bytes.fromhex("5541574156415541545657534883ec784c8bf9488bc248c1e0044d8d3407")
                   + b"\x48\xbb" + struct.pack("<Q", C.addressof(self.level))
                   + bytes.fromhex("31ff488d6c2470")
                   + b"\x48\xb8" + struct.pack("<Q", BASE + 0x32CB7A6) + b"\xff\xe0")
        self.map(0x32CD000, wrapper, executable=True)
        self.call = C.WINFUNCTYPE(None, C.c_void_p, C.c_size_t)(BASE + 0x32CD000)

    def next_draw(self, *_):
        self.consumed += 1
        return self.draws[self.consumed - 1] if self.consumed <= len(self.draws) else 0

    def run(self, count, draws):
        records = self.buffer(max(16, count * 16))
        for i in range(count):
            self.put(records, i * 16, "IIQ", i, 0x12345678, 0x100000000 + i)
        self.draws, self.consumed = draws, 0
        self.call(C.addressof(records), count)
        if self.consumed != len(draws):
            raise RuntimeError(f"native shuffle consumed {self.consumed} draws, expected {len(draws)}")
        order = []
        for offset in range(0, count * 16, 16):
            tag, padding, pointer = struct.unpack_from("<IIQ", records, offset)
            if padding != 0x12345678 or pointer != 0x100000000 + tag:
                raise RuntimeError("native shuffle did not preserve whole actor records")
            order.append(tag)
        return {"count": count, "draws": draws, "order": order}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected 1.26.51.1 image")
    spans = {}
    for name, rva, size in (
        ("LevelChunk::tickBlockEntities", 0x32CB470, 0x84B),
        ("original actor shuffle", 0x32CB7A6, 0x93),
        ("actor insertion", 0x32C11AC, 0x54),
        ("actor erasure", 0x32C2DE8, 0xE1),
        ("comparator sampling interval gate", 0x4AB6956, 0x25),
        ("sculk receive signal", 0x4AE91E0, 0x9B),
    ):
        spans[name] = {"rva": hex(rva), "size": size, "sha256": hashlib.sha256(pe.read(rva, size)).hexdigest()}
    native = NativeActorShuffle(pe)
    cases = [native.run(0, []), native.run(1, [])]
    for n in range(2, 5):
        cases += [native.run(n, list(draws)) for draws in itertools.product(*(range(i+1) for i in range(1,n)))]
    rng = random.Random(0x2651)
    for n in (2, 3, 4, 7, 16, 33, 128, 257):
        for _ in range(12):
            cases.append(native.run(n, [rng.getrandbits(32) for _ in range(n-1)]))
    native.close()
    output = {"complete": True, "reference": __doc__, "client_sha256": digest, "spans": spans, "scenarios": cases}
    args.output.write_text(json.dumps(output, separators=(",", ":"))+"\n", encoding="utf-8")
    print(json.dumps({"complete": True, "cases": len(cases), "output": str(args.output)}))
