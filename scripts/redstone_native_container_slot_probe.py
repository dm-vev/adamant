#!/usr/bin/env python3
"""Execute the original dispenser reservoir-selection loop with supplied slots/RNG.

The copied native loop is unchanged. Host callbacks supply only isNull, container
size and bounded draws. No live process or on-disk executable is modified.
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


class NativeContainerSlot(NativePistonShapes):
    def __init__(self,client):
        super().__init__(client)
        self.slots=self.buffer(9*0x98)
        self.container,self.container_vt=self.buffer(8),self.buffer(0xa8)
        self.random,self.random_vt=self.buffer(8),self.buffer(16)
        self.put(self.container,0,'Q',C.addressof(self.container_vt))
        self.put(self.random,0,'Q',C.addressof(self.random_vt))

        def empty(pointer):
            index=(pointer-C.addressof(self.slots))//0x98
            if not 0<=index<9:self.error='invalid native slot address';return True
            return not (self.mask&(1<<index))

        def draw(_,bound):
            if bound!=len(self.bounds)+1 or len(self.bounds)>=len(self.words):
                self.error='unexpected native bounded draw';return 0
            self.bounds.append(bound)
            return self.words[len(self.bounds)-1]%bound

        is_null=self.callback(C.c_bool,[C.c_void_p],empty)
        size=self.callback(C.c_int,[C.c_void_p],lambda _:9)
        bounded=self.callback(C.c_uint32,[C.c_void_p,C.c_uint32],draw)
        self.bridge(0x2486F60,C.cast(is_null,C.c_void_p).value)
        self.put(self.container_vt,0xa0,'Q',C.cast(size,C.c_void_p).value)
        self.put(self.random_vt,8,'Q',C.cast(bounded,C.c_void_p).value)
        # Preserve every nonvolatile register used by the original window.
        # RBP+0x44 is its selected-slot local; RSP has Win64 shadow space.
        wrapper=(bytes.fromhex('535556574155415641574881ec80000000488d6c2430'
                               '4889cb4989d64d89c548b8')
                 +struct.pack('<Q',BASE+0x9787796)+bytes.fromhex('ffe0'))
        tail=bytes.fromhex('8b45444881c480000000415f415e415d5f5e5d5bc3')
        self.map(0x9787796,client.read(0x9787796,0x70)+tail,executable=True)
        self.map(0x10000000,wrapper,executable=True)
        self.function=C.WINFUNCTYPE(C.c_int,C.c_void_p,C.c_void_p,C.c_void_p)(BASE+0x10000000)
        dropper_wrapper=(bytes.fromhex('5355565741544155415641574881ec88000000'
                                      '4989cc4989d64c89c748b8')
                         +struct.pack('<Q',BASE+0x9791CC4)+bytes.fromhex('ffe0'))
        dropper_tail=bytes.fromhex('4489f84881c488000000415f415e415d415c5f5e5d5bc3')
        self.map(0x9791CC4,client.read(0x9791CC4,0x75)+dropper_tail,executable=True)
        self.map(0x10001000,dropper_wrapper,executable=True)
        self.dropper_function=C.WINFUNCTYPE(C.c_int,C.c_void_p,C.c_void_p,C.c_void_p)(BASE+0x10001000)
        transfer_wrapper=(bytes.fromhex('5355565741544155415641574881ec88000000'
                                       '4989cd4889d74c89c648b8')
                          +struct.pack('<Q',BASE+0x4AC43C5)+bytes.fromhex('ffe0'))
        transfer_tail=bytes.fromhex('4489e04881c488000000415f415e415d415c5f5e5d5bc3')
        self.map(0x4AC43C5,client.read(0x4AC43C5,0x6D)+transfer_tail,executable=True)
        self.map(0x10002000,transfer_wrapper,executable=True)
        self.transfer_function=C.WINFUNCTYPE(C.c_int,C.c_void_p,C.c_void_p,C.c_void_p)(BASE+0x10002000)

    def run(self,mask,words):
        self.mask,self.words,self.bounds,self.error=mask,words,[],None
        selected=self.function(C.addressof(self.random),C.addressof(self.container),C.addressof(self.slots))
        if self.error or len(self.bounds)!=mask.bit_count():raise RuntimeError(self.error or 'native draw budget differs')
        bounds=self.bounds[:]
        self.bounds=[]
        dropper_selected=self.dropper_function(C.addressof(self.random),C.addressof(self.container),C.addressof(self.slots))
        if self.error or selected!=dropper_selected or self.bounds!=bounds:raise RuntimeError(self.error or 'native dispenser/dropper loop results differ')
        self.bounds=[]
        transfer_selected=self.transfer_function(C.addressof(self.random),C.addressof(self.container),C.addressof(self.slots))
        if self.error or selected!=transfer_selected or self.bounds!=bounds:raise RuntimeError(self.error or 'native dropper container loop results differ')
        return dict(mask=mask,words=words,selected=selected,bounds=self.bounds)


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    client=ClientPE(args.client)
    digest=hashlib.sha256(client.data).hexdigest()
    if digest!='8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':raise RuntimeError('client differs from inspected image')
    native=NativeContainerSlot(client)
    try:
        cases=[native.run(mask,words) for mask in range(512) for words in (
            [0]*9,[0xffffffff]*9,list(range(9)),[0x12345678,0x98765432,17,29,0x80000000,55,89,144,233])]
    finally:native.close()
    result=dict(complete=True,reference=__doc__,client_sha256=digest,scenarios=cases,
                windows=[dict(rva=hex(rva),size=size,sha256=hashlib.sha256(client.read(rva,size)).hexdigest())
                         for rva,size in ((0x9787796,0x70),(0x9791CC4,0x75),(0x4AC43C5,0x6D))])
    args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=True,cases=len(cases),output=str(args.output))))
