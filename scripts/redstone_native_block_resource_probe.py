#!/usr/bin/env python3
"""Execute the complete original BlockType::popResource with supplied world callbacks.

Native guards, float draws and spawn-point arithmetic run unchanged in a local
copy of the pinned client function. The factory captures its arguments instead
of constructing an actor; constructor RNG and item movement require live BDS
evidence separately. No game process or binary is modified.
"""
import argparse
import ctypes as C
import hashlib
import itertools
import json
from pathlib import Path
import struct

from redstone_bds_level_random_probe import NativeCoreRandom
from redstone_native_lightning_search_probe import BASE
from redstone_native_probe import ClientPE


class NativeBlockResource(NativeCoreRandom):
    def __init__(self, pe):
        super().__init__(pe)
        for rva, size in ((0xE760F14, 4), (0xE6AE4DC, 4), (0xE6AE3F0, 8)):
            self.map(rva, pe.read(rva, size))
        self.map(0x26DCB0, pe.read(0x26DCB0, 0x23), executable=True)
        self.float_function = C.WINFUNCTYPE(C.c_float, C.c_void_p)(BASE+0x26DCB0)
        self.resource_source, self.resource_source_vt = self.buffer(16), self.buffer(0x1D0)
        self.level, self.level_vt = self.buffer(16), self.buffer(0xA78)
        self.random, self.random_vt = self.buffer(16), self.buffer(0x30)
        self.factory, self.factory_vt = self.buffer(16), self.buffer(0x50)
        self.position, self.stack = self.buffer(12), self.buffer(0x80)
        for obj, table in ((self.resource_source, self.resource_source_vt), (self.level, self.level_vt),
                           (self.random, self.random_vt), (self.factory, self.factory_vt)):
            self.put(obj, 0, 'Q', C.addressof(table))

        def can_drop(_, owner):
            self.calls.append(dict(call='can_drop', has_owner=bool(owner)))
            return self.case['drops']

        def get_level(_):
            self.calls.append(dict(call='get_level'))
            return C.addressof(self.level)

        def get_random(_):
            self.calls.append(dict(call='get_random'))
            return C.addressof(self.random)

        def draw(_):
            value = self.float_function(C.addressof(self.state))
            self.calls.append(dict(call='float', value=value))
            return value

        def get_factory(_):
            self.calls.append(dict(call='get_factory'))
            return C.addressof(self.factory)

        def create(_, source, stack, owner, position, delay):
            self.calls.append(dict(call='create', source_matches=source==C.addressof(self.resource_source),
                                   stack_matches=stack==C.addressof(self.stack), has_owner=bool(owner),
                                   position=list(struct.unpack('<3f', C.string_at(position, 12))), pickup_delay=delay))
            return 0x123450

        for table, offset, result, params, callback in (
            (self.resource_source_vt, 0x1C8, C.c_bool, [C.c_void_p]*2, can_drop),
            (self.resource_source_vt, 0x158, C.c_void_p, [C.c_void_p], get_level),
            (self.level_vt, 0xA70, C.c_void_p, [C.c_void_p], get_random),
            (self.random_vt, 0x28, C.c_float, [C.c_void_p], draw),
            (self.level_vt, 0xA8, C.c_void_p, [C.c_void_p], get_factory),
            (self.factory_vt, 0x48, C.c_void_p, [C.c_void_p]*5+[C.c_int], create)):
            cb = self.callback(result, params, callback)
            self.put(table, offset, 'Q', C.cast(cb, C.c_void_p).value)
        self.map(0x31840D0, pe.read(0x31840D0, 0x1C0), executable=True)
        self.pop_function = C.WINFUNCTYPE(C.c_void_p, *([C.c_void_p]*4))(BASE+0x31840D0)

    def run(self, case):
        self.case, self.calls = case, []
        self.seed(case['seed'])
        before = C.string_at(self.state, 0xA00)
        self.put(self.position, 0, '3i', *case['position'])
        self.put(self.stack, 0x22, 'B', not case['empty'])
        returned = self.pop_function(C.addressof(self.resource_source), C.addressof(self.position),
                                     C.addressof(self.stack), 0x246800 if case['owner'] else 0)
        after = C.string_at(self.state, 0xA00)
        self.reset(before)
        consumed = 3 if returned else 0
        words = [self.draw() for _ in range(consumed)]
        floats = [entry['value'] for entry in self.calls if entry['call']=='float']
        expected_floats = [struct.unpack('<f',struct.pack('<f',word/4294967296))[0] for word in words]
        if floats != expected_floats:
            raise RuntimeError('original float callback does not match the consumed raw words')
        if C.string_at(self.state, 0xA00) != after:
            raise RuntimeError('original popResource RNG consumption differs')
        next_word = self.draw()
        expected = case['drops'] and not case['empty']
        if bool(returned) != expected or (returned and returned != 0x123450):
            raise RuntimeError('original popResource resource guard differs')
        create = [entry for entry in self.calls if entry['call']=='create']
        if len(create) != int(expected) or (create and (create[0]['has_owner'] or create[0]['pickup_delay']!=10 or
                                                       not create[0]['source_matches'] or not create[0]['stack_matches'])):
            raise RuntimeError('original popResource factory arguments differ')
        return dict(**case, created=bool(returned), calls=self.calls, words=words, next_word=next_word,
                    origin=create[0]['position'] if create else None)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    native, scenarios = NativeBlockResource(pe), []
    try:
        for seed, position, drops, empty, owner in itertools.product(
                (0, 1, 5489, 0x12345678, 0x80000000, 0xFFFFFFFF),
                ((0,65,0), (-1,-60,-1), (1000000,319,-1000000),
                 (16777215,64,-16777216), (29999983,60,-29999984)),
                (False,True), (False,True), (False,True)):
            scenarios.append(native.run(dict(seed=seed,position=list(position),drops=drops,empty=empty,owner=owner)))
    finally:
        native.close()
    result = dict(complete=True, reference=__doc__, client_sha256=digest, scenarios=scenarios,
                  window=dict(rva='0x31840d0', size=0x1C0,
                              sha256=hashlib.sha256(pe.read(0x31840D0,0x1C0)).hexdigest()))
    args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=True,scenarios=len(scenarios),output=str(args.output))),flush=True)
