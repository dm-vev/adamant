#!/usr/bin/env python3
"""Execute original per-chunk snow LCG/coordinate instructions on local inputs.

The contiguous arithmetic window is unchanged. A small ABI wrapper supplies
the original frame/chunk fields and returns immediately after the three output
stores, before height lookup and weather block callbacks. No live process is
modified; this corpus proves coordinate arithmetic, not precipitation effects.
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


class NativeSnowRandom(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        self.frame, self.chunk = self.buffer(0x300), self.buffer(0x1530)
        self.put(self.frame, 0x1F0, 'Q', C.addressof(self.chunk))
        self.map(0x32C9B55, pe.read(0x32C9B55, 0x4E) + bytes.fromhex('5b5dc3'), executable=True)
        wrapper = bytes.fromhex('55534889cd89d348b8') + struct.pack('<Q', BASE + 0x32C9B55) + bytes.fromhex('ffe0')
        self.map(0x10000000, wrapper, executable=True)
        self.function = C.WINFUNCTYPE(None, C.c_void_p, C.c_int32)(BASE + 0x10000000)

    def sample(self, state, x, z, height):
        self.put(self.chunk, 0x1528, 'I', state)
        self.put(self.chunk, 0x70, 'i', height)
        self.put(self.frame, 0x1B8, 'i', z)
        self.function(C.addressof(self.frame), x)
        return dict(state=struct.unpack_from('<I', self.chunk, 0x1528)[0],
                    position=list(struct.unpack_from('<iii', self.frame, 0x1C0)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    client = ClientPE(args.client)
    digest = hashlib.sha256(client.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    native, cases = NativeSnowRandom(client), []
    try:
        for state, x, z, height in itertools.product(
                (0, 1, 0x283AE83, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff),
                (0, 16, -16, 1000000, -1000000, 29999984, -30000000),
                (0, 16, -16), (-64, 0, 65, 319)):
            cases.append(dict(initial_state=state, x=x, z=z, height=height,
                              **native.sample(state, x, z, height)))
    finally:
        native.close()
    result = dict(complete=True, reference=__doc__, client_sha256=digest,
                  function=dict(rva='0x32c9b55', size=0x4e,
                                sha256=hashlib.sha256(client.read(0x32C9B55, 0x4E)).hexdigest()),
                  scenarios=cases)
    args.output.write_text(json.dumps(result, separators=(',', ':')) + '\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, scenarios=len(cases), output=str(args.output))), flush=True)


if __name__ == '__main__':
    main()
