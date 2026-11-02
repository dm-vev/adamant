#!/usr/bin/env python3
"""Trace block-resource construction in a fresh owned BDS using hardware breakpoints.

Original instruction bytes remain unchanged. This records the world RNG words
inside popResource and its constructor plus the returned actor's initial body.
It is debugger-observed evidence, separate from uninstrumented item captures.
"""
import argparse
import ctypes as C
import hashlib
import json
from pathlib import Path
import struct
import time

from redstone_bds_actor_random_probe import actor_tick
from redstone_bds_circuit_order_probe import Memory, occurrences, rva_at
from redstone_bds_level_random_probe import NativeCoreRandom, locate_level_random, random_vtable
from redstone_bds_probe import Reference
from redstone_native_probe import ClientPE
from redstone_owned_hardware_trace import OwnedHardwareTrace

CASES = [dict(kind='command_destroy',block=b,top=top,drops=drops)
         for b in ('wooden_door','iron_door','copper_door') for top in (False,True) for drops in (False,True)]
CASES += [dict(kind='api_spawn',block='stone',top=False,drops=True)]

SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_RESOURCE "+JSON.stringify(x));
let d,load=0,ready=false,running=false,index=0,current=null,frames=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const body=e=>{const s=e.getComponent("minecraft:item").itemStack;return {id:s.typeId,count:s.amount,position:e.location,velocity:e.getVelocity(),rotation:e.getRotation()};};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.afterEvents.entitySpawn.subscribe(e=>{if(current&&e.entity.typeId==="minecraft:item")current.spawns.push({tick:system.currentTick,body:body(e.entity)});});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 47 100 31 resource_random true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");return;}
 if(!ready){if(!get([0,65,0])||!get([47,65,31])){if(system.currentTick-load>480)throw new Error("chunks not ready");return;}
  ready=true;emit({ready:true});return;}
 if(!running)return;
 if(!current){const c=cases[index],base=[4+index%6*6,65,4+Math.floor(index/6)*6],target=[base[0],65+(c.top?1:0),base[2]];
  cmd(`gamerule dotiledrops ${c.drops}`);cmd(`setblock ${base[0]} 64 ${base[2]} stone`);
  if(c.kind==="command_destroy"&&cmd(`setblock ${base.join(" ")} ${c.block} ["upper_block_bit"=false]`).successCount!==1)throw new Error("door setup failed");
  current={scenario:c,base,target,setup_tick:system.currentTick,spawns:[],samples:[]};
 }
 const c=current.scenario,elapsed=system.currentTick-current.setup_tick;
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),lower:read(current.base),upper:read([current.base[0],66,current.base[2]])});
 if(elapsed===4){current.change_tick=system.currentTick;
  if(c.kind==="api_spawn")d.spawnItem(new ItemStack("minecraft:stone",1),{x:current.base[0]+.5,y:65.5,z:current.base[2]+.5});
  else current.success_count=cmd(`setblock ${current.target.join(" ")} air destroy`).successCount;
 }
 const deadline_ms=Date.now()+200;emit({checkpoint:true,tick:system.currentTick,index,deadline_ms});frames++;while(Date.now()<deadline_ms){}
 if(elapsed===12){emit(current);index++;current=null;if(index===cases.length){emit({complete:true,count:index,frames});system.clearRun(h);}}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

def unique_prefix(bds,client,rva,size,label):
    matches=[rva_at(bds,p) for p in occurrences(bds.data,client.read(rva,size))]
    if len(matches)!=1:
        raise RuntimeError(f'{label} locator differs: {matches}')
    return matches[0]

def validate_result(result):
    resources=[r for frame in result['checkpoints'] for r in frame['resources']]
    if len(resources)!=12 or sum(r['created'] for r in resources)!=6:
        raise RuntimeError('missing live enabled/disabled block-resource scopes')
    for resource in resources:
        if not resource['created']:
            if resource['words']:raise RuntimeError('guarded resource consumed world words')
            continue
        if resource['pickup_delay']!=10 or len(resource['words'])!=4:
            raise RuntimeError('native resource pickup delay/world word budget differs')
        if [w.get('float_caller') for w in resource['words']]!=['0x1a2426d','0x1a2429c','0x1a242b9','0xd1c84d']:
            raise RuntimeError('native resource position/constructor call order differs')
        if any(not resource['entry_sequence']<w['sequence']<resource['exit_sequence'] for w in resource['words']):
            raise RuntimeError('native word lies outside its resource scope')
    api=[s for s in result['scenarios'] if s['scenario']['kind']=='api_spawn']
    if len(api)!=1:raise RuntimeError('missing direct API constructor control')
    frame=next(c for c in result['checkpoints'] if c['tick']==api[0]['change_tick'])
    constructors=[w for w in frame['outside'] if w.get('float_caller')=='0xd1c84d']
    if len(constructors)!=1:raise RuntimeError('direct API item does not expose one constructor float')

