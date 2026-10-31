#!/usr/bin/env python3
"""Observe native wind activation of doors, trapdoors, gates and lit blocks."""
import json

from redstone_bds_timing_probe import arguments, capture


def material_cases():
    kinds = [(k,'door') for k in ('wooden_door','spruce_door','crimson_door','bamboo_door',
        'copper_door','exposed_copper_door','oxidized_copper_door','waxed_oxidized_copper_door','iron_door')]
    kinds += [(k,'trapdoor') for k in ('trapdoor','spruce_trapdoor','copper_trapdoor','iron_trapdoor')]
    kinds += [(k,'gate') for k in ('fence_gate','crimson_fence_gate')]
    kinds += [(k,'candle') for k in ('candle','red_candle')]
    kinds += [(k,'cake') for k in ('candle_cake','red_candle_cake')]
    kinds += [(k,'campfire') for k in ('campfire','soul_campfire')]
    kinds += [('fire','fire')]
    return [dict(kind=k,form=f,initial=i,powered=p,phase=phase)
        for k,f in kinds for i in (False,True) for p in (False,True) for phase in range(2)]


SCRIPT=r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
function definition(c,top=false){
 const s=c.initial;
 if(c.form==="door")return `${c.kind} ["minecraft:cardinal_direction"="east","door_hinge_bit"=false,"upper_block_bit"=${top},"open_bit"=${top?false:s}]`;
 if(c.form==="trapdoor")return `${c.kind} ["direction"=0,"open_bit"=${s},"upside_down_bit"=false]`;
 if(c.form==="gate")return `${c.kind} ["minecraft:cardinal_direction"="south","open_bit"=${s},"in_wall_bit"=false]`;
 if(c.form==="candle")return `${c.kind} ["candles"=0,"lit"=${s}]`;
 if(c.form==="cake")return `${c.kind} ["lit"=${s}]`;
 if(c.form==="campfire")return `${c.kind} ["minecraft:cardinal_direction"="south","extinguished"=${!s}]`;
 if(c.form==="fire")return s?"fire [\"age\"=0]":"air";
 throw new Error("unknown native wind material");
}
world.afterEvents.projectileHitBlock.subscribe(e=>{const c=cells.find(c=>c.entity?.id===e.projectile.id);if(!c)return;
 const h=e.getBlockHit();c.hits.push({tick:system.currentTick,position:h.block.location,face:String(h.face),face_location:h.faceLocation});
});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<3;z++)cmd(`tickingarea add 0 0 ${z*128} 127 100 ${z*128+127} audit_wind_material_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");cmd("gamerule dofiretick false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,383])){if(system.currentTick-loaded>480)throw new Error("native wind material chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){const scenario=cases[i],base=[8+i%8*16,65,8+Math.floor(i/8)*16],target=[base[0]+1,65,base[2]+1];
   cmd(`fill ${base[0]-3} 63 ${base[2]-3} ${base[0]+4} 69 ${base[2]+4} air`);cmd(`fill ${base[0]-3} 64 ${base[2]-3} ${base[0]+4} 64 ${base[2]+4} stone`);
   cmd(`setblock ${base.join(" ")} stone`);cmd(`setblock ${target[0]} 64 ${target[2]} ${scenario.powered?"redstone_block":"stone"}`);
   cmd(`setblock ${target.join(" ")} ${definition(scenario)}`);
   if(scenario.form==="door")cmd(`setblock ${target[0]} 66 ${target[2]} ${definition(scenario,true)}`);
   const positions=[base,target,[target[0],66,target[2]],[target[0],64,target[2]]];
   cells.push({scenario,base,target,positions,setup_tick:system.currentTick,initial:positions.map(state),changes:[],hits:[],samples:[],entity:null});
  }ready=true;start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){c.samples.push({tick:system.currentTick,blocks:c.positions.map(state)});
  if(t===20+c.scenario.phase){const position={x:c.base[0]+.5,y:65.5,z:c.base[2]+1.9};
   const e=d.spawnEntity("minecraft:wind_charge_projectile",position);e.getComponent("minecraft:projectile").shoot({x:0,y:0,z:-.75});c.entity=e;
   c.changes.push({tick:system.currentTick,spawn:{id:e.typeId,position:e.location,velocity:e.getVelocity()}});
  }
 }
 if(t===56){for(const c of cells){if(c.hits.length!==1||c.hits[0].position.x!==c.base[0]||c.hits[0].position.y!==65||c.hits[0].position.z!==c.base[2])throw new Error("native wind material impact differs: "+JSON.stringify(c.scenario));delete c.entity;emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__=='__main__':
    args=arguments()
    args.offline_reference=True
    cases=material_cases()
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
