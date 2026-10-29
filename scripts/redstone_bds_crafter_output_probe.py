#!/usr/bin/env python3
"""Capture native crafter outputs into all-sided containers and world leftovers.

The Script API reads destination slots, block states and dropped stack sizes.
Initial/final source grids use unique native structure snapshots read only after
the owned BDS stops. An already built readback helper is supplied explicitly.
"""
import argparse
import json
from pathlib import Path
import subprocess

from redstone_audit_structure import byte,string,listing,one_block
from redstone_bds_timing_probe import capture


SCRIPT=r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const vectors=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
let d,loaded=0,ready=false,start=0,offset=0,cells=[];
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const block=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded output cell "+p);return b;};
const read=p=>{const b=block(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const inv=p=>{const c=block(p).getComponent("minecraft:inventory")?.container;if(!c)throw new Error("missing destination inventory "+p);return c;};
const slots=p=>{const c=inv(p);return Array.from({length:c.size},(_,i)=>{const s=c.getItem(i);return s?{id:s.typeId,count:s.amount}:null;});};
const snapshot=(c,phase)=>{const name=`audit_crafter_output_${phase}_${c.index}`;if(!cmd(`structure save ${name} ${c.base.join(" ")} ${c.base.join(" ")} false disk true`).successCount)throw new Error("source snapshot failed");return "mystructure:"+name;};
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];
 for(let i=0;i<12&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[32+i%4*64,65,32+Math.floor(i/4)*64],v=vectors[scenario.face];
  const target=base.map((x,j)=>x+v[j]),source=base.map((x,j)=>x+vectors[scenario.face===4?5:4][j]);
  cmd(`fill ${base[0]-8} 62 ${base[2]-8} ${base[0]+8} 70 ${base[2]+8} air`);
  cmd(`fill ${base[0]-8} 63 ${base[2]-8} ${base[0]+8} 63 ${base[2]+8} bedrock`);
  set(base,`crafter ["orientation"="${scenario.orientation}"]`);
  if(scenario.face!==1)set([target[0],target[1]-1,target[2]],"stone");
  set(target,scenario.target);
  let lock=null;
  if(scenario.target==="hopper"){lock=target.map((x,j)=>x+vectors[scenario.face>=4?2:5][j]);set(lock,"redstone_block");}
  cells.push({scenario,index:offset+i,base,target,source,lock,setup_tick:system.currentTick,changes:[{tick:system.currentTick,kind:"setup"}],samples:[]});
 }start=system.currentTick;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<192;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${z+95} audit_crafter_output_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){for(let x=0;x<256;x+=16)for(let z=0;z<192;z+=16)if(!d.getBlock({x,y:65,z})){
  if(system.currentTick-loaded>480)throw new Error("output chunk load timeout");return;
 }if(system.currentTick%20!==2)return;ready=true;setup();return;}
 const t=system.currentTick-start;
 for(const c of cells){
  if(read(c.target).id!=="minecraft:"+c.scenario.target)throw new Error("destination lost support");
  c.samples.push({tick:system.currentTick,block:read(c.base),target:read(c.target),target_items:slots(c.target),
   drops:d.getEntities({type:"minecraft:item",location:{x:c.base[0]+.5,y:65.5,z:c.base[2]+.5},maxDistance:12}).map(e=>{const s=e.getComponent("minecraft:item").itemStack;return {id:s.typeId,count:s.amount};})});
  if(t===40){
   if(read(c.base).states.triggered_bit)throw new Error("destination lock powers source");
   if(!cmd(`structure load ${c.scenario.structure.replace("/",":")} ${c.base.join(" ")}`).successCount)throw new Error("crafter source grid load failed");
   const target=inv(c.target);if(target.size!==c.scenario.initial_target.length)throw new Error("destination slot count differs");
   for(let i=0;i<target.size;i++){const s=c.scenario.initial_target[i];if(s)target.setItem(i,new ItemStack(s.id,s.count));}
   c.initial_source_ref=snapshot(c,"initial");c.load_control=slots(c.target);c.changes.push({tick:system.currentTick,kind:"load"});
  }
  if(t===60||t===76){set(c.source,t===60?"redstone_block":"air");c.changes.push({tick:system.currentTick,kind:t===60?"power":"off"});}
 }
 if(t===128){for(const c of cells){c.final_source_ref=snapshot(c,"final");emit(c);}
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else{ready=false;cells=[];}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('binary','assets','output','reader'):parser.add_argument('--'+name,type=Path,required=True)
    parser.add_argument('--port',type=int,default=30910)
    parser.add_argument('--remainders',action='store_true')
    args=parser.parse_args();args.offline_reference=True
    destination=args.output;args.output=Path(f'.tmp/crafter_output_raw_{args.port}.json')
    stack=lambda name,n:dict(id='minecraft:'+name,count=n)
    inputs={
        'iron_block':[stack('iron_ingot',4)]*9,
        'planks':[stack('oak_log',4)]+[None]*8,
        'bottles':[stack('glass',4) if i in (3,5,7) else None for i in range(9)],
    }
    if args.remainders:
        inputs={
            'cake':[stack(name,1) for name in ('milk_bucket','milk_bucket','milk_bucket','sugar','egg','sugar','wheat','wheat','wheat')],
            'honey_block':[stack('honey_bottle',4) if i in (0,1,3,4) else None for i in range(9)],
        }
    orientations=('down_north','up_north','north_up','south_up','west_up','east_up')
    cases=[];structures={}
    for face,orientation in enumerate(orientations):
        for recipe,items in inputs.items():
            name=f'audit/crafter_output_{recipe}_{face}'
            stacks=[{'Slot':byte(i),'Name':string(s['id']),'Count':byte(s['count']),'Damage':(2,0)} for i,s in enumerate(items) if s]
            structures[name]=one_block('minecraft:crafter',{'orientation':string(orientation),'crafting':byte(0),'triggered_bit':byte(0)},
                {'id':string('Crafter'),'disabled_slots':(2,0),'Items':listing(10,stacks)})
            output={'iron_block':'iron_block','planks':'oak_planks','bottles':'glass_bottle','cake':'cake','honey_block':'honey_block'}[recipe]
            for target,size in (('barrel',27),('chest',27),('hopper',5),('furnace',3),('brewing_stand',5)):
                for pattern in (('empty','earlier_empty') if target in ('barrel','chest','hopper') else ('empty',)):
                    initial=[None]*size
                    if pattern=='earlier_empty':initial[1]=stack(output,62)
                    cases.append(dict(kind='crafter',face=face,orientation=orientation,target=target,recipe=recipe,fill=pattern,
                                      phase=0,structure=name,initial_source=items,initial_target=initial))
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases),structures=structures)
    result=json.loads(args.output.read_text(encoding='utf-8'))
    result['chunk_phase_control']='each setup at tick%20==2; crafting samples avoid both ticking-area shuffle phases'
    completed=subprocess.run([str(args.reader.resolve()),'-db',str(Path(result['log_directory'])/'worlds/redstone-reference/db'),
                              '-prefix','structuretemplate_mystructure:audit_crafter_output_','-immutable-audit-wal'],
                             capture_output=True,text=True,encoding='utf-8',check=True)
    snapshots={}
    for line in completed.stdout.splitlines():
        record=json.loads(line);root=record['structure'];palette=root['structure']['palette']['default']
        if root['size']!=[1,1,1] or palette['block_palette'][0]['name']!='minecraft:crafter':raise RuntimeError('invalid source snapshot')
        if record['state_types']!=[{'orientation':'string','crafting':'uint8','triggered_bit':'uint8'}]:raise RuntimeError('native source state types differ')
        actor=palette['block_position_data']['0']['block_entity_data']
        if actor['id']!='Crafter' or actor['disabled_slots']!=0:raise RuntimeError('invalid source actor snapshot')
        items=[None]*9
        for s in actor['Items']:
            slot=s['Slot']
            if not 0<=slot<9 or items[slot] is not None:raise RuntimeError('invalid source snapshot slot')
            if s['Count']:items[slot]=dict(id=s['Name'],count=s['Count'])
        if record['name'] in snapshots:raise RuntimeError('duplicate source snapshot')
        snapshots[record['name']]=dict(items=items,states=palette['block_palette'][0]['states'])
    used=set()
    for c in result['scenarios']:
        for phase in ('initial','final'):
            key=c.pop(phase+'_source_ref')
            if key in used or key not in snapshots:raise RuntimeError('missing/reused source snapshot')
            used.add(key);snap=snapshots[key]
            if snap['states']['orientation']!=c['scenario']['orientation']:raise RuntimeError('native source orientation differs')
            if snap['states']['crafting']!=0 or snap['states']['triggered_bit']!=0:raise RuntimeError('source snapshot is unexpectedly powered/crafting')
            c[phase+'_source']=snap['items']
        if c['initial_source']!=c['scenario']['initial_source'] or c['load_control']!=c['scenario']['initial_target']:
            raise RuntimeError('native input controls differ')
    if len(used)!=len(cases)*2 or len(snapshots)!=len(used):raise RuntimeError('incomplete source readback')
    result['source_readback']=dict(snapshots=len(used),method='unique native structures read from stopped database',journal=completed.stderr.strip())
    destination.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=True,scenarios=len(cases),snapshots=len(used),output=str(destination))),flush=True)


if __name__=='__main__':main()
