#!/usr/bin/env python3
"""Execute original dispenser ejection velocity, including its cached Gaussian stream.

The unchanged contiguous arithmetic window receives a supplied actor state and
native seeded Core RNG. The original float draw runs through a host vtable
callback. logf/sqrtf resolve to the Windows CRT; no live process is modified.
"""
import argparse
import ctypes as C
import hashlib
import json
from pathlib import Path
import struct

from redstone_bds_level_random_probe import NativeCoreRandom
from redstone_native_probe import ClientPE
from redstone_native_lightning_search_probe import BASE


def xmm_transfer(store,register,offset):
    return bytes.fromhex('f3')+(bytes.fromhex('44') if register>=8 else b'')+bytes.fromhex('0f')+bytes([0x7f if store else 0x6f,0x84|((register&7)<<3),0x24])+struct.pack('<I',offset)


class NativeEjectionVelocity(NativeCoreRandom):
    def __init__(self,client):
        super().__init__(client)
        for rva,size in ((0xE6AE3F0,8),(0xE6AE460,4),(0xE6AE454,4),(0xE5E9578,4),
                         (0xE6022D4,4),(0xE7F89FC,4),(0xE6022B4,4)):
            self.map(rva,client.read(rva,size))
        self.map(0x26DCB0,client.read(0x26DCB0,0x23),executable=True)
        self.float_function=C.WINFUNCTYPE(C.c_float,C.c_void_p)(BASE+0x26DCB0)
        crt=C.WinDLL('ucrtbase')
        self.crt=crt
        crt.logf.argtypes=[C.c_float];crt.logf.restype=C.c_float
        self.keep.append(crt)
        self.bridge(0xE5A65E0,C.cast(crt.logf,C.c_void_p).value)
        self.map(0xF1045D0,struct.pack('<Q',C.cast(crt.sqrtf,C.c_void_p).value))
        self.wrapper,self.wrapper_vt=self.buffer(0xa00),self.buffer(0x30)
        self.actor,self.vector=self.buffer(0x220),self.buffer(0x30)
        self.put(self.wrapper,0,'Q',C.addressof(self.wrapper_vt))
        self.put(self.wrapper,0x9f8,'Q',C.addressof(self.state))
        self.put(self.actor,0x218,'Q',C.addressof(self.vector))
        draw=self.callback(C.c_float,[C.c_void_p],lambda _:self.float_function(C.addressof(self.state)))
        self.put(self.wrapper_vt,0x28,'Q',C.cast(draw,C.c_void_p).value)
        save=b''.join(xmm_transfer(True,n,0x90+(n-6)*16) for n in range(6,13))
        restore=b''.join(xmm_transfer(False,n,0x90+(n-6)*16) for n in range(6,13))
        wrapper=(bytes.fromhex('5355575641564881ec10010000')+save
                 +bytes.fromhex('488d6c24304889cf4889c84889d344894d504489455449ba')
                 +struct.pack('<Q',BASE+0x9788485)+bytes.fromhex('41ffe2'))
        tail=restore+bytes.fromhex('4881c410010000415e5e5f5d5bc3')
        self.map(0x9788485,client.read(0x9788485,0x3af)+tail,executable=True)
        self.map(0x10000000,wrapper,executable=True)
        self.velocity_function=C.WINFUNCTYPE(None,C.c_void_p,C.c_void_p,C.c_int,C.c_int)(BASE+0x10000000)

        # Original crafter launch-point arithmetic, before generic ejection's
        # vertical position correction. Orientation is the native 0..11 value.
        for rva in (0xE760F14,0xE760F18,0xE758C38,0xE5C6370):self.map(rva,client.read(rva,4))
        save_pos=b''.join(xmm_transfer(True,n,(n-6)*16) for n in range(6,10))
        restore_pos=b''.join(xmm_transfer(False,n,(n-6)*16) for n in range(6,10))
        position_wrapper=(bytes.fromhex('574883ec70')+save_pos
            +bytes.fromhex('4889cff3440f100af3440f104204f30f107a084589c149ba')
            +struct.pack('<Q',BASE+0x6EC2569)+bytes.fromhex('41ffe2'))
        position_tail=bytes.fromhex('f3440f110ff3440f114704f30f117f08')+restore_pos+bytes.fromhex('4883c4705fc3')
        self.map(0x6EC2569,client.read(0x6EC2569,0x88)+position_tail,executable=True)
        self.map(0x10001000,position_wrapper,executable=True)
        self.position_function=C.WINFUNCTYPE(None,C.c_void_p,C.c_void_p,C.c_uint32)(BASE+0x10001000)
        self.map(0xEA6BE70,client.read(0xEA6BE70,8))
        correction_wrapper=(bytes.fromhex('53555641554883ec284889d34588c5488d69a849ba')
            +struct.pack('<Q',BASE+0x97883E8)+bytes.fromhex('41ffe2'))
        correction_tail=bytes.fromhex('4883c428415d5e5d5bc3')
        self.map(0x97883E8,client.read(0x97883E8,0x33)+correction_tail,executable=True)
        self.map(0x10002000,correction_wrapper,executable=True)
        self.correction_function=C.WINFUNCTYPE(None,C.c_void_p,C.c_void_p,C.c_uint32)(BASE+0x10002000)
        self.position_input,self.position_output,self.corrected_output=self.buffer(12),self.buffer(12),self.buffer(12)

    def seed(self,seed):
        C.memset(self.state,0,0xa00)
        super().seed(seed)

    def run(self,face):
        initial=C.string_at(self.state,0xa00)
        before=struct.unpack_from('<I',self.state,0x9cc)[0]
        dx,dz=((0,0),(0,0),(0,-1),(0,1),(-1,0),(1,0))[face]
        self.velocity_function(C.addressof(self.wrapper),C.addressof(self.actor),dx,dz)
        after=struct.unpack_from('<I',self.state,0x9cc)[0]
        saved=C.string_at(self.state,0xa00)
        next_word=self.draw();self.reset(saved)
        consumed=(after-before)%624
        self.reset(initial)
        words=[self.draw() for _ in range(consumed)]
        self.reset(saved)
        return dict(face=face,velocity=list(struct.unpack_from('<3f',self.vector,0x18)),
                    words=words,words_consumed=consumed,next_word=next_word,
                    cached_gaussian=bool(saved[0x9e4]),cache_value=struct.unpack_from('<f',saved,0x9d4)[0])

    def position(self,pos,orientation):
        self.put(self.position_input,0,'3f',*pos)
        self.position_function(C.addressof(self.position_output),C.addressof(self.position_input),orientation)
        self.correction_function(C.addressof(self.corrected_output),C.addressof(self.position_output),int(orientation<8))
        return dict(position=pos,orientation=orientation,origin=list(struct.unpack('<3f',self.corrected_output)))

    def logs(self):
        values=set()
        for exponent in range(-48,0):
            for i in range(128):
                for offset in (0,0x7fff,0x8000,0x8001,0xffff):
                    values.add(((127+exponent)<<23)|(i<<16)|offset)
        values.update(range(0x3f700000-4,0x3f700000+5))
        result=[]
        for bits in sorted(values):
            x=struct.unpack('<f',struct.pack('<I',bits))[0]
            y=self.crt.logf(x)
            result.append(dict(input_bits=bits,output_bits=struct.unpack('<I',struct.pack('<f',y))[0]))
        return result


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args();client=ClientPE(args.client)
    digest=hashlib.sha256(client.data).hexdigest()
    if digest!='8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':raise RuntimeError('client differs from inspected image')
    native=NativeEjectionVelocity(client);cases=[]
    try:
        for seed in (0,1,5489,0x12345678,0x80000000,0xffffffff):
            for face in range(6):
                native.seed(seed)
                cases.append(dict(seed=seed,face=face,frames=[native.run(face) for _ in range(40)]))
        origins=[native.position(list(pos),orientation) for pos in ((0,70,0),(32,70,32),(1000000,-60,-1000000),(-1875000,319,1874999)) for orientation in range(12)]
        logs=native.logs()
        crt=dict(sha256=hashlib.sha256(Path('C:/Windows/System32/ucrtbase.dll').read_bytes()).hexdigest(),
                 logf_rva=hex(C.cast(native.crt.logf,C.c_void_p).value-native.crt._handle),
                 dispatch=C.c_uint32.from_address(native.crt._handle+0x139e60).value)
    finally:native.close()
    result=dict(complete=True,reference=__doc__,client_sha256=digest,scenarios=cases,origins=origins,gaussian_logs=logs,math_library=crt,
                window=dict(rva='0x9788485',size=0x3af,sha256=hashlib.sha256(client.read(0x9788485,0x3af)).hexdigest()))
    args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=True,cases=len(cases),velocities=len(cases)*40,output=str(args.output))),flush=True)
