#!/usr/bin/env python3
"""Execute the original positional sort called by PistonBlockActor.

Only byte movement is supplied by the host runtime. Inputs contain at most
twelve positions, the native piston limit, so the original insertion path runs.
The PDB's nearest unrelated name does not identify this template instantiation;
the call at client RVA 0x4AE33E0 establishes its piston use.
"""

import argparse
import ctypes as C
from ctypes import wintypes as W
import hashlib
import json
from pathlib import Path
import random

from redstone_native_probe import ClientPE
from redstone_native_lightning_search_probe import NativeSearch, BASE


class NativePistonSort(NativeSearch):
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
        self.map(0x117DA080, pe.read(0x117DA080, 8))
        self.map(0x4AF9D70, pe.read(0x4AF9D70, 0x903), executable=True)
        self.crt = C.CDLL("msvcrt")
        self.bridge(0xE5A62C0, C.cast(self.crt.memmove, C.c_void_p).value)
        self.sort = C.WINFUNCTYPE(None, C.c_void_p, C.c_void_p, C.c_size_t, C.c_void_p)(BASE + 0x4AF9D70)

    def run(self, face, positions):
        if len(positions) > 12:
            raise ValueError("input exceeds piston limit")
        vector = (C.c_int32 * max(3, len(positions) * 3))(*[v for p in positions for v in p])
        direction = (C.c_float * 3)(*[(0,-1,0),(0,1,0),(0,0,-1),(0,0,1),(-1,0,0),(1,0,0)][face])
        start = C.addressof(vector)
        self.sort(start, start + len(positions) * 12, len(positions), C.addressof(direction))
        return {"face":face,"before":positions,"after":[list(vector[i*3:i*3+3]) for i in range(len(positions))]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != "8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected image")
    native = NativePistonSort(pe)
    rng = random.Random(2651)
    cases = []
    try:
        for face in range(6):
            for size in range(13):
                points = [[n//3,65+(n%3),n//4] for n in range(size)]
                for order in (points,list(reversed(points)),rng.sample(points,len(points))):
                    cases.append(native.run(face,order))
        for _ in range(100):
            anchor = rng.choice((0,29999980,-29999980,2147483640,-2147483640))
            positions = [[C.c_int32(anchor+rng.randrange(-12,13)).value for _ in range(3)] for _ in range(rng.randrange(13))]
            cases.append(native.run(rng.randrange(6),positions))
    finally:
        native.close()
    args.output.write_text(json.dumps({"complete":True,"reference":__doc__,"client_sha256":digest,
        "function":{"rva":"0x4af9d70","size":0x903,"sha256":hashlib.sha256(pe.read(0x4AF9D70,0x903)).hexdigest()},
        "scenarios":cases},separators=(",",":"))+"\n",encoding="utf-8")
    print(json.dumps({"complete":True,"cases":len(cases)}))
