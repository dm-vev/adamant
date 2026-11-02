#!/usr/bin/env python3
"""Execute the original ItemActor phase/yaw/launch windows using native Core RNG.

Two unchanged arithmetic windows are connected by a host wrapper; unrelated
entity-component construction is supplied by buffers. This is not execution
of the complete actor constructor. No game process or binary is modified.
"""
import argparse
import ctypes as C
import hashlib
import json
from pathlib import Path
import struct

from redstone_bds_level_random_probe import NativeCoreRandom
from redstone_native_lightning_search_probe import BASE
from redstone_native_probe import ClientPE

WINDOWS=((0x3347AEA,0x2C),(0x3347BE3,0x83))

class NativeItemLaunch(NativeCoreRandom):
    def __init__(self,pe):
        super().__init__(pe)
        for rva,size in ((0xE6AE3F0,8),(0xE6022B0,4),(0xE6AE458,4),(0xE6AE454,4),(0xE6AE48C,4)):
            self.map(rva,pe.read(rva,size))
        self.map(0x26DCB0,pe.read(0x26DCB0,0x23),executable=True)
        self.float_function=C.WINFUNCTYPE(C.c_float,C.c_void_p)(BASE+0x26DCB0)
        self.random,self.random_vt=self.buffer(16),self.buffer(0x30)
        self.actor,self.motion,self.rotation=self.buffer(0x470),self.buffer(0x30),self.buffer(8)
        self.put(self.random,0,'Q',C.addressof(self.random_vt))
        self.put(self.actor,0x218,'Q',C.addressof(self.motion))
        self.put(self.actor,0x228,'Q',C.addressof(self.rotation))
        def draw(_):
            value=self.float_function(C.addressof(self.state))
            self.floats.append(value)
            return value
        callback=self.callback(C.c_float,[C.c_void_p],draw)
        self.put(self.random_vt,0x28,'Q',C.cast(callback,C.c_void_p).value)
        jump=lambda rva:bytes.fromhex('49ba')+struct.pack('<Q',BASE+rva)+bytes.fromhex('41ffe2')
        self.map(WINDOWS[0][0],pe.read(*WINDOWS[0])+jump(WINDOWS[1][0]),executable=True)
        tail=bytes.fromhex('4881c490000000415e5f5dc3')
        self.map(WINDOWS[1][0],pe.read(*WINDOWS[1])+tail,executable=True)
        wrapper=bytes.fromhex('555741564881ec90000000488d6c2420488955504889cf488d87e8dfffff')+jump(WINDOWS[0][0])
        self.map(0x10000000,wrapper,executable=True)
        self.launch_function=C.WINFUNCTYPE(None,C.c_void_p,C.c_void_p)(BASE+0x10000000)

    def run(self):
        before=C.string_at(self.state,0x9D4)
        self.floats=[]
        self.launch_function(C.addressof(self.random),C.addressof(self.actor))
        after=C.string_at(self.state,0x9D4)
        self.reset(before)
        words=[self.draw() for _ in range(4)]
        if C.string_at(self.state,0x9D4)!=after:raise RuntimeError('original launch did not consume four native words')
        expected=[struct.unpack('<f',struct.pack('<f',word/4294967296))[0] for word in words]
        if self.floats!=expected:raise RuntimeError('original launch float callbacks differ from their native words')
        next_word=self.draw();self.reset(after)
        return dict(words=words,phase=struct.unpack_from('<f',self.actor,0x454)[0],
                    yaw=struct.unpack_from('<f',self.rotation,4)[0],velocity=list(struct.unpack_from('<3f',self.motion,0x18)),next_word=next_word)

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--live',type=Path)
    args=parser.parse_args()
    pe=ClientPE(args.client)
    digest=hashlib.sha256(pe.data).hexdigest()
    if digest!='8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':raise RuntimeError('client differs from inspected build')
    native=NativeItemLaunch(pe);scenarios=[];live_cases=[]
    try:
        for seed in (0,1,5489,0x12345678,0x80000000,0xFFFFFFFF):
            native.seed(seed)
            scenarios.append(dict(seed=seed,launches=[native.run() for _ in range(200)]))
        if args.live:
            live=json.loads(args.live.read_text())
            if not live['complete']:raise RuntimeError('live TLS capture is incomplete')
            for checkpoint in live['checkpoints']:
                for resource in checkpoint['resources']:
                    if not resource['created']:continue
                    before=resource['tls_before']
                    if before['core_initialized']:native.reset(bytes.fromhex(before['raw_state']))
                    else:native.seed(resource['tls_after']['seed'])
                    launch=native.run()
                    if (launch['words']!=resource['tls_words'] or launch['velocity']!=resource['velocity'] or
                            launch['phase']!=resource['phase'] or resource['yaw']!=0):
                        raise RuntimeError('executed client launch differs from live BDS TLS/body evidence')
                    live_cases.append(dict(kind='block_resource',position=resource['position'],launch=launch,factory_yaw=resource['yaw']))
            api=next(s for s in live['scenarios'] if s['scenario']['kind']=='api_spawn')
            native.reset(bytes.fromhex(api['tls_before']['raw_state']))
            launch=native.run()
            if len(api['spawns'])!=1:raise RuntimeError('missing live API item body')
            body=api['spawns'][0]['body']
            if (launch['words']!=api['tls_words'] or launch['velocity']!=[body['velocity'][axis] for axis in ('x','y','z')] or
                    body['rotation']['y']!=0):
                raise RuntimeError('executed client launch differs from direct API TLS/body evidence')
            live_cases.append(dict(kind='api_spawn',position=api['base'],launch=launch,factory_yaw=body['rotation']['y']))
    finally:native.close()
    result=dict(complete=True,reference=__doc__,client_sha256=digest,scenarios=scenarios,live_cases=live_cases,
                windows=[dict(rva=hex(rva),size=size,sha256=hashlib.sha256(pe.read(rva,size)).hexdigest()) for rva,size in WINDOWS])
    args.output.write_text(json.dumps(result,separators=(',',':'))+'\n')
    print(json.dumps(dict(complete=True,launches=1200,live_cases=len(live_cases),output=str(args.output))),flush=True)
