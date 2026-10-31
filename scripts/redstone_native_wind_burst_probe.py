#!/usr/bin/env python3
"""Execute the original client explosion ray loop with supplied world/RNG inputs.

Entry/exit adapters bypass explosion entity effects and publication. The ray
loop, normalization, stepping, resistance arithmetic and constants are original
instructions. World reads and ray-loop insertions are observed callbacks;
original affected-set insertion/rehash run separately to verify traversal.
Complete world packet publication is outside this probe's scope.
"""
import argparse
import ctypes as C
import hashlib
import json
import math
from pathlib import Path
import struct

from redstone_native_lightning_search_probe import BASE
from redstone_native_piston_shapes_probe import NativePistonShapes
from redstone_native_probe import ClientPE

RAY_RVA, RAY_SIZE = 0x1134B44, 0x331


class NativeWindBurst(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        for rva in (0xE7A78B0, 0xE61E154, 0xE5E9578, 0xE6AE498, 0xE760F14, 0xE758C38):
            self.map(rva, pe.read(rva, 4))
        for rva, value in ((0x11DB4018, 1234), (0x11DFBF28, 5678), (0x11DFBEF8, 6789)):
            self.map(rva, struct.pack('<Q', value))
        self.map(0x11346C0, pe.read(0x11346C0, 0x90), executable=True)
        self.map(RAY_RVA, pe.read(RAY_RVA, RAY_SIZE), executable=True)
        self.map(0x1136ED5, pe.read(0x1136ED5, 0x62), executable=True)
        # Preserve the original prologue/epilogue and initialize only the
        # skipped startup's saved IRandom pointer and absent source actor.
        self.map(0x1134750, bytes.fromhex('4889bd7801000031db48b8')+
            struct.pack('<Q', BASE+RAY_RVA)+bytes.fromhex('ffe0'), executable=True)
        self.bridge(0x1134E75, BASE+0x1136ED5)
        self.explosion, self.random, self.random_vt = self.buffer(0x90), self.buffer(8), self.buffer(0x30)
        self.air, self.air_type = self.buffer(0xB0), self.buffer(0xF0)
        self.put(self.air, 0x68, 'Q', C.addressof(self.air_type))
        self.put(self.air_type, 0xE0, 'Q', 1234)
        self.put(self.base_type, 0xE0, 'Q', 42)
        self.put(self.explosion, 0x52, 'BBB', 1, 1, 0)
        self.put(self.explosion, 0x58, 'B', 1)
        self.put(self.explosion, 0x68, 'Q', C.addressof(self.source))
        self.put(self.random, 0, 'Q', C.addressof(self.random_vt))
        get_block = self.callback(C.c_void_p, [C.c_void_p, C.c_void_p], self.get_cell)
        random_float = self.callback(C.c_float, [C.c_void_p], self.next_float)
        insert = self.callback(None, [C.c_void_p, C.c_void_p, C.c_void_p], self.insert)
        self.put(self.source_vt, 0x10, 'Q', C.cast(get_block, C.c_void_p).value)
        self.put(self.random_vt, 0x28, 'Q', C.cast(random_float, C.c_void_p).value)
        self.insert_callback = insert
        self.bridge(0x1137D60, C.cast(insert, C.c_void_p).value)
        self.function = C.WINFUNCTYPE(None, C.c_void_p, C.c_void_p)(BASE+0x11346C0)

    def collection_order(self, positions):
        # The original MSVC insertion/rehash functions keep their own buckets
        # and linked list. Supply allocation/free and scalar ceilf only.
        self.map(0xE5B2BA0, self.pe.read(0xE5B2BA0, 4))
        for rva,size in ((0x1137D60,0x35B),(0x119EDB0,0x27C),(0x76E00,0x1D8)):
            self.map(rva,self.pe.read(rva,size),executable=True)
        allocator,vt = self.buffer(8),self.buffer(16)
        alloc = self.callback(C.c_void_p,[C.c_void_p,C.c_size_t],lambda _,size:C.addressof(self.buffer(size)))
        free = self.callback(None,[C.c_void_p,C.c_size_t],lambda *_:None)
        ceil = self.callback(C.c_float,[C.c_float],lambda x:math.ceil(x))
        self.put(allocator,0,'Q',C.addressof(vt));self.put(vt,8,'Q',C.cast(alloc,C.c_void_p).value)
        self.map(0x11805230,struct.pack('<Q',C.addressof(allocator)))
        self.bridge(0xE021350,C.cast(free,C.c_void_p).value)
        self.bridge(0xE5A68E0,C.cast(ceil,C.c_void_p).value)
        collection,head,buckets,output,pos = self.buffer(64),self.buffer(32),self.buffer(128),self.buffer(16),self.buffer(12)
        address=C.addressof(head)
        self.put(head,0,'QQ',address,address)
        self.put(collection,0,'f',1)
        self.put(collection,8,'QQQQQQQ',address,0,C.addressof(buckets),C.addressof(buckets)+128,C.addressof(buckets)+128,7,8)
        for i in range(16):self.put(buckets,i*8,'Q',address)
        insert=C.WINFUNCTYPE(C.c_void_p,C.c_void_p,C.c_void_p,C.c_void_p)(BASE+0x1137D60)
        for position in positions:
            self.put(pos,0,'iii',*position);insert(C.addressof(collection),C.addressof(output),C.addressof(pos))
        order=[];node=C.c_void_p.from_address(address).value
        while node!=address:
            order.append(list(self.pos(node+16)));node=C.c_void_p.from_address(node).value
            if len(order)>len(positions):raise RuntimeError('native affected-list cycle')
        self.bridge(0x1137D60,C.cast(self.insert_callback,C.c_void_p).value)
        return order

    def get_cell(self, _, pos):
        return C.addressof(self.base_block if self.pos(pos) in self.cells else self.air)

    def next_float(self, _):
        value = self.words[self.used]
        self.used += 1
        return value / 4294967296.0

    def insert(self, _, output, pos):
        self.visits.append(self.pos(pos))

    def query(self, position, radius, cells, words):
        self.cells, self.words, self.used, self.visits = set(map(tuple, cells)), words, 0, []
        self.put(self.explosion, 0, 'ffff', *position, radius)
        self.function(C.addressof(self.explosion), C.addressof(self.random))
        if self.used != 1352:
            raise RuntimeError('native explosion did not consume 1352 boundary-ray inputs')
        return dict(position=position, radius=radius, random_words_used=self.used,
                    insertion_visits=[list(p) for p in self.visits],
                    affected=[list(p) for p in dict.fromkeys(self.visits)])


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    pe = ClientPE(args.client)
    digest = hashlib.sha256(pe.data).hexdigest()
    if digest != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    corpus = json.loads(args.input.read_text())
    if not corpus['complete']:
        raise RuntimeError('incomplete native BDS input corpus')
    native = NativeWindBurst(pe)
    results, calibration = [], []
    try:
        # Candidate controls distinguish the burst's offset, power and stream
        # position; these are recorded rather than silently fitted per scene.
        for index,c in enumerate(corpus['scenarios']):
            direction = ((0,0,1),(-1,0,0),(0,0,-1),(1,0,0))[c['scenario']['direction']]
            hit = c['hits'][0]
            position = [hit['position'][k]+hit['face_location'][k] for k in ('x','y','z')]
            for axis,v in enumerate(direction):
                if v:
                    position[axis] = hit['position'][('x','y','z')[axis]]+(1 if v>0 else 0)
            cells = [p['position'] for p in c['cells']]
            cells += [[x,64,z] for x in range(3,14) for z in range(3,14)]
            if c['scenario']['wall']:
                cells += [[8-direction[0]+direction[2]*n,y,8-direction[2]-direction[0]*n] for n in range(-3,4) for y in (66,67)]
            want = [p['position'] for p,v in zip(c['cells'],c['samples'][24]['values']) if v==1]
            switches=set(map(tuple,[p['position'] for p in c['cells'] if p['block']['id']!='minecraft:stone']))
            def matches(observed):
                return set(map(tuple,observed['affected'])) & switches == set(map(tuple,want))
            if index==0:
                for radius in (1.0, 1.2, 1.5, 2.0, 3.0):
                    for offset in (60,120,180):
                        for bias in (0,0.1,0.25):
                            origin = [p+v*bias for p,v in zip(position,direction)]
                            observed = native.query(origin,radius,cells,c['random_input']['words'][offset:offset+1352])
                            calibration.append(dict(radius=radius,word_offset=offset,face_bias=bias,matches_bds=matches(observed)))
            origin=[p+v*.25 for p,v in zip(position,direction)]
            radius,offset=(3.0,80) if corpus.get('breeze') else (1.2,80)
            words=c['random_input']['words'][offset:offset+1352]
            if 'explosion' in c:
                scope=c['explosion']
                origin,radius,words=scope['position'],scope['radius'],scope['words']
                if words!=c['random_input']['words'][offset:offset+1352]:raise RuntimeError('scoped native words differ from the validated stream window')
            observed=native.query(origin,radius,cells,words)
            observed.update(scenario=c['scenario'],word_offset=offset,words=words,
                cells=cells,matches_bds=matches(observed))
            observed['affected_order']=native.collection_order(observed['insertion_visits'])
            results.append(observed)
        order_controls=[]
        for count in (0,1,8,9,64,65,512,513,600):
            positions=[[1000000+(i%17)*(-1 if i&1 else 1),-64+i%384,-1000000+i//17] for i in range(count)]
            positions+=positions[::3]
            order_controls.append(dict(input=positions,order=native.collection_order(positions)))
    finally:
        native.close()
    complete=all(c['matches_bds'] for c in results)
    functions=[dict(rva=hex(rva),size=size,sha256=hashlib.sha256(pe.read(rva,size)).hexdigest()) for rva,size in ((RAY_RVA,RAY_SIZE),(0x1137D60,0x35B),(0x119EDB0,0x27C),(0x76E00,0x1D8))]
    args.output.write_text(json.dumps(dict(complete=complete,client_sha256=digest,functions=functions,
        supplied=__doc__,calibration=calibration,order_controls=order_controls,cases=results),separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=complete,cases=len(results),matching=sum(c['matches_bds'] for c in results))),flush=True)
    if not complete:
        raise RuntimeError('executed client footprint differs from the native BDS controls')
