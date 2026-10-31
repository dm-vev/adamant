#!/usr/bin/env python3
"""Capture native wind-burst switch grids with copied level-RNG input words.

Only the disposable owned BDS is read. Bounded Script checkpoints permit two
stable ReadProcessMemory snapshots; original client RNG instructions on copies
verify their transition and supply exact inputs for the footprint replay.
"""
import argparse
from contextlib import nullcontext
import hashlib
import json
from pathlib import Path
import time

from redstone_bds_circuit_order_probe import Memory
from redstone_bds_bell_breeze_probe import HARVEST
from redstone_bds_level_random_probe import NativeCoreRandom, locate_level_random, random_vtable
from redstone_bds_probe import Reference
from redstone_native_probe import ClientPE


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_WIND_GRID "+JSON.stringify(x));
const delta=[[0,0,1],[-1,0,0],[0,0,-1],[1,0,0]],base=[8,65,8];
let d,ready=false,requested=false,index=0,current=null,start=0,serial=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});
world.afterEvents.projectileHitBlock.subscribe(e=>{if(current?.entity?.id!==e.projectile.id)return;
 const hit=e.getBlockHit();current.hits.push({tick:system.currentTick,position:hit.block.location,face:String(hit.face),face_location:hit.faceLocation});
});
function hold(stage){const deadline_ms=Date.now()+500;emit({checkpoint:true,scene:index,stage,tick:system.currentTick,deadline_ms});while(Date.now()<deadline_ms){}}
function setup(){
 const scenario=cases[index],v=delta[scenario.direction];
 cmd("fill 3 63 3 13 69 13 air");cmd("fill 3 64 3 13 64 13 stone");
 const definition=scenario.kind==="lever"?'lever ["lever_direction"="up_north_south","open_bit"=false]':scenario.kind+' ["facing_direction"=1,"button_pressed_bit"=false]';
 for(let x=-3;x<=3;x++)for(let z=-3;z<=3;z++)if(x||z)cmd(`setblock ${8+x} 65 ${8+z} ${definition}`);
 cmd("setblock 8 65 8 stone");
 if(scenario.wall){for(let n=-3;n<=3;n++)cmd(`fill ${8-v[0]+v[2]*n} 65 ${8-v[2]-v[0]*n} ${8-v[0]+v[2]*n} 67 ${8-v[2]-v[0]*n} stone`);}
 const cells=[];for(let x=-3;x<=3;x++)for(let z=-3;z<=3;z++){const position=[8+x,65,8+z];cells.push({position,block:state(position)});}
 current={scenario,base,setup_tick:system.currentTick,cells,changes:[],hits:[],samples:[],entity:null};start=system.currentTick;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add 0 0 0 31 100 31 audit_wind_grid true");
 cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule doweathercycle false");cmd("weather clear");return;}
 if(!ready){if(!get([0,65,0])||!get([31,65,31]))return;ready=true;emit({ready:true});return;}
 if(!requested)return;
 if(!current){if(system.currentTick%20!==2)return;setup();return;}
 const t=system.currentTick-start,values=[];
 for(const c of current.cells){const s=state(c.position),bit=s.id==="minecraft:lever"?"open_bit":s.id.endsWith("_button")?"button_pressed_bit":null;
  if(s.id!==c.block.id)throw new Error("wind grid block identity changed");
  for(const k of Object.keys(c.block.states))if(k!==bit&&s.states[k]!==c.block.states[k])throw new Error("wind grid non-toggle property changed");
  values.push(bit?Number(s.states[bit]):-1);
 }
 current.samples.push({tick:system.currentTick,values});
 if(t===20+current.scenario.phase){const v=delta[current.scenario.direction],position={x:8.5+v[0]*1.4,y:65.5,z:8.5+v[2]*1.4};
  const entity=d.spawnEntity("minecraft:wind_charge_projectile",position);entity.getComponent("minecraft:projectile").shoot({x:-v[0]*.75,y:0,z:-v[2]*.75});
  current.entity=entity;current.changes.push({tick:system.currentTick,spawn:{id:entity.typeId,position:entity.location,velocity:entity.getVelocity()}});hold("before");
 }
 if(t===24+current.scenario.phase)hold("after");
 if(t===56){if(current.hits.length!==1||current.hits[0].position.x!==8||current.hits[0].position.y!==65||current.hits[0].position.z!==8)throw new Error("wind grid impact control missing");
  delete current.entity;emit(current);current=null;index++;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def breeze_grid_script(script):
    script=script.replace('world.afterEvents.projectileHitBlock.subscribe',HARVEST+'\nworld.afterEvents.projectileHitBlock.subscribe')
    script=script.replace('const h=system.runInterval(()=>{try{',
        'const h=system.runInterval(()=>{try{for(const p of pool){if(!p.entity.isValid)throw new Error("held breeze grid charge expired");p.entity.clearVelocity();p.entity.teleport(p.hold);}')
    script=script.replace('cmd("tickingarea add 0 0 0 31 100 31 audit_wind_grid true");',
        'cmd("tickingarea add 0 0 0 31 100 31 audit_wind_grid true");cmd("tickingarea add -16 0 256 143 100 383 audit_wind_factory true");cmd("difficulty normal");')
    script=script.replace('if(!ready){if(!get([0,65,0])||!get([31,65,31]))return;ready=true;',
        'if(!ready){if(!get([0,65,0])||!get([31,65,31])||!get([143,65,383]))return;'
        'if(!factoryReady){factory();factoryReady=true;factoryStart=system.currentTick;return;}'
        'if(pool.length<required){if(system.currentTick-factoryStart>600)throw new Error("breeze grid harvest incomplete");return;}'
        'for(const e of d.getEntities({type:"minecraft:breeze"}))e.remove();for(const e of d.getEntities({type:"minecraft:iron_golem"}))e.remove();'
        'for(let i=0;i<pool.length;i++){const p=pool[i];p.hold={x:4+i%16,y:160,z:4+Math.floor(i/16)};p.entity.clearVelocity();p.entity.teleport(p.hold);}'
        'cmd("tickingarea remove audit_wind_factory");ready=true;')
    script=script.replace('const entity=d.spawnEntity("minecraft:wind_charge_projectile",position);',
        'const entry=pool.shift();if(!entry?.entity.isValid)throw new Error("breeze grid shot missing");'
        'const entity=entry.entity;entity.clearVelocity();entity.teleport(position);')
    return script.replace('spawn:{id:entity.typeId,position:', 'spawn:{id:entity.typeId,native_birth:entry.birth,position:')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('binary', 'assets', 'client', 'output'):
        parser.add_argument('--'+name, type=Path, required=True)
    parser.add_argument('--port', type=int, default=30891)
    parser.add_argument('--breeze', action='store_true')
    parser.add_argument('--hardware-explosion', action='store_true')
    parser.add_argument('--limit', type=int)
    args = parser.parse_args()
    cases = [dict(kind=k, direction=d, phase=p, wall=w)
             for k in ('lever', 'stone_button', 'wooden_button', 'polished_blackstone_button')
             for d in range(4) for w in (False, True) for p in range(2)]
    if args.breeze:
        cases=[dict(kind=k,direction=d,wall=False,phase=(d+i)&1) for i,k in enumerate(('lever','stone_button','wooden_button','polished_blackstone_button')) for d in range(4)]
    if args.limit is not None:
        if not 1<=args.limit<=len(cases):raise ValueError('limit must select a nonempty prefix of the native scenes')
        cases=cases[:args.limit]
    bds, client = ClientPE(args.binary), ClientPE(args.client)
    client_hash = hashlib.sha256(client.data).hexdigest()
    if client_hash != '8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('client differs from inspected image')
    table, locator = random_vtable(bds, client)
    native = NativeCoreRandom(client)
    script=SCRIPT.replace('CASES',json.dumps(cases))
    if args.breeze:
        script=breeze_grid_script(script)
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=script, local_client=True)
    result = dict(complete=False, reference=__doc__, binary_sha256=hashlib.sha256(bds.data).hexdigest(),
        client_sha256=client_hash, locator=locator, breeze=args.breeze, scenarios=[])
    memory, core, checkpoints, trace = None, None, {}, None
    hardware={}
    try:
        while True:
            line = reference.until(lambda s: 'ADAMANT_WIND_GRID ' in s, timeout=45)[-1]
            record = json.loads(line.split('ADAMANT_WIND_GRID ', 1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if record.get('ready'):
                memory = Memory(reference.process)
                memory.snapshot()
                _, core, result['owner'] = locate_level_random(memory, bds, table)
                memory.live = True
                if args.hardware_explosion:
                    from redstone_bds_actor_random_probe import actor_tick
                    from redstone_bds_circuit_order_probe import occurrences,rva_at
                    from redstone_owned_hardware_trace import OwnedHardwareTrace
                    matches=[rva_at(bds,p) for p in occurrences(bds.data,client.read(0x11346C0,55)) if bds.read(rva_at(bds,p)+0x74,22)==client.read(0x1134734,22)]
                    if len(matches)!=1:raise RuntimeError('native explosion entry/layout is ambiguous')
                    core_rva=int(locator['core_rva'],16)
                    chunk_rva,_=actor_tick(bds,client,core_rva)
                    trace=OwnedHardwareTrace(reference.process,memory,core,core_rva,chunk_rva,native,explosion_rva=matches[0])
                    trace.start()
                    result['hardware_explosion_rva']=hex(matches[0])
                reference.command('scriptevent audit:start now')
            if record.get('checkpoint'):
                if core is None or time.time()*1000 >= record['deadline_ms']-20:
                    raise RuntimeError('missed native wind RNG checkpoint')
                raw = memory.read(core, 0x9D4)
                if raw != memory.read(core, 0x9D4):
                    raise RuntimeError('native RNG changed in held wind checkpoint')
                checkpoints[record['scene'], record['stage']] = (raw, record)
                if trace:
                    observed=trace.checkpoint()
                    if record['stage']=='after':
                        if len(observed['explosions'])!=1:raise RuntimeError('native explosion scope count differs')
                        explosion=observed['explosions'][0]
                        draws=observed['outside']+[w for c in observed['chunks'] for w in c['words']]
                        draws=sorted((w for w in draws if w['sequence']>explosion['sequence']),key=lambda w:w['sequence'])
                        if [w['value'] for w in draws[:1352]]!=explosion['words']:raise RuntimeError('observed ray draws do not match the copied entry state')
                        hardware[record['scene']]=explosion
            if 'scenario' in record:
                index = len(result['scenarios'])
                before, pre = checkpoints[index, 'before']
                after, post = checkpoints[index, 'after']
                with (trace.native_lock if trace else nullcontext()):
                    native.reset(before)
                    words = []
                    while native.copied_state() != after[8:] and len(words) <= 16384:
                        words.append(native.draw())
                    if native.copied_state() != after[8:] or not words:
                        raise RuntimeError('unexplained native wind RNG transition')
                    record['random_input'] = dict(words=words, before=pre, after=post,
                        before_sha256=hashlib.sha256(before).hexdigest(), after_sha256=hashlib.sha256(after).hexdigest(),
                        next_words=[native.draw() for _ in range(16)])
                if trace:record['explosion']=hardware[index]
                result['scenarios'].append(record)
                print(json.dumps(dict(scene=index, scenario=record['scenario'], draws=len(words))), flush=True)
            if record.get('complete'):
                if record['count'] != len(cases) or len(result['scenarios']) != len(cases):
                    raise RuntimeError('incomplete native wind grid corpus')
                result['complete'] = True
                break
    finally:
        if trace:
            trace.stop()
        if memory:
            memory.close()
        reference.stop()
        native.close()
        result['log_directory'] = str(reference.root)
        args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'], scenes=len(result['scenarios']), logs=str(reference.root))), flush=True)
