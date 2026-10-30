#!/usr/bin/env python3
"""Read rail component direction/source lists in an owned original BDS.

The stable-API scene holds its own callback briefly at each checkpoint. Only
ReadProcessMemory observes the owned child; there are no process writes,
injection, debugger attachment or thread suspension.
"""
import argparse
import hashlib
import json
from pathlib import Path
import struct

from redstone_bds_probe import Reference
from redstone_bds_circuit_order_probe import Memory, inspect, torch_vtables
from redstone_bds_powered_rail_probe import SCRIPT
from redstone_native_probe import ClientPE


def sample(memory, system, positions):
    descriptor = memory.read(system+8, 0x40)
    head, count = struct.unpack_from('<QQ', descriptor, 8)
    if count > 2048:
        raise RuntimeError('invalid native component count')
    pointer = struct.unpack('<Q', memory.read(head, 8))[0]
    result, visited = [], 0
    while pointer != head and visited <= count:
        entry = memory.read(pointer, 0x28)
        pos = struct.unpack_from('<iii', entry, 0x10)
        if pos in positions:
            component = struct.unpack_from('<Q', entry, 0x20)[0]
            state = memory.read(component, 0x70)
            begin, end = struct.unpack_from('<QQ', state, 8)
            if end < begin or (end-begin)%32 or end-begin > 32768:
                raise RuntimeError('invalid native rail source vector')
            records = memory.read(begin, end-begin) if begin != end else b''
            sources = []
            for offset in range(0, len(records), 32):
                actor, loss, x, y, z, face, direct, remaining = struct.unpack_from('<QiiiiBB2xi', records, offset)
                source = memory.read(actor, 0x60) if actor else None
                sources.append(dict(position=[x,y,z], loss=loss, face=face, direct=bool(direct), remaining=remaining,
                                    strength=struct.unpack_from('<h',source,0x4E)[0] if source else None,
                                    type=hex(struct.unpack_from('<Q',source,0x58)[0]) if source else None))
            result.append(dict(position=pos, strength=struct.unpack_from('<h',state,0x4E)[0], direction=state[0x52],
                               type=hex(struct.unpack_from('<Q',state,0x60)[0]), rail_type=struct.unpack_from('<i',state,0x68)[0], sources=sources))
        pointer = struct.unpack_from('<Q', entry)[0]
        visited += 1
    if visited != count or memory.read(system+8,0x40) != descriptor or {tuple(c['position']) for c in result} != positions:
        raise RuntimeError('native map changed or rail components are missing')
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('binary','assets','client','output'):
        parser.add_argument('--'+name,type=Path,required=True)
    parser.add_argument('--port',type=int,default=30931)
    args = parser.parse_args()
    cases = [dict(block='golden_rail',axis=axis,length=10,phase=0,topology=topology)
             for axis in ('x','z') for topology in ('rise','wrong_axis')]
    script = SCRIPT.replace('CASES',json.dumps(cases))
    script = script.replace('let d,load=0,start=0,cells=[];', 'let d,load=0,start=0,cells=[],located=false,locateSent=false;')
    script = script.replace('cmd("gamerule dotiledrops false");return;', '''cmd("gamerule dotiledrops false");
 cmd("tickingarea add -16 0 -16 16 100 16 audit_locator true");
 cmd("scoreboard objectives add audit_gate dummy");cmd("scoreboard players set advance audit_gate 0");return;''')
    script = script.replace('if(system.currentTick&1)return;start=system.currentTick;', 'if(!get([-1,60,-1]))return;if(system.currentTick&1)return;start=system.currentTick;')
    script = script.replace('if(system.currentTick&1)return;start=system.currentTick;', '''if(system.currentTick&1)return;start=system.currentTick;
 set([0,60,0],"stone");set([0,61,0],"redstone_wire");set([0,59,0],'lever ["lever_direction"="down_north_south","open_bit"=false]');
 const positions=[[1,60,0],[0,60,-1],[-1,60,0],[0,60,1]],faces=["west","south","east","north"];
 for(let i=0;i<4;i++){set([positions[i][0],61,positions[i][2]],"stone");set(positions[i],`redstone_torch ["torch_facing_direction"="${faces[i]}"]`);}''')
    script = script.replace('const t=system.currentTick-start;', '''const t=system.currentTick-start;
 if(!located){
  if(t<16)return;
  if(!locateSent){emit({locate:true,positions:[[1,60,0],[0,60,-1],[-1,60,0],[0,60,1]]});locateSent=true;return;}
  if(world.scoreboard.getObjective("audit_gate").getScore("advance")!==1)return;
  located=true;start=system.currentTick;for(const c of cells)c.setup_tick=start;return;
 }
 if([10,35,60].includes(t)){
  emit({checkpoint:t,tick:system.currentTick,cells:cells.map(c=>({scenario:c.scenario,positions:c.rail_positions,source:c.source,blocks:c.rail_positions.map(read)}))});
  const until=Date.now()+750;while(Date.now()<until){}
 }''')
    vtables, locator = torch_vtables(ClientPE(args.binary),ClientPE(args.client))
    reference = Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=script,local_client=True)
    result = dict(complete=False,reference=__doc__,binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),locator=locator,checkpoints=[])
    memory = None
    try:
        while True:
            lines = reference.until(lambda line:'ADAMANT_TIMING ' in line,timeout=90)
            record = json.loads(lines[-1].split('ADAMANT_TIMING ',1)[1].strip())
            if record.get('error'):
                raise RuntimeError(record)
            if record.get('locate'):
                memory = Memory(reference.process)
                inspect(memory,vtables,{tuple(p) for p in record['positions']})
                memory.live=True
                reference.process.stdin.write('scoreboard players set advance audit_gate 1\n');reference.process.stdin.flush()
            if 'checkpoint' in record:
                positions = {tuple(p) for c in record['cells'] for p in c['positions']}
                result['checkpoints'].append(dict(**record,components=sample(memory,memory.system_address,positions)))
                print(json.dumps(dict(checkpoint=record['checkpoint'],components=len(positions))),flush=True)
            if record.get('complete'):
                if record['count'] != 4 or len(result['checkpoints']) != 3:
                    raise RuntimeError('incomplete rail component checkpoints')
                result['complete']=True
                break
    finally:
        if memory: memory.close()
        reference.stop()
        result['log_directory']=str(reference.root)
        result['build_log']=[line for line in reference.log if 'Version:' in line or 'Build ID:' in line]
        args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'],output=str(args.output),logs=str(reference.root))),flush=True)
