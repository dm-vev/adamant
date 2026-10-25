#!/usr/bin/env python3
"""Capture torch orientation/permutation changes with independent live inputs."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const northLamp of [false,true])for(const method of ["state","variant","replace"])for(const westPowered of [false,true])for(const phase of [0,1])cases.push({method,westPowered,phase,northLamp});
let dimension,index=-1,start=0,loadStarted=0,samples=[],changes=[],waiting=false;
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk is not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function torch(block,facing){cmd(`setblock 0 65 0 ${block}`+(block==="air"?"":` ["torch_facing_direction"="${facing}"]`));changes.push({tick:system.currentTick,kind:"torch",pos:[0,65,0],block,facing});}
function lever(side,powered){if(side==="north"&&cases[index].northLamp)return;const p=side==="west"?[-2,65,0]:[0,65,-2];cmd(`setblock ${p.join(" ")} lever ["lever_direction"="${side}","open_bit"=${powered}]`);changes.push({tick:system.currentTick,kind:"lever",pos:p,facing:side,powered});}
function setup(){
    const c=cases[index];cmd("fill -4 63 -4 3 68 3 air");cmd("setblock -1 65 0 stone");cmd("setblock 0 65 -1 "+(c.northLamp?"redstone_lamp":"stone"));
    cmd("setblock 0 66 0 redstone_lamp");
    lever("west",c.westPowered);lever("north",!c.westPowered);
    cmd('setblock 0 65 0 redstone_torch ["torch_facing_direction"="west"]');
    start=system.currentTick;samples=[];changes=[{tick:start,kind:"setup"}];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-4,y:65,z:-4})||!dimension.getBlock({x:3,y:65,z:3})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }emit({ready:true});index=0;waiting=true;}
        const c=cases[index];if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        const t=system.currentTick-start;
        samples.push({tick:system.currentTick,torch:read([0,65,0]),lamp:read([0,66,0]),west:read([-2,65,0]),north:read([0,65,-2]),north_support:read([0,65,-1])});
        if(t===12){let block=read([0,65,0]).id.split(":")[1];
            if(c.method==="variant")block=block==="redstone_torch"?"unlit_redstone_torch":"redstone_torch";
            if(c.method==="replace")torch("air","");torch(block,"north");
        }
        if(t===24){lever("west",!c.westPowered);lever("north",c.westPowered);}
        // Refresh an already rotated block without changing its visible face.
        if(t===30){const block=read([0,65,0]).id.endsWith(":redstone_torch")?"unlit_redstone_torch":"redstone_torch";torch(block,"north");}
        if(t===36)torch("air","");if(t===38)torch("redstone_torch","north");
        if(t===48){lever("west",c.westPowered);lever("north",!c.westPowered);}
        if(t>=64){emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 24)
