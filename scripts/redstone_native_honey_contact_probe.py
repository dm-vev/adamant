#!/usr/bin/env python3
"""Execute the original client honey-contact leaf with explicit contact inputs.

The original instructions modify StateVector velocity and FallDistance. Contact
collection and actor eligibility are explicit inputs, not simulated by Python.
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


class NativeHoney(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        for rva,size in ((0xE602290,4),(0xE98A5D0,4),(0xE780AE8,4),(0xE5C6370,4),(0xE5C6380,16),(0xE98A5CC,4)):
            self.map(rva,pe.read(rva,size))
        self.map(0x63AF270,pe.read(0x63AF270,0x135),executable=True)
        self.honey=C.WINFUNCTYPE(None,*([C.c_void_p]*4))(BASE+0x63AF270)
        self.map(0x66BA420,pe.read(0x66BA420,0x196),executable=True)
        for rva in (0xE6AE460,0xE6AE454):self.map(rva,pe.read(rva,4))
        self.flags,self.state=self.buffer(24),self.buffer(36)
        self.flag_storage,self.state_storage=self.buffer(0x58),self.buffer(0x58)
        self.sparse=(C.c_uint32*2048)()
        self.sparse_pages=(C.c_uint64*1)(C.addressof(self.sparse))
        self.flag_pages=(C.c_uint64*1)(C.addressof(self.flags))
        self.state_pages=(C.c_uint64*1)(C.addressof(self.state))
        for storage, pages in ((self.flag_storage,self.flag_pages),(self.state_storage,self.state_pages)):
            self.put(storage,8,"QQ",C.addressof(self.sparse_pages),C.addressof(self.sparse_pages)+8)
            self.put(storage,0x50,"Q",C.addressof(pages))
        self.context=self.buffer(24)
        flag_lookup=self.callback(C.c_void_p,[C.c_void_p,C.c_uint32],lambda *_:C.addressof(self.flag_storage))
        state_lookup=self.callback(C.c_void_p,[C.c_void_p,C.c_uint32],lambda *_:C.addressof(self.state_storage))
        self.bridge(0x226360,C.cast(flag_lookup,C.c_void_p).value)
        self.bridge(0x21F330,C.cast(state_lookup,C.c_void_p).value)
        self.on_stand=C.WINFUNCTYPE(None,*([C.c_void_p]*3))(BASE+0x66BA420)

    def run(self,case):
        entries=C.create_string_buffer(b"".join(struct.pack("<iiiIQ",*p,0,0) for p in case["contacts"]) or bytes(24))
        begin=C.addressof(entries)
        vector=(C.c_uint64*3)(begin,begin+24*len(case["contacts"]),begin+24*len(case["contacts"]))
        shape=C.create_string_buffer(32)
        struct.pack_into("<f",shape,24,case["width"])
        state=(C.c_float*9)(*case["origin"],0,0,0,*case["velocity"])
        fall=C.c_float(case["fall_distance"])
        self.honey(C.addressof(vector),C.addressof(shape),C.addressof(state),C.addressof(fall))
        return {**case,"motion":list(state)[6:],"fall_after":fall.value}

    def stand(self,velocity,sneaking):
        self.put(self.flags,0,"Q",2 if sneaking else 0)
        self.put(self.state,24,"3f",*velocity)
        self.on_stand(0,C.addressof(self.context),0)
        return dict(velocity=velocity,sneaking=sneaking,motion=struct.unpack_from("<3f",self.state,24))


if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client",type=Path,required=True)
    parser.add_argument("--output",type=Path,required=True)
    args=parser.parse_args();pe=ClientPE(args.client)
    digest=hashlib.sha256(pe.data).hexdigest()
    if digest!="8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":raise RuntimeError("client differs from inspected image")
    native=NativeHoney(pe);cases=[];standing=[]
    try:
        for anchor,width,axis,distance,height,velocity,count in itertools.product(
                ((0,65,0),(1000000,65,-1000000),(-1000000,65,1000000),(29999980,65,29999980)),
                (.25,.5,.6,2), (0,2), (0,.4375,.5,.55,.5625,.75,1), (0,.8125,.9375,1),
                ((.4,-.2,-.2),(.4,-.1,-.2),(.4,0,-.2),(.4,.1,-.2)), (0,1,2)):
            origin=[anchor[0]+.5,anchor[1]+height,anchor[2]+.5];origin[axis]+=distance
            contacts=[list(anchor)]*count
            cases.append(native.run(dict(origin=origin,width=width,velocity=velocity,contacts=contacts,fall_distance=5.25)))
        for x,y,z,sneaking in itertools.product((-.4,0,.4),(-1,-.2,-.0784,0,.09999999,.1,.2,1),(-.4,0,.4),(False,True)):
            standing.append(native.stand([x,y,z],sneaking))
    finally:native.close()
    args.output.write_text(json.dumps(dict(complete=True,reference=__doc__,client_sha256=digest,
        function=dict(rva="0x63af270",size=0x135,sha256=hashlib.sha256(pe.read(0x63AF270,0x135)).hexdigest()),
        standing_function=dict(rva="0x66ba420",size=0x196,sha256=hashlib.sha256(pe.read(0x66BA420,0x196)).hexdigest()),
        standing=standing,scenarios=cases),separators=(",",":"))+"\n",encoding="utf-8")
    print(json.dumps(dict(complete=True,cases=len(cases))))
