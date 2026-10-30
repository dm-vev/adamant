#!/usr/bin/env python3
"""Read the owned BDS level RNG and execute original client draws on copies.

ReadProcessMemory is limited to the freshly created child. The Script callback
holds at bounded checkpoints. No process writes, debugger, injection or suspend
are used. Original Core::Random instructions run only on local copied state.
"""
import argparse
import ctypes as C
import hashlib
import json
from pathlib import Path
import struct
import time

from redstone_bds_circuit_order_probe import Memory, occurrences, rva_at
from redstone_bds_probe import Reference
from redstone_native_probe import ClientPE
from redstone_native_piston_shapes_probe import NativePistonShapes
from redstone_native_lightning_search_probe import BASE


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_LEVEL_RANDOM "+JSON.stringify(x));
let d,ready=false,start=null,loaded=0;
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start"){start=system.currentTick;emit({started:true,tick:start});}});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;d.runCommand("tickingarea add 0 0 0 31 100 31 audit true");d.runCommand("gamerule randomtickspeed 0");d.runCommand("gamerule domobspawning false");return;}
 if(!ready){if(!d.getBlock({x:0,y:65,z:0})||!d.getBlock({x:31,y:65,z:31})){if(system.currentTick-loaded>240)throw new Error("load timeout");return;}
  d.runCommand("fill 4 62 4 16 69 12 air");d.runCommand('setblock 8 65 8 hopper ["facing_direction"=2]');d.runCommand("setblock 8 65 7 barrel");d.runCommand("setblock 9 65 8 brewing_stand");ready=true;emit({ready:true,tick:system.currentTick});return;
 }
 if(start===null)return;
 const age=system.currentTick-start;
 if(age>=1&&age<=20){const deadline_ms=Date.now()+500;emit({checkpoint:true,tick:system.currentTick,age,deadline_ms});while(Date.now()<deadline_ms){}}
 if(age===21){emit({complete:true,count:20});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def random_vtable(bds, client):
    # The short load/call prefix is shared by several Random methods. The
    # post-call shift/epilogue uniquely identifies unbounded nextInt instead.
    prefix, tail = client.read(0xDA7310, 13), client.read(0xDA7322, 7)
    header = struct.unpack_from('<I', bds.data, 0x3C)[0]
    image_base = struct.unpack_from('<Q', bds.data, header + 48)[0]
    candidates = []
    for offset in occurrences(bds.data, prefix):
        rva = rva_at(bds, offset)
        if bds.read(rva + 18, 7) != tail:
            continue
        refs = list(occurrences(bds.data, struct.pack('<Q', image_base + rva)))
        if len(refs) != 1:
            raise RuntimeError('ambiguous Random::nextInt table reference')
        table = rva_at(bds, refs[0]) - 0x10
        # The actual inspected build has bounded nextInt before unbounded
        # nextInt. Do not infer this slot order from later-patch headers.
        destructor = struct.unpack('<Q', bds.read(table, 8))[0] - image_base
        if bds.read(destructor, 4) != bytes.fromhex('564883ec'):
            raise RuntimeError('unexpected Random destructor')
        core = rva + 18 + struct.unpack('<i', bds.read(rva + 14, 4))[0]
        if bds.read(core, 12) != client.read(0x26DAE0, 12):
            raise RuntimeError('Core RNG state-index instructions differ')
        candidates.append((table, dict(next_int_rva=hex(rva), core_rva=hex(core), vtable_rva=hex(table))))
    if len(candidates) != 1:
        raise RuntimeError(f'expected one validated native Random table, got {len(candidates)}')
    return candidates[0]


def locate_level_random(memory, bds, table):
    candidates = []
    for wrapper in memory.find(struct.pack('<Q', memory.base + table)):
        try:
            parent_table = struct.unpack('<Q', memory.cached(wrapper - 8, 8))[0] - memory.base
            if not 0 <= parent_table < memory.size:
                continue
            first, second = struct.unpack('<QQ', bds.read(parent_table + 8, 16))
            header = struct.unpack_from('<I', bds.data, 0x3C)[0]
            image_base = struct.unpack_from('<Q', bds.data, header + 48)[0]
            if first != second or bds.read(first - image_base, 5) != bytes.fromhex('488d4108c3'):
                continue
            core = struct.unpack('<Q', memory.cached(wrapper + 0x9F8, 8))[0]
            raw = memory.cached(core, 0x9D4)
            seed = struct.unpack_from('<I', raw, 8)[0]
            index, initialized = struct.unpack_from('<ii', raw, 0x9CC)
            if 0 <= index <= 625 and 0 <= initialized <= 625:
                candidates.append((wrapper, core, dict(parent_vtable=hex(parent_table), seed=seed, index=index,
                                                       core_offset=core-wrapper, initialized=initialized)))
        except (ValueError, OSError, struct.error):
            continue
    if len(candidates) != 1:
        raise RuntimeError(f'expected one LevelRandom owner, got {len(candidates)}: {[c[2] for c in candidates]}')
    return candidates[0]


class NativeCoreRandom(NativePistonShapes):
    def __init__(self, pe):
        super().__init__(pe)
        self.map(0x26DAE0, pe.read(0x26DAE0, 0x1C8), executable=True)
        self.map(0xE6D7D78, pe.read(0xE6D7D78, 8))
        self.map(0x26DD50, pe.read(0x26DD50, 0x22), executable=True)
        self.function = C.WINFUNCTYPE(C.c_uint32, C.c_void_p, C.c_bool)(BASE + 0x26DAE0)
        self.bounded_function = C.WINFUNCTYPE(C.c_int32, C.c_void_p, C.c_uint32)(BASE + 0x26DD50)
        self.state = self.buffer(0xA00)

    def seed(self, seed):
        self.map(0x2203820, self.pe.read(0x2203820, 0xC7), executable=True)
        wrapper = self.buffer(0xA00)
        constructor = C.WINFUNCTYPE(C.c_void_p, C.c_void_p, C.c_uint32, C.c_bool)(BASE + 0x2203820)
        constructor(C.addressof(wrapper), seed, False)
        self.reset(C.string_at(C.addressof(wrapper) + 8, 0x9D4))

    def reset(self, raw):
        C.memmove(self.state, raw, len(raw))

    def draw(self):
        return self.function(C.addressof(self.state), False)

    def bounded(self, bound):
        return self.bounded_function(C.addressof(self.state), bound)

    def copied_state(self):
        return C.string_at(C.addressof(self.state) + 8, 0x9D4 - 8)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('binary', 'assets', 'client', 'output'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--port', type=int, default=30872)
    parser.add_argument('--seed', type=int, default=1)
    args = parser.parse_args()
    bds, client = ClientPE(args.binary), ClientPE(args.client)
    if hashlib.sha256(client.data).hexdigest() != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    table, locator = random_vtable(bds, client)
    native = NativeCoreRandom(client)
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=SCRIPT, local_client=True, seed=args.seed)
    result = dict(complete=False, binary_sha256=hashlib.sha256(bds.data).hexdigest(),
                  client_sha256=hashlib.sha256(client.data).hexdigest(), level_seed=args.seed, locator=locator, checkpoints=[])
    memory, core, raw_states = None, None, []
    try:
        while True:
            lines = reference.until(lambda line:'ADAMANT_LEVEL_RANDOM ' in line, timeout=90)
            record = json.loads(lines[-1].split('ADAMANT_LEVEL_RANDOM ', 1)[1].strip())
            if record.get('error'):
                raise RuntimeError(record)
            if record.get('ready'):
                memory = Memory(reference.process)
                memory.snapshot()
                wrapper, core, description = locate_level_random(memory, bds, table)
                result['owner'] = description
                memory.live = True
                reference.process.stdin.write('scriptevent audit:start\n');reference.process.stdin.flush()
            if record.get('started'):
                result['start_tick'] = record['tick']
            if record.get('checkpoint'):
                if core is None or time.time()*1000 >= record['deadline_ms'] - 20:
                    raise RuntimeError('missed native RNG checkpoint')
                raw = memory.read(core, 0x9D4)
                if raw != memory.read(core, 0x9D4):
                    raise RuntimeError('RNG state changed inside held checkpoint')
                native.reset(raw)
                record.update(seed=struct.unpack_from('<I', raw, 8)[0], state=list(struct.unpack_from('<624I', raw, 12)),
                              index=struct.unpack_from('<i', raw, 0x9CC)[0], initialized=struct.unpack_from('<i', raw, 0x9D0)[0],
                              next_words=[native.draw() for _ in range(16)])
                result['checkpoints'].append(record);raw_states.append(raw)
                print(json.dumps(dict(age=record['age'], seed=record['seed'], index=record['index'])), flush=True)
            if record.get('complete'):
                if len(raw_states) != 20 or record['count'] != 20:
                    raise RuntimeError('incomplete native RNG capture')
                for i, (before, after) in enumerate(zip(raw_states, raw_states[1:])):
                    native.reset(before)
                    for count in range(16385):
                        if native.copied_state() == after[8:]:
                            result['checkpoints'][i]['draws_to_next'] = count
                            break
                        native.draw()
                    else:
                        raise RuntimeError(f'native state transition {i} not explained by original draws')
                result['complete'] = True
                break
    finally:
        if memory:memory.close()
        reference.stop();native.close()
        result['log_directory'] = str(reference.root)
        result['function'] = dict(rva='0x26dae0', size=0x1c8, sha256=hashlib.sha256(client.read(0x26DAE0, 0x1C8)).hexdigest())
        args.output.write_text(json.dumps(result, separators=(',', ':')) + '\n', encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'], output=str(args.output), logs=str(reference.root))), flush=True)
