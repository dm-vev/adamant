#!/usr/bin/env python3
"""Execute original seeded Random construction and 2,000 raw draws per seed."""
import argparse
import hashlib
import json
from pathlib import Path

from redstone_native_probe import ClientPE
from redstone_bds_level_random_probe import NativeCoreRandom


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    client = ClientPE(args.client)
    digest = hashlib.sha256(client.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    native, cases, bounded = NativeCoreRandom(client), [], []
    try:
        for seed in (0, 1, 2, 5489, 0x12345678, 0x80000000, 0xFFFFFFFF):
            native.seed(seed)
            cases.append(dict(seed=seed, words=[native.draw() for _ in range(2000)]))
            for bound in (0, 1, 2, 3, 16, 100000, 2147483647):
                native.seed(seed)
                values = [native.bounded(bound) for _ in range(40)]
                bounded.append(dict(seed=seed, bound=bound, values=values, next_word=native.draw()))
    finally:
        native.close()
    spans = [dict(rva=hex(rva), size=size, sha256=hashlib.sha256(client.read(rva, size)).hexdigest())
             for rva, size in ((0x2203820, 0xC7), (0x26DAE0, 0x1C8), (0x26DD50, 0x22))]
    args.output.write_text(json.dumps(dict(complete=True, client_sha256=digest, reference=__doc__, functions=spans,
                                         scenarios=cases, bounded=bounded), separators=(',', ':')) + '\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, seeds=len(cases), draws=len(cases)*2000, bounded=len(bounded))))
