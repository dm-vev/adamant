#!/usr/bin/env python3
"""Record actual player barrier placement on powered/unpowered supports."""
import json

import redstone_bds_switch_use_probe as player_probe
from redstone_bds_barrier_use_probe import scope_packets
from redstone_bds_timing_probe import arguments


# Expected held IDs from the pinned vanilla palette, checked against the actual
# BDS inventory before sending. Native interaction events also retain item names.
ITEMS = [('wooden_door',362,'door'),('copper_door',-784,'door'),('iron_door',375,'door'),
         ('trapdoor',96,'trapdoor'),('copper_trapdoor',-792,'trapdoor'),
         ('iron_trapdoor',167,'trapdoor'),('fence_gate',107,'gate')]
CASES = [dict(block=name,held_network_id=rid,form=form,direction=direction,powered=power,request_phase=phase)
         for name,rid,form in ITEMS for direction in range(4) for power in (False,True) for phase in (0,1)]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
const delta=[[0,-1],[1,0],[0,1],[-1,0]],yaw=[180,-90,0,90];
let d,load=0,running=false,index=0,current=null,ready=false,lastSample=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const blocks=()=>current.positions.map(read),player=()=>world.getAllPlayers().find(p=>p.name==="RedstoneAudit");
const at=(b,p)=>b.location.x===p[0]&&b.location.y===p[1]&&b.location.z===p[2];
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit"||!at(e.block,current.support))return;
 current.changes.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),before_sample:lastSample!==system.currentTick,
  blocks:blocks(),rotation:e.player.getRotation(),held:e.itemStack?.typeId,count:e.itemStack?.amount});
});
world.afterEvents.playerPlaceBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit"||!current.positions.some(p=>at(e.block,p)))return;
 current.results.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),position:[e.block.location.x,e.block.location.y,e.block.location.z],blocks:blocks()});
});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 79 100 79 audit_barrier_place true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([72,65,72])){if(system.currentTick-load>480)throw new Error("barrier placement chunks not ready");return;}
  cmd("fill -3 64 -3 3 64 3 stone");cmd("setworldspawn 0 65 2");ready=true;emit({ready:true});return;}
 if(!running)return;
 if(!current){const c=cases[index],base=[4+index%12*6,65,4+Math.floor(index/12)*6],support=[base[0],64,base[2]],v=delta[c.direction];
  cmd(`fill ${base[0]-2} 64 ${base[2]-2} ${base[0]+2} 64 ${base[2]+2} stone`);
  cmd(`fill ${base[0]-2} 65 ${base[2]-2} ${base[0]+2} 68 ${base[2]+2} air`);
  if(c.powered)cmd(`setblock ${support.join(" ")} redstone_block`);
  const p=player();if(!p)throw new Error("audit player missing");cmd('gamemode survival "RedstoneAudit"');
  cmd(`replaceitem entity "RedstoneAudit" slot.hotbar 0 ${c.block} 2`);p.selectedSlotIndex=0;
  const feet=[base[0]+.5-v[0]*2,65,base[2]+.5-v[1]*2];p.teleport({x:feet[0],y:feet[1],z:feet[2]});
  current={scenario:c,base,support,feet,positions:c.form==="door"?[base,[base[0],66,base[2]]]:[base],
   setup_tick:system.currentTick,initial_support:read(support),samples:[],changes:[],results:[],requested:0};
  current.initial=blocks();emit({action:{action:"look",rotation:[yaw[c.direction],0]},block_state:read(support)});return;
 }
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),blocks:blocks(),support:read(current.support)});lastSample=system.currentTick;
 const elapsed=system.currentTick-current.setup_tick;
 if(elapsed===12)player().sendMessage("ADAMANT_BARRIER_USE "+JSON.stringify({index,tick:system.currentTick,stage:"settled"}));
 if(!current.requested&&elapsed>=16&&(system.currentTick&1)===current.scenario.request_phase){
  const held=player().getComponent("minecraft:inventory").container.getItem(0);
  if(held?.typeId!=="minecraft:"+current.scenario.block||held.amount!==2)throw new Error("native held placement item differs "+JSON.stringify(current.scenario));
  emit({action:{action:"click",block:current.support,face:1,hit:[.5,1,.5],feet:current.feet,
    held_count:2,held_network_id:current.scenario.held_network_id},block_state:read(current.support)});
  current.requested=system.currentTick;
 }
 if(current.requested&&!current.results.length&&system.currentTick-current.requested>60)throw new Error("native barrier placement not accepted "+JSON.stringify({scenario:current.scenario,changes:current.changes,blocks:blocks()}));
 if(current.results.length&&system.currentTick-current.results[0].tick>=20){
  if(current.changes.length!==1||current.changes[0].held!=="minecraft:"+current.scenario.block||current.changes[0].count!==2||blocks().some(b=>b.id!=="minecraft:"+current.scenario.block))throw new Error("native barrier placement control failed "+JSON.stringify(current));
  emit(current);index++;current=null;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def capture(args, cases=CASES):
    player_probe.SCRIPT = SCRIPT
    player_probe.capture_player_use(args, cases, sound_prefixes=('door.', 'trapdoor.', 'fence_gate.', 'place'))
    result = json.loads(args.output.read_text(encoding='utf-8'))
    result['complete'] = False
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    result = scope_packets(result)
    result['kind'] = 'actual_player_placement'
    result['complete'] = True
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, scenes=len(result['scenarios']))), flush=True)


if __name__ == '__main__':
    capture(arguments())
