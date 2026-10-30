#!/usr/bin/env python3
"""Run native clip traversal and ray/AABB tests with supplied air/wool cells.

The original BlockSource::clip, full ray/AABB constructor and wool predicate
execute unchanged. World block lookup and unit-cube wool shapes are supplied
callbacks. This is component execution, not a full native vibration listener.
Vibration endpoints are centered/offset using the inspected binary32 operations.
No extracted game code is stored in the repository.
"""

import argparse
import ctypes as C
from ctypes import wintypes as W
import hashlib
import itertools
import json
import math
from pathlib import Path
import random
import struct

from redstone_native_probe import ClientPE
from redstone_native_lightning_search_probe import NativeSearch, BASE


SPANS = {"clip": (0x2D8AAA0, 0x1435), "ray_aabb": (0x2D95AA0, 0xDA0),
         "wool_predicate": (0x25F12D0, 0x11), "vibration_offsets": (0x25D892C, 0x1B5)}


def f32(x):
    return C.c_float(x).value


class NativeOcclusion(NativeSearch):
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
        for rva, size in ((0xE6D7DE8,24), (0xE876814,24), (0xE5A7060,4), (0xE83F6FC,4),
                          (0xE76CD78,4), (0xE82C178,4), (0x117DA080,8),
                          (0xF0FBAA8,8), (0xE829950,40)):
            self.map(rva, pe.read(rva,size))
        for name in ("clip", "ray_aabb", "wool_predicate"):
            rva,size=SPANS[name]
            self.map(rva,pe.read(rva,size),executable=True)
        if pe.read(0xE585660,2) != b"\xff\xe0":
            raise RuntimeError("unexpected indirect dispatcher")
        self.map(0xE585660,pe.read(0xE585660,2),executable=True)
        # Compiler cookie validation is plumbing, not the clipping algorithm.
        self.map(0xE03C880,b"\xc3",executable=True)
        self.source,self.source_vtable=self.buffer(0x80),self.buffer(0x80)
        self.put(self.source,0,"Q",C.addressof(self.source_vtable))
        self.air_type,self.wool_type=self.buffer(0x180),self.buffer(0x180)
        self.air_vtable,self.wool_vtable=self.buffer(0x200),self.buffer(0x200)
        self.air_material,self.wool_material=self.buffer(8),self.buffer(8)
        self.air,self.wool=self.buffer(0x90),self.buffer(0x90)
        self.put(self.air_type,0,"Q",C.addressof(self.air_vtable))
        self.put(self.wool_type,0,"Q",C.addressof(self.wool_vtable))
        self.put(self.air_type,0x28,"Q",C.addressof(self.air_material))
        self.put(self.wool_type,0x28,"Q",C.addressof(self.wool_material))
        self.put(self.wool_material,2,"B",1)
        self.put(self.wool_type,0x134,"B",0x10)
        self.put(self.air,0x68,"Q",C.addressof(self.air_type))
        self.put(self.wool,0x68,"Q",C.addressof(self.wool_type))
        self.is_air=self.callback(C.c_bool,[C.c_void_p,C.c_int],lambda *_: True)
        self.is_wool=self.callback(C.c_bool,[C.c_void_p,C.c_int],lambda *_: False)
        self.put(self.air_vtable,0x1D0,"Q",C.cast(self.is_air,C.c_void_p).value)
        self.put(self.wool_vtable,0x1D0,"Q",C.cast(self.is_wool,C.c_void_p).value)
        self.lookup=self.callback(C.c_void_p,[C.c_void_p,C.c_void_p],self.get_block)
        self.extra=self.callback(C.c_void_p,[C.c_void_p,C.c_void_p],lambda *_: C.addressof(self.air))
        self.border=self.callback(C.c_bool,[C.c_void_p,C.c_void_p],lambda *_: False)
        for offset,callback in ((0x10,self.lookup),(0x28,self.extra),(0x68,self.border)):
            self.put(self.source_vtable,offset,"Q",C.cast(callback,C.c_void_p).value)
        self.shape_clip=self.callback(C.c_void_p,[C.c_void_p]*8,self.clip_wool)
        self.bridge(0x2A6B5D0,C.cast(self.shape_clip,C.c_void_p).value)
        self.ray_box=C.WINFUNCTYPE(C.c_void_p,*([C.c_void_p]*5))(BASE+0x2D95AA0)
        self.clip=C.WINFUNCTYPE(C.c_void_p,C.c_void_p,C.c_void_p,C.c_void_p,C.c_void_p,
                               C.c_bool,C.c_ubyte,C.c_int,C.c_bool,C.c_bool,C.c_void_p,
                               C.c_void_p,C.c_bool)(BASE+0x2D8AAA0)
        self.predicate,self.functor=self.buffer(64),self.buffer(8)
        self.put(self.functor,0,"Q",BASE+0xE829950)
        self.put(self.predicate,0x38,"Q",C.addressof(self.functor))
        self.epsilon=struct.unpack("<f",pe.read(0xE69FDC0,4))[0]

    def get_block(self,_source,pos):
        cell=tuple(C.cast(pos,C.POINTER(C.c_int))[:3])
        self.cells.append(list(cell))
        return C.addressof(self.wool if cell in self.occluders else self.air)

    def clip_wool(self,_block,result,_source,pos,start,end,_shape,_interface):
        cell=C.cast(pos,C.POINTER(C.c_int))[:3]
        box=(C.c_float*6)(*(f32(x) for x in cell),*(f32(f32(x)+1) for x in cell))
        self.ray_box(result,pos,start,end,C.addressof(box))
        if C.c_int.from_address(result+0x18).value==0:
            self.unit_hits.append(list(cell))
        return result

    def ray(self,start,end,occluders):
        self.cells,self.unit_hits=[],[]
        self.occluders={tuple(p) for p in occluders}
        a,b=(C.c_float*3)(*start),(C.c_float*3)(*end)
        result=self.buffer(0x88)
        self.clip(C.addressof(self.source),C.addressof(result),C.addressof(a),C.addressof(b),
                  False,0,200,False,False,None,C.addressof(self.predicate),False)
        return {"start":list(a),"end":list(b),"cells":self.cells,"unit_hits":self.unit_hits,
                "hit":struct.unpack_from("<i",result,0x18)[0]==0}

    def vibration(self,origin,listener,occluders):
        centre=lambda p:[f32(f32(math.floor(f32(x)))+.5) for x in p]
        start,end=centre(origin),centre(listener)
        rays=[]
        for face in range(6):
            a=start.copy()
            axis=(1,1,2,2,0,0)[face]
            a[axis]=f32(a[axis]+(-self.epsilon if face%2==0 else self.epsilon))
            rays.append(self.ray(a,end,occluders))
        return {"origin":origin,"listener":listener,"occluders":occluders,
                "rays":rays,"occluded":all(r["hit"] for r in rays)}

    def unit_intersection(self,pos,start,end):
        cell=(C.c_int*3)(*pos)
        a,b=(C.c_float*3)(*start),(C.c_float*3)(*end)
        result=self.buffer(0x88)
        self.unit_hits=[]
        self.clip_wool(None,C.addressof(result),None,C.addressof(cell),C.addressof(a),C.addressof(b),None,None)
        return {"pos":pos,"start":list(a),"end":list(b),
                "hit":struct.unpack_from("<i",result,0x18)[0]==0}


