#!/usr/bin/env python3
"""Execute original hopper collision construction and empty item-query paths.

Host callbacks supply vector storage, a facing-state reader and an empty actor
query. Original shape arithmetic, translation, filtering and query construction
remain untouched. No live game process or on-disk binary is modified.
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


class NativeHopperShapes(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        self.spans = ((0x66BB380,0x4A1),(0x66BB870,0x286),(0x2060EB0,0x672),(0x2062B80,0x137),(0x318C730,0x1FA))
        for rva,size in self.spans:
            self.map(rva,pe.read(rva,size),executable=True)
        for rva,size in ((0xE997410,64),(0xE978F10,16),(0xE99DA00,64),
                         (0xE61E140,4),(0xE86B850,4),(0xE7FC130,4),
                         (0xE5C6370,4),(0xE6022D0,4),(0xE7D84F8,4),
                         (0xE75D7C4,4),(0xE7F8A18,4),(0xE86B80C,12),
                         (0xE7F8A70,4),(0xE61E13C,4)):
            self.map(rva,pe.read(rva,size))
        self.vector_buffers = {}
        def append_box(vector, data):
            begin,end,capacity=struct.unpack('<QQQ',C.string_at(vector,24))
            if not begin:
                storage=self.buffer(1024)
                begin=end=C.addressof(storage);capacity=begin+1024
                self.vector_buffers[vector]=storage
            if end+24>capacity:raise RuntimeError('native hopper vector exceeded supplied capacity')
            C.memmove(end,data,24)
            C.memmove(vector,struct.pack('<QQQ',begin,end+24,capacity),24)
            return end
        def append_refs(vector,_iterator,*values):
            return append_box(vector,struct.pack('<6f',*[C.c_float.from_address(p).value for p in values]))
        refs=self.callback(C.c_void_p,[C.c_void_p]*8,append_refs)
        copy=self.callback(C.c_void_p,[C.c_void_p]*3,lambda vector,_iterator,box:append_box(vector,C.string_at(box,24)))
        noop=self.callback(None,[C.c_void_p],lambda *_:None)
        self.bridge(0x66C8CA0,C.cast(refs,C.c_void_p).value)
        self.bridge(0x70EF30,C.cast(copy,C.c_void_p).value)
        self.bridge(0xE021350,C.cast(noop,C.c_void_p).value)
        self.bridge(0xE03C880,C.cast(noop,C.c_void_p).value)
        self.bridge(0x9B4A0,C.cast(noop,C.c_void_p).value)

        self.legacy,self.block,self.state,self.getter,self.getter_vt,self.meta,self.entries,self.sentinel=[
            self.buffer(n) for n in (0x340,0x128,8,0x10,8,0x10,16,0x30)]
        self.put(self.legacy,0x1D8,'Q',C.addressof(self.sentinel))
        self.put(self.sentinel,8,'Q',C.addressof(self.sentinel));self.put(self.sentinel,0x19,'B',1)
        self.put(self.legacy,0x320,'QQ',C.addressof(self.entries),C.addressof(self.entries)+16)
        self.put(self.entries,0,'Q',C.addressof(self.getter))
        self.put(self.getter,0,'QQ',C.addressof(self.getter_vt),C.addressof(self.meta))
        self.put(self.meta,8,'Q',C.addressof(self.state));self.put(self.block,0x68,'Q',C.addressof(self.legacy))
        self.map(0x11E2EDF0,struct.pack('<Q',C.addressof(self.state)))
        def read_state(_,output,*_args):
            C.memmove(output,struct.pack('<iI',self.face,1),8)
            return output
        read=self.callback(C.c_void_p,[C.c_void_p]*4,read_state)
        self.put(self.getter_vt,0,'Q',C.cast(read,C.c_void_p).value)
        self.output,self.box_storage,self.position=[self.buffer(n) for n in (24,1024,12)]
        self.shape_function=C.WINFUNCTYPE(None,*([C.c_void_p]*6))(BASE+0x66BB380)

        self.source,self.source_vt,self.above,self.above_type,self.above_vt,self.hopper=[
            self.buffer(n) for n in (0x80,0x120,0x70,0x80,0xC0,0x100)]
        self.put(self.source,0,'Q',C.addressof(self.source_vt))
        self.put(self.above,0x68,'Q',C.addressof(self.above_type));self.put(self.above_type,0,'Q',C.addressof(self.above_vt))
        get_block=self.callback(C.c_void_p,[C.c_void_p]*2,lambda *_:C.addressof(self.above))
        is_container=self.callback(C.c_bool,[C.c_void_p],lambda *_:self.above_container)
        def fetch(_source,result,_kind,query,_excluded,cache):
            self.queries.append(list(struct.unpack('<6f',C.string_at(query,24))))
            C.memset(result,0,16);C.memset(cache,0,56)
            return result
        fetch_cb=self.callback(C.c_void_p,[C.c_void_p,C.c_void_p,C.c_uint32,C.c_void_p,C.c_void_p,C.c_void_p],fetch)
        self.put(self.source_vt,0x10,'Q',C.cast(get_block,C.c_void_p).value)
        self.put(self.source_vt,0xF0,'Q',C.cast(fetch_cb,C.c_void_p).value)
        self.put(self.above_vt,0xB8,'Q',C.cast(is_container,C.c_void_p).value)
        self.query_function=C.WINFUNCTYPE(C.c_bool,*([C.c_void_p]*4))(BASE+0x2062B80)

    def shapes(self,face,position):
        self.face=face
        self.put(self.position,0,'iii',*position)
        begin=C.addressof(self.box_storage)
        self.put(self.output,0,'QQQ',begin,begin,begin+1024)
        self.shape_function(C.addressof(self.legacy),C.addressof(self.block),C.addressof(self.source),C.addressof(self.position),0,C.addressof(self.output))
        _,end,_=struct.unpack_from('<QQQ',self.output)
        count=(end-begin)//24
        if not 0<=count<=7:raise RuntimeError('unexpected original hopper shape count')
        return dict(face=face,position=position,boxes=[list(struct.unpack_from('<6f',self.box_storage,i*24)) for i in range(count)])

    def query(self,position,entity,above_container):
        self.above_container,self.queries=above_container,[]
        self.put(self.hopper,9,'B',entity)
        point=(C.c_float*3)(*position)
        accepted=self.query_function(C.addressof(self.hopper),C.addressof(self.source),C.addressof(self.block),C.addressof(point))
        if accepted or len(self.queries)!=(0 if above_container else 2 if entity else 1):
            raise RuntimeError('unexpected original hopper empty-query result')
        return dict(position=position,entity=entity,above_container=above_container,queries=self.queries)

    def brewing_shapes(self,position):
        original=self.shape_function
        try:
            self.shape_function=C.WINFUNCTYPE(None,*([C.c_void_p]*6))(BASE+0x318C730)
            return self.shapes(0,position)
        finally:self.shape_function=original


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client',type=Path,required=True);parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args();pe=ClientPE(args.client)
    digest=hashlib.sha256(pe.data).hexdigest()
    if digest!='8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':raise ValueError('client differs from inspected image')
    native=NativeHopperShapes(pe)
    try:
        shapes=[native.shapes(face,[anchor,y,anchor]) for face,anchor,y in itertools.product(
            range(6),(0,-17,1000000,-1000000,16777215,-29999999,29999999),(0,65,319))]
        queries=[native.query([x,y,x],entity,above) for x,y,entity,above in itertools.product(
            (0.,-17.,1000000.,16777215.,-29999999.),(65.,65.5), (False,True),(False,True))]
        brewing=[native.brewing_shapes([anchor,y,anchor]) for anchor,y in itertools.product(
            (0,-17,1000000,-1000000,16777215,-29999999,29999999),(0,65,319))]
        spans=[dict(rva=hex(rva),size=size,sha256=hashlib.sha256(pe.read(rva,size)).hexdigest()) for rva,size in native.spans]
    finally:native.close()
    args.output.write_text(json.dumps(dict(complete=True,client_sha256=digest,functions=spans,shapes=shapes,queries=queries,brewing_shapes=brewing),separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(shapes=len(shapes),queries=len(queries),brewing_shapes=len(brewing),output=str(args.output))))
