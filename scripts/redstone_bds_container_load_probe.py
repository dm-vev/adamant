#!/usr/bin/env python3
"""Capture initial consumer publication after loading stored container flags."""
import json

from redstone_audit_structure import byte, string, listing, one_block
from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  cmd("tickingarea add 0 0 0 127 100 63 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,63])){if(system.currentTick-loaded>240)throw new Error("consumer chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(const kind of ["crafter","dispenser","dropper","hopper"])for(const stored of [false,true])for(const input of [false,true])for(const phase of [0,1]){
   const i=cells.length,base=[4+(i%8)*16,65,4+Math.floor(i/8)*16],source=[base[0]-1,65,base[2]];
   cmd(`fill ${base[0]-2} 64 ${base[2]-2} ${base[0]+2} 68 ${base[2]+2} air`);
   cells.push({scenario:{kind,stored,input,phase},base,source,setup_tick:system.currentTick,changes:[],samples:[],load:null});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){c.samples.push({tick:system.currentTick,block:state(c.base)});
  if(t===10+c.scenario.phase){
   cmd(`setblock ${c.source.join(" ")} ${c.scenario.input?"redstone_block":"air"}`);
   if(!cmd(`structure load audit:consumer_${c.scenario.kind}_${Number(c.scenario.stored)} ${c.base.join(" ")}`).successCount)throw new Error("consumer actor load failed");
   c.load={tick:system.currentTick,block:state(c.base)};c.changes.push({tick:system.currentTick,powered:c.scenario.input});
  }
  if(t===40+c.scenario.phase){const powered=!c.scenario.input;
   cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered});}
 }
 if(t===72){for(const c of cells){if(!c.load)throw new Error("missing native consumer load control");emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def structures():
    result = {}
    for kind in ('crafter', 'dispenser', 'dropper', 'hopper'):
        for stored in (False, True):
            states = {'orientation': string('north_up'), 'crafting': byte(0), 'triggered_bit': byte(stored)} if kind == 'crafter' else {
                'facing_direction': (3, 2), 'toggle_bit' if kind == 'hopper' else 'triggered_bit': byte(stored)}
            actor = {'id': string(kind.title()), 'Items': listing(10, [])}
            if kind == 'crafter':
                actor['disabled_slots'] = (2, 0)
            if kind == 'hopper':
                actor['TransferCooldown'] = (3, 0)
            result[f'audit/consumer_{kind}_{int(stored)}'] = one_block('minecraft:'+kind, states, actor)
    return result


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    capture(args, SCRIPT, 32, structures=structures())