class ThreadLocalRandomReader:
    def __init__(self,trace,bds,table):
        self.trace,self.memory,self.table=trace,trace.memory,table
        for rva,expected in ((0x168B645,'80b86018000000'),(0x168B661,'488d80600e0000'),(0x168B6B9,'488d8860040000')):
            if bds.read(rva,len(bytes.fromhex(expected)))!=bytes.fromhex(expected):
                raise RuntimeError('inspected BDS thread-local layout differs')
        header=struct.unpack_from('<I',bds.data,0x3C)[0]
        tls_rva,tls_size=struct.unpack_from('<II',bds.data,header+24+112+9*8)
        image_base=struct.unpack_from('<Q',bds.data,header+48)[0]
        if not tls_rva or tls_size<40:raise RuntimeError('owned BDS has no expected static TLS directory')
        index_rva=struct.unpack_from('<Q',bds.read(tls_rva,40),16)[0]-image_base
        self.index=struct.unpack('<I',self.memory.read(self.memory.base+index_rva,4))[0]
        if self.index>=64:raise RuntimeError('unexpected static TLS index')
        self.ntdll=C.WinDLL('ntdll')
        self.query=self.ntdll.NtQueryInformationThread
        self.query.argtypes=[C.c_void_p,C.c_int,C.c_void_p,C.c_ulong,C.c_void_p]
        self.query.restype=C.c_long

    def __call__(self,tid):
        basic=C.create_string_buffer(48)
        status=self.query(self.trace.handles[tid],0,basic,len(basic),None)
        if status!=0:raise RuntimeError(f'owned thread basic information failed: {status:#x}')
        teb,pid=struct.unpack_from('<QQ',basic,8)
        if pid!=self.trace.process.pid:raise RuntimeError('TLS reader thread belongs to another process')
        read=self.memory.read
        array=struct.unpack('<Q',read(teb+0x58,8))[0]
        data=struct.unpack('<Q',read(array+self.index*8,8))[0]
        core_ready=bool(read(data+0xE48,1)[0])
        wrapper_ready=bool(read(data+0x1860,1)[0])
        result=dict(core_initialized=core_ready,wrapper_initialized=wrapper_ready)
        if wrapper_ready:
            wrapper=data+0xE60
            if struct.unpack('<Q',read(wrapper,8))[0]!=self.memory.base+self.table:
                raise RuntimeError('TLS random wrapper vtable differs')
            if struct.unpack('<Q',read(wrapper+0x9F8,8))[0]!=data+0x460:
                raise RuntimeError('TLS random wrapper does not reference its native core')
        if core_ready:
            raw=read(data+0x460,0x9D4)
            result.update(raw_state=raw.hex(),seed=struct.unpack_from('<I',raw,8)[0],index=struct.unpack_from('<I',raw,0x9CC)[0])
            if result['index']>624:raise RuntimeError('invalid TLS MT state index')
        return result

