#!/usr/bin/env python3
"""Read hopper actor clocks/cooldowns in an owned, unmodified BDS.

The authored Script API callback holds briefly for each read checkpoint. No
process writes, injection, debugger attachment or suspension are used.
"""
import argparse
import hashlib
import json
from pathlib import Path
import struct
import time

from redstone_bds_circuit_order_probe import Memory, occurrences, rva_at
from redstone_bds_probe import Reference
from redstone_native_probe import ClientPE


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_HOPPER_ACTOR "+JSON.stringify(x));
const positions=[[8,65,8],[12,65,8]];
let d,load=0,ready=false,start=0,running=false;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const inv=p=>{const c=get(p)?.getComponent("minecraft:inventory")?.container;if(!c)throw new Error("missing inventory");return c;};
const count=p=>{const c=inv(p);let n=0;for(let i=0;i<c.size;i++)n+=c.getItem(i)?.amount??0;return n;};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start"){start=system.currentTick;running=true;emit({started:true,tick:start});}});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 31 100 31 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");return;}
 if(!ready){
  if(!get([0,65,0])||!get([31,65,31])){if(system.currentTick-load>240)throw new Error("load timeout");return;}
  cmd("fill 4 62 4 16 69 12 air");
  for(const p of positions){cmd(`setblock ${p.join(" ")} hopper ["facing_direction"=2]`);cmd(`setblock ${p[0]} 65 7 barrel`);}
  ready=true;emit({ready:true,tick:system.currentTick,positions});return;
 }
 if(!running)return;
 const t=system.currentTick-start;
 const cells=positions.map(p=>({position:p,source:count(p),destination:count([p[0],65,7])}));
 if(t===40||t===41){const i=t-40;inv(positions[i]).setItem(0,new ItemStack("minecraft:diamond",4));emit({loaded:true,tick:system.currentTick,position:positions[i],count:count(positions[i])});}
 if(t>=36&&t<=60){const deadline_ms=Date.now()+500;emit({checkpoint:true,tick:system.currentTick,age:t,cells,deadline_ms});while(Date.now()<deadline_ms){}}
 if(t===64){emit({complete:true,count:25});system.clearRun(h);running=false;}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def locate_vtable(bds, client):
    prefix = client.read(0x4ACB390, 0x20)
    matches = list(occurrences(bds.data, prefix))
    if len(matches) != 1:
        raise RuntimeError('no unique BDS hopper tick prefix')
    tick = rva_at(bds, matches[0])
    if bds.read(tick+0xB5, 1) != b'\xe9':
        raise RuntimeError('native hopper tick tail changed')
    body = tick+0xBA+struct.unpack('<i', bds.read(tick+0xB6, 4))[0]
    for pattern in ('48898610060000', '8b8688020000', '8b8e8c020000'):
        if bytes.fromhex(pattern) not in bds.read(body, 0xB0):
            raise RuntimeError('native hopper field layout changed')
    header = struct.unpack_from('<I', bds.data, 0x3C)[0]
    image_base = struct.unpack_from('<Q', bds.data, header+48)[0]
    pointers = list(occurrences(bds.data, struct.pack('<Q', image_base+tick)))
    if len(pointers) != 1:
        raise RuntimeError('no unique hopper tick-component vtable')
    vtable = rva_at(bds, pointers[0])-0x20
    position = struct.unpack('<Q', bds.read(vtable+0x10, 8))[0]-image_base
    if bds.read(position, 5) != bytes.fromhex('488d41c8c3'):
        raise RuntimeError('unexpected hopper position accessor')
    return vtable, dict(tick_rva=hex(tick),body_rva=hex(body),vtable_rva=hex(vtable),
                        prefix_sha256=hashlib.sha256(prefix).hexdigest())


def sample(memory, pointer, position, vtable):
    base = pointer-0x40
    before = memory.read(base+0x288, 0x10)
    if struct.unpack('<Q', memory.read(pointer, 8))[0] != memory.base+vtable:
        raise RuntimeError('hopper actor was replaced')
    if struct.unpack('<iii', memory.read(base+8, 12)) != tuple(position):
        raise RuntimeError('hopper position changed')
    ticks = struct.unpack('<i', memory.read(pointer+8, 4))[0]
    last_tick = struct.unpack('<Q', memory.read(base+0x610, 8))[0]
    cooldown, move_cooldown = struct.unpack_from('<ii', before)
    speed = struct.unpack_from('<i', before, 12)[0]
    # The inspected HopperBlockActor constructor stores 4 at 0x294. Do not
    # infer this internal counter's units from the observed eight-GT interval.
    if before != memory.read(base+0x288, 0x10) or not -1 <= cooldown <= 16 or not -1 <= move_cooldown <= 16 or speed != 4:
        raise RuntimeError(f'hopper read was not stable or field layout differs: cooldown={cooldown}, move={move_cooldown}, speed={speed}, bytes={before.hex()}, last_tick={last_tick}, ticks={ticks}')
    return dict(position=position,actor_ticks=ticks,last_tick=last_tick,cooldown=cooldown,move_cooldown=move_cooldown,speed=speed)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('binary','assets','client','output'):
        parser.add_argument('--'+name,type=Path,required=True)
    parser.add_argument('--port',type=int,default=30901)
    args = parser.parse_args()
    vtable, locator = locate_vtable(ClientPE(args.binary), ClientPE(args.client))
    reference = Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT,local_client=True)
    memory = None
    result = dict(complete=False,binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),locator=locator,checkpoints=[],loads=[])
    try:
        actors = {}
        while True:
            lines = reference.until(lambda line:'ADAMANT_HOPPER_ACTOR ' in line,timeout=90)
            record = json.loads(lines[-1].split('ADAMANT_HOPPER_ACTOR ',1)[1].strip())
            if record.get('error'):
                raise RuntimeError(record)
            if record.get('ready'):
                result['setup_tick']=record['tick']
                memory = Memory(reference.process)
                memory.snapshot()
                positions = {tuple(p) for p in record['positions']}
                for pointer in memory.find(struct.pack('<Q',memory.base+vtable)):
                    try:
                        pos = struct.unpack('<iii',memory.cached(pointer-0x38,12))
                        if pos in positions:
                            if pos in actors:raise RuntimeError('duplicate hopper actor')
                            actors[pos]=pointer
                    except (OSError,ValueError):
                        continue
                if set(actors) != positions:raise RuntimeError('could not locate both owned hopper actors')
                memory.live = True
                reference.process.stdin.write('scriptevent audit:start\n')
                reference.process.stdin.flush()
            if record.get('started'):
                result['start_tick']=record['tick']
            if record.get('loaded'):
                if record['count'] != 4:raise RuntimeError('native load failed')
                result['loads'].append(record)
            if record.get('checkpoint'):
                if time.time()*1000 >= record['deadline_ms']-20:raise RuntimeError('missed native checkpoint')
                record['actors']=[sample(memory,pointer,list(pos),vtable) for pos,pointer in actors.items()]
                result['checkpoints'].append(record)
                print(json.dumps(dict(age=record['age'],actors=record['actors'])),flush=True)
            if record.get('complete'):
                if record['count'] != 25 or len(result['checkpoints']) != 25 or len(result['loads']) != 2:raise RuntimeError('incomplete actor trace')
                result['complete']=True
                break
    finally:
        if memory:memory.close()
        reference.stop()
        result['log_directory']=str(reference.root)
        args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'],logs=str(reference.root))),flush=True)
