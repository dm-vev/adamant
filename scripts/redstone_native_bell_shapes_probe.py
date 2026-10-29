#!/usr/bin/env python3
"""Execute the original client BellBlock::getVisualShape with supplied states."""
import argparse
import ctypes as C
import hashlib
import json
from pathlib import Path
import struct

from redstone_native_probe import ClientPE
from redstone_native_piston_shapes_probe import NativePistonShapes
from redstone_native_lightning_search_probe import BASE


class NativeBellShapes(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        for rva,size in ((0xE6AE4D4,4),(0xE61E140,4),(0xE8425D4,4),(0xE5A7060,4),
                         (0xE780AE8,4),(0xE8472D0,64),(0xE8473E4,32)):
            self.map(rva,pe.read(rva,size))
        self.map(0x2A5FBE0,pe.read(0x2A5FBE0,0x4A1),executable=True)
        self.legacy,self.block,self.sentinel,self.entries,self.vt = [self.buffer(n) for n in (0x340,0x128,0x30,32,8)]
        self.getters = [self.buffer(16) for _ in range(2)]
        self.metadata = [self.buffer(16) for _ in range(2)]
        self.put(self.legacy,0x1D8,'Q',C.addressof(self.sentinel))
        self.put(self.sentinel,8,'Q',C.addressof(self.sentinel));self.put(self.sentinel,0x19,'B',1)
        self.put(self.legacy,0x320,'QQ',C.addressof(self.entries),C.addressof(self.entries)+32)
        self.put(self.block,0x68,'Q',C.addressof(self.legacy))
        self.map(0x11E2DDD0,struct.pack('<Q',1111));self.map(0x11E2E370,struct.pack('<Q',2222))
        for i in range(2):
            self.put(self.entries,i*16,'Q',C.addressof(self.getters[i]))
            self.put(self.getters[i],0,'QQ',C.addressof(self.vt),C.addressof(self.metadata[i]))
            self.put(self.metadata[i],8,'Q',(i+1)*1111)
        def read_state(getter,output,*_):
            value=self.direction if getter==C.addressof(self.getters[0]) else self.attachment
            self.reads.append(value)
            C.memmove(output,struct.pack('<iI',value,1),8)
            return output
        reader=self.callback(C.c_void_p,[C.c_void_p]*4,read_state)
        self.put(self.vt,0,'Q',C.cast(reader,C.c_void_p).value)
        self.box=self.buffer(24)
        self.function=C.WINFUNCTYPE(C.c_void_p,*([C.c_void_p]*3))(BASE+0x2A5FBE0)
        self.map(0xE6D7DE8,pe.read(0xE6D7DE8,24))
        self.map(0xE6258C8,pe.read(0xE6258C8,16))
        self.map(0x3182A80,pe.read(0x3182A80,0x330),executable=True)
        self.legacy_vt=self.buffer(0x58)
        self.put(self.legacy,0,'Q',C.addressof(self.legacy_vt))
        self.put(self.legacy_vt,0x50,'Q',BASE+0x2A5FBE0)
        no_offset=self.callback(C.c_void_p,[C.c_void_p],lambda *_:0)
        security=self.callback(None,[C.c_void_p],lambda *_:None)
        self.bridge(0x2A6B060,C.cast(no_offset,C.c_void_p).value)
        self.bridge(0xE03C880,C.cast(security,C.c_void_p).value)
        self.collision_function=C.WINFUNCTYPE(C.c_void_p,*([C.c_void_p]*6))(BASE+0x3182A80)
        self.position=self.buffer(12)

    def shape(self,attachment,direction):
        self.attachment,self.direction,self.reads=attachment,direction,[]
        result=self.function(C.addressof(self.legacy),C.addressof(self.block),C.addressof(self.box))
        if result!=C.addressof(self.box) or self.reads!=[direction,attachment]:
            raise RuntimeError('unexpected native bell shape state-reader path')
        return dict(attachment=attachment,direction=direction,box=list(struct.unpack('<6f',self.box)))

    def collision(self,attachment,direction,position):
        self.attachment,self.direction,self.reads=attachment,direction,[]
        self.put(self.position,0,'iii',*position)
        result=self.collision_function(C.addressof(self.legacy),C.addressof(self.box),C.addressof(self.block),0,C.addressof(self.position),0)
        if result!=C.addressof(self.box) or self.reads!=[direction,attachment]:
            raise RuntimeError('unexpected native bell collision virtual dispatch')
        return dict(attachment=attachment,direction=direction,position=position,box=list(struct.unpack('<6f',self.box)))


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client',type=Path,required=True);parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args();pe=ClientPE(args.client);digest=hashlib.sha256(pe.data).hexdigest()
    if digest!='8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    native=NativeBellShapes(pe)
    try:
        cases=[native.shape(a,d) for a in range(4) for d in range(4)]
        collisions=[native.collision(a,d,p) for a in range(4) for d in range(4)
                    for p in ([0,0,0],[-17,65,1000000],[29999980,319,-29999980])]
    finally:
        native.close()
    args.output.write_text(json.dumps(dict(complete=True,client_sha256=digest,function=dict(rva='0x2a5fbe0',size=0x4A1,
        sha256=hashlib.sha256(pe.read(0x2A5FBE0,0x4A1)).hexdigest()),collision_function=dict(rva='0x3182a80',size=0x330,
        sha256=hashlib.sha256(pe.read(0x3182A80,0x330)).hexdigest()),supplied='native state readers, absent per-state collision override and offset component; original visual/virtual collision arithmetic and tables unchanged',cases=cases,collisions=collisions),separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=True,shapes=len(cases),collisions=len(collisions),output=str(args.output))),flush=True)
