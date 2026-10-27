#!/usr/bin/env python3
"""Execute the original client hook sound/event dispatcher with supplied world access.

The whole _emitState body is untouched. Source block lookup, level lookup,
sound broadcast and event delivery are supplied callbacks. Game-event constants
are initialized to the inspected enum IDs. No native world or circuit is run.
"""

import argparse
import ctypes as C
from ctypes import wintypes as W
import hashlib
import itertools
import json
from pathlib import Path
import struct

from redstone_native_probe import ClientPE
from redstone_native_lightning_search_probe import NativeSearch, BASE
from redstone_bds_client import network_state_hash


class NativeHook(NativeSearch):
    def __init__(self, pe):
        self.pe, self.keep, self.committed = pe, [], set()
        self.kernel = C.WinDLL("kernel32", use_last_error=True)
        self.kernel.VirtualAlloc.argtypes = [C.c_void_p,C.c_size_t,W.DWORD,W.DWORD]
        self.kernel.VirtualAlloc.restype = C.c_void_p
        self.kernel.VirtualProtect.argtypes = [C.c_void_p,C.c_size_t,W.DWORD,C.POINTER(W.DWORD)]
        self.kernel.VirtualFree.argtypes = [C.c_void_p,C.c_size_t,W.DWORD]
        self.address = self.kernel.VirtualAlloc(BASE,0x12000000,0x2000,1)
        if self.address != BASE:
            raise RuntimeError("preferred client range unavailable")
        self.map(0x4038B70,pe.read(0x4038B70,0x4B7),executable=True)
        self.map(0xF0FBAA8,pe.read(0xF0FBAA8,8))
        if pe.read(0xE585660,2) != b"\xff\xe0":
            raise RuntimeError("unexpected indirect dispatcher")
        self.map(0xE585660,pe.read(0xE585660,2),executable=True)
        for rva in (0xE5C6370,0xE6AE460):
            self.map(rva,pe.read(rva,4))
        for rva,event in ((0x11D69074,1),(0x11D6907C,5),(0x11D690DC,7),(0x11D690E4,2)):
            self.map(rva,struct.pack("<IB3x",event,1))
        self.source,self.source_vtable,self.level,self.level_vtable,self.block = (
            self.buffer(n) for n in (8,0x200,8,0x6B8,0x118))
        self.put(self.source,0,"Q",C.addressof(self.source_vtable))
        self.put(self.level,0,"Q",C.addressof(self.level_vtable))
        callbacks = [
            (self.source_vtable,0x10,self.callback(C.c_void_p,[C.c_void_p,C.c_void_p],lambda *_: C.addressof(self.block))),
            (self.source_vtable,0x158,self.callback(C.c_void_p,[C.c_void_p],lambda _: C.addressof(self.level))),
            (self.source_vtable,0x1F0,self.callback(None,[C.c_void_p]*5,self.post_event)),
            (self.level_vtable,0x6B0,self.callback(None,[C.c_void_p,C.c_void_p,C.c_uint,C.c_void_p,C.c_void_p,C.c_void_p,C.c_bool,C.c_void_p],self.play_sound)),
        ]
        for table,offset,callback in callbacks:
            self.put(table,offset,"Q",C.cast(callback,C.c_void_p).value)
        # Actual optimized ABI: source, pos, new attached/powered, old flags.
        self.call = C.WINFUNCTYPE(None,C.c_void_p,C.c_void_p,C.c_bool,C.c_bool,C.c_bool,C.c_bool)(BASE+0x4038B70)

    def post_event(self,source,actor,event,pos,affected):
        self.output.append({"kind":"event","event_id":struct.unpack("<I",C.string_at(event,4))[0],
                            "pos":list(struct.unpack("<iii",C.string_at(pos,12))),
                            "source_nil":actor is None,"affected_nil":affected is None})

    def play_sound(self,level,source,event,pos,block,*_):
        data=struct.unpack("<i",C.string_at(block+0x114,4))[0]
        self.output.append({"kind":"sound","sound_id":event,"extra_data":data,
                            "pos":list(struct.unpack("<fff",C.string_at(pos,12)))})

    def run(self,before_a,before_p,after_a,after_p,visible,pos):
        a,p = (before_a,before_p) if visible=="before" else (after_a,after_p)
        state = {"id":"minecraft:tripwire_hook","states":{"attached_bit":a,"direction":3,"powered_bit":p}}
        runtime = network_state_hash(state)
        self.put(self.block,0x114,"I",runtime)
        coords = (C.c_int*3)(*pos)
        self.output = []
        self.call(C.addressof(self.source),C.addressof(coords),after_a,after_p,before_a,before_p)
        return {"before_attached":before_a,"before_powered":before_p,"after_attached":after_a,
                "after_powered":after_p,"visible":visible,"pos":pos,"visible_runtime_id":runtime,"output":self.output}


if __name__ == "__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--client",type=Path,required=True)
    parser.add_argument("--output",type=Path,required=True)
    args=parser.parse_args()
    pe=ClientPE(args.client)
    digest=hashlib.sha256(pe.data).hexdigest()
    if digest!="8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("client differs from inspected 1.26.51.1 image")
    native=NativeHook(pe)
    cases=[native.run(*flags,visible,pos) for flags,visible,pos in itertools.product(
        itertools.product((False,True),repeat=4),("before","after"),([0,65,0],[-19,71,23]))]
    native.close()
    output={"complete":True,"reference":__doc__,"client_sha256":digest,
            "spans":{"hook_emit_state":{"rva":"0x4038b70","size":0x4B7,"sha256":hashlib.sha256(pe.read(0x4038B70,0x4B7)).hexdigest()}},
            "scenarios":cases}
    args.output.write_text(json.dumps(output,separators=(",",":"))+"\n",encoding="utf-8")
    print(json.dumps({"complete":True,"cases":len(cases)}))