def validate_tls_resources(result,native):
    for frame in result['checkpoints']:
        for resource in frame['resources']:
            before,after=resource['tls_before'],resource['tls_after']
            if not resource['created']:
                if before!=after:raise RuntimeError('guarded resource changed the TLS core')
                continue
            if not after['core_initialized']:raise RuntimeError('native resource did not initialise its TLS core')
            if before['core_initialized']:native.reset(bytes.fromhex(before['raw_state']))
            else:native.seed(after['seed'])
            words=[native.draw() for _ in range(4)]
            if C.string_at(native.state,0x9D4)!=bytes.fromhex(after['raw_state']):
                raise RuntimeError('item constructor TLS consumption is not four native MT words')
            resource['tls_words']=words
    api=next(s for s in result['scenarios'] if s['scenario']['kind']=='api_spawn')
    before=next(f['tls_checkpoint'] for f in result['checkpoints'] if f['tick']==api['change_tick']-1)
    after=next(f['tls_checkpoint'] for f in result['checkpoints'] if f['tick']==api['change_tick'])
    native.reset(bytes.fromhex(before['raw_state']))
    words=[native.draw() for _ in range(4)]
    if C.string_at(native.state,0x9D4)!=bytes.fromhex(after['raw_state']):
        raise RuntimeError('direct API item constructor does not consume four TLS MT words')
    api['tls_before'],api['tls_after'],api['tls_words']=before,after,words

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('binary','client','assets','output'):
        parser.add_argument('--'+name,type=Path,required=True)
    parser.add_argument('--port',type=int,default=30953)
    parser.add_argument('--tls',action='store_true')
    args=parser.parse_args()
    bds,client=ClientPE(args.binary),ClientPE(args.client)
    bds_hash,client_hash=hashlib.sha256(bds.data).hexdigest(),hashlib.sha256(client.data).hexdigest()
    if bds_hash!='76d547f82e02c18d0986c30b47132c9cc4171d0f2df1c00649e50ff35788b321' or client_hash!='8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5':
        raise RuntimeError('native image differs from inspected build')
    table,locator=random_vtable(bds,client)
    core_rva=int(locator['core_rva'],16)
    chunk_rva,_=actor_tick(bds,client,core_rva)
    resource_rva=unique_prefix(bds,client,0x31840D0,0x39,'popResource')
    float_matches=[rva_at(bds,p) for p in occurrences(bds.data,client.read(0xDA7390,13))
                   if bds.read(rva_at(bds,p)+33,9)==client.read(0xDA73B1,9)]
    if len(float_matches)!=1:raise RuntimeError('native float wrapper locator differs')
    locator.update(resource_rva=hex(resource_rva),chunk_rva=hex(chunk_rva),random_float_caller=hex(float_matches[0]+18))
    result=dict(complete=False,reference=__doc__,binary_sha256=bds_hash,client_sha256=client_hash,
                locator=locator,scenarios=[],checkpoints=[])
    native=NativeCoreRandom(client)
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT.replace('CASES',json.dumps(CASES)))
    memory=trace=None
    try:
        while True:
            line=reference.until(lambda s:'ADAMANT_RESOURCE ' in s,timeout=60)[-1]
            record=json.loads(line.split('ADAMANT_RESOURCE ',1)[1])
            if record.get('error'):raise RuntimeError(record)
            if record.get('ready'):
                memory=Memory(reference.process);memory.snapshot()
                _,core,owner=locate_level_random(memory,bds,table);memory.live=True
                result['owner']=owner
                trace=OwnedHardwareTrace(reference.process,memory,core,core_rva,chunk_rva,native,resource_rva=resource_rva)
                trace.random_float_caller=float_matches[0]+18
                if args.tls:trace.tls_reader=ThreadLocalRandomReader(trace,bds,table)
                trace.start();reference.command('scriptevent audit:start now')
            if record.get('checkpoint'):
                if time.time()*1000>=record['deadline_ms']-20:raise RuntimeError('missed held resource checkpoint')
                record.update(trace.checkpoint())
                if args.tls and record['index']==len(CASES)-1:
                    record['tls_checkpoint']=trace.tls_reader(trace.owner_thread)
                result['checkpoints'].append(record)
            if record.get('scenario'):
                result['scenarios'].append(record)
                print(json.dumps(dict(count=len(result['scenarios']),scenario=record['scenario'])),flush=True)
            if record.get('complete'):
                if record['count']!=len(CASES) or len(result['scenarios'])!=len(CASES) or record['frames']!=len(result['checkpoints']):
                    raise RuntimeError('incomplete native resource trace')
                validate_result(result)
                if args.tls:
                    with trace.native_lock:validate_tls_resources(result,native)
                result['complete']=True;break
    finally:
        try:
            if trace:trace.stop()
        finally:
            if memory:memory.close()
            reference.stop();native.close()
            result['log_directory']=str(reference.root)
            args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
            print(json.dumps(dict(complete=result['complete'],scenes=len(result['scenarios']),logs=str(reference.root))),flush=True)
