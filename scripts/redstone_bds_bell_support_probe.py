#!/usr/bin/env python3
"""Capture bell support changes, attachment conversion and dormant command states."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const delta=[[0,0,1],[-1,0,0],[0,0,-1],[1,0,0]];
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
function write(c,p,id){const before=state(p);cmd(`setblock ${p.join(" ")} ${id}`);return {position:p,before,after:state(p)};}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  cmd("tickingarea add 0 0 0 127 100 63 audit_bell_support true");cmd("gamerule randomtickspeed 0");
  cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,63])){if(system.currentTick-loaded>480)throw new Error("bell support chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){
   const scenario=cases[i],base=[4+i%16*8,65,4+Math.floor(i/16)*8],v=delta[scenario.direction],perp=delta[(scenario.direction+1)%4];
   const floor=[base[0],64,base[2]],ceiling=[base[0],66,base[2]],back=[base[0]-v[0],65,base[2]-v[2]],front=[base[0]+v[0],65,base[2]+v[2]],unrelated=[base[0]+perp[0],65,base[2]+perp[2]];
   cmd(`fill ${base[0]-3} 63 ${base[2]-3} ${base[0]+3} 68 ${base[2]+3} air`);
   if(scenario.action!=="unsupported"){
    cmd(`setblock ${floor.join(" ")} stone`);
    if(scenario.attachment==="hanging")cmd(`setblock ${ceiling.join(" ")} stone`);
    if(scenario.attachment==="side"||scenario.attachment==="multiple")cmd(`setblock ${back.join(" ")} stone`);
    if(scenario.attachment==="multiple")cmd(`setblock ${front.join(" ")} stone`);
   }
   cmd(`setblock ${base.join(" ")} bell ["attachment"="${scenario.attachment}","direction"=${scenario.direction},"toggle_bit"=false]`);
   const main=scenario.attachment==="standing"?floor:scenario.attachment==="hanging"?ceiling:back;
   cells.push({scenario,base,floor,ceiling,back,front,unrelated,main,setup_tick:system.currentTick,changes:[],samples:[]});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  c.samples.push({tick:system.currentTick,cells:[c.base,c.floor,c.ceiling,c.back,c.front,c.unrelated].map(p=>({position:p,block:state(p)}))});
  if(t===20+c.scenario.phase){const writes=[];
   switch(c.scenario.action){
    case "remove_main":writes.push(write(c,c.main,"air"));break;
    case "replace_main":writes.push(write(c,c.main,"glass"));break;
    case "unrelated":case "unsupported":writes.push(write(c,c.unrelated,"stone"));break;
    case "add_front":writes.push(write(c,c.front,"stone"));break;
    case "add_front_remove_main":writes.push(write(c,c.front,"stone"),write(c,c.main,"air"));break;
    case "remove_front":writes.push(write(c,c.front,"air"));break;
    case "remove_both":writes.push(write(c,c.main,"air"),write(c,c.front,"air"));break;
    default:throw new Error("unknown bell support control");
   }c.changes.push({tick:system.currentTick,writes});
  }
 }
 if(t===64){for(const c of cells){if(c.changes.length!==1)throw new Error("missing bell support change");emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def bell_support_cases():
    actions=dict(standing=('remove_main','replace_main','unrelated','unsupported'),
                 hanging=('remove_main','replace_main','unrelated','unsupported'),
                 side=('add_front','remove_main','add_front_remove_main','unsupported'),
                 multiple=('remove_main','remove_front','remove_both','unsupported'))
    return [dict(attachment=a,action=action,direction=d,phase=phase) for a,values in actions.items()
            for action in values for d in range(4) for phase in (0,1)]


if __name__=='__main__':
    args=arguments();args.offline_reference=True
    cases=bell_support_cases()
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
