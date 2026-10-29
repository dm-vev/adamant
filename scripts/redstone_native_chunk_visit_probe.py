#!/usr/bin/env python3
"""Execute original BDS rectangular chunk traversal and client LFSR arithmetic.

The full inspected BDS helper receives supplied bounded RNG inputs and a
callback that records every visited coordinate. A separate unmodified client
arithmetic window supplies all 255 nonzero LFSR transitions. No live process
or on-disk executable is changed; callbacks do not simulate chunk contents.
"""
import argparse
import ctypes as C
import hashlib
import json
from pathlib import Path
import struct

from redstone_native_probe import ClientPE
from redstone_native_piston_shapes_probe import NativePistonShapes
from redstone_native_lightning_search_probe import BASE


class NativeChunkVisit(NativePistonShapes):
    def __init__(self, client, bds):
        super().__init__(client)
        self.map(0x581E840, bds.read(0x581E840, 0x165), executable=True)
        self.map(0xADA9220, struct.pack('<Q', BASE + 0xE585660))
        self.random, self.random_vt = self.buffer(8), self.buffer(16)
        self.function_object, self.callback_object, self.callback_vt = self.buffer(0x40), self.buffer(8), self.buffer(0x28)
        self.put(self.random, 0, 'Q', C.addressof(self.random_vt))
        self.put(self.function_object, 0x38, 'Q', C.addressof(self.callback_object))
        self.put(self.callback_object, 0, 'Q', C.addressof(self.callback_vt))

        def draw(_, bound):
            if bound != 255 or self.consumed >= len(self.words):
                self.error = 'unexpected native RNG request';return 0
            value = self.words[self.consumed] % bound
            self.consumed += 1
            return value

        def visit(_, position):
            x, y, z = struct.unpack('<iii', C.string_at(position, 12))
            if y != 0:self.error = 'unexpected native Y coordinate'
            self.positions.append([x,z])

        random = self.callback(C.c_uint32, [C.c_void_p,C.c_uint32], draw)
        visitor = self.callback(None, [C.c_void_p,C.c_void_p], visit)
        destructor = self.callback(None, [C.c_void_p,C.c_bool], lambda *_:None)
        self.put(self.random_vt, 8, 'Q', C.cast(random,C.c_void_p).value)
        self.put(self.callback_vt, 0x10, 'Q', C.cast(visitor,C.c_void_p).value)
        self.put(self.callback_vt, 0x20, 'Q', C.cast(destructor,C.c_void_p).value)
        self.function = C.WINFUNCTYPE(None,C.c_ubyte,C.c_ubyte,C.c_void_p,C.c_void_p)(BASE+0x581E840)

        # Original client outer-loop arithmetic uses EDI. Preserve its Win64
        # nonvolatile value in the authored ABI wrapper and return the result.
        self.map(0x351AB19,client.read(0x351AB19,36)+bytes.fromhex('89f85fc3'),executable=True)
        wrapper=bytes.fromhex('5789cf48b8')+struct.pack('<Q',BASE+0x351AB19)+bytes.fromhex('ffe0')
        self.map(0x10000000,wrapper,executable=True)
        self.step=C.WINFUNCTYPE(C.c_uint32,C.c_uint32)(BASE+0x10000000)
        self.tick_value=self.buffer(8)
        self.map(0x1171B4B,client.read(0x1171B4B,0x37)+bytes.fromhex('4488c05e415f415ec3'),executable=True)
        phase_wrapper=(bytes.fromhex('41564157564889ce4989d649bf')+struct.pack('<Q',0xcccccccccccccccd)
                       +bytes.fromhex('48b8')+struct.pack('<Q',BASE+0x1171B4B)+bytes.fromhex('ffe0'))
        self.map(0x10001000,phase_wrapper,executable=True)
        self.phase=C.WINFUNCTYPE(C.c_bool,C.c_void_p,C.c_uint64)(BASE+0x10001000)
        self.circle_bounds=self.buffer(0x190)
        self.map(0xE5C6370,client.read(0xE5C6370,4))
        self.map(0xE7B9B88,client.read(0xE7B9B88,4))
        # Mutex operations surround this contiguous native arithmetic window;
        # local supplied Bounds need no mutex. Preserve all Win64 nonvolatile
        # registers, including XMM6..9 used by the original radius calculation.
        circle_tail=bytes.fromhex('450f2ec80f97c0f30f6f3424f30f6f7c2410f3440f6f442420f3440f6f4c24304883c4485e415f415e415cc3')
        self.map(0x351AC1A,client.read(0x351AC1A,0xA9)+circle_tail,executable=True)
        circle_wrapper=(bytes.fromhex('415441564157564883ec48f30f7f3424f30f7f7c2410f3440f7f442420f3440f7f4c2430'
                                      '4989cc4189d64489c64589cf48b8')+struct.pack('<Q',BASE+0xE5C6370)
                        +bytes.fromhex('f30f103048b8')+struct.pack('<Q',BASE+0xE7B9B88)
                        +bytes.fromhex('f30f103848b8')+struct.pack('<Q',BASE+0x351AC1A)+bytes.fromhex('ffe0'))
        self.map(0x10002000,circle_wrapper,executable=True)
        self.circle_function=C.WINFUNCTYPE(C.c_bool,C.c_void_p,C.c_int32,C.c_int32,C.c_int32)(BASE+0x10002000)

    def run(self,width,depth,words):
        self.words,self.consumed,self.positions,self.error=words,0,[],None
        self.put(self.function_object,0x38,'Q',C.addressof(self.callback_object))
        self.function(width,depth,C.addressof(self.random),C.addressof(self.function_object))
        if self.error or self.consumed!=2:raise RuntimeError(self.error or 'wrong native draw count')
        if len(self.positions)!=width*depth or len({tuple(p) for p in self.positions})!=width*depth:
            raise RuntimeError('native traversal did not visit each cell once')
        return dict(width=width,depth=depth,words=words,positions=self.positions)

    def randomized(self,tick,index):
        self.put(self.tick_value,0,'Q',tick)
        return self.phase(C.addressof(self.tick_value),index)

    def circle(self,origin,radius,offset):
        self.put(self.circle_bounds,0x140,'3i',origin[0]-radius,0,origin[1]-radius)
        self.put(self.circle_bounds,0x14C,'3i',origin[0]+radius,0,origin[1]+radius)
        self.put(self.circle_bounds,0x158,'i',radius*2+1)
        return self.circle_function(C.addressof(self.circle_bounds),origin[0]+offset[0],0,origin[1]+offset[1])


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('client','binary','output'):parser.add_argument('--'+name,type=Path,required=True)
    args=parser.parse_args()
    client,bds=ClientPE(args.client),ClientPE(args.binary)
    if hashlib.sha256(client.data).hexdigest()!='8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    if hashlib.sha256(bds.data).hexdigest()!='76d547f82e02c18d0986c30b47132c9cc4171d0f2df1c00649e50ff35788b321':
        raise RuntimeError('BDS differs from inspected image')
    native=NativeChunkVisit(client,bds)
    try:
        steps=[dict(state=s,next=native.step(s)) for s in range(1,256)]
        cases=[native.run(w,d,list(words)) for w,d in ((1,1),(1,3),(3,1),(3,3),(4,4),(7,5),(16,12),(1,255),(255,1),(16,16))
               for words in ((0,0),(1,254),(254,1),(0xffffffff,0x80000000),(123,77),(254,254))]
        ticks=list(range(41))+[0x7fffffff,0xffffffff,0x7fffffffffffffff,0xffffffffffffffff]
        indices=list(range(42))+[255,0x7fffffff,0xffffffff,0x7fffffffffffffff,0xffffffffffffffff]
        phases=[dict(tick=tick,index=index,randomize=native.randomized(tick,index)) for tick in ticks for index in indices]
        circles=[]
        for origin in ((0,0),(17,-17),(1000000,-1000000),(-1875000,1874999)):
            for radius in (1,2,4,6,8,16,32,64):
                offsets=sorted({-radius-1,-radius,-radius+1,-radius//2,0,radius//2,radius-1,radius,radius+1})
                for x in offsets:
                    for z in offsets:
                        circles.append(dict(origin=list(origin),radius=radius,offset=[x,z],inside=native.circle(origin,radius,(x,z))))
    finally:native.close()
    result=dict(complete=True,reference=__doc__,client_sha256=hashlib.sha256(client.data).hexdigest(),
                binary_sha256=hashlib.sha256(bds.data).hexdigest(),steps=steps,scenarios=cases,phases=phases,circles=circles,
                spans=dict(bds=dict(rva='0x581e840',size=0x165,sha256=hashlib.sha256(bds.read(0x581E840,0x165)).hexdigest()),
                           client=dict(rva='0x351ab19',size=36,sha256=hashlib.sha256(client.read(0x351AB19,36)).hexdigest()),
                           phase=dict(rva='0x1171b4b',size=0x37,sha256=hashlib.sha256(client.read(0x1171B4B,0x37)).hexdigest()),
                           circle=dict(rva='0x351ac1a',size=0xa9,sha256=hashlib.sha256(client.read(0x351AC1A,0xA9)).hexdigest())))
    args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=True,steps=len(steps),cases=len(cases),phases=len(phases),circles=len(circles),output=str(args.output))),flush=True)


if __name__=='__main__':main()
