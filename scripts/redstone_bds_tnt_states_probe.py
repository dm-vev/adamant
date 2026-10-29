#!/usr/bin/env python3
"""Capture native TNT canonical states, legacy structure upgrades and wire hashes."""
import hashlib
import json

from redstone_audit_structure import byte, one_block
from redstone_bds_client import LocalClient, network_state_hash
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TNT_STATES "+JSON.stringify(x));
let d,loaded=0,ready=false,running=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;cmd("tickingarea add -16 0 -16 31 100 31 audit true");
  cmd("gamerule domobspawning false");cmd("gamerule randomtickspeed 0");cmd("gamerule tntexplodes false");return;}
 if(!ready){if(!get([-4,65,-4])||!get([28,65,28])){if(system.currentTick-loaded>240)throw new Error("TNT state chunks not ready");return;}
  cmd("fill -4 64 -4 28 64 28 stone");cmd("fill -4 65 -4 28 68 28 air");cmd("setworldspawn 8 65 8");ready=true;emit({ready:true});return;}
 if(!running)return;if(!start){start=system.currentTick;return;}
 if(system.currentTick-start===20){for(let i=0;i<cases.length;i++){
  const scenario=cases[i],base=[4+i%4*5,65,4+Math.floor(i/4)*5];
  const command=scenario.structure?`structure load audit:${scenario.structure} ${base.join(" ")}`:`setblock ${base.join(" ")} ${scenario.command}`;
  let accepted=false,error=null;try{accepted=cmd(command).successCount>0;}catch(e){error=String(e);}
  const b=get(base);cells.push({scenario,base,accepted,error,block:{id:b.typeId,states:b.permutation.getAllStates()}});
 }}
 if(system.currentTick-start===40){for(const c of cells)emit(c);emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def wire_state(block):
    # The SDK reports the old underwater selector even after typeId has
    # resolved to tnt/underwater_tnt. Native UpdateSubChunkBlocks hashes prove
    # that this selector is absent from the corresponding wire permutations.
    states = dict(block['states'])
    if block['id'] in ('minecraft:tnt', 'minecraft:underwater_tnt'):
        alias = states.pop('allow_underwater_bit')
        if alias != (block['id'] == 'minecraft:underwater_tnt'):
            raise RuntimeError('native TNT API alias contradicts the resolved type')
    return dict(id=block['id'], states=states)


if __name__ == '__main__':
    args = arguments()
    cases = [dict(command=f'tnt ["allow_underwater_bit"={str(underwater).lower()},"explode_bit"={str(explode).lower()}]')
             for underwater in (False, True) for explode in (False, True)]
    cases += [dict(command=f'underwater_tnt ["explode_bit"={str(explode).lower()}]') for explode in (False, True)]
    structures = {}
    for name in ('tnt', 'underwater_tnt'):
        for explode in (False, True):
            key = f'legacy_{name}_{int(explode)}'
            structures['audit/'+key] = one_block('minecraft:'+name, {'explode_bit': byte(explode)}, {})
            cases.append(dict(structure=key))
    result = dict(complete=False, reference=__doc__, binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),
                  scenarios=[], packet_observations=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port,
                          script=SCRIPT.replace('CASES', json.dumps(cases)), structures=structures, local_client=True)
    client = None
    try:
        reference.until(lambda s: 'ADAMANT_TNT_STATES ' in s and '"ready":true' in s, timeout=45)
        client = LocalClient(reference, args.port)
        reference.command('scriptevent audit:start now')
        while True:
            line = reference.until(lambda s: 'ADAMANT_TNT_STATES ' in s, timeout=30)[-1]
            r = json.loads(line.split('ADAMANT_TNT_STATES ', 1)[1])
            if r.get('error') and 'scenario' not in r:
                raise RuntimeError(r)
            if 'scenario' in r:
                result['scenarios'].append(r)
            if r.get('complete'):
                if r['count'] != 10 or len(result['scenarios']) != 10:
                    raise RuntimeError('incomplete native TNT state scenes')
                result['complete'] = True
                break
    finally:
        if client:
            client.stop()
            for line in client.log:
                try:
                    r = json.loads(line)
                except (ValueError, TypeError):
                    continue
                if 'block_update' in r:
                    result['packet_observations'].append(r)
                if 'subchunk_update' in r:
                    result.setdefault('subchunk_observations', []).append(r)
                    for block in r['subchunk_update']['Blocks']:
                        result['packet_observations'].append(dict(block_update=dict(Position=block['BlockPos'],
                            NewBlockRuntimeID=block['BlockRuntimeID'],Layer=0),received_at_ms=r['received_at_ms']))
        reference.stop()
        for c in result['scenarios']:
            packets = [r['block_update'] for r in result['packet_observations']
                       if r['block_update']['Layer'] == 0 and r['block_update']['Position'] == c['base']]
            c['wire_block'] = wire_state(c['block'])
            c['network_hash'] = network_state_hash(c['wire_block'])
            c['updates'] = packets
            if c['accepted'] and (not packets or packets[-1]['NewBlockRuntimeID'] != c['network_hash']):
                result['complete'] = False
                result['error'] = 'native TNT wire hash differs from state'
        if len(result['scenarios']) == 10 and not all(c['accepted'] for c in result['scenarios'][:4]):
            result['complete'] = False
            result['error'] = 'native canonical TNT states were not accepted'
        result['log_directory'] = str(reference.root)
        args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'], scenes=len(result['scenarios']), logs=str(reference.root))), flush=True)
        if not result['complete']:
            raise RuntimeError('native TNT state capture incomplete')
