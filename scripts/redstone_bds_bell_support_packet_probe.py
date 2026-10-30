#!/usr/bin/env python3
"""Observe original BDS bell attachment/support updates and their client packets."""
import hashlib
import json

from redstone_bds_bell_support_probe import SCRIPT, bell_support_cases
from redstone_bds_client import LocalClient
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


if __name__=='__main__':
    args=arguments();cases=bell_support_cases()
    script=SCRIPT.replace('CASES',json.dumps(cases))
    script=replace_once(script,'let d,loaded=0,ready=false,start=0,cells=[];',
        'let d,loaded=0,ready=false,start=0,cells=[],requested=false,announced=false;'
        'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});')
    script=replace_once(script,'cmd("tickingarea add 0 0 0 127 100 63 audit_bell_support true");',
        'cmd("tickingarea add -16 0 -16 143 100 79 audit_bell_support true");cmd("setworldspawn 64 65 32");')
    script=replace_once(script,'  if(system.currentTick%20!==2)return;',
        '  if(!requested){if(!announced){announced=true;emit({ready:true});}return;}if(system.currentTick%20!==2)return;')
    result=dict(complete=False,reference=__doc__,binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),scenarios=[],packet_observations=[])
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=script,local_client=True)
    client=None
    try:
        reference.until(lambda s:'ADAMANT_TIMING ' in s and '"ready":true' in s,timeout=45)
        client=LocalClient(reference,args.port)
        reference.command('scriptevent audit:start now')
        while True:
            line=reference.until(lambda s:'ADAMANT_TIMING ' in s,timeout=45)[-1]
            record=json.loads(line.split('ADAMANT_TIMING ',1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if 'scenario' in record:
                result['scenarios'].append(record)
            if record.get('complete'):
                if record['count']!=len(cases) or len(result['scenarios'])!=len(cases):
                    raise RuntimeError('incomplete native bell support packet scenes')
                result['complete']=True
                break
    finally:
        if client:
            client.stop()
            for line in client.log:
                try:p=json.loads(line)
                except (ValueError,TypeError):continue
                if any(k in p for k in ('block_actor','block_update','subchunk_update','block_event','sound','level_event')):
                    result['packet_observations'].append(p)
        reference.stop()
        for c in result['scenarios']:
            c['packets']=[]
            for p in result['packet_observations']:
                if 'subchunk_update' in p:
                    for b in p['subchunk_update']['Blocks']:
                        if b['BlockPos']==c['base']:
                            c['packets'].append(dict(block_update=dict(Position=b['BlockPos'],NewBlockRuntimeID=b['BlockRuntimeID'],Flags=b['Flags'],Layer=0),received_at_ms=p['received_at_ms']))
                else:
                    key=next(k for k in ('block_actor','block_update','block_event','sound','level_event') if k in p)
                    if p[key]['Position']==c['base']:
                        c['packets'].append(p)
            actors=[p for p in c['packets'] if 'block_actor' in p]
            updates=[p for p in c['packets'] if 'block_update' in p]
            if not actors or not updates or actors[0]['block_actor']['NBTData']['id']!='Bell':
                result['complete']=False;result['error']='native bell setup packet control missing'
        result['log_directory']=str(reference.root)
        args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'],scenes=len(result['scenarios']),logs=str(reference.root))),flush=True)
        if not result['complete']:
            raise RuntimeError('native bell support packet capture incomplete')