if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client",type=Path,required=True)
    parser.add_argument("--output",type=Path,required=True)
    args=parser.parse_args()
    pe=ClientPE(args.client)
    digest=hashlib.sha256(pe.data).hexdigest()
    if digest!="8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected 1.26.51.1 image")
    native=NativeOcclusion(pe)
    try:
        cases=[]
        for anchor,(dx,dz),mask in itertools.product((0,128,256,-257,32768,1000000,-1000000,8388608,16777216,29999980),
                                                    ((1,1),(1,-1),(-1,1),(-1,-1)),range(5)):
            a=[anchor+.5,65.5,anchor+.5]
            b=[anchor+4*dx+.5,65.5,anchor+4*dz+.5]
            choices=[[],[[anchor+2*dx,65,anchor+dz]],[[anchor+dx,65,anchor+2*dz]],
                     [[anchor+2*dx,65,anchor+dz],[anchor+dx,65,anchor+2*dz]],
                     [[anchor+2*dx,65,anchor+2*dz]]]
            cases.append(native.vibration(a,b,choices[mask]))
        for a,b,wool in (
            ([.01,65.01,.01],[.99,65.99,.99],[[0,65,0]]),
            ([.5,65.5,.5],[.5,65.5,.5],[]),
            ([.5,65.5,.5],[1.5,65.5,.5],[[0,65,0]]),
            ([.5,65.5,.5],[1.5,65.5,.5],[[1,65,0]]),
            ([-.01,65.9,-.01],[.01,65.1,.01],[[-1,65,-1]]),
            ([1000000.01,65.01,1000000.01],[1000000.99,65.99,1000000.99],[[1000000,65,1000000]]),
            ([.99999999,65.01,.99999999],[1.99999999,65.99,1.99999999],[[1,65,1]]),
            ([-.00000001,65.5,-.00000001],[3.99,65.5,3.99],[[1,65,1]]),
        ):
            cases.append(native.vibration(a,b,wool))
        # Exact boundary and non-45-degree rays observe the full original DDA.
        rng=random.Random(2651)
        paths=[]
        for _ in range(100):
            a=[rng.randrange(-8,9)+rng.choice((0,.5,.999999)) for _ in range(3)]
            b=[rng.randrange(-8,9)+rng.choice((0,.5,.999999)) for _ in range(3)]
            paths.append(native.ray(a,b,[]))
        for anchor,(dx,dz) in itertools.product((0,128,256,32768,1000000,8388608,29999980),
                                                ((1,1),(1,-1),(-1,1),(-1,-1))):
            paths.append(native.ray([anchor+.5,65.5,anchor+.5],
                                    [anchor+7*dx+.5,68.5,anchor+3*dz+.5],[]))
        for a,b in (([0,65,0],[1,65,0]),([1,65,0],[0,65,0]),
                    ([.5,65.5,.5],[.5,65.5,.5]),([0,0,0],[-1,-1,-1]),
                    ([.5,65.5,.5],[512.5,65.5,.5])):
            paths.append(native.ray(a,b,[]))
        unit=[]
        offsets=(-.5,0,1e-8,.25,.5,1-1e-8,1,1.5)
        for anchor in (0,1000000,8388608,16777216):
            pos=[anchor,65,anchor]
            for _ in range(48):
                a=[x+rng.choice(offsets) for x in pos]
                b=[x+rng.choice(offsets) for x in pos]
                unit.append(native.unit_intersection(pos,a,b))
        for a,b in (([-.000001,65.5,.5],[.000001,65.5,.5]),
                    ([-.0001581,65.5,.5],[.0001581,65.5,.5]),
                    ([-.0001582,65.5,.5],[.0001582,65.5,.5]),
                    ([0,65.1,.5],[0,65.9,.5]),
                    ([-.5,66,.5],[1.5,66,.5]),
                    ([-.5,66.0000001,.5],[1.5,66.0000001,.5]),
                    ([-.5,66.00001,.5],[1.5,66.00001,.5]),
                    ([.5,65.5,.5],[1,65.5,.5]),
                    ([1,65.5,.5],[1.5,65.5,.5]),
                    ([.5,65.5,.5],[.5,65.5,.5])):
            unit.append(native.unit_intersection([0,65,0],a,b))
        output={"complete":True,"binary_sha256":digest,"reference":__doc__,
                "epsilon":native.epsilon,"functions":{name:{"rva":hex(rva),"size":size,"executed":name!="vibration_offsets",
                "sha256":hashlib.sha256(pe.read(rva,size)).hexdigest()} for name,(rva,size) in SPANS.items()},
                "scenarios":cases,"paths":paths,"unit_intersections":unit}
        args.output.write_text(json.dumps(output,separators=(",",":"))+"\n",encoding="utf-8")
        print(json.dumps({"cases":len(cases),"paths":len(paths),"unit_intersections":len(unit),"epsilon":native.epsilon}))
    finally:
        native.close()
