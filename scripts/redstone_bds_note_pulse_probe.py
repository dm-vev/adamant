#!/usr/bin/env python3
"""Native note-block pulse controls with authored pitches, instruments and occlusion."""
import json

from redstone_audit_structure import byte, string, one_block
from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<192;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${z+95} audit_note_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");return;}
 if(!ready){if(!get([0,65,0])||!get([255,65,191])){if(system.currentTick-loaded>480)throw new Error("note chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){
   const scenario=cases[i],base=[8+i%12*20,65,8+Math.floor(i/12)*20],source=[base[0]-1,65,base[2]];
   cmd(`fill ${base[0]-2} 64 ${base[2]-2} ${base[0]+2} 67 ${base[2]+2} air`);
   cmd(`setblock ${base[0]} 63 ${base[2]} stone`);
   cmd(`setblock ${base[0]} 64 ${base[2]} ${scenario.below}`);
   if(!cmd(`structure load audit:note_${scenario.pitch} ${base.join(" ")}`).successCount)throw new Error("note pitch input failed");
   if(scenario.sealed_water){for(const v of [[-1,0],[1,0],[0,-1],[0,1]])cmd(`setblock ${base[0]+v[0]} 66 ${base[2]+v[1]} stone`);cmd(`setblock ${base[0]} 67 ${base[2]} stone`);}
   cmd(`setblock ${base[0]} 66 ${base[2]} ${scenario.above}`);
   cells.push({scenario,base,source,setup_tick:system.currentTick,changes:[],samples:[]});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){c.samples.push({tick:system.currentTick,block:state(c.base),source:state(c.source),below:state([c.base[0],64,c.base[2]]),above:state([c.base[0],66,c.base[2]])});
  for(let edge=0;edge<c.scenario.edges.length;edge++)if(t===40+c.scenario.phase+c.scenario.edges[edge]){
   const powered=edge%2===0;cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered});
  }
 }
 if(t===128){for(const c of cells){if(c.changes.length!==c.scenario.edges.length||c.samples.some(s=>s.below.id!=="minecraft:"+c.scenario.below))throw new Error("note source/instrument control differs");emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def note_cases():
    below = ('air', 'stone', 'sand', 'glass', 'oak_planks', 'clay', 'gold_block', 'white_wool',
             'packed_ice', 'bone_block', 'iron_block', 'soul_sand', 'pumpkin', 'emerald_block', 'hay_block', 'glowstone')
    cases = [dict(below=b, above='air', pitch=(i%3)*12, phase=phase, edges=[0,width])
             for i,b in enumerate(below) for phase in (0,1) for width in (1,2,8)]
    cases += [dict(below='air', above=a, pitch=12, phase=phase, edges=[0,8], sealed_water=a=='water')
              for a in ('stone','glass','water','oak_leaves') for phase in (0,1)]
    cases += [dict(below=b, above='air', pitch=24, phase=phase, edges=[0,8,20,28])
              for b in ('air','oak_planks','glass','gold_block') for phase in (0,1)]
    return cases


def note_structures():
    return {f'audit/note_{pitch}':one_block('minecraft:noteblock',{},
        {'id':string('Music'),'note':byte(pitch)}) for pitch in (0,12,24)}


if __name__ == '__main__':
    args=arguments();args.offline_reference=True
    cases=note_cases()
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases),structures=note_structures())
