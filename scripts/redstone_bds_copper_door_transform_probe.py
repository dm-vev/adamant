#!/usr/bin/env python3
"""Capture actual player waxing, unwaxing and scraping of both copper door halves."""
import argparse
from pathlib import Path

from redstone_bds_switch_use_probe import capture_player_use


BASES = ('copper_door', 'exposed_copper_door', 'weathered_copper_door', 'oxidized_copper_door')
CASES = [dict(block=b, tool=tool, top=top)
         for tool, names in (('honeycomb', BASES), ('iron_axe', BASES[1:]+tuple('waxed_'+b for b in BASES)))
         for b in names for top in (False, True)]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
let d,load=0,ready=false,running=false,index=0,current=null,lastSample=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const blocks=()=>current.positions.map(read);
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
const ours=e=>current&&e.player.name==="RedstoneAudit"&&e.block.location.x===current.target[0]&&e.block.location.y===current.target[1]&&e.block.location.z===current.target[2];
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{if(ours(e))current.changes.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),before_sample:lastSample!==system.currentTick,item:e.itemStack?.typeId,sneaking:e.player.isSneaking,blocks:blocks()});});
world.afterEvents.playerInteractWithBlock.subscribe(e=>{if(ours(e))current.results.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),blocks:blocks()});});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 79 100 31 copper_transforms true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([72,65,28])){if(system.currentTick-load>480)throw new Error("copper transform chunks not ready");return;}
  cmd("fill -3 63 -3 3 63 3 stone");cmd("setworldspawn 0 64 2");ready=true;emit({ready:true});return;}
 if(!running)return;
 if(!current){const c=cases[index],base=[4+index%12*6,65,4+Math.floor(index/12)*6];
  cmd(`fill ${base[0]-2} 64 ${base[2]-2} ${base[0]+2} 64 ${base[2]+2} stone`);
  if(cmd(`setblock ${base.join(" ")} ${c.block} ["minecraft:cardinal_direction"="north","door_hinge_bit"=true,"upper_block_bit"=false,"open_bit"=false]`).successCount!==1)throw new Error("copper transform setup failed");
  const target=[base[0],65+(c.top?1:0),base[2]],feet=[base[0]+.5,65,base[2]+2.5];
  const p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");if(!p)throw new Error("audit player missing");
  p.teleport({x:feet[0],y:feet[1],z:feet[2]});
  cmd(`replaceitem entity @a[name=RedstoneAudit] slot.hotbar 0 ${c.tool} 1`);
  current={scenario:c,base,target,feet,positions:[base,[base[0],66,base[2]]],setup_tick:system.currentTick,samples:[],changes:[],results:[],requested:0};
  current.initial=blocks();emit({action:{action:"look",rotation:[180,0],sneaking:false},block_state:read(target)});return;
 }
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),blocks:blocks()});
 lastSample=system.currentTick;
 if(system.currentTick-current.setup_tick===8)emit({action:{action:"look",rotation:[180,0],sneaking:true},block_state:read(current.target)});
 if(!current.requested&&system.currentTick-current.setup_tick>=16){current.requested=system.currentTick;
  if(!world.getAllPlayers().find(p=>p.name==="RedstoneAudit").isSneaking)throw new Error("native sneak input was not acknowledged before item use");
  emit({action:{action:"click",block:current.target,face:3,hit:[.5,.5,.5],feet:current.feet},block_state:read(current.target)});}
 if(current.requested&&system.currentTick-current.requested>=16){
  if(current.changes.length!==1||current.changes[0].item!=="minecraft:"+current.scenario.tool||!current.changes[0].sneaking||current.results.length!==1)throw new Error("actual copper item-use control failed "+JSON.stringify(current));
  current.final=blocks();
  if(current.final.some(b=>b.id==="minecraft:air")||current.final[0].id===current.initial[0].id||current.final[1].id===current.initial[1].id)throw new Error("native copper item transformation positive control failed "+JSON.stringify(current));
  emit(current);index++;current=null;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--assets', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--port', type=int, default=30951)
    parser.add_argument('--client-executable', type=Path, required=True)
    parser.add_argument('--limit', type=int, default=len(CASES))
    args = parser.parse_args()
    if not 1 <= args.limit <= len(CASES):
        parser.error('limit must select at least one of the retained cases')
    capture_player_use(args, CASES[:args.limit], require_parities=args.limit>1, source_script=SCRIPT,
                       client_executable=args.client_executable.resolve(), sound_prefixes=())
