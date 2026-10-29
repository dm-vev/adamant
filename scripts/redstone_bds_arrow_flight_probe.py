#!/usr/bin/env python3
"""Control native arrow free flight used by the redstone projectile probes."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  cmd("tickingarea add 0 0 0 191 256 63 audit_arrow_a true");cmd("tickingarea add 0 0 64 191 256 159 audit_arrow_b true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");return;}
 if(!ready){if(!get([0,128,0])||!get([191,192,159])){if(system.currentTick-loaded>480)throw new Error("arrow flight chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){
   const scenario=cases[i],base=[8+i%6*32,scenario.height,8+Math.floor(i/6)*32];
   cells.push({scenario,base,setup_tick:system.currentTick,changes:[],samples:[],entity:null,spawn:null});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){const e=c.entity?.isValid?c.entity:null;
  c.samples.push({tick:system.currentTick,projectile:e?{id:e.typeId,position:e.location,velocity:e.getVelocity(),aabb:typeof e.getAABB==="function"?e.getAABB():null}:null});
  if(t===20+c.scenario.phase){const position={x:c.base[0]+.3,y:c.base[1]+.6,z:c.base[2]+.7},v=c.scenario.velocity;
   const arrow=d.spawnEntity("minecraft:arrow",position),projectile=arrow.getComponent("minecraft:projectile");
   if(!projectile)throw new Error("native arrow lacks projectile component");
   projectile.shoot({x:v[0],y:v[1],z:v[2]});c.entity=arrow;
   c.spawn={id:arrow.typeId,position:arrow.location,velocity:arrow.getVelocity()};c.changes.push({tick:system.currentTick,kind:"spawn"});
  }
 }
 if(t===64){for(const c of cells){if(!c.entity?.isValid||!c.spawn)throw new Error("native free-flight arrow disappeared");delete c.entity;emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments(); args.offline_reference = True
    velocities = ([.75,0,0],[-.75,0,0],[0,0,.75],[0,0,-.75],[.5,.5,.5],[0,1.5,0],[0,-.75,0])
    cases = [dict(velocity=v,height=height,phase=phase) for height in (128,192) for v in velocities for phase in (0,1)]
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
