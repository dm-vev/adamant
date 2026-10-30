#!/usr/bin/env python3
"""Execute the original rail evaluator over supplied native source records.

Original BaseRailTransporter::evaluate and BaseCircuitComponent::getStrength
choose the remaining rail strength and change flag. Source records are inputs;
this does not execute circuit graph construction or world publication timing.
"""
import argparse
import ctypes as C
import hashlib
import itertools
import json
from pathlib import Path
import random
import struct

from redstone_native_probe import ClientPE
from redstone_native_piston_shapes_probe import NativePistonShapes
from redstone_native_lightning_search_probe import BASE


class NativeRailEvaluator(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        self.map(0x66D76D0, pe.read(0x66D76D0, 5), executable=True)
        self.map(0x66D8220, pe.read(0x66D8220, 0x82), executable=True)
        self.map(0x66D7850, pe.read(0x66D7850, 0x52E), executable=True)
        self.map(0x66D76C0, pe.read(0x66D76C0, 5), executable=True)
        self.map(0x2A72B90, pe.read(0x2A72B90, 5), executable=True)
        self.map(0xE7D61D0, pe.read(0xE7D61D0, 6))
        self.map(0x66D7F80, pe.read(0x66D7F80, 0x69), executable=True)
        self.rail, self.source_vt = self.buffer(0x70), self.buffer(16)
        self.put(self.source_vt, 8, 'Q', BASE + 0x66D76D0)
        self.evaluate = C.WINFUNCTYPE(C.c_bool, C.c_void_p, C.c_void_p, C.c_void_p)(BASE + 0x66D8220)
        self.add_source = C.WINFUNCTYPE(None, C.c_void_p, C.c_void_p)(BASE + 0x66D7850)
        self.allow_connection = C.WINFUNCTYPE(C.c_bool, C.c_void_p, C.c_void_p, C.c_void_p, C.c_void_p)(BASE + 0x66D7F80)
        self.other, self.other_vt, self.tracking = self.buffer(0x70), self.buffer(0xD8), self.buffer(0x90)
        self.put(self.other,0,'Q',C.addressof(self.other_vt))
        self.put(self.other_vt,0x10,'Q',BASE+0x66D76C0)
        self.put(self.other_vt,0xD0,'Q',BASE+0x2A72B90)
        self.put(self.tracking,0,'Q',C.addressof(self.other))

    def run(self, initial, sources):
        records = self.buffer(max(1, len(sources)) * 32)
        for n, source in enumerate(sources):
            actor = self.buffer(0x58)
            self.put(actor, 0, 'Q', C.addressof(self.source_vt))
            self.put(actor, 0x4E, 'h', source['strength'])
            self.put(records, n*32, 'Q', 0 if source.get('missing') else C.addressof(actor))
            self.put(records, n*32+8, 'i', source['loss'])
            self.put(records, n*32+28, 'i', source['remaining'])
        self.put(self.rail, 8, 'QQ', C.addressof(records), C.addressof(records)+len(sources)*32)
        self.put(self.rail, 0x4E, 'hh', initial, -123)
        changed = self.evaluate(C.addressof(self.rail), 0, 0)
        strength, previous = struct.unpack('<hh', C.string_at(C.addressof(self.rail)+0x4E, 4))
        return dict(initial=initial, sources=sources, strength=strength, previous=previous, changed=changed)

    def replace(self, old_remaining, new_remaining, face, target):
        # Supply an existing matching identity+face. The original complete
        # function takes its replacement branch; no host allocation or set
        # implementation is involved. Actor pointers are opaque identity input.
        records = self.buffer(4*32)
        for n in range(4):
            self.put(records,n*32,'QiiiiBB2xi',0x100000+n*256,n,n,65,n,face if n==target else (face+1)%6,1,old_remaining)
        before = C.string_at(C.addressof(records),len(records))
        incoming = self.buffer(32)
        self.put(incoming,0,'QiiiiBB2xi',0x100000+target*256,7,100,70,-100,face,0,new_remaining)
        self.put(self.rail,8,'QQQ',C.addressof(records),C.addressof(records)+len(records),C.addressof(records)+len(records))
        self.add_source(C.addressof(self.rail),C.addressof(incoming))
        after = C.string_at(C.addressof(records),len(records))
        if after != before[:target*32]+bytes(incoming)+before[(target+1)*32:]:
            raise RuntimeError('native source replacement did not replace exactly the matching record')
        return dict(old_remaining=old_remaining,new_remaining=new_remaining,face=face,target=target,
                    before=before.hex(),incoming=bytes(incoming).hex(),after=after.hex())

    def connection(self, direction, other_direction, face, family, other_family, source_type, direct):
        self.put(self.rail,0x52,'B',direction)
        self.put(self.rail,0x68,'i',family)
        self.put(self.other,0x52,'B',other_direction)
        self.put(self.other,0x60,'Q',source_type)
        self.put(self.other,0x68,'i',other_family)
        self.put(self.tracking,0x14,'B',face)
        flag=C.c_bool(direct)
        allowed=self.allow_connection(C.addressof(self.rail),0,C.addressof(self.tracking),C.addressof(flag))
        return dict(direction=direction,other_direction=other_direction,face=face,family=family,other_family=other_family,
                    source_type=source_type,direct=direct,direct_after=flag.value,allowed=allowed)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    native = NativeRailEvaluator(pe)
    try:
        cases = [native.run(initial, [dict(strength=strength, loss=loss, remaining=remaining)])
                 for initial, strength, loss, remaining in itertools.product(
                     (0, 1, 9, 15), (0, 1, 2, 8, 9, 15), (0, 1, 2, 8, 15), (-1, 0, 1, 2, 8, 9))]
        cases += [native.run(initial, []) for initial in (0, 1, 9, 15)]
        rng = random.Random(20261028)
        for _ in range(128):
            cases.append(native.run(rng.randrange(16), [dict(strength=rng.randrange(16), loss=rng.randrange(16),
                remaining=rng.randrange(10), missing=rng.randrange(5)==0) for _ in range(rng.randrange(1,9))]))
        replacements = [native.replace(old,new,face,target) for old,new,face,target in itertools.product((1,8,9),(1,8,9),range(6),range(4))]
        connections = [native.connection(*c) for c in itertools.product(range(6),range(6),range(6),(0,1),(0,1),(0x10000,0x80000),(False,True))]
    finally:
        native.close()
    args.output.write_text(json.dumps(dict(complete=True, reference=__doc__, client_sha256=digest,
        functions=[dict(rva=hex(rva), size=size, sha256=hashlib.sha256(pe.read(rva,size)).hexdigest())
                   for rva,size in ((0x66D8220,0x82),(0x66D76D0,5),(0x66D7850,0x52E),(0x66D7F80,0x69),(0x66D76C0,5),(0x2A72B90,5))],
        scenarios=cases,replacements=replacements,connections=connections), separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, cases=len(cases),replacements=len(replacements),connections=len(connections))))
